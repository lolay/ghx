from __future__ import annotations

import re
from dataclasses import dataclass

from github import Auth, Github, GithubException


@dataclass
class RepoInfo:
    name: str
    clone_url: str
    ssh_url: str
    default_branch: str
    archived: bool
    private: bool
    fork: bool
    size_kb: int
    language: str | None
    has_wiki: bool
    pushed_at: str | None


def validate_token(token: str) -> str:
    """Validate the PAT by calling GET /user.

    Args:
        token: GitHub Personal Access Token to validate.

    Returns:
        The authenticated username.

    Raises:
        SystemExit: If the token is invalid or the API call fails.
    """
    g = Github(auth=Auth.Token(token))
    try:
        user = g.get_user()
        login = user.login
    except GithubException as exc:
        raise SystemExit(
            f"Token validation failed ({exc.status}): {exc.data.get('message', str(exc))}"
        ) from exc
    finally:
        g.close()
    return login


def list_org_repos(
    token: str,
    org: str,
) -> list[RepoInfo]:
    """List all repos for an organization.

    Args:
        token: GitHub Personal Access Token.
        org: GitHub organization name.

    Returns:
        List of RepoInfo dataclasses for all repos in the org.

    Raises:
        SystemExit: If the organization cannot be accessed.
    """
    g = Github(auth=Auth.Token(token), per_page=100)
    try:
        organization = g.get_organization(org)
    except GithubException as exc:
        raise SystemExit(
            f"Failed to access organization '{org}' ({exc.status}): "
            f"{exc.data.get('message', str(exc))}"
        ) from exc

    repos: list[RepoInfo] = []
    for repo in organization.get_repos(type="all"):
        repos.append(
            RepoInfo(
                name=repo.name,
                clone_url=repo.clone_url,
                ssh_url=repo.ssh_url,
                default_branch=repo.default_branch or "main",
                archived=repo.archived,
                private=repo.private,
                fork=repo.fork,
                size_kb=repo.size,
                language=repo.language,
                has_wiki=repo.has_wiki,
                pushed_at=(
                    repo.pushed_at.isoformat()
                    if repo.pushed_at else None
                ),
            )
        )

    g.close()
    return repos


def filter_repos(
    repos: list[RepoInfo],
    repo_type: str = "all",
    include: str | None = None,
    exclude: str | None = None,
) -> list[RepoInfo]:
    """Filter repos by type, include, and exclude patterns.

    Args:
        repos: Full list of repos from the org.
        repo_type: Filter by type (all, public, private, forks, sources).
        include: Regex pattern; only repos whose name matches are kept.
        exclude: Regex pattern; repos whose name matches are removed.

    Returns:
        Filtered list of RepoInfo dataclasses.
    """
    filtered = repos

    if repo_type != "all":
        type_predicates = {
            "public": lambda r: not r.private,
            "private": lambda r: r.private,
            "forks": lambda r: r.fork,
            "sources": lambda r: not r.fork,
        }
        pred = type_predicates.get(repo_type)
        if pred:
            filtered = [r for r in filtered if pred(r)]

    if include:
        include_re = re.compile(include)
        filtered = [
            r for r in filtered
            if include_re.search(r.name)
        ]
    if exclude:
        exclude_re = re.compile(exclude)
        filtered = [
            r for r in filtered
            if not exclude_re.search(r.name)
        ]

    return filtered
