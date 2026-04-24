mod cloner;
mod config;
mod display;
mod github;
mod manifest;

use std::collections::BTreeSet;
use std::path::{Path, PathBuf};

use anyhow::Result;
use clap::Parser;
use owo_colors::OwoColorize;

use crate::cloner::{clone_repos, delete_repo, move_repo, Action, RepoResult, SyncSummary};
use crate::config::{Protocol, Source};
use crate::display::SettingsRow;
use crate::github::{filter_repos, list_org_repos, validate_token, RepoInfo};
use crate::manifest::ManifestRepo;

#[derive(Debug, Parser)]
#[command(
    name = "ghx-rust",
    version,
    about = "Export and sync all GitHub repos for an organization"
)]
pub struct Args {
    /// GitHub organization name
    pub org: String,

    /// Output directory
    pub path: PathBuf,

    /// GitHub Personal Access Token
    #[arg(long, short = 't')]
    pub token: Option<String>,

    /// Repo type: all|public|private|forks|sources
    #[arg(long = "type")]
    pub repo_type: Option<String>,

    /// Parallel workers
    #[arg(long, short = 'c')]
    pub concurrency: Option<usize>,

    /// Preview changes without doing anything
    #[arg(long = "dry-run")]
    pub dry_run: bool,

    /// Also clone associated wikis
    #[arg(long = "clone-wiki")]
    pub clone_wiki: bool,

    /// Regex to include only matching repo names
    #[arg(long)]
    pub include: Option<String>,

    /// Regex to exclude matching repo names
    #[arg(long)]
    pub exclude: Option<String>,

    /// Git transport protocol
    #[arg(long, value_enum)]
    pub protocol: Option<Protocol>,

    /// Shortcut for --protocol ssh
    #[arg(long)]
    pub ssh: bool,

    /// Set git user.name in each cloned repo
    #[arg(long = "git-author")]
    pub git_author: Option<String>,

    /// Set git user.email in each cloned repo
    #[arg(long = "git-email")]
    pub git_email: Option<String>,

    /// Permanently delete removed/archived repos
    #[arg(long)]
    pub delete: bool,

    /// Directory for removed repos
    #[arg(long = "deleted-dir")]
    pub deleted_dir: Option<String>,

    /// Directory for archived repos
    #[arg(long = "archived-dir")]
    pub archived_dir: Option<String>,

    /// Skip repos larger than this size in MB
    #[arg(long = "max-size")]
    pub max_size: Option<u64>,
}

#[tokio::main]
async fn main() -> Result<()> {
    let args = Args::parse();
    run(args).await
}

