# Handoff: CLI UX spike (cli-ux-sparkling)

- **Branch:** `claude/magical-ride-7h4rhi` in `lolay/ghx` (assigned by the cloud
  session; cut from `main` at d984df8).
- **Plan:** `specs/handoffs/plan-cli-ux-sparkling.md` (re-opened for wave 2).
- **Mode:** unattended (confirmed 2026-10-13; runner=cloud).
- **Done:** wave 1 [exec] `ghx s1-s5`, 56d21d8..33a21fd, review PASS. No fix-ups,
  retries or checkpoints.
- **Spend:** ~$8.0 of ~$4.6 expected (Token log in the plan).
- **Next unit:** wave 2 [exec] `ghx s6-s7`: pterm status symbols, and a new
  `GHX_PROGRESS=table` renderer that redraws the repo table in place.
- **Later:** Gary picks a renderer; a follow-up keeps it, deletes the rest and
  `GHX_PROGRESS`, tidies `go.mod`, updates `specs/`, and removes or promotes this
  plan and handoff before the branch merges.
- **Resume:** paste the plan's Kickoff prompt into a new session on this branch.

## How to try it

    make build
    GHX_PROGRESS=gopretty  ./bin/ghx <org> <dir>   # original bar, also the default
    GHX_PROGRESS=buildx    ./bin/ghx <org> <dir>
    GHX_PROGRESS=indicatif ./bin/ghx <org> <dir>
    GHX_PROGRESS=pterm     ./bin/ghx <org> <dir>
    ./bin/ghx --help

Run twice against the same dir to see pulls. Piped output gets no progress.

## Pending question

None. Gary asked for wave 2 ("Yes, try it as a new variant table and keep pterm").

## Decisions so far

- fang module path is `github.com/charmbracelet/fang` (v1.0.0); `charm.land/fang`
  serves a go.mod declaring the github path, so don't use the alias.
- Kong was considered and dropped: it changes code shape, not the experience.
- RepoDone fires once per repo with its primary result (wikis add none); an
  unusable name gets RepoDone with no RepoStarted; Stop is deferred after Start.
- fang's `man` and `completion` are kept; an org named `help`, `completion` or
  `man` must follow `--` (README and CHANGELOG say so).
- Under NO_COLOR, `execute` strips ANSI itself: fang still emits bold and reset.
- pterm renderer uses an AreaPrinter over private buffers, with the cursor target
  pointed at `Out` only between Start and Stop.

## Environment notes

- env: The container has Go 1.26.2; `.go-version` wants 1.27.2. Run every Go and
  make command with `GOTOOLCHAIN=go1.27.2` exported.
- env: `/usr/local/bin/golangci-lint` is v2.5.0 built with go1.25 and fails on
  this module. Shim in the gitignored `.scratch/bin/golangci-lint`
  (`exec go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 "$@"`)
  and run `PATH="$PWD/.scratch/bin:$PATH" GOTOOLCHAIN=go1.27.2 make ci`.
- env: a `script` pty has 0 rows and mpb draws nothing; use
  `script -qc "stty rows 30 cols 100; cmd" /dev/null`.

## Deviations from plan

- Re-plan after wave 1: wave 2 (s6, s7) added at Gary's request; its Cost table row is unplanned (expected —).

Wave 1 follow-ups: Follow-ups: `make vuln` not run on fang/mpb/pterm; no real-org or Windows
console run; `specs/testing.md` lacks the renderer smoke tests and has a stale cli
coverage figure (92% vs 94.1%); `waitFor` asserts off the test goroutine; pterm
never unregisters stopped bars (small leak, pterm renderer only).
