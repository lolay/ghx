# Changelog

All notable changes to `ghx` are recorded here.
Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

---

## [Unreleased]

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
