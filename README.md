# ghx

> **tldr** -- Clone every repository in a GitHub organization, then keep the
> copies in sync: one command, safe to re-run.

## What is ghx?

`ghx` lists the repositories of a GitHub organization through the API, clones
the ones you don't have, fast-forwards the ones you do, and moves repositories
that were deleted or archived upstream out of the way. A `.ghx.json` manifest in
the output directory records each run so the next one can tell what changed.

- **Idempotent.** The first run clones; every later run updates.
- **Safe with local work.** A repository with uncommitted changes is skipped, not
  pulled.
- **Filterable.** Select by type (`public`, `private`, `forks`, `sources`), by
  name regex, and by size.
- **Previewable.** `--dry-run` shows what would happen and changes nothing.
- **Configurable once.** Keep your token and defaults in `~/.ghx.toml`, and
  override them per organization.

This is the Go port of the original Python tool; the Python version lives on the
`old/python` branch.

## Quick start

```sh
# Needs a GitHub personal access token (see Authentication). Replace the
# placeholder value and the organization name `acme` with your own.
export GITHUB_TOKEN=ghp_xxxxx

# Preview what would happen
ghx acme ./acme --dry-run

# Clone every repo in the org into ./acme, then re-run any time to sync
ghx acme ./acme
```

## Install

Requires [Git](https://git-scm.com/) and Go 1.26.2 or newer (the `go` line in
[`go.mod`](./go.mod)). Development uses the version pinned in
[`.go-version`](./.go-version).

From source, with the repository's Makefile:

```sh
git clone https://github.com/lolay/ghx.git
cd ghx
make build
./bin/ghx --help
```

Or with `go install`:

```sh
go install github.com/lolay/ghx/cmd/ghx@latest
ghx --help
```

Until the first release, `@latest` resolves to the newest commit on `main`, so
this works once the module rename (see [`CHANGELOG.md`](./CHANGELOG.md)) has
merged there.

`go install` puts the `ghx` binary in `$GOBIN`, or `$(go env GOPATH)/bin`
(typically `~/go/bin`) when `GOBIN` is unset. Make sure that directory is on your
`PATH`:

```sh
export PATH="$(go env GOPATH)/bin:$PATH"   # add to ~/.zshrc or ~/.bashrc
```

To install somewhere else, such as `~/.local/bin`, set `GOBIN` for the install:

```sh
mkdir -p ~/.local/bin
GOBIN=$HOME/.local/bin go install github.com/lolay/ghx/cmd/ghx@latest
```

Homebrew and Scoop packages come with the first release.

## Authentication

You need a **GitHub personal access token (PAT)**. Generate one at
[github.com/settings/tokens](https://github.com/settings/tokens):

- **Classic token** scopes:
  - `repo` -- required for private repos (covers listing and cloning)
  - `public_repo` -- sufficient if you only need public repos
  - No other scopes are needed. If your org uses SAML SSO, authorize the token
    for the org after creation.
- **Fine-grained token:** scope it to the target organization with
  **"Metadata" read** and **"Contents" read** permissions on repositories. The
  organization may need to allow fine-grained tokens in its settings.

The token is resolved in this order, highest precedence first:

1. `--token` / `-t` flag
2. `GITHUB_TOKEN` environment variable
3. `token` in `<PATH>/.ghx.toml`
4. `token` in the global config (see below)

If none is set, `ghx` exits with an error that lists these options. Avoid
`--token` on a shared machine: the value lands in your shell history and the
process list. The environment variable or the config file is safer. See
[`SECURITY.md`](./SECURITY.md) for how `ghx` handles the token.

## Configuration

Settings load from TOML config files. A global file in your home directory
provides defaults, and an optional file in the output directory overrides any
value for that one organization:

| File | macOS and Linux | Windows | Purpose |
|------|-----------------|---------|---------|
| Global | `~/.ghx.toml` | `%USERPROFILE%\.ghx.toml` | Defaults for every run (token, protocol, identity) |
| Per organization | `<PATH>/.ghx.toml` | `<PATH>\.ghx.toml` | Same keys; takes precedence over the global file |

> Legacy `.gh-export.toml` and `.gh-export.json` files are renamed to
> `.ghx.toml` and `.ghx.json` in place on the first run, with an informational
> message.

Every command-line option can be set as a config key, hyphenated like the flag.
Underscores in keys are accepted and read as hyphens:

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

For example, keep your token and default protocol in the global config:

```toml
# ~/.ghx.toml
token = "ghp_xxxxx"
protocol = "ssh"
```

and set a git identity for one organization in its output directory:

```toml
# ./acme/.ghx.toml
git-email = "gary@acme.org"
```

When syncing `./acme`, the token and protocol come from `~/.ghx.toml` and
`git-email` comes from `./acme/.ghx.toml`.

Command-line flags always win. The full order, highest precedence first:

1. Command-line flags (`--protocol ssh`, `--git-email`, ...)
2. `GITHUB_TOKEN` environment variable (token only)
3. `<PATH>/.ghx.toml`
4. `~/.ghx.toml` (`%USERPROFILE%\.ghx.toml` on Windows)
5. The built-in default

A config value of the wrong type (for example `concurrency = "8"`) is ignored and
the next source is used. A TOML float given for an integer key is truncated.

| Option | Default |
|--------|---------|
| `type` | `all` |
| `concurrency` | `4` |
| `protocol` | `https` |
| `deleted-dir` | `DELETED` |
| `archived-dir` | `ARCHIVED` |
| `dry-run`, `clone-wiki`, `delete` | off |
| `include`, `exclude`, `max-size`, `git-author`, `git-email` | unset |

Before any API call, `ghx` prints the resolved settings, with an obfuscated
token and where each value came from, so you can check what is in effect.

## Usage

```sh
ghx <ORG> <PATH> [OPTIONS]
```

`ORG` is the GitHub organization name. `PATH` is the output directory, created
if it doesn't exist. Run once to clone; run again to pull the latest changes,
clone new repos, and handle removed or archived ones.

```sh
# Clone all repos for an org into ./acme
ghx acme ./acme

# Dry run: preview what would happen
ghx acme ./acme --dry-run

# Only private repos, excluding names that match a pattern
ghx acme ./acme --type private --exclude "^test-"

# Also clone wikis, over SSH
ghx acme ./acme --clone-wiki --ssh

# Permanently delete removed and archived repos instead of moving them
ghx acme ./acme --delete

# Custom directory names for moved repos
ghx acme ./acme --deleted-dir removed --archived-dir inactive
```

The organization `acme` in these examples is a placeholder; each command needs a
token and network access.

## How it works

- Queries the GitHub API for the organization's current repository list.
- **Missing repos:** cloned.
- **Existing repos:** `git pull --ff-only` when the working tree is clean;
  skipped when there are local changes.
- **Removed repos** (in the previous manifest, gone from the organization): moved
  to `DELETED/`, or permanently deleted with `--delete`.
- **Archived repos:** cloned into `ARCHIVED/`. A repo that becomes archived is
  moved there on the next run. With `--delete`, archived repos are deleted or
  skipped rather than moved.
- Writes a `.ghx.json` manifest to the output directory after each run, used to
  detect removed and newly archived repos next time.

## Platforms

ghx runs on macOS, Linux and Windows. It needs `git` on your `PATH` (Git for
Windows on Windows); a sync without it stops before contacting GitHub, while
`--dry-run` works without it.

- **Repository names.** A name that isn't a single directory name (such as
  `..`, which only an edited `.ghx.json` could hold) is skipped on every system
  and reported as `skipped (unusable name)`. On Windows, names Windows can't
  hold as a directory are skipped the same way: device names such as `CON`,
  `AUX`, `NUL` or `COM1` (with or without an extension), and names ending in a
  dot or a space. The summary lists each one with the reason.
