# Contributing to ghx

Thanks for your interest in improving ghx. This guide covers setup, the
build and test loop, and the workflow we expect for pull requests.

## Ground rules

- Open an issue before a large change so we can agree on the approach.
- Security issues do **not** go in public issues; see [`SECURITY.md`](./SECURITY.md).
- By contributing, you agree that your contributions are licensed under the
  project's [Apache 2.0 license](./LICENSE).

## Development setup

ghx is a single Go binary. The [`Makefile`](./Makefile) is the source of truth:
humans, agents and CI run the same `make <target>` verbs, and `make help` lists
them. [`Makefile.md`](./Makefile.md) is the reference.

```sh
git clone https://github.com/lolay/ghx.git
cd ghx
make init       # download Go modules (INSTALL_PACKAGES=1 also installs golangci-lint)
make build      # compile bin/ghx
make test       # go test -race -cover ./...
make ci         # build, lint and test: the full pre-push gate
make doctor     # check your environment with triage (needs triage installed)
```

You need Git and the Go version pinned in [`.go-version`](./.go-version). The
`go` line in [`go.mod`](./go.mod) is the minimum that builds ghx; develop on the
pinned version. `make lint` requires `golangci-lint`; `make init
INSTALL_PACKAGES=1` installs the pinned release. `make doctor` runs
[`triage`](https://github.com/lolay/triage) against [`triage.yaml`](./triage.yaml)
and tells you what is missing and how to install it.

## Continuous integration

[`ci.yml`](./.github/workflows/ci.yml) runs on every push and pull request, on
macOS, Linux and Windows. Linux and macOS run `make ci`; Linux also runs
`make vuln`. Windows runs `go build`, `go vet` and `go test` without `-race`,
because the race detector needs cgo there, so a race is caught on Linux and
macOS only. The job-to-target map is in the "CI map" of
[`Makefile.md`](./Makefile.md).

## The loop

1. Make a change.
2. `make format` to apply `gofmt` and tidy `go.mod`.
3. `make ci` before you push. It must pass.

## Making changes

- **Match the surrounding code.** Follow the existing naming and structure, keep
  functions small, and wrap errors with context (`fmt.Errorf("...: %w", err)`).
- **New behavior needs tests.** Tests sit beside the code as `_test.go` files,
  table-driven, and assert with [testify](https://github.com/stretchr/testify):
  `require` for preconditions, `assert` for the checks that follow. The layout,
  seams and coverage notes are in [`specs/testing.md`](./specs/testing.md).
- **No network or real home directory in tests.** Use `t.TempDir()` and
  `t.Setenv` as the existing tests do.
- **Keep it cross-platform.** Build paths with `filepath`, never a hard-coded
  `/`, and avoid anything that works only on macOS.
- **Update the docs in the same pull request.** A user-visible change goes in the
  `## [Unreleased]` section of [`CHANGELOG.md`](./CHANGELOG.md), and a change to
  a flag or config key updates the README and its CLI reference.

## Commits

- Imperative mood, 72 characters or fewer in the subject, no trailing period
  (`add max-size filter`).
- A blank line, then a body that explains what and why, when it helps.
- Trailers go in the last paragraph. If an AI tool helped, add
  `Assisted-by: Claude Code` (name the tool). Don't add `Co-authored-by` for a
  tool.
- One logical change per commit.

## Pull requests

- Branch off `main` with a short-lived `feature/<name>` or `fix/<name>` branch,
  and rebase on the latest `main` before you open the pull request.
- One feature or fix per pull request.
- Run `make ci` first, and describe the change and how you tested it.
- Pull requests are **squash-merged**, with the title and description as the
  commit message on `main`. Write the title like a commit subject.
- A maintainer must approve before merge; green checks alone are not enough.
- Agent contributors: see [`AGENTS.md`](./AGENTS.md).
