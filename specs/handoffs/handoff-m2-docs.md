# Handoff: m2 docs (plan-ghx-baseline-heron)

Milestone handoff from m1 to m2 of
[`plan-ghx-baseline-heron.md`](plan-ghx-baseline-heron.md), branch
`feature/ghx-baseline-heron`. Removed, or promoted, before the PR merges.

## Where we are

m1 is done and reviewed (wave 1 `3e99c74..92f4ee3`, wave 2
`cd6de0a..2797c2d`, both PASS):
- Module `github.com/lolay/ghx`, binary `ghx` from `cmd/ghx`; `ghx --help`
  prints `ghx <ORG> <PATH>`.
- `Makefile` (Develop and GitHub sections) with `help`, `init`, `doctor`,
  `build`, `lint`, `format`, `test`, `vuln`, `ci`, `pre-commit`, `clean`,
  `gh-runs-*`; `Makefile.md`; `triage.yaml`; `.golangci.yml`
  (standard + misspell + errorlint); `.go-version` 1.27.2.
- Unit tests for `config`, `ghapi.FilterRepos`, `manifest` (golden
  `.ghx.json`) and `ui`; `specs/testing.md` records the seams and the
  coverage exemptions.
- Nothing skipped.

## What m2 delivers

- m2.s1: README restructured on triage's, `CHANGELOG.md` (Keep a
  Changelog, `## [Unreleased]`).
- m2.s2: `CONTRIBUTING.md`, `SECURITY.md`, `.github/CODEOWNERS`,
  `AGENTS.md`.

## Decisions already made

- Go floor: `.go-version` is 1.27.2; `go.mod`'s `go` line stays 1.26.2.
- `make format` also runs `golangci-lint fmt` when installed.
- `make doctor` exits 1 with an install hint when `triage` is missing.
- Test seams: `HOME`/`USERPROFILE` and `GITHUB_TOKEN` via `t.Setenv`; no
  production seam.

## Gotchas

- The README still says "Requires Go 1.22+"; s1 should state the real
  floor (go.mod `go 1.26.2`, `.go-version` for development).
- Behaviour the tests pin, worth a line in docs only if it matters to
  users: `ResolveInt` truncates TOML floats; a wrong-typed config value
  falls through to the next source.
- `specs/handoffs/` holds plan scratch removed before merge; `AGENTS.md`
  says so.

## Files to reference

- `~/Projects/lolay/triage/triage/` README, CHANGELOG, CONTRIBUTING,
  SECURITY, `.github/CODEOWNERS` (read only).
- `Makefile.md`, `specs/testing.md`, `internal/config/` (config paths and
  precedence), `internal/cli/` (the cobra command).
