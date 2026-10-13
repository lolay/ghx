# Changelog

All notable changes to `ghx` are recorded here.
Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

---

## [Unreleased]

### Changed
- `ghx --help` and command errors are styled with fang: a titled usage block,
  examples, and an error message with a pointer to `--help`. Piped output, and
  any output under `NO_COLOR`, has no escape codes. The exit code is still 0 on
  success and 1 on any error.
- Clone and pull get a 60-minute limit (other git commands keep 5 minutes) and
  give up on a connection that stays under 1000 bytes a second for 60 seconds,
  so a stalled transfer fails in a minute instead of holding a worker.
- A clone or pull that fails on a dropped connection, a DNS or timeout error,
  a rate limit or a server error is tried once more after 2 seconds.
  "Repository not found" and authentication failures are never retried.
- A failed repo's detail is git's last `fatal:` or `error:` line, not its
  progress output and `hint:` blocks.
- Every existing clone is fetched on every run, so every remote branch is
  backed up whatever state the working tree is in. `fetched only`, with the
  reason, replaces `skipped (local changes)` for a clone whose working tree
  couldn't be updated. Wikis are synced the same way.
- The summary marks each kind of result with a glyph beside its words: `✗`
  failed, `⚠` fetched only, `∅` skipped (empty). A "Skipped (empty)" section
  lists empty repos, and a "Notes" section lists clones and pulls that carry a
  detail (a branch fallback, a reset after a force-push, a rename switch).

### Added
- `ghx completion <shell>` prints a shell completion script, and `ghx man` a man
  page. An organization named `help`, `completion` or `man` needs `--` before it
  (`ghx -- help <PATH>`).

### Fixed
- A repo whose HEAD names a branch that doesn't exist, which GitHub reports for
  an empty repo or one whose pushes never made `main`, is cloned and checked
  out on `main`, else `master`, else its newest branch, instead of failing.
- An empty repo is skipped (`skipped (empty)`) and its empty clone removed,
  instead of failing on every run.
- A force-pushed default branch is followed when the clone has no unpushed
  work, with the old tip kept under `refs/ghx/forced/<branch>/<timestamp>`,
  instead of being skipped on every run.
- A default branch renamed on GitHub is switched to when nothing would be lost.
- A clone on a feature branch or a detached HEAD is fetched, and reported as
  fetched only with the reason, instead of failing its pull.
- A big clone is no longer killed at 5 minutes.
- A wiki that fails to clone for a reason other than "not found" is reported as
  `failed wiki` with git's message, not as not found.

## [v0.2.0] - 2026-10-13

### Security
- Hardening: ghx no longer puts the token in HTTPS clone URLs. It gives `git`
  the token through `git`'s environment for the length of each clone or pull
  (an `http.extraheader` scoped to the clone URL's host), so the token is in no
  command line, no clone URL and no `.git/config`, and never reaches a
  credential helper. ghx never writes credentials to disk.
- A sync cleans the origin URLs of clones made by older versions, which held the
  token: in each repository it pulls, and in every clone directly under the
  output directory, `DELETED/` and `ARCHIVED/`. Rotate a token that older
  versions used, since copies of those clones may still hold it.
- The token and its header value are masked in `git` failure messages and
  GitHub API errors.

### Added
- `ghx --version` (and `-v`) prints the build version as
  `ghx <release>+<sha> (<build code>)`, for example `ghx 0.1.0+abc1234 (42)`,
  with `.dirty` after the sha for a build from a tree with uncommitted changes.
  `make build` stamps it; a plain `go build` or `go install` prints `ghx dev`.
- Release pipeline: the Release workflow (`release.yml`) bumps `version.txt`
  and the changelog, commits and tags `vX.Y.Z`; the tag starts the Publish
  workflow (`publish.yml`), which builds six archives and `checksums.txt` with
  goreleaser, publishes the GitHub release, then updates `lolay/homebrew-tap`
  and `lolay/scoop-bucket`, in that order. `make snapshot` builds the same
  archives locally without publishing, `make release-check` validates
  `.goreleaser.yaml` (also a CI job), and the Danger targets
  `publish-formula`, `publish-scoop` and `release` refuse to run without a
  `CONFIRM_*` variable. The procedure, secrets and first-release checklist are
  in [`specs/releasing.md`](./specs/releasing.md).
- Install with `brew install lolay/tap/ghx` or `scoop install lolay/ghx`, or
  download an archive from GitHub Releases, once the first release is out.
- `version.txt` holds the last released version (`0.1.0` until the first
  release), and `make bump LEVEL=patch|minor|major` runs
  `scripts/bump_version.py` to bump it and move these entries under the new
  version's heading.
- `Makefile` with the standard targets (`make help` lists them; `make ci` is
  the pre-push gate), documented in [`Makefile.md`](./Makefile.md).
- Unit tests for configuration loading and precedence, repository filtering,
  the `.ghx.json` manifest (checked byte for byte against a file written by the
  Python implementation) and terminal output; see
  [`specs/testing.md`](./specs/testing.md).
- `CONTRIBUTING.md`, `SECURITY.md`, `AGENTS.md` and `.github/CODEOWNERS`.
- `.go-version` pins the Go version used for development.

### Changed
- The binary is now named `ghx` (it was `ghx-go`), built from `cmd/ghx`. Update
  any script or alias that calls `ghx-go`.
- The Go module path is now `github.com/lolay/ghx` (it was
  `github.com/garyrudolph/ghx`), so
  `go install github.com/lolay/ghx/cmd/ghx@latest` works.
- The README is restructured, with install, authentication and configuration
  (config file paths on macOS, Linux and Windows) and a CLI reference that
  matches `ghx --help`. It states the real Go requirement: 1.26.2 or newer.

### Fixed
- `gofmt` drift in `internal/config/resolve.go`.
- Repository names that can't be a directory are skipped and reported as
  `skipped (unusable name)` instead of failing mid-sync: on every system a name
  that isn't a single directory name (a `..` in an edited `.ghx.json` could
  otherwise move or delete a directory outside the output directory), and on
  Windows device names such as `CON` or `NUL` and names ending in a dot or space.
- `--deleted-dir` and `--archived-dir` must now be directories inside the output
  directory; `.` would have deleted each repo it moved.
- `git` is looked up once, before anything is asked of GitHub, with a clear
  error when it's missing or older than 2.31, the minimum ghx now needs. A
  `git` found only in the current directory is not run.
- A repository whose `git status` fails is reported as failed instead of being
  pulled.
- On Windows, git commands run with `core.longpaths` on, and moves and deletes
  wait briefly for a file another program holds open.
- On macOS and Linux, a git that ghx stops (Ctrl-C, or the 5-minute timeout)
  is interrupted rather than killed, so it can remove a half-made clone.
- The progress bar is drawn only on a terminal, so piped output holds no cursor
  escapes.
