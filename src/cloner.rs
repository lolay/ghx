use std::path::{Path, PathBuf};
use std::process::Output;
use std::sync::Arc;
use std::time::Duration;

use anyhow::{Context, Result};
use futures::stream::{FuturesUnordered, StreamExt};
use indicatif::{ProgressBar, ProgressStyle};
use tokio::process::Command;
use tokio::sync::Semaphore;
use tokio::time::timeout;

use crate::github::RepoInfo;

/// Matches the Python `subprocess.run(..., timeout=300)` used for each git
/// invocation.
const GIT_TIMEOUT: Duration = Duration::from_secs(300);

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Action {
    Cloned,
    Pulled,
    SkippedDirty,
    SkippedArchived,
    MovedDeleted,
    MovedArchived,
    Deleted,
    Failed,
    ClonedWiki,
    SkippedWiki,
    SkippedTooLarge,
}

impl Action {
    pub fn as_str(&self) -> &'static str {
        match self {
            Self::Cloned => "cloned",
            Self::Pulled => "pulled",
            Self::SkippedDirty => "skipped (local changes)",
            Self::SkippedArchived => "skipped (archived)",
            Self::MovedDeleted => "moved (deleted)",
            Self::MovedArchived => "moved (archived)",
            Self::Deleted => "deleted",
            Self::Failed => "failed",
            Self::ClonedWiki => "cloned wiki",
            Self::SkippedWiki => "skipped wiki (not found)",
            Self::SkippedTooLarge => "skipped (too large)",
        }
    }

    /// All actions in the same order as the Python `Action` enum, so the
    /// Sync Summary rows appear in a stable, familiar sequence.
    pub fn all() -> &'static [Action] {
        &[
            Action::Cloned,
            Action::Pulled,
            Action::SkippedDirty,
            Action::SkippedArchived,
            Action::MovedDeleted,
            Action::MovedArchived,
            Action::Deleted,
            Action::Failed,
            Action::ClonedWiki,
            Action::SkippedWiki,
            Action::SkippedTooLarge,
        ]
    }
}

#[derive(Debug, Clone)]
pub struct RepoResult {
    pub name: String,
    pub action: Action,
    pub detail: String,
}

impl RepoResult {
    pub fn new(name: impl Into<String>, action: Action) -> Self {
        Self {
            name: name.into(),
            action,
            detail: String::new(),
        }
    }

    pub fn with_detail(name: impl Into<String>, action: Action, detail: impl Into<String>) -> Self {
        Self {
            name: name.into(),
            action,
            detail: detail.into(),
        }
    }
}

#[derive(Debug, Default)]
pub struct SyncSummary {
    pub results: Vec<RepoResult>,
}

impl SyncSummary {
    pub fn count(&self, action: Action) -> usize {
        self.results.iter().filter(|r| r.action == action).count()
    }
}

fn authenticated_url(clone_url: &str, token: &str) -> String {
    clone_url.replace("https://", &format!("https://{}@", token))
}

fn resolve_url(repo: &RepoInfo, token: &str, use_ssh: bool) -> String {
    if use_ssh {
        repo.ssh_url.clone()
    } else {
        authenticated_url(&repo.clone_url, token)
    }
}

fn resolve_wiki_url(repo: &RepoInfo, token: &str, use_ssh: bool) -> String {
    if use_ssh {
        if let Some(stripped) = repo.ssh_url.strip_suffix(".git") {
            format!("{}.wiki.git", stripped)
        } else {
            format!("{}.wiki", repo.ssh_url)
        }
    } else {
        let wiki_url = repo.clone_url.replace(".git", ".wiki.git");
        let wiki_url = if wiki_url.ends_with(".wiki.git") {
            wiki_url
        } else {
            format!("{}.wiki.git", wiki_url)
        };
        authenticated_url(&wiki_url, token)
    }
}

async fn run_git(args: &[&str], cwd: Option<&Path>) -> Result<Output> {
    let mut cmd = Command::new("git");
    cmd.args(args);
    if let Some(dir) = cwd {
        cmd.current_dir(dir);
    }
    let output = timeout(GIT_TIMEOUT, cmd.output())
        .await
        .context("git command timed out")?
        .context("failed to run git")?;
    Ok(output)
}

