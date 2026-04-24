use anyhow::{Context, Result};
use octocrab::params::repos::Type;
use octocrab::Octocrab;
use regex::Regex;
use serde::{Deserialize, Serialize};

/// Subset of GitHub `Repository` fields the rest of the program cares about.
/// Mirrors the Python `RepoInfo` dataclass.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RepoInfo {
    pub name: String,
    pub clone_url: String,
    pub ssh_url: String,
    pub default_branch: String,
    pub archived: bool,
    pub private: bool,
    pub fork: bool,
    pub size_kb: u64,
    pub language: Option<String>,
    pub has_wiki: bool,
    pub pushed_at: Option<String>,
}

fn build_client(token: &str) -> Result<Octocrab> {
    Octocrab::builder()
        .personal_token(token.to_string())
        .build()
        .context("failed to build GitHub client")
}

/// Format an octocrab error with the GitHub status code and API message when
/// available, mirroring the Python `GithubException.status / .data['message']`
/// error strings.
fn format_error(err: octocrab::Error) -> String {
    if let octocrab::Error::GitHub { source, .. } = &err {
        return format!(
            "({}) {}",
            source.status_code.as_u16(),
            source.message
        );
    }
    err.to_string()
}

/// Validate a token by calling `GET /user`. Returns the authenticated login.
pub async fn validate_token(token: &str) -> Result<String> {
    let client = build_client(token)?;
    let user = client
        .current()
        .user()
        .await
        .map_err(|e| anyhow::anyhow!("Token validation failed: {}", format_error(e)))?;
    Ok(user.login)
}

/// List all repositories for an organization, following pagination.
pub async fn list_org_repos(token: &str, org: &str) -> Result<Vec<RepoInfo>> {
    let client = build_client(token)?;

    let first_page = client
        .orgs(org)
        .list_repos()
        .repo_type(Some(Type::All))
        .per_page(100)
        .send()
        .await
        .map_err(|e| {
            anyhow::anyhow!(
                "Failed to access organization '{}': {}",
                org,
                format_error(e)
            )
        })?;

    let repos = client.all_pages(first_page).await.map_err(|e| {
        anyhow::anyhow!(
            "Failed to paginate repos for '{}': {}",
            org,
            format_error(e)
        )
    })?;

    Ok(repos.into_iter().map(to_repo_info).collect())
}

fn to_repo_info(repo: octocrab::models::Repository) -> RepoInfo {
    let clone_url = repo
        .clone_url
        .as_ref()
        .map(|u| u.to_string())
        .unwrap_or_default();
    let ssh_url = repo.ssh_url.unwrap_or_default();
    let default_branch = repo
        .default_branch
        .unwrap_or_else(|| "main".to_string());
    let pushed_at = repo
        .pushed_at
        .map(|dt| dt.to_rfc3339_opts(chrono::SecondsFormat::Secs, false));
    let language = repo.language.and_then(|v| match v {
        serde_json::Value::String(s) => Some(s),
        _ => None,
    });

    RepoInfo {
        name: repo.name,
        clone_url,
        ssh_url,
        default_branch,
        archived: repo.archived.unwrap_or(false),
        private: repo.private.unwrap_or(false),
        fork: repo.fork.unwrap_or(false),
        size_kb: repo.size.unwrap_or(0) as u64,
        language,
        has_wiki: repo.has_wiki.unwrap_or(false),
        pushed_at,
    }
}

/// Filter a repo list by type (all/public/private/forks/sources) and optional
/// include/exclude regex patterns. Regexes use `regex::Regex::is_match`
/// semantics, which matches Python's `re.search`.
pub fn filter_repos(
    repos: Vec<RepoInfo>,
    repo_type: &str,
    include: Option<&str>,
    exclude: Option<&str>,
) -> Result<Vec<RepoInfo>> {
    let typed: Vec<RepoInfo> = repos
        .into_iter()
        .filter(|r| match repo_type {
            "public" => !r.private,
            "private" => r.private,
            "forks" => r.fork,
            "sources" => !r.fork,
            _ => true,
        })
        .collect();

    let included = if let Some(pattern) = include {
        let re =
            Regex::new(pattern).with_context(|| format!("invalid --include regex: {}", pattern))?;
        typed.into_iter().filter(|r| re.is_match(&r.name)).collect()
    } else {
        typed
    };

    let excluded = if let Some(pattern) = exclude {
        let re =
            Regex::new(pattern).with_context(|| format!("invalid --exclude regex: {}", pattern))?;
        included
            .into_iter()
            .filter(|r| !re.is_match(&r.name))
            .collect()
    } else {
        included
    };

    Ok(excluded)
}
