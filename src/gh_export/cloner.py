from __future__ import annotations

import shutil
import subprocess
from concurrent.futures import ThreadPoolExecutor, as_completed
from dataclasses import dataclass, field
from enum import Enum
from pathlib import Path

from rich.progress import BarColumn, Progress, SpinnerColumn, TextColumn, TimeElapsedColumn


class Action(Enum):
    CLONED = "cloned"
    PULLED = "pulled"
    SKIPPED_DIRTY = "skipped (local changes)"
    SKIPPED_ARCHIVED = "skipped (archived)"
    MOVED_DELETED = "moved (deleted)"
    MOVED_ARCHIVED = "moved (archived)"
    DELETED = "deleted"
    FAILED = "failed"
    CLONED_WIKI = "cloned wiki"
    SKIPPED_WIKI = "skipped wiki (not found)"
    SKIPPED_TOO_LARGE = "skipped (too large)"


@dataclass
class RepoResult:
    name: str
    action: Action
    detail: str = ""


@dataclass
class SyncSummary:
    results: list[RepoResult] = field(default_factory=list)

    def count(self, action: Action) -> int:
        return sum(1 for r in self.results if r.action == action)


def _authenticated_url(clone_url: str, token: str) -> str:
    """Embed the token in an HTTPS clone URL."""
    return clone_url.replace("https://", f"https://{token}@")


def _run_git(args: list[str], cwd: Path | None = None) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        ["git", *args],
        cwd=cwd,
        capture_output=True,
        text=True,
        timeout=300,
    )


def _is_dirty(repo_dir: Path) -> bool:
    result = _run_git(["status", "--porcelain"], cwd=repo_dir)
    return bool(result.stdout.strip())


def _set_git_identity(repo_dir: Path, author: str | None, email: str | None) -> None:
    if author:
        _run_git(["config", "user.name", author], cwd=repo_dir)
    if email:
        _run_git(["config", "user.email", email], cwd=repo_dir)


def _clone_repo(url: str, dest: Path, branch: str) -> subprocess.CompletedProcess[str]:
    return _run_git(["clone", "--branch", branch, url, str(dest)])


def _pull_repo(repo_dir: Path) -> subprocess.CompletedProcess[str]:
    return _run_git(["pull", "--ff-only"], cwd=repo_dir)


def _resolve_url(repo: dict, token: str, use_ssh: bool) -> str:
    if use_ssh:
        return repo["ssh_url"]
    return _authenticated_url(repo["clone_url"], token)


def _resolve_wiki_url(repo: dict, token: str, use_ssh: bool) -> str:
    if use_ssh:
        # git@github.com:org/repo.git -> git@github.com:org/repo.wiki.git
        ssh = repo["ssh_url"]
        if ssh.endswith(".git"):
            return ssh[:-4] + ".wiki.git"
        return ssh + ".wiki"
    clone_url = repo["clone_url"]
    wiki_url = clone_url.replace(".git", ".wiki.git")
    if not wiki_url.endswith(".wiki.git"):
        wiki_url += ".wiki.git"
    return _authenticated_url(wiki_url, token)


def _process_single_repo(
    repo: dict,
    token: str,
    output_dir: Path,
    clone_wiki: bool,
    use_ssh: bool,
    git_author: str | None = None,
    git_email: str | None = None,
) -> list[RepoResult]:
    name = repo["name"]
    results: list[RepoResult] = []
    dest = output_dir / name
    url = _resolve_url(repo, token, use_ssh)

    if dest.exists() and (dest / ".git").is_dir():
        _set_git_identity(dest, git_author, git_email)
        if _is_dirty(dest):
            results.append(RepoResult(name, Action.SKIPPED_DIRTY))
        else:
            proc = _pull_repo(dest)
            if proc.returncode == 0:
                results.append(RepoResult(name, Action.PULLED))
            else:
                results.append(RepoResult(name, Action.FAILED, proc.stderr.strip()))
    else:
        proc = _clone_repo(url, dest, repo["default_branch"])
        if proc.returncode == 0:
            _set_git_identity(dest, git_author, git_email)
            results.append(RepoResult(name, Action.CLONED))
        else:
            results.append(RepoResult(name, Action.FAILED, proc.stderr.strip()))

    if clone_wiki and repo["has_wiki"] and results[-1].action not in (Action.FAILED,):
        wiki_dest = output_dir / f"{name}.wiki"
        wiki_url = _resolve_wiki_url(repo, token, use_ssh)

        if wiki_dest.exists() and (wiki_dest / ".git").is_dir():
            _set_git_identity(wiki_dest, git_author, git_email)
            if not _is_dirty(wiki_dest):
                _pull_repo(wiki_dest)
        else:
            proc = _run_git(["clone", wiki_url, str(wiki_dest)])
            if proc.returncode == 0:
                _set_git_identity(wiki_dest, git_author, git_email)
                results.append(RepoResult(name, Action.CLONED_WIKI))
            else:
                results.append(RepoResult(name, Action.SKIPPED_WIKI))

    return results


def clone_repos(
    repos: list[dict],
    token: str,
    output_dir: Path,
    concurrency: int = 4,
    clone_wiki: bool = False,
    use_ssh: bool = False,
    git_author: str | None = None,
    git_email: str | None = None,
) -> SyncSummary:
    """Clone or pull a list of repos concurrently.

    Args:
        repos: Repo dicts with keys name, clone_url, ssh_url,
            default_branch, archived, has_wiki.
        token: GitHub PAT used for HTTPS authentication.
        output_dir: Directory to clone repos into (created if missing).
        concurrency: Number of parallel clone/pull workers.
        clone_wiki: Also clone each repo's wiki when available.
        use_ssh: Use SSH URLs instead of HTTPS.
        git_author: Value to set as git user.name in each repo.
        git_email: Value to set as git user.email in each repo.

    Returns:
        A SyncSummary containing a RepoResult per repo processed.
    """
    summary = SyncSummary()
    output_dir.mkdir(parents=True, exist_ok=True)

    with Progress(
        SpinnerColumn(),
        TextColumn("[progress.description]{task.description}"),
        BarColumn(),
        TextColumn("[progress.percentage]{task.percentage:>3.0f}%"),
        TimeElapsedColumn(),
    ) as progress:
        task = progress.add_task("Syncing repos...", total=len(repos))

        with ThreadPoolExecutor(max_workers=concurrency) as pool:
            futures = {
                pool.submit(
                    _process_single_repo,
                    r,
                    token,
                    output_dir,
                    clone_wiki,
                    use_ssh,
                    git_author,
                    git_email,
                ): r["name"]
                for r in repos
            }
            for future in as_completed(futures):
                repo_name = futures[future]
                try:
                    results = future.result()
                    summary.results.extend(results)
                except Exception as exc:
                    summary.results.append(
                        RepoResult(repo_name, Action.FAILED, str(exc))
                    )
                progress.advance(task)

    return summary


def move_repo(repo_dir: Path, target_dir: Path) -> None:
    """Move a repo directory into the target directory.

    Args:
        repo_dir: Path to the repo to move.
        target_dir: Destination parent directory (created if missing).
    """
    target_dir.mkdir(parents=True, exist_ok=True)
    dest = target_dir / repo_dir.name
    if dest.exists():
        shutil.rmtree(dest)
    shutil.move(str(repo_dir), str(dest))


def delete_repo(repo_dir: Path) -> None:
    """Permanently delete a repo directory.

    Args:
        repo_dir: Path to the repo directory to remove.
    """
    if repo_dir.exists():
        shutil.rmtree(repo_dir)