async fn is_dirty(repo_dir: &Path) -> bool {
    let Ok(out) = run_git(&["status", "--porcelain"], Some(repo_dir)).await else {
        return false;
    };
    !String::from_utf8_lossy(&out.stdout).trim().is_empty()
}

async fn set_git_identity(repo_dir: &Path, author: Option<&str>, email: Option<&str>) {
    if let Some(a) = author {
        let _ = run_git(&["config", "user.name", a], Some(repo_dir)).await;
    }
    if let Some(e) = email {
        let _ = run_git(&["config", "user.email", e], Some(repo_dir)).await;
    }
}

async fn clone_repo(url: &str, dest: &Path, branch: &str) -> Result<Output> {
    let dest_str = dest
        .to_str()
        .context("destination path is not valid UTF-8")?;
    run_git(&["clone", "--branch", branch, url, dest_str], None).await
}

async fn pull_repo(repo_dir: &Path) -> Result<Output> {
    run_git(&["pull", "--ff-only"], Some(repo_dir)).await
}

/// Clone (or pull) a single repository and optionally its wiki. Returns one
/// or two `RepoResult`s describing what happened.
async fn process_single_repo(
    repo: RepoInfo,
    token: String,
    output_dir: PathBuf,
    clone_wiki: bool,
    use_ssh: bool,
    git_author: Option<String>,
    git_email: Option<String>,
) -> Vec<RepoResult> {
    let name = repo.name.clone();
    let mut results: Vec<RepoResult> = Vec::new();
    let dest = output_dir.join(&name);
    let url = resolve_url(&repo, &token, use_ssh);

    let has_git = dest.exists() && dest.join(".git").is_dir();
    if has_git {
        set_git_identity(&dest, git_author.as_deref(), git_email.as_deref()).await;
        if is_dirty(&dest).await {
            results.push(RepoResult::new(&name, Action::SkippedDirty));
        } else {
            match pull_repo(&dest).await {
                Ok(out) if out.status.success() => {
                    results.push(RepoResult::new(&name, Action::Pulled));
                }
                Ok(out) => {
                    let stderr = String::from_utf8_lossy(&out.stderr).trim().to_string();
                    results.push(RepoResult::with_detail(&name, Action::Failed, stderr));
                }
                Err(e) => {
                    results.push(RepoResult::with_detail(&name, Action::Failed, e.to_string()));
                }
            }
        }
    } else {
        match clone_repo(&url, &dest, &repo.default_branch).await {
            Ok(out) if out.status.success() => {
                set_git_identity(&dest, git_author.as_deref(), git_email.as_deref()).await;
                results.push(RepoResult::new(&name, Action::Cloned));
            }
            Ok(out) => {
                let stderr = String::from_utf8_lossy(&out.stderr).trim().to_string();
                results.push(RepoResult::with_detail(&name, Action::Failed, stderr));
            }
            Err(e) => {
                results.push(RepoResult::with_detail(&name, Action::Failed, e.to_string()));
            }
        }
    }

    let last_action = results.last().map(|r| r.action);
    if clone_wiki && repo.has_wiki && last_action != Some(Action::Failed) {
        let wiki_dest = output_dir.join(format!("{}.wiki", name));
        let wiki_url = resolve_wiki_url(&repo, &token, use_ssh);

        if wiki_dest.exists() && wiki_dest.join(".git").is_dir() {
            set_git_identity(&wiki_dest, git_author.as_deref(), git_email.as_deref()).await;
            if !is_dirty(&wiki_dest).await {
                let _ = pull_repo(&wiki_dest).await;
            }
        } else if let Some(wiki_dest_str) = wiki_dest.to_str() {
            match run_git(&["clone", &wiki_url, wiki_dest_str], None).await {
                Ok(out) if out.status.success() => {
                    set_git_identity(&wiki_dest, git_author.as_deref(), git_email.as_deref())
                        .await;
                    results.push(RepoResult::new(&name, Action::ClonedWiki));
                }
                _ => {
                    results.push(RepoResult::new(&name, Action::SkippedWiki));
                }
            }
        }
    }

    results
}

