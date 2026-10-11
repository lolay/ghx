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