- **`--deleted-dir` and `--archived-dir`** must be directories inside `PATH`,
  such as `DELETED` or `old/removed`: not `.`, `..` or an absolute path.
- **Long paths on Windows.** ghx turns on `core.longpaths` for its own git
  commands and in each clone it makes, so git can check out paths longer than
  260 characters. Other Windows tools may still fail on them, so keep `PATH`
  short (for example `C:\src\acme`).
- **Files held open on Windows.** Moving or deleting a repository waits briefly
  for a virus scanner or indexer to let go of its files. A terminal or editor
  open inside the repository blocks the move; ghx reports it as failed and
  tries again on the next run.
- **Output.** The progress bar is drawn only on a terminal; piped or redirected
  output gets the summary without it. Set `NO_COLOR=1` to turn colors off.

## CLI reference

This is the output of `ghx --help`:

```
Export and sync all GitHub repos for an organization

Usage:
  ghx <ORG> <PATH> [flags]

Flags:
      --archived-dir string   Directory for archived repos.
      --clone-wiki            Also clone associated wikis.
  -c, --concurrency int       Parallel workers.
      --delete                Permanently delete removed/archived repos.
      --deleted-dir string    Directory for removed repos.
      --dry-run               Preview changes without doing anything.
      --exclude string        Regex to exclude matching repo names.
      --git-author string     Set git user.name in each cloned repo.
      --git-email string      Set git user.email in each cloned repo.
  -h, --help                  help for ghx
      --include string        Regex to include only matching repo names.
      --max-size int          Skip repos larger than this size in MB.
      --protocol string       Git transport protocol (https|ssh).
      --ssh                   Shortcut for --protocol ssh.
  -t, --token string          GitHub Personal Access Token.
      --type string           Repo type: all|public|private|forks|sources.
```

`ghx` exits 0 on success and 1 on any error.

## Development

The [`Makefile`](./Makefile) is the single source of truth for building, linting
and testing; humans, agents and CI run the same targets. See
[`Makefile.md`](./Makefile.md) for the reference.

```sh
make help      # list the targets
make init      # download Go modules
make doctor    # check your environment with triage (needs triage installed)
make ci        # build, lint and test: the gate to pass before you push
```

Test layout and conventions are in [`specs/testing.md`](./specs/testing.md).
Setup, commit style and the pull request flow are in
[`CONTRIBUTING.md`](./CONTRIBUTING.md).

## Contributing

Bug reports, feature requests and pull requests are welcome. Start with
[`CONTRIBUTING.md`](./CONTRIBUTING.md). Report security issues privately, as
[`SECURITY.md`](./SECURITY.md) describes. Notable changes are listed in
[`CHANGELOG.md`](./CHANGELOG.md).

## License

Apache 2.0 -- see [`LICENSE`](./LICENSE).