async fn run(args: Args) -> Result<()> {
    let output_dir = args.path.clone();
    let cfg = config::load(Some(&output_dir))?;

    let (token, token_src) = config::resolve_token(args.token.as_deref(), &cfg)?;
    let (protocol, protocol_src) = config::resolve_protocol(args.protocol, args.ssh, &cfg)?;

    let (repo_type, repo_type_src) =
        config::resolve_string(args.repo_type.as_deref(), &cfg, "type", Some("all"));
    let repo_type = repo_type.unwrap_or_else(|| "all".to_string());

    let (concurrency, _) = config::resolve_int(
        args.concurrency.map(|v| v as i64),
        &cfg,
        "concurrency",
        Some(4),
    );
    let concurrency = concurrency.unwrap_or(4).max(1) as usize;

    let (dry_run, _) = config::resolve_bool_flag(args.dry_run, &cfg, "dry-run", false);
    let (clone_wiki, wiki_src) =
        config::resolve_bool_flag(args.clone_wiki, &cfg, "clone-wiki", false);
    let (include, include_src) =
        config::resolve_string(args.include.as_deref(), &cfg, "include", None);
    let (exclude, exclude_src) =
        config::resolve_string(args.exclude.as_deref(), &cfg, "exclude", None);
    let (git_author, author_src) =
        config::resolve_string(args.git_author.as_deref(), &cfg, "git-author", None);
    let (git_email, email_src) =
        config::resolve_string(args.git_email.as_deref(), &cfg, "git-email", None);
    let (should_delete, _) = config::resolve_bool_flag(args.delete, &cfg, "delete", false);

    let (deleted_dir, _) = config::resolve_string(
        args.deleted_dir.as_deref(),
        &cfg,
        "deleted-dir",
        Some("DELETED"),
    );
    let deleted_dir = deleted_dir.unwrap_or_else(|| "DELETED".to_string());
    let (archived_dir, _) = config::resolve_string(
        args.archived_dir.as_deref(),
        &cfg,
        "archived-dir",
        Some("ARCHIVED"),
    );
    let archived_dir = archived_dir.unwrap_or_else(|| "ARCHIVED".to_string());

    let (max_size, max_size_src) =
        config::resolve_int(args.max_size.map(|v| v as i64), &cfg, "max-size", None);
    let max_size = max_size.map(|v| v as u64);

    let mut rows: Vec<SettingsRow> = vec![
        ("Organization:", args.org.clone(), Source::Argument.label()),
        (
            "Output:",
            output_dir.display().to_string(),
            Source::Argument.label(),
        ),
        (
            "Protocol:",
            protocol.as_str().to_string(),
            protocol_src.label(),
        ),
        ("Token:", obfuscate_token(&token), token_src.label()),
        ("Type:", repo_type.clone(), repo_type_src.label()),
        (
            "Clone wiki:",
            if clone_wiki { "yes".into() } else { "no".into() },
            wiki_src.label(),
        ),
    ];
    if let Some(ref inc) = include {
        rows.push(("Include:", inc.clone(), include_src.label()));
    }
    if let Some(ref exc) = exclude {
        rows.push(("Exclude:", exc.clone(), exclude_src.label()));
    }
    if let Some(ms) = max_size {
        rows.push(("Max size:", format!("{} MB", ms), max_size_src.label()));
    }
    rows.push((
        "Git author:",
        git_author.clone().unwrap_or_else(|| "-".to_string()),
        author_src.label(),
    ));
    rows.push((
        "Git email:",
        git_email.clone().unwrap_or_else(|| "-".to_string()),
        email_src.label(),
    ));
    display::print_settings(&rows);

    println!("{}", "Authenticating...".dimmed());
    let username = validate_token(&token).await?;
    println!("Authenticated as {}", username.bold());

    println!("Fetching repos for {}...", args.org.bold());
    let all_repos = list_org_repos(&token, &args.org).await?;
    let mut repos = filter_repos(
        all_repos.clone(),
        &repo_type,
        include.as_deref(),
        exclude.as_deref(),
    )?;
    println!("Found {} repos", repos.len().to_string().bold());

    let max_size_kb = max_size.map(|v| v * 1024);
    let mut too_large: Vec<RepoInfo> = Vec::new();
    if let Some(limit) = max_size_kb {
        too_large = repos.iter().filter(|r| r.size_kb > limit).cloned().collect();
        repos.retain(|r| r.size_kb <= limit);
        if !too_large.is_empty() {
            println!(
                "Skipping {} repo(s) exceeding {} MB",
                too_large.len().to_string().bold(),
                max_size.unwrap()
            );
        }
    }

    let skipped_large_results: Vec<RepoResult> = too_large
        .iter()
        .map(|r| RepoResult::new(r.name.clone(), Action::SkippedTooLarge))
        .collect();

    let previous_manifest = manifest::read(&output_dir)?;
    let previous_repos: Vec<ManifestRepo> = previous_manifest
        .map(|m| m.repos)
        .unwrap_or_default();

    display::print_repo_table(&repos);

    let mut removal_summary = SyncSummary::default();
    if !previous_repos.is_empty() {
        removal_summary = handle_removed_and_archived(
            &output_dir,
            &all_repos,
            &previous_repos,
            should_delete,
            &deleted_dir,
            &archived_dir,
            dry_run,
        )?;
        if !removal_summary.results.is_empty() {
            println!();
            println!("{}", "Changes for removed/archived repos:".bold());
            for r in &removal_summary.results {
                println!("  {}: {}", r.name, r.action.as_str());
            }
        }
    }

    if !skipped_large_results.is_empty() {
        println!();
        println!("{}", "Repos skipped (too large):".bold());
        for r in &skipped_large_results {
            println!("  {}: {}", r.name, r.action.as_str());
        }
    }

    if dry_run {
        println!();
        println!("{}", "Dry run complete. No changes made.".dimmed());
        return Ok(());
    }

    let (active_repos, archived_repos): (Vec<RepoInfo>, Vec<RepoInfo>) =
        repos.into_iter().partition(|r| !r.archived);

    let use_ssh = matches!(protocol, Protocol::Ssh);

    let mut summary = clone_repos(
        active_repos,
        token.clone(),
        output_dir.clone(),
        concurrency,
        clone_wiki,
        use_ssh,
        git_author.clone(),
        git_email.clone(),
    )
    .await?;

    if !archived_repos.is_empty() {
        if should_delete {
            println!(
                "Skipping {} archived repo(s) (--delete)",
                archived_repos.len()
            );
        } else {
            println!(
                "Cloning {} archived repo(s) into {}/...",
                archived_repos.len(),
                archived_dir
            );
            let archived_summary = clone_repos(
                archived_repos,
                token.clone(),
                output_dir.join(&archived_dir),
                concurrency,
                clone_wiki,
                use_ssh,
                git_author.clone(),
                git_email.clone(),
            )
            .await?;
            summary.results.extend(archived_summary.results);
        }
    }

    summary.results.extend(removal_summary.results);
    summary.results.extend(skipped_large_results);

    manifest::write(&output_dir, &args.org, &all_repos)?;
    display::print_summary(&summary);

    Ok(())
}

