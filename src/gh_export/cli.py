from __future__ import annotations

import json
import os
import tomllib
from datetime import datetime, timezone
from pathlib import Path

import click
from rich.console import Console
from rich.table import Table

from gh_export.cloner import (
    Action,
    RepoResult,
    SyncSummary,
    clone_repos,
    delete_repo,
    move_repo,
)
from gh_export.github_api import (
    RepoInfo,
    filter_repos,
    list_org_repos,
    validate_token,
)

MANIFEST_FILENAME = ".gh-export.json"
CONFIG_FILENAME = ".gh-export.toml"

console = Console()


# ---------------------------------------------------------------------------
# Config resolution
# ---------------------------------------------------------------------------

def _load_config(
    local_dir: Path | None = None,
) -> tuple[dict, dict]:
    """Load home and local config files separately.

    Returns:
        Tuple of (home_config, local_config) dicts.
    """
    def _normalize(raw: dict) -> dict:
        return {k.replace("_", "-"): v for k, v in raw.items()}

    home: dict = {}
    local: dict = {}

    home_path = Path.home() / CONFIG_FILENAME
    if home_path.exists():
        with open(home_path, "rb") as f:
            home = _normalize(tomllib.load(f))

    if local_dir is not None:
        local_path = local_dir / CONFIG_FILENAME
        if local_path.exists():
            with open(local_path, "rb") as f:
                local = _normalize(tomllib.load(f))

    return home, local


def _resolve_option(
    cli_value,
    home_config: dict,
    local_config: dict,
    key: str,
    default=None,
) -> tuple:
    """Resolve an option and return (value, source)."""
    if cli_value is not None:
        return cli_value, "option"
    if key in local_config:
        return local_config[key], f"<path>/{CONFIG_FILENAME}"
    if key in home_config:
        return home_config[key], f"~/{CONFIG_FILENAME}"
    return default, "default"


def _resolve_token(
    token_flag: str | None,
    home_config: dict,
    local_config: dict,
) -> tuple[str, str]:
    """Resolve token and return (value, source).

    Raises:
        click.UsageError: If no token is found anywhere.
    """
    if token_flag:
        return token_flag, "option"

    env_token = os.environ.get("GITHUB_TOKEN")
    if env_token:
        return env_token, "GITHUB_TOKEN env"

    if "token" in local_config:
        return (
            local_config["token"],
            f"<path>/{CONFIG_FILENAME}",
        )
    if "token" in home_config:
        return (
            home_config["token"],
            f"~/{CONFIG_FILENAME}",
        )

    raise click.UsageError(
        "No GitHub token provided. Supply one via:\n"
        "  1. --token / -t flag\n"
        "  2. GITHUB_TOKEN environment variable\n"
        f"  3. ~/{CONFIG_FILENAME} config file"
        ' (token = "ghp_...")'
    )


def _obfuscate_token(token: str) -> str:
    if len(token) <= 8:
        return "****"
    return token[:4] + "****" + token[-4:]


def _print_settings(
    rows: list[tuple[str, str, str]],
) -> None:
    """Print resolved settings with source provenance.

    Args:
        rows: List of (label, display_value, source) tuples.
    """
    max_val_len = max(len(v) for _, v, _ in rows)

    console.print()
    console.print("[bold]Settings:[/bold]")
    for label, value, source in rows:
        padding = max_val_len - len(value) + 2
        console.print(
            f"  {label:<15}{value}{' ' * padding}"
            f"[dim]({source})[/dim]"
        )
    console.print()


# ---------------------------------------------------------------------------
# Manifest read / write
# ---------------------------------------------------------------------------

def _write_manifest(
    output_dir: Path,
    org: str,
    repos: list[RepoInfo],
) -> None:
    manifest = {
        "org": org,
        "exported_at": datetime.now(timezone.utc).isoformat(),
        "repos": [
            {
                "name": r.name,
                "default_branch": r.default_branch,
                "archived": r.archived,
                "has_wiki": r.has_wiki,
            }
            for r in repos
        ],
    }
    path = output_dir / MANIFEST_FILENAME
    with open(path, "w") as f:
        json.dump(manifest, f, indent=2)
        f.write("\n")


