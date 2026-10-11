# AGENTS.md

Orientation for coding agents working in ghx. Humans should read
[`CONTRIBUTING.md`](./CONTRIBUTING.md); the commands and rules there apply to you
too.

ghx is a Go CLI that clones and syncs every repository of a GitHub organization.

## Layout

| Path | Contents |
|------|----------|
| `cmd/ghx/` | `main`: calls `cli.Execute` and exits with its code |
| `internal/cli/` | The cobra command, flags, and the sync flow (`run.go`) |
| `internal/config/` | `.ghx.toml` loading, legacy migration, and flag > env > local > home resolution |
| `internal/ghapi/` | GitHub API: token validation, listing, repo filtering |
| `internal/cloner/` | Cloning, pulling, moving and deleting repos with `git` |
| `internal/manifest/` | The `.ghx.json` manifest (byte-compatible with the Python tool) |
| `internal/ui/` | Terminal output: settings table, progress, summary |
| `specs/` | Specs; [`specs/testing.md`](./specs/testing.md) records test layout and seams |
| `specs/handoffs/` | Plan and handoff scratch (see below) |
| `Makefile`, [`Makefile.md`](./Makefile.md) | The build, lint and test verbs and their reference |

## The gate

`make ci` (build, lint, test) is the gate. Run it before you commit a step and
before you hand work back; it must pass. `make help` lists every target. Don't
call `go build` or `golangci-lint` directly as a substitute: the Makefile holds
the exact commands CI runs.

## Tests and specs

- Tests are `_test.go` files beside the code they cover, as black-box packages
  (`package config_test`), table-driven, with testify. Fixtures live in the
  package's `testdata/`.
- No test may touch the network or the real home directory. Use `t.TempDir()`
  and `t.Setenv` (`HOME` and `USERPROFILE`, `GITHUB_TOKEN`); see
  [`specs/testing.md`](./specs/testing.md) for the seams.
- Never regenerate `internal/manifest/testdata/golden/.ghx.json` from Go output;
  it was written by the Python implementation and guards byte compatibility.
- Specs live in `specs/`. Update the doc in the same change as the code.

## Go standards

- Target the Go version in [`.go-version`](./.go-version); `go.mod` carries the
  minimum.
- `gofmt` formatting, imports in three blocks (standard library, third party,
  this module), and no underscores in identifiers.
- Every exported identifier has a doc comment that starts with its name; a
  package comment on one file per package.
- Return errors wrapped with context (`%w`) and inspect them with `errors.Is`
  and `errors.As`; don't log and return.
- Build paths with `filepath`, never a hard-coded `/`, and write nothing that
  works only on macOS: Windows and Linux are supported targets.
- Comments explain why, not what.

## Git

- Work on the checked-out task branch; don't create or switch branches unasked.
- Commit subjects are imperative, 72 characters or fewer, with no period. End the
  message with a blank line and `Assisted-by: Claude Code`; never add
  `Co-authored-by` or `Signed-off-by` for yourself.
- Never push, tag, release, open a pull request or change GitHub settings unless
  asked.

## Plan scratch

`specs/handoffs/` holds plan and handoff files from agent runs. They are scratch:
they are removed (or promoted to `specs/`) before the branch merges, so don't
link to them from permanent docs and don't edit them unless your task is the
plan itself. `.scratch/` is gitignored and is for throwaway notes.
