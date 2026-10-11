# Changelog

All notable changes to `ghx` are recorded here.
Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

---

## [Unreleased]

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
  error when it's missing. A `git` found only in the current directory is not
  run.
- A repository whose `git status` fails is reported as failed instead of being
  pulled.
- On Windows, git commands run with `core.longpaths` on, and moves and deletes
  wait briefly for a file another program holds open.
- On macOS and Linux, a git that ghx stops (Ctrl-C, or the 5-minute timeout)
  is interrupted rather than killed, so it can remove a half-made clone.
- The progress bar is drawn only on a terminal, so piped output holds no cursor
  escapes.