def _read_manifest(export_dir: Path) -> dict | None:
    path = export_dir / MANIFEST_FILENAME
    if not path.exists():
        return None
    with open(path) as f:
        return json.load(f)


# ---------------------------------------------------------------------------
# Display helpers
# ---------------------------------------------------------------------------

def _print_repo_table(repos: list[RepoInfo]) -> None:
    table = Table(title="Repos to export")
    table.add_column("Name", style="cyan", min_width=30)
    table.add_column("Visibility")
    table.add_column("Language")
    table.add_column("Size (MB)", justify="right")
    table.add_column("Default Branch")
    table.add_column("Archived")
    table.add_column("Last Pushed")

    for r in repos:
        table.add_row(
            r.name,
            "private" if r.private else "public",
            r.language or "-",
            f"{r.size_kb / 1024:.1f}",
            r.default_branch,
            "yes" if r.archived else "no",
            r.pushed_at or "-",
        )

    console.print()
    console.print(table)
    console.print(
        f"\nTotal: [bold]{len(repos)}[/bold] repos"
    )


def _print_summary(summary: SyncSummary) -> None:
    table = Table(title="Sync Summary")
    table.add_column("Action", style="cyan")
    table.add_column("Count", justify="right")

    for action in Action:
        count = summary.count(action)
        if count:
            table.add_row(action.value, str(count))

    console.print()
    console.print(table)

    failed = [
        r for r in summary.results
        if r.action == Action.FAILED
    ]
    if failed:
        console.print("\n[bold red]Failed repos:[/bold red]")
        for r in failed:
            console.print(f"  {r.name}: {r.detail}")

    dirty = [
        r for r in summary.results
        if r.action == Action.SKIPPED_DIRTY
    ]
    if dirty:
        console.print(
            "\n[bold yellow]Skipped (local changes):[/bold yellow]"
        )
        for r in dirty:
            console.print(f"  {r.name}")


def _repos_to_dicts(repos: list[RepoInfo]) -> list[dict]:
    return [
        {
            "name": r.name,
            "clone_url": r.clone_url,
            "ssh_url": r.ssh_url,
            "default_branch": r.default_branch,
            "archived": r.archived,
            "has_wiki": r.has_wiki,
        }
        for r in repos
    ]


# ---------------------------------------------------------------------------
# Handle archived / deleted repos
# ---------------------------------------------------------------------------

def _handle_removed_and_archived(
    output_dir: Path,
    current_repos: list[RepoInfo],
    previous_repos: list[dict],
    should_delete: bool,
    deleted_dir: str,
    archived_dir: str,
    dry_run: bool,
) -> SyncSummary:
    summary = SyncSummary()
    current_names = {r.name for r in current_repos}
    current_archived = {
        r.name for r in current_repos if r.archived
    }
    previous_names = {r["name"] for r in previous_repos}
    previously_archived = {
        r["name"] for r in previous_repos
        if r.get("archived")
    }

    removed = previous_names - current_names
    newly_archived = current_archived - previously_archived

    for name in sorted(removed):
        repo_dir = output_dir / name
        if not repo_dir.exists():
            continue
        if dry_run:
            action = (
                Action.DELETED if should_delete
                else Action.MOVED_DELETED
            )
            summary.results.append(
                RepoResult(name, action, "dry-run")
            )
            continue
        if should_delete:
            delete_repo(repo_dir)
            summary.results.append(
                RepoResult(name, Action.DELETED)
            )
        else:
            move_repo(repo_dir, output_dir / deleted_dir)
            summary.results.append(
                RepoResult(name, Action.MOVED_DELETED)
            )

    for name in sorted(newly_archived):
        repo_dir = output_dir / name
        if not repo_dir.exists():
            continue
        if dry_run:
            action = (
                Action.DELETED if should_delete
                else Action.MOVED_ARCHIVED
            )
            summary.results.append(
                RepoResult(name, action, "dry-run")
            )
            continue
        if should_delete:
            delete_repo(repo_dir)
            summary.results.append(
                RepoResult(name, Action.DELETED)
            )
        else:
            move_repo(repo_dir, output_dir / archived_dir)
            summary.results.append(
                RepoResult(name, Action.MOVED_ARCHIVED)
            )

    return summary


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------

