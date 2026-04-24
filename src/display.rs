use comfy_table::presets::UTF8_FULL;
use comfy_table::{Cell, Color, Table};
use owo_colors::OwoColorize;

use crate::cloner::{Action, RepoResult, SyncSummary};
use crate::github::RepoInfo;

/// A single row in the Settings panel: (label, value, source).
pub type SettingsRow = (&'static str, String, String);

/// Print the "Settings:" panel with aligned values and dimmed `(source)`
/// annotations, analog of the Python `_print_settings` helper.
pub fn print_settings(rows: &[SettingsRow]) {
    let max_val_len = rows.iter().map(|(_, v, _)| v.len()).max().unwrap_or(0);

    println!();
    println!("{}", "Settings:".bold());
    for (label, value, source) in rows {
        let padding = max_val_len.saturating_sub(value.len()) + 2;
        let pad = " ".repeat(padding);
        println!(
            "  {:<15}{}{}{}",
            label,
            value,
            pad,
            format!("({})", source).dimmed()
        );
    }
    println!();
}

/// Print the "Repos to export" table, analog of Python `_print_repo_table`.
pub fn print_repo_table(repos: &[RepoInfo]) {
    let mut table = Table::new();
    table.load_preset(UTF8_FULL);
    table.set_header(vec![
        Cell::new("Name"),
        Cell::new("Visibility"),
        Cell::new("Language"),
        Cell::new("Size (MB)"),
        Cell::new("Default Branch"),
        Cell::new("Archived"),
        Cell::new("Last Pushed"),
    ]);

    for r in repos {
        table.add_row(vec![
            Cell::new(&r.name).fg(Color::Cyan),
            Cell::new(if r.private { "private" } else { "public" }),
            Cell::new(r.language.as_deref().unwrap_or("-")),
            Cell::new(format!("{:.1}", (r.size_kb as f64) / 1024.0)),
            Cell::new(&r.default_branch),
            Cell::new(if r.archived { "yes" } else { "no" }),
            Cell::new(r.pushed_at.as_deref().unwrap_or("-")),
        ]);
    }

    println!();
    println!("{}", "Repos to export".bold());
    println!("{table}");
    println!();
    println!("Total: {} repos", repos.len().to_string().bold());
}

/// Print the Sync Summary table plus lists of failed and dirty-skipped repos.
pub fn print_summary(summary: &SyncSummary) {
    let mut table = Table::new();
    table.load_preset(UTF8_FULL);
    table.set_header(vec![Cell::new("Action"), Cell::new("Count")]);

    for action in Action::all() {
        let count = summary.count(*action);
        if count > 0 {
            table.add_row(vec![
                Cell::new(action.as_str()).fg(Color::Cyan),
                Cell::new(count.to_string()),
            ]);
        }
    }

    println!();
    println!("{}", "Sync Summary".bold());
    println!("{table}");

    let failed: Vec<&RepoResult> = summary
        .results
        .iter()
        .filter(|r| r.action == Action::Failed)
        .collect();
    if !failed.is_empty() {
        println!();
        println!("{}", "Failed repos:".red().bold());
        for r in &failed {
            println!("  {}: {}", r.name, r.detail);
        }
    }

    let dirty: Vec<&RepoResult> = summary
        .results
        .iter()
        .filter(|r| r.action == Action::SkippedDirty)
        .collect();
    if !dirty.is_empty() {
        println!();
        println!("{}", "Skipped (local changes):".yellow().bold());
        for r in &dirty {
            println!("  {}", r.name);
        }
    }
}
