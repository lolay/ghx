# ghx

[![CI](https://github.com/lolay/ghx/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/lolay/ghx/actions/workflows/ci.yml)

> **tldr** -- Clone every repository in a GitHub organization, then keep the
> copies in sync: one command, safe to re-run.

## What is ghx?

`ghx` lists the repositories of a GitHub organization through the API, clones
the ones you don't have, fast-forwards the ones you do, and moves repositories
that were deleted or archived upstream out of the way. A `.ghx.json` manifest in
the output directory records each run so the next one can tell what changed.

- **Idempotent.** The first run clones; every later run updates.
- **Safe with local work.** Every repository is fetched, so each remote branch
  is backed up, but the working tree is only fast-forwarded when that loses
  nothing. A repository with uncommitted changes is fetched, not updated, and
  the summary lists it as fetched only.
- **Filterable.** Select by type (`public`, `private`, `forks`, `sources`), by
  name regex, and by size.
- **Previewable.** `--dry-run` shows what would happen and changes nothing.
- **Configurable once.** Keep your token and defaults in `~/.ghx.toml`, and
  override them per organization.

Its companion, `ghb`, reports every branch of every repository in the
organization, read-only, from the API alone: which ones merged and were never
deleted, which are stale, and which repos don't default to `main`. See
[Branches report](#branches-report-ghb).

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

Requires [Git](https://git-scm.com/) 2.31 or newer, and Go 1.26.2 or newer
(the `go` line in [`go.mod`](./go.mod)) to build it. Development uses the version pinned in
[`.go-version`](./.go-version).

### Homebrew (macOS, Linux)

```sh
brew trust lolay/tap
brew install lolay/tap/ghx
```

### Scoop (Windows)

```powershell
scoop bucket add lolay https://github.com/lolay/scoop-bucket
scoop install lolay/ghx
```

### GitHub Releases

Download the archive for your system (macOS, Linux or Windows, each on amd64 or
arm64) and `checksums.txt` from the
[Releases page](https://github.com/lolay/ghx/releases), check the archive's
sha256 against `checksums.txt`, and put `ghx` on your `PATH`.

Releases are cut as [`specs/releasing.md`](./specs/releasing.md) describes.

### From source

With the repository's Makefile:

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

`@latest` resolves to the newest release tag. A `go install` build isn't
stamped, so `ghx --version` prints `ghx dev`.

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

## Authentication

You need a **GitHub personal access token (PAT)**. Generate one at
[github.com/settings/tokens](https://github.com/settings/tokens):

- **Classic token** scopes:
  - `repo` -- required for private repos (covers listing and cloning)
  - `public_repo` -- sufficient if you only need public repos
  - No other scopes are needed. If your org uses SAML SSO, authorize the token
    for the org after creation.
- **Fine-grained token:** scope it to the target organization with
  **"Metadata" read** and **"Contents" read** permissions on repositories, and
  **"Pull requests" read** for `ghb`. The organization may need to allow
  fine-grained tokens in its settings.

The token is resolved in this order, highest precedence first:

1. `--token` / `-t` flag
2. `GITHUB_TOKEN` environment variable
3. `token` in `<PATH>/.ghx.toml`
4. `token` in the global config (see below)

If none is set, `ghx` exits with an error that lists these options. Avoid
`--token` on a shared machine: the value lands in your shell history and the
process list. The environment variable or the config file is safer. `ghx` hands
the token to `git` through `git`'s environment, never in a clone URL, so no
clone's `.git/config` holds it. See
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

Every command-line option except `--ssh` can be set as a config key, hyphenated
like the flag; `protocol` covers `--ssh`. Underscores in keys are accepted and
read as hyphens:

```toml
token = "ghp_xxxxx"
protocol = "ssh"
type = "all"
include = "^web-"
exclude = "^test-"
clone-wiki = true
concurrency = 8
dry-run = false
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

`ghb` reads only the global file (it has no `PATH`) and adds three keys:
`stale-days` (default `90`), `expected-default` (default `main`) and
`include-archived` (default off). `ghx` ignores them, and `ghb` ignores the
sync-only keys.

A config value of the wrong type (for example `concurrency = "8"`), or a float
given for an integer key (`concurrency = 8.0` included), stops the run with an
error that names the file and the key. A key `ghx` does not know (a typo such as
`concurency`, or `ssh`) gets a warning and is ignored.

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

## Branches report (ghb)

```sh
ghb <ORG> [OPTIONS]
```

`ghb` lists the org's repos, reads every branch of each over the GraphQL API
(no clones, no output directory), and classifies each branch but the default
one, first match wins:

| Status | Meaning |
|---|---|
| `merged` | Its PR merged, it has no open PR, and nothing was committed after the merge: left behind. Catches squash and rebase merges |
| `stale` | No open PR and no commit for `--stale-days` (default 90) |
| `open-pr` | It has an open PR |
| `active` | Anything else |

It prints the live repo table (each repo's default branch, flagged when it isn't
`main`, and its counts), then every branch with its status, last commit date,
age, author and protection, then the org totals. Archived repos are skipped
unless you pass `--include-archived`. It never writes to GitHub and exits 0
whatever it finds. On a rate limit it waits, with a countdown under the table,
and carries on when the limit lifts; Ctrl-C stops it.

```sh
# Every branch of every active repo in acme
ghb acme

# Stale after 30 days; repos should default to trunk
ghb acme --stale-days 30 --expected-default trunk

# Only private repos, as JSON
ghb acme --type private --format json | jq '.totals'
```

The rules, edge cases (a branch reused after its PR merged, PRs from forks),
API cost and limits are in
[`specs/branches-report.md`](./specs/branches-report.md).

## How it works

- Queries the GitHub API for the organization's current repository list.
- **Missing repos:** cloned.
- **Existing repos:** fetched every run, so every remote branch is backed up
  whatever state the working tree is in. The working tree fast-forwards when it
  is on the default branch with no changes to tracked files. Otherwise the
  summary lists the repo under "Fetched only" with the reason (local changes,
  naming the first few changed files, commits not on origin, another branch checked out, a detached HEAD, a rebase
  or merge in progress). Details:
  - Untracked files don't block an update; git itself refuses only when an
    incoming file would overwrite one.
  - A force-pushed default branch is followed when the local branch has no
    unpushed work: it is reset to the new tip, never overwriting files, and the
    old tip is kept under `refs/ghx/forced/<branch>/<timestamp>`. That holds
    even when an earlier fetch (a failed pull, or `git fetch` by hand) already
    brought in the rewrite: ghx checks the history of `origin/<branch>` that
    git keeps in its reflog. Commits only the force-push dropped aren't counted
    as local work.
  - If the default branch was renamed on GitHub, ghx switches to the new name
    when nothing would be lost.
  - A repo whose HEAD names a branch that doesn't exist is checked out on
    `main`, else `master`, else its newest branch, and the summary notes it.
  - An empty repo has nothing to back up: it is skipped (`∅`, listed under
    "Skipped (empty)") and cloned on the first run after it gains a commit.
  - A big clone isn't capped at 5 minutes: a transfer is aborted when it
    stalls (below 1 KB/s for 60 seconds), and any clone or fetch is capped at
    60 minutes. A transient network error (a dropped connection, a timeout, a
    rate limit) is retried once.
  - With `--clone-wiki`, a wiki is synced the same way. A wiki that doesn't
    exist is reported as not found; any other wiki failure is `failed wiki`
    with git's message.
- **Removed repos** (in the previous manifest, gone from the organization): moved
  to `DELETED/`, or permanently deleted with `--delete`; a wiki goes with its
  repo. An earlier clone already in `DELETED/` is never replaced: a second one
  of the same name gets a UTC timestamp suffix (`DELETED/x.20261014T013134Z`).
- **Archived repos:** cloned into `ARCHIVED/`. Every run checks where each
  synced repo's clone is: an archived repo's clone found in `PATH` is moved
  into `ARCHIVED/`, and an unarchived repo's clone found in `ARCHIVED/` is moved
  back, then synced there; a wiki moves with its repo. That holds with no
  manifest, and a move that failed is tried again next run. With `--delete`,
  archived repos, and their wikis, are deleted or skipped rather than moved. A repo left out by
  `--include`, `--exclude` or `--type` stays where it is. Where every repo lives,
  and the rules above in full, are in [`specs/repo-layout.md`](./specs/repo-layout.md).
- **Moves and deletes are checked.** ghx moves or deletes a clone only when its
  origin URL names the repo (the last part of the URL, so SSH, HTTPS, an
  Enterprise host or a renamed organization all match); otherwise it is left
  alone and reported as `skipped (origin mismatch)` with the URL it found. A
  clone is never moved onto a directory that is already there: both are left in
  place and reported as `conflict`.
- Writes a `.ghx.json` manifest to the output directory after each run, used to
  detect removed repos next time.

Each repo gets a mark beside its status: `✓` cloned, `↓` pulled (new commits
came in), `–` up to date (nothing new), `⇣` fetched only (its remote branches are
backed up, but the working tree is only partly downloaded: it wasn't updated),
`⚠` skipped or a conflict, `∅` empty, `✗` failed. The summary lists what needs a
look (failures, repos fetched only, empty repos, clones left in place for their
origin or a conflict), and a "Notes" section lists repos that were updated
with something to say (a branch fallback, a reset after a force-push, a switch
after a rename). The words always come with the mark, so piped output reads.

**Not backed up:** Git LFS content and submodules' contents.

## Platforms

ghx runs on macOS, Linux and Windows. It needs `git` 2.31 or newer on your
`PATH` (Git for Windows on Windows); a sync without it, or with an older one,
stops before contacting GitHub, while `--dry-run` works without it.

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
- **Output.** On a terminal a sync draws one live table: a row per repo in fixed
  columns (Visibility reads `public`, `private` or `archived`), active repos by
  name and then archived ones, finished rows scrolling up into your terminal's
  history in that order, the repos still running (and any finished behind a
  slower one) and a progress line (`Syncing 34/60 ... 4 running · 22 queued`) at
  the bottom, and the totals when it closes. Piped or redirected output gets
  the plain repo table, a note as each pass starts, and the full summary
  instead, with no escape codes. A `--dry-run` gets only the plain repo table
  and `Dry run complete. No changes made.`, on a terminal too. Set `NO_COLOR=1`
  to turn colors off.

## CLI reference

This is the output of `ghx --help` with colors off (on a terminal the help is
styled):

```text
  Export and sync all GitHub repos for an organization

  USAGE

    ghx <ORG> <PATH> [command] [--flags]

  EXAMPLES

    # Preview what would be cloned, without touching the disk
    ghx --dry-run acme ~/src/acme

    # Clone over SSH instead of HTTPS
    ghx --ssh acme ~/src/acme

    # Only repos whose names match a pattern
    ghx --include '^(api|web)-' acme ~/src/acme

  COMMANDS

    completion [command]  Generate the autocompletion script for the specified shell
    help [command]        Help about any command

  FLAGS

    --archived-dir        Directory for archived repos.
    --clone-wiki          Also clone associated wikis.
    -c --concurrency      Parallel workers.
    --delete              Permanently delete removed/archived repos.
    --deleted-dir         Directory for removed repos.
    --dry-run             Preview changes without doing anything.
    --exclude             Regex to exclude matching repo names.
    --git-author          Set git user.name in each cloned repo.
    --git-email           Set git user.email in each cloned repo.
    -h --help             Help for ghx
    --include             Regex to include only matching repo names.
    --max-size            Skip repos larger than this size in MB.
    --protocol            Git transport protocol (https|ssh).
    --ssh                 Shortcut for --protocol ssh.
    -t --token            Github Personal Access Token.
    --type                Repo type: all|public|private|forks|sources.
    -v --version          Version for ghx
```

This is `ghb --help`:

```text
  Report every branch of every repo in a GitHub organization, read-only, from
  the GitHub API: no clones, no output directory. Each branch but the default one
  is merged (its PR merged and it was never deleted), stale (no commit for
  --stale-days and no open PR), open-pr, or active.

  Settings resolve flag, then environment (GITHUB_TOKEN), then ~/.ghx.toml.

  USAGE

    ghb <ORG> [command] [--flags]

  EXAMPLES

    # Every branch of every active repo in acme
    ghb acme

    # Stale after 30 days, and repos should default to trunk
    ghb --stale-days 30 --expected-default trunk acme

    # The report as JSON, for jq
    ghb --format json acme | jq '.totals'

  COMMANDS

    completion [command]  Generate the autocompletion script for the specified shell
    help [command]        Help about any command

  FLAGS

    -c --concurrency      Repos read in parallel.
    --exclude             Regex to exclude matching repo names.
    --expected-default    Default branch name repos should use (default main).
    --format              Output format: text|json. (text)
    -h --help             Help for ghb
    --include             Regex to include only matching repo names.
    --include-archived    Also report archived repos.
    --stale-days          Days without a commit before a branch is stale (default 90).
    -t --token            Github Personal Access Token.
    --type                Repo type: all|public|private|forks|sources.
    -v --version          Version for ghb
```

Besides syncing, `ghx completion <shell>` prints a shell completion script, and
`ghx man` prints a man page (`ghb` has both too). The Homebrew formula installs
both man pages, so `man ghx` and `man ghb` work; release archives carry them
under `manpages/`. A repository owner named `help`, `completion` or
`man` is read as that command; put `--` before it (`ghx -- help <PATH>`) to sync
it.

`ghx` and `ghb` exit 0 on success and 1 on any error.

`ghx --version` prints `ghx <release>+<sha> (<build code>)`, for example
`ghx 0.1.0+abc1234 (42)`: the last release in [`version.txt`](./version.txt),
the commit the binary was built from (`.dirty` when the tree had uncommitted
changes) and the number of commits up to it. `make build` and release builds
stamp it; a plain `go build`, `go run` or `go install` doesn't, and prints
`ghx dev`.

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
