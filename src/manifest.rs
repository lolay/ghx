use std::fs;
use std::path::Path;

use anyhow::{Context, Result};
use chrono::{SecondsFormat, Utc};
use serde::{Deserialize, Serialize};

use crate::config::migrate_legacy_file;
use crate::github::RepoInfo;

pub const MANIFEST_FILENAME: &str = ".ghx.json";
pub const LEGACY_MANIFEST_FILENAME: &str = ".gh-export.json";

#[derive(Debug, Serialize, Deserialize)]
pub struct Manifest {
    pub org: String,
    pub exported_at: String,
    pub repos: Vec<ManifestRepo>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ManifestRepo {
    pub name: String,
    pub default_branch: String,
    pub archived: bool,
    pub has_wiki: bool,
}

/// Read and parse the manifest at `<dir>/.ghx.json`, returning `Ok(None)` if
/// the file does not exist. Migrates a legacy `.gh-export.json` file first.
pub fn read(dir: &Path) -> Result<Option<Manifest>> {
    if dir.exists() {
        migrate_legacy_file(dir, LEGACY_MANIFEST_FILENAME, MANIFEST_FILENAME)?;
    }
    let path = dir.join(MANIFEST_FILENAME);
    if !path.exists() {
        return Ok(None);
    }
    let text = fs::read_to_string(&path)
        .with_context(|| format!("failed to read {}", path.display()))?;
    let manifest: Manifest = serde_json::from_str(&text)
        .with_context(|| format!("failed to parse {}", path.display()))?;
    Ok(Some(manifest))
}

/// Write a manifest describing `repos` as the current state for `org`.
///
/// Uses 2-space indent + trailing newline to match the Python
/// `json.dump(..., indent=2)` output byte-for-byte on the fields we share.
pub fn write(dir: &Path, org: &str, repos: &[RepoInfo]) -> Result<()> {
    let manifest = Manifest {
        org: org.to_string(),
        exported_at: Utc::now().to_rfc3339_opts(SecondsFormat::Micros, false),
        repos: repos
            .iter()
            .map(|r| ManifestRepo {
                name: r.name.clone(),
                default_branch: r.default_branch.clone(),
                archived: r.archived,
                has_wiki: r.has_wiki,
            })
            .collect(),
    };
    let mut json = serde_json::to_string_pretty(&manifest)
        .context("failed to serialize manifest")?;
    json.push('\n');
    let path = dir.join(MANIFEST_FILENAME);
    fs::write(&path, json).with_context(|| format!("failed to write {}", path.display()))?;
    Ok(())
}
