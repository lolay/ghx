# ghx

Export and sync all GitHub repositories for an organization.

This is the Go port of the original Python tool. During the comparison
period the binary is named `ghx-go` so it can coexist with the existing
Python `ghx` on your PATH. When you're happy with the Go version, rename
`cmd/ghx-go/` to `cmd/ghx/` and the binary will ship as `ghx`.

## Setup

Requires Go 1.22+ and Git.

### Build from source

```bash
go build -o ghx-go ./cmd/ghx-go
./ghx-go --help
```

### Install globally

```bash
go install github.com/garyrudolph/ghx/cmd/ghx-go@latest
ghx-go --help
```

By default this drops a `ghx-go` binary in `$GOBIN`, or if `GOBIN` is unset,
`$(go env GOPATH)/bin` (typically `~/go/bin`). Make sure that directory is on
your `PATH`:

```bash
export PATH="$(go env GOPATH)/bin:$PATH"   # add to ~/.zshrc or ~/.bashrc
```

#### Installing to ~/.local/bin instead

If you prefer the XDG-style `~/.local/bin` (alongside tools installed with
`pipx`, `uv tool install`, etc.), set `GOBIN` before running `go install`:

```bash
mkdir -p ~/.local/bin
GOBIN=$HOME/.local/bin go install github.com/garyrudolph/ghx/cmd/ghx-go@latest
```

To make that the default for every `go install`, export `GOBIN` in your shell
startup file:

```bash
# ~/.zshrc or ~/.bashrc
export GOBIN="$HOME/.local/bin"
export PATH="$HOME/.local/bin:$PATH"
```

After that, `go install github.com/garyrudolph/ghx/cmd/ghx-go@latest` will
drop the binary straight into `~/.local/bin/ghx-go`.

## Authentication

You need a **GitHub Personal Access Token (PAT)**. Generate one at
[github.com/settings/tokens](https://github.com/settings/tokens):

- **Classic token** scopes:
  - `repo` -- required for private repos (covers listing and cloning)
  - `public_repo` -- sufficient if you only need public repos
  - No other scopes are needed. If your org uses SAML SSO, authorize the token
    for the org after creation.
- **Fine-grained token:** scope it to the target organization with
  **"Metadata" read** and **"Contents" read** permissions on repositories. Note:
  the organization may need to allow fine-grained tokens in its settings.

The token is resolved in this order:

1. `--token` / `-t` CLI flag
2. `GITHUB_TOKEN` environment variable
3. Config files (see below)

## Configuration files

Settings are loaded from TOML config files. A global config in your home directory
provides defaults, and an optional per-directory config in the output directory can
override any value:

| File | Purpose |
|------|---------|
| `~/.ghx.toml` | Global defaults (token, protocol, identity, etc.) |
| `<path>/.ghx.toml` | Per-organization overrides (same keys, takes precedence) |

> Legacy `.gh-export.toml` and `.gh-export.json` files are automatically
> renamed to `.ghx.toml` / `.ghx.json` in place on first run, with an
> informational message.

All CLI options can be set as config keys (hyphenated to match CLI style):

```toml
token = "ghp_xxxxx"
protocol = "ssh"
type = "all"
include = "^web-"
exclude = "^test-"
clone-wiki = true
concurrency = 8
git-author = "Your Name"
git-email = "you@example.com"
delete = false
archived-dir = "ARCHIVED"
deleted-dir = "DELETED"
max-size = 500
```

For example, you might keep your token and default protocol in the global config:

```toml
# ~/.ghx.toml
token = "ghp_xxxxx"
protocol = "ssh"
```

And set a per-organization git identity in the output directory:

```toml
# ./acme/.ghx.toml
git-email = "gary@acme.org"
```

When syncing the `acme` directory, the effective config is the global file with the
directory-level values merged on top -- so the token and protocol come from
`~/.ghx.toml` while `git-email` comes from `./acme/.ghx.toml`.

Command-line options always override configuration file values. The full resolution
order (highest precedence first):

1. Command-line flags (e.g. `--protocol ssh`, `--git-email`)
2. `GITHUB_TOKEN` environment variable _(token only)_
3. `<path>/.ghx.toml`
4. `~/.ghx.toml`

Before any API calls, `ghx-go` prints the resolved settings (with an obfuscated
token) so you can verify what values are in effect.

## Usage

```bash
ghx-go <ORG> <PATH> [OPTIONS]
```

`ORG` is the GitHub organization name. `PATH` is the output directory (created
automatically if it doesn't exist).

The command is idempotent: run it once to do the initial clone, run it again to
pull the latest changes, clone any new repos, and handle removed or archived repos.

### Examples

```bash
# Clone all repos for an org into ./acme
ghx-go acme ./acme

# Dry-run to preview what would happen
ghx-go acme ./acme --dry-run

# Only private repos, exclude names matching a pattern
ghx-go acme ./acme --type private --exclude "^test-"

# Also clone wikis, use SSH transport
ghx-go acme ./acme --clone-wiki --ssh

# Permanently delete removed/archived repos instead of moving them
ghx-go acme ./acme --delete

# Custom directory names for moved repos
ghx-go acme ./acme --deleted-dir removed --archived-dir inactive
```

### How it works

- Queries the GitHub API for the current repo list.
- **Missing repos:** cloned automatically.
- **Existing repos:** `git pull` if the working tree is clean; skipped if there
  are local changes.
- **Removed repos:** moved to `DELETED/` (or permanently deleted with `--delete`).
- **Archived repos:** moved to `ARCHIVED/` (or permanently deleted with `--delete`).
- Writes a `.ghx.json` manifest to the output directory after each run to
  track which repos were synced (used for detecting removed/archived repos on
  subsequent runs).

## CLI Reference

```
ghx-go <ORG> <PATH> [OPTIONS]

Arguments:
  ORG                        GitHub organization name
  PATH                       Output directory

Options:
  --token, -t TEXT           GitHub PAT
  --type TEXT                Repo type: all|public|private|forks|sources
  --concurrency, -c INT      Parallel workers (default: 4)
  --dry-run                  Preview changes without doing anything
  --clone-wiki               Also clone associated wikis
  --include TEXT             Regex to include matching repo names
  --exclude TEXT             Regex to exclude matching repo names
  --protocol [https|ssh]     Git transport protocol (default: https)
  --ssh                      Shortcut for --protocol ssh
  --git-author TEXT          Set git user.name in each cloned repo
  --git-email TEXT           Set git user.email in each cloned repo
  --delete                   Permanently delete removed/archived repos
  --deleted-dir TEXT         Directory for removed repos (default: DELETED)
  --archived-dir TEXT        Directory for archived repos (default: ARCHIVED)
  --max-size INT             Skip repos larger than this size in MB
```
