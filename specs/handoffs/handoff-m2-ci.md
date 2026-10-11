# Handoff: m2 - CI on three operating systems

Milestone handoff from m1 to m2 of
[`plan-ghx-platforms-lynx.md`](./plan-ghx-platforms-lynx.md). Scratch: removed
or promoted before `feature/ghx-platforms-lynx` merges.

## Where we are

m1 (cross-platform correctness) is done and reviewed, on
`feature/ghx-platforms-lynx` at `83231a0`:

- **m1.s1-m1.s3** (`fb00a5e`, `8dfa741`, `9f0db0c`): hermetic tests for
  `cloner` (local bare repos from `internal/gittest`), `ghapi` (an unexported
  `baseURL` seam and the fake API in `internal/ghapi/ghapitest`) and the cli
  run flow end to end.
- **m1.s4** (`580d54a`): every OS touchpoint audited and recorded in the
  "Platforms" table of `specs/testing.md` (and the README). New:
  `cloner.CheckRepoName` (Windows device names, trailing dots and spaces, and
  any name that isn't one local path element), `FindGit` (one `exec.LookPath`,
  refuses `ErrDot`), `platform_windows.go` (`core.longpaths`, retrying moves
  and deletes on access-denied or sharing violations) and `platform_unix.go`
  (SIGINT, then kill after 10 s), and a progress bar drawn only on an ANSI
  terminal.
- **m1.s5** (`83231a0`): the token reaches `git` only through
  `GIT_CONFIG_COUNT`/`KEY`/`VALUE` as an `http.<scheme>://<host>/.extraheader`
  for clone and pull; `authenticatedHTTPS` is gone; old token-bearing origins
  are cleaned; `internal/redact` masks the token in failure details and API
  errors; git 2.31 is the minimum.

Not done in m1 (follow-ups, not m2 work): `resolveWikiURL` replaces the first
`.git` anywhere in the URL; `manifest.Write` gets the repo list before
`--include` / `--exclude` filtering; `GIT_TERMINAL_PROMPT=0` on a 401 needs a
decision.

## What m2 needs to deliver

- **m2.s1** `.github/workflows/ci.yml`: push (every branch) and
  `pull_request`, a per-ref `concurrency` group that cancels superseded runs,
  `permissions: contents: read`, current majors of `actions/checkout` and
  `actions/setup-go` (`go-version-file: .go-version`). Jobs: Linux
  (`go mod verify`, golangci-lint at the Makefile's pinned version,
  `make ci`, `make vuln`), macOS (`make ci`, same lint install), Windows
  (`go build`, `go vet`, `go test` without `-race`, `shell: bash`).
  `actionlint` clean.
- **m2.s2** the CI map in `Makefile.md`, a README badge for `lolay/ghx`'s
  `ci.yml` on `main`, and a `CONTRIBUTING.md` note on the three OSes.
- **m2.s3** read this branch's first CI run with `gh` and fix what fails.

## Key decisions already made

- Model is `lolay/triage`'s `ci.yml` (on the Mac at
  `~/Projects/lolay/triage/triage/.github/workflows/ci.yml`), plus a macOS job.
- golangci-lint is installed with the Makefile's `GOLANGCI_LINT_VERSION`
  (`v2.14.0` today); `ci.yml` must carry the same value.
- Windows runs without `-race` (it needs cgo there).
- m2.s3 is read-only on Actions: `gh run list`, `gh run view`, `gh run watch`.
  No dispatches or re-runs; a run that never starts is `needs_info`.

## Suggested plan

1. Read `Makefile`, `Makefile.md`, `specs/testing.md` and triage's `ci.yml`.
2. Look up the current majors of `actions/checkout` and `actions/setup-go`.
3. Write `ci.yml`, run `actionlint`, run `make ci`; commit m2.s1.
4. Fill the CI map, badge and CONTRIBUTING note; commit m2.s2.
5. The orchestrator pushes; m2.s3 reads that run.

## Gotchas

- Windows-only code and tests have only been type-checked on the Mac. The
  first Windows run is where these first execute: the `git.bat` fake on a
  `PATH` holding only a temp directory, renaming a directory while a file is
  held open, `gittest.RewriteURL` with `C:/` paths, `GIT_CONFIG_GLOBAL` set
  to `NUL`, and removing read-only `.git/objects`.
- Tests need `git` 2.31 or later on every runner (`FindGit` checks it).
- `make ci` never compiles `_windows.go` files on the Mac; only cross vets and
  `GOOS=windows golangci-lint run` check them locally.

## Files to reference

- `specs/testing.md` (seams and the "Platforms" table), `Makefile`,
  `Makefile.md`, `CONTRIBUTING.md`, `README.md`
- `internal/cloner/platform_windows.go`, `internal/cloner/credential.go`,
  `internal/gittest/`
- `.scratch/orchestrate-plan-ghx-platforms-lynx-2-ghx-m1-s4-s5.md` (wave 2
  worker artifact, local only)
