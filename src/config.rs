use std::collections::BTreeMap;
use std::env;
use std::fs;
use std::path::Path;

use anyhow::{bail, Context, Result};

pub const CONFIG_FILENAME: &str = ".ghx.toml";
pub const LEGACY_CONFIG_FILENAME: &str = ".gh-export.toml";

/// Git transport protocol. Used both as a clap value and as a TOML value.
#[derive(Debug, Clone, Copy, PartialEq, Eq, clap::ValueEnum)]
pub enum Protocol {
    Https,
    Ssh,
}

impl Protocol {
    pub fn as_str(&self) -> &'static str {
        match self {
            Protocol::Https => "https",
            Protocol::Ssh => "ssh",
        }
    }

    fn parse(s: &str) -> Result<Self> {
        match s.to_ascii_lowercase().as_str() {
            "https" => Ok(Protocol::Https),
            "ssh" => Ok(Protocol::Ssh),
            other => bail!("invalid protocol: '{}' (expected 'https' or 'ssh')", other),
        }
    }
}

/// Where a resolved setting came from, used for the Settings provenance display.
#[derive(Debug, Clone)]
pub enum Source {
    Argument,
    Option,
    EnvVar(&'static str),
    Local,
    Home,
    Default,
}

impl Source {
    pub fn label(&self) -> String {
        match self {
            Source::Argument => "argument".to_string(),
            Source::Option => "option".to_string(),
            Source::EnvVar(name) => format!("{} env", name),
            Source::Local => format!("<path>/{}", CONFIG_FILENAME),
            Source::Home => format!("~/{}", CONFIG_FILENAME),
            Source::Default => "default".to_string(),
        }
    }
}

type ConfigTable = BTreeMap<String, toml::Value>;

#[derive(Debug, Default)]
pub struct ConfigFiles {
    pub home: ConfigTable,
    pub local: ConfigTable,
}

/// Normalize a TOML table by replacing underscores with hyphens in keys so
/// `clone_wiki` and `clone-wiki` resolve to the same setting.
fn normalize(raw: toml::Table) -> ConfigTable {
    raw.into_iter()
        .map(|(k, v)| (k.replace('_', "-"), v))
        .collect()
}

fn read_toml(path: &Path) -> Result<ConfigTable> {
    if !path.exists() {
        return Ok(ConfigTable::new());
    }
    let text = fs::read_to_string(path)
        .with_context(|| format!("failed to read {}", path.display()))?;
    let parsed: toml::Table = toml::from_str(&text)
        .with_context(|| format!("failed to parse {}", path.display()))?;
    Ok(normalize(parsed))
}

/// Rename a legacy config/manifest file in-place. If the legacy file exists
/// and the new file does not, moves legacy -> new and prints an informational
/// message. If both exist, leaves the legacy file alone so the user can
/// resolve the conflict.
pub fn migrate_legacy_file(dir: &Path, legacy: &str, new: &str) -> Result<()> {
    let legacy_path = dir.join(legacy);
    let new_path = dir.join(new);
    if !legacy_path.exists() {
        return Ok(());
    }
    if new_path.exists() {
        println!(
            "Found legacy {} but {} already exists; leaving legacy file in place.",
            legacy_path.display(),
            new_path.display()
        );
        return Ok(());
    }
    fs::rename(&legacy_path, &new_path)
        .with_context(|| format!("failed to rename {} -> {}", legacy_path.display(), new_path.display()))?;
    println!("Renamed legacy {} -> {}", legacy_path.display(), new_path.display());
    Ok(())
}

/// Load the home config (`~/.ghx.toml`) and per-directory config
/// (`<local_dir>/.ghx.toml`). Migrates legacy `.gh-export.toml` files first.
pub fn load(local_dir: Option<&Path>) -> Result<ConfigFiles> {
    let mut files = ConfigFiles::default();

    if let Some(home_dir) = dirs::home_dir() {
        migrate_legacy_file(&home_dir, LEGACY_CONFIG_FILENAME, CONFIG_FILENAME)?;
        files.home = read_toml(&home_dir.join(CONFIG_FILENAME))?;
    }

    if let Some(local_dir) = local_dir {
        if local_dir.exists() {
            migrate_legacy_file(local_dir, LEGACY_CONFIG_FILENAME, CONFIG_FILENAME)?;
        }
        files.local = read_toml(&local_dir.join(CONFIG_FILENAME))?;
    }

    Ok(files)
}

/// Resolve a string-valued option by precedence: CLI > local > home > default.
pub fn resolve_string(
    cli: Option<&str>,
    cfg: &ConfigFiles,
    key: &str,
    default: Option<&str>,
) -> (Option<String>, Source) {
    if let Some(v) = cli {
        return (Some(v.to_string()), Source::Option);
    }
    if let Some(v) = cfg.local.get(key).and_then(|v| v.as_str()) {
        return (Some(v.to_string()), Source::Local);
    }
    if let Some(v) = cfg.home.get(key).and_then(|v| v.as_str()) {
        return (Some(v.to_string()), Source::Home);
    }
    (default.map(String::from), Source::Default)
}

/// Resolve a boolean flag by precedence. CLI flags can only set the value to
/// true (matching Click's `is_flag=True, default=None` semantics), so a
/// false CLI means "not provided".
pub fn resolve_bool_flag(
    cli_set: bool,
    cfg: &ConfigFiles,
    key: &str,
    default: bool,
) -> (bool, Source) {
    if cli_set {
        return (true, Source::Option);
    }
    if let Some(v) = cfg.local.get(key).and_then(|v| v.as_bool()) {
        return (v, Source::Local);
    }
    if let Some(v) = cfg.home.get(key).and_then(|v| v.as_bool()) {
        return (v, Source::Home);
    }
    (default, Source::Default)
}

/// Resolve an integer option by precedence.
pub fn resolve_int(
    cli: Option<i64>,
    cfg: &ConfigFiles,
    key: &str,
    default: Option<i64>,
) -> (Option<i64>, Source) {
    if let Some(v) = cli {
        return (Some(v), Source::Option);
    }
    if let Some(v) = cfg.local.get(key).and_then(|v| v.as_integer()) {
        return (Some(v), Source::Local);
    }
    if let Some(v) = cfg.home.get(key).and_then(|v| v.as_integer()) {
        return (Some(v), Source::Home);
    }
    (default, Source::Default)
}

/// Resolve the GitHub token with the order:
/// CLI flag > `GITHUB_TOKEN` env var > local config > home config.
pub fn resolve_token(cli: Option<&str>, cfg: &ConfigFiles) -> Result<(String, Source)> {
    if let Some(v) = cli {
        return Ok((v.to_string(), Source::Option));
    }
    if let Ok(v) = env::var("GITHUB_TOKEN") {
        if !v.is_empty() {
            return Ok((v, Source::EnvVar("GITHUB_TOKEN")));
        }
    }
    if let Some(v) = cfg.local.get("token").and_then(|v| v.as_str()) {
        return Ok((v.to_string(), Source::Local));
    }
    if let Some(v) = cfg.home.get("token").and_then(|v| v.as_str()) {
        return Ok((v.to_string(), Source::Home));
    }
    bail!(
        "No GitHub token provided. Supply one via:\n  \
         1. --token / -t flag\n  \
         2. GITHUB_TOKEN environment variable\n  \
         3. ~/{} config file (token = \"ghp_...\")",
        CONFIG_FILENAME
    )
}

/// Resolve the transport protocol. `--ssh` is a shortcut that always wins when
/// supplied, mirroring Click's `flag_value="ssh"` behavior.
pub fn resolve_protocol(
    cli: Option<Protocol>,
    ssh_flag: bool,
    cfg: &ConfigFiles,
) -> Result<(Protocol, Source)> {
    if ssh_flag {
        return Ok((Protocol::Ssh, Source::Option));
    }
    if let Some(p) = cli {
        return Ok((p, Source::Option));
    }
    if let Some(v) = cfg.local.get("protocol").and_then(|v| v.as_str()) {
        return Ok((Protocol::parse(v)?, Source::Local));
    }
    if let Some(v) = cfg.home.get("protocol").and_then(|v| v.as_str()) {
        return Ok((Protocol::parse(v)?, Source::Home));
    }
    Ok((Protocol::Https, Source::Default))
}