/// Clone or pull a batch of repositories concurrently, capped at `concurrency`
/// in-flight git subprocesses via a semaphore.
pub async fn clone_repos(
    repos: Vec<RepoInfo>,
    token: String,
    output_dir: PathBuf,
    concurrency: usize,
    clone_wiki: bool,
    use_ssh: bool,
    git_author: Option<String>,
    git_email: Option<String>,
) -> Result<SyncSummary> {
    std::fs::create_dir_all(&output_dir).with_context(|| {
        format!("failed to create output directory {}", output_dir.display())
    })?;

    let sem = Arc::new(Semaphore::new(concurrency.max(1)));
    let mut tasks = FuturesUnordered::new();

    let total = repos.len() as u64;
    let progress = ProgressBar::new(total);
    progress.set_style(
        ProgressStyle::with_template("{spinner} {msg} [{bar:40}] {percent:>3}% ({elapsed})")
            .unwrap()
            .progress_chars("=>-"),
    );
    progress.set_message("Syncing repos...");
    progress.enable_steady_tick(Duration::from_millis(100));

    for repo in repos {
        let sem = sem.clone();
        let token = token.clone();
        let output_dir = output_dir.clone();
        let git_author = git_author.clone();
        let git_email = git_email.clone();
        let repo_name = repo.name.clone();
        tasks.push(tokio::spawn(async move {
            let _permit = sem.acquire_owned().await.expect("semaphore closed");
            let results = process_single_repo(
                repo,
                token,
                output_dir,
                clone_wiki,
                use_ssh,
                git_author,
                git_email,
            )
            .await;
            (repo_name, results)
        }));
    }

    let mut summary = SyncSummary::default();
    while let Some(joined) = tasks.next().await {
        match joined {
            Ok((_, results)) => summary.results.extend(results),
            Err(e) => {
                summary.results.push(RepoResult::with_detail(
                    format!("<task panic>"),
                    Action::Failed,
                    e.to_string(),
                ));
            }
        }
        progress.inc(1);
    }
    progress.finish_and_clear();

    Ok(summary)
}

/// Move a repo directory into `target_dir` (created if missing). Falls back
/// to copy+remove when `rename` fails (e.g. crossing filesystems).
pub fn move_repo(repo_dir: &Path, target_dir: &Path) -> Result<()> {
    std::fs::create_dir_all(target_dir).with_context(|| {
        format!("failed to create target directory {}", target_dir.display())
    })?;
    let file_name = repo_dir
        .file_name()
        .with_context(|| format!("{} has no file name", repo_dir.display()))?;
    let dest = target_dir.join(file_name);
    if dest.exists() {
        std::fs::remove_dir_all(&dest)
            .with_context(|| format!("failed to remove existing {}", dest.display()))?;
    }
    match std::fs::rename(repo_dir, &dest) {
        Ok(()) => Ok(()),
        Err(_) => {
            copy_dir_recursive(repo_dir, &dest)?;
            std::fs::remove_dir_all(repo_dir).with_context(|| {
                format!("failed to remove {} after cross-fs move", repo_dir.display())
            })?;
            Ok(())
        }
    }
}

pub fn delete_repo(repo_dir: &Path) -> Result<()> {
    if repo_dir.exists() {
        std::fs::remove_dir_all(repo_dir)
            .with_context(|| format!("failed to delete {}", repo_dir.display()))?;
    }
    Ok(())
}

fn copy_dir_recursive(src: &Path, dst: &Path) -> Result<()> {
    std::fs::create_dir_all(dst)?;
    for entry in std::fs::read_dir(src)? {
        let entry = entry?;
        let file_type = entry.file_type()?;
        let dst_path = dst.join(entry.file_name());
        if file_type.is_dir() {
            copy_dir_recursive(&entry.path(), &dst_path)?;
        } else if file_type.is_symlink() {
            let target = std::fs::read_link(entry.path())?;
            #[cfg(unix)]
            std::os::unix::fs::symlink(target, &dst_path)?;
            #[cfg(windows)]
            {
                if target.is_dir() {
                    std::os::windows::fs::symlink_dir(target, &dst_path)?;
                } else {
                    std::os::windows::fs::symlink_file(target, &dst_path)?;
                }
            }
        } else {
            std::fs::copy(entry.path(), &dst_path)?;
        }
    }
    Ok(())
}