@click.command()
@click.argument("org")
@click.argument("path", type=click.Path())
@click.option(
    "--token", "-t", default=None,
    help="GitHub Personal Access Token.",
)
@click.option(
    "--type", "repo_type", default=None,
    help="Repo type: all|public|private|forks|sources.",
)
@click.option(
    "--concurrency", "-c", default=None, type=int,
    help="Parallel workers.",
)
@click.option(
    "--dry-run", is_flag=True, default=None,
    help="Preview changes without doing anything.",
)
@click.option(
    "--clone-wiki", is_flag=True, default=None,
    help="Also clone associated wikis.",
)
@click.option(
    "--include", default=None,
    help="Regex to include only matching repo names.",
)
@click.option(
    "--exclude", default=None,
    help="Regex to exclude matching repo names.",
)
@click.option(
    "--protocol", type=click.Choice(["https", "ssh"]),
    default=None, help="Git transport protocol.",
)
@click.option(
    "--ssh", "protocol", flag_value="ssh",
    help="Shortcut for --protocol ssh.",
)
@click.option(
    "--git-author", default=None,
    help="Set git user.name in each cloned repo.",
)
@click.option(
    "--git-email", default=None,
    help="Set git user.email in each cloned repo.",
)
@click.option(
    "--delete", "should_delete", is_flag=True, default=None,
    help="Permanently delete removed/archived repos.",
)
@click.option(
    "--deleted-dir", default=None,
    help="Directory for removed repos.",
)
@click.option(
    "--archived-dir", default=None,
    help="Directory for archived repos.",
)
@click.option(
    "--max-size", default=None, type=int,
    help="Skip repos larger than this size in MB.",
)
def cli(
    org: str,
    path: str,
    token: str | None,
    repo_type: str | None,
    concurrency: int | None,
    dry_run: bool | None,
    clone_wiki: bool | None,
    include: str | None,
    exclude: str | None,
    protocol: str | None,
    git_author: str | None,
    git_email: str | None,
    should_delete: bool | None,
    deleted_dir: str | None,
    archived_dir: str | None,
    max_size: int | None,
) -> None:
    """Export and sync all GitHub repos for an organization."""
    output_dir = Path(path)
    home_cfg, local_cfg = _load_config(output_dir)

    def _opt(cli_val, key, default=None):
        return _resolve_option(
            cli_val, home_cfg, local_cfg, key, default,
        )

    token, token_src = _resolve_token(
        token, home_cfg, local_cfg,
    )
    protocol, protocol_src = _opt(
        protocol, "protocol", "https",
    )
    repo_type, type_src = _opt(
        repo_type, "type", "all",
    )
    concurrency, _ = _opt(
        concurrency, "concurrency", 4,
    )
    dry_run, _ = _opt(dry_run, "dry-run", False)
    clone_wiki, wiki_src = _opt(
        clone_wiki, "clone-wiki", False,
    )
    include, include_src = _opt(include, "include")
    exclude, exclude_src = _opt(exclude, "exclude")
    git_author, author_src = _opt(
        git_author, "git-author",
    )
    git_email, email_src = _opt(
        git_email, "git-email",
    )
    should_delete, _ = _opt(
        should_delete, "delete", False,
    )
    deleted_dir, _ = _opt(
        deleted_dir, "deleted-dir", "DELETED",
    )
    archived_dir, _ = _opt(
        archived_dir, "archived-dir", "ARCHIVED",
    )
    max_size, max_size_src = _opt(
        max_size, "max-size",
    )

    rows = [
        ("Organization:", org, "argument"),
        ("Output:", str(output_dir), "argument"),
        ("Protocol:", protocol, protocol_src),
        ("Token:", _obfuscate_token(token), token_src),
        ("Type:", repo_type, type_src),
        (
            "Clone wiki:",
            "yes" if clone_wiki else "no",
            wiki_src,
        ),
    ]
    if include:
        rows.append(("Include:", include, include_src))
    if exclude:
        rows.append(("Exclude:", exclude, exclude_src))
    if max_size is not None:
        rows.append((
            "Max size:", f"{max_size} MB", max_size_src,
        ))
    rows.append((
        "Git author:", git_author or "-", author_src,
    ))
    rows.append((
        "Git email:", git_email or "-", email_src,
    ))

    _print_settings(rows)

    console.print("Authenticating...", style="dim")
    username = validate_token(token)
    console.print(f"Authenticated as [bold]{username}[/bold]")

    console.print(
        f"Fetching repos for [bold]{org}[/bold]..."
    )
    all_repos = list_org_repos(token, org)
    repos = filter_repos(
        all_repos,
        repo_type=repo_type,
        include=include,
        exclude=exclude,
    )
    console.print(f"Found [bold]{len(repos)}[/bold] repos")
    max_size_kb = max_size * 1024 if max_size is not None else None
    too_large = []
    if max_size_kb is not None:
        too_large = [r for r in repos if r.size_kb > max_size_kb]
        repos = [r for r in repos if r.size_kb <= max_size_kb]
        if too_large:
            console.print(
                f"Skipping [bold]{len(too_large)}[/bold]"
                f" repo(s) exceeding {max_size} MB"
            )

    skipped_large_results = [
        RepoResult(r.name, Action.SKIPPED_TOO_LARGE)
        for r in too_large
    ]

    manifest = _read_manifest(output_dir)
    previous_repos = (
        manifest.get("repos", []) if manifest else []
    )

    _print_repo_table(repos)

    removal_summary = SyncSummary()
    if previous_repos:
        removal_summary = _handle_removed_and_archived(
            output_dir, all_repos, previous_repos,
            should_delete=should_delete,
            deleted_dir=deleted_dir,
            archived_dir=archived_dir,
            dry_run=dry_run,
        )
        if removal_summary.results:
            console.print(
                "\n[bold]Changes for"
                " removed/archived repos:[/bold]"
            )
            for r in removal_summary.results:
                console.print(
                    f"  {r.name}: {r.action.value}"
                )

    if skipped_large_results:
        console.print(
            "\n[bold]Repos skipped (too large):[/bold]"
        )
        for r in skipped_large_results:
            console.print(
                f"  {r.name}: {r.action.value}"
            )

    if dry_run:
        console.print(
            "\n[dim]Dry run complete."
            " No changes made.[/dim]"
        )
        return

    active_repos = [r for r in repos if not r.archived]
    archived_repos = [r for r in repos if r.archived]

    summary = clone_repos(
        _repos_to_dicts(active_repos),
        token,
        output_dir,
        concurrency=concurrency,
        clone_wiki=clone_wiki,
        use_ssh=protocol == "ssh",
        git_author=git_author,
        git_email=git_email,
    )

    if archived_repos:
        if should_delete:
            console.print(
                f"Skipping {len(archived_repos)}"
                " archived repo(s) (--delete)"
            )
        else:
            console.print(
                f"Cloning {len(archived_repos)}"
                f" archived repo(s) into {archived_dir}/..."
            )
            archived_summary = clone_repos(
                _repos_to_dicts(archived_repos),
                token,
                output_dir / archived_dir,
                concurrency=concurrency,
                clone_wiki=clone_wiki,
                use_ssh=protocol == "ssh",
                git_author=git_author,
                git_email=git_email,
            )
            summary.results.extend(archived_summary.results)

    summary.results.extend(removal_summary.results)
    summary.results.extend(skipped_large_results)

    _write_manifest(output_dir, org, all_repos)
    _print_summary(summary)