fn obfuscate_token(token: &str) -> String {
    if token.len() <= 8 {
        return "****".to_string();
    }
    format!("{}****{}", &token[..4], &token[token.len() - 4..])
}

/// Compare the current repo list to the previously-exported manifest and
/// produce the set of removed/archived actions. Directory mutations are
/// skipped in `dry_run` mode; actions are still recorded.
fn handle_removed_and_archived(
    output_dir: &Path,
    current_repos: &[RepoInfo],
    previous_repos: &[ManifestRepo],
    should_delete: bool,
    deleted_dir: &str,
    archived_dir: &str,
    dry_run: bool,
) -> Result<SyncSummary> {
    let mut summary = SyncSummary::default();

    let current_names: BTreeSet<String> =
        current_repos.iter().map(|r| r.name.clone()).collect();
    let current_archived: BTreeSet<String> = current_repos
        .iter()
        .filter(|r| r.archived)
        .map(|r| r.name.clone())
        .collect();
    let previous_names: BTreeSet<String> =
        previous_repos.iter().map(|r| r.name.clone()).collect();
    let previously_archived: BTreeSet<String> = previous_repos
        .iter()
        .filter(|r| r.archived)
        .map(|r| r.name.clone())
        .collect();

    let removed: Vec<String> = previous_names
        .difference(&current_names)
        .cloned()
        .collect();
    let newly_archived: Vec<String> = current_archived
        .difference(&previously_archived)
        .cloned()
        .collect();

    for name in &removed {
        let repo_dir = output_dir.join(name);
        if !repo_dir.exists() {
            continue;
        }
        if dry_run {
            let action = if should_delete {
                Action::Deleted
            } else {
                Action::MovedDeleted
            };
            summary
                .results
                .push(RepoResult::with_detail(name.clone(), action, "dry-run"));
            continue;
        }
        if should_delete {
            delete_repo(&repo_dir)?;
            summary
                .results
                .push(RepoResult::new(name.clone(), Action::Deleted));
        } else {
            move_repo(&repo_dir, &output_dir.join(deleted_dir))?;
            summary
                .results
                .push(RepoResult::new(name.clone(), Action::MovedDeleted));
        }
    }

    for name in &newly_archived {
        let repo_dir = output_dir.join(name);
        if !repo_dir.exists() {
            continue;
        }
        if dry_run {
            let action = if should_delete {
                Action::Deleted
            } else {
                Action::MovedArchived
            };
            summary
                .results
                .push(RepoResult::with_detail(name.clone(), action, "dry-run"));
            continue;
        }
        if should_delete {
            delete_repo(&repo_dir)?;
            summary
                .results
                .push(RepoResult::new(name.clone(), Action::Deleted));
        } else {
            move_repo(&repo_dir, &output_dir.join(archived_dir))?;
            summary
                .results
                .push(RepoResult::new(name.clone(), Action::MovedArchived));
        }
    }

    Ok(summary)
}
