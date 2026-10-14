# Handoff: CLI UX spike (cli-ux-sparkling)

- **Branch:** `claude/magical-ride-7h4rhi` in `lolay/ghx` (assigned by the cloud
  session; cut from `main` at d984df8).
- **Plan:** `specs/handoffs/plan-cli-ux-sparkling.md` (re-opened for wave 4).
- **Mode:** unattended (confirmed 2026-10-13; runner=cloud).
- **Done:** wave 1 [exec] `ghx s1-s5`, 56d21d8..33a21fd, review PASS. No fix-ups,
  retries or checkpoints.
  Wave 2 [exec] `ghx s6-s7` (re-plan), 7ee0a3c..0592684, review PASS.
- **Spend:** ~$19.1 of ~$4.6 expected (Token log in the plan).
  Wave 3 [exec] `ghx s8-s10`, c7bf5b6..4a5e995, review CONCERNS (live-area box
  drawing: the column-closing rule sits above the in-flight rows, not above the footer).
  Fix-up 3-fix 683d367, review PASS.
- **Merged `origin/main`** (ee8d164, lolay/ghx#6) after the fix-up. Conflicts in
  CHANGELOG, `internal/ui/ui.go`, `specs/testing.md`, resolved by the parent: main's
  summary sections kept, `PrintSummaryLists` now prints them without the counts;
  `statusMark` uses main's glyphs (✗ failed, ⚠ fetched only / unusable name,
  ∅ empty, ⚠ any other skip, ✓ the rest); the table's closing footer says
  `⚠ N not updated` and `∅ N empty`. `make ci` green, race x3 on ui/cli/cloner.
- **Merged `origin/main` again** (bb82990: d366f93 up-to-date split, bc9653f
  force-push follow-up); only CHANGELOG conflicted; one progress test now expects
  `up to date` on a second sync.
- **Next unit:** wave 4 [exec] `ghx s11-s12`: download-style marks (↓ pulled,
  – up to date, ⇣ fetched only), then the table becomes the only progress display.
- **Later:** Gary picks a renderer; a follow-up keeps it, deletes the rest and
  `GHX_PROGRESS`, tidies `go.mod`, updates `specs/`, and removes or promotes this
  plan and handoff before the branch merges.
- **Resume:** paste the plan's Kickoff prompt into a new session on this branch.

## How to try it

    make build
    GHX_PROGRESS=gopretty  ./bin/ghx <org> <dir>   # original bar, also the default
    GHX_PROGRESS=buildx    ./bin/ghx <org> <dir>
    GHX_PROGRESS=indicatif ./bin/ghx <org> <dir>
    GHX_PROGRESS=pterm     ./bin/ghx <org> <dir>   # now with ✓ / ⚠ / ✗
    GHX_PROGRESS=table     ./bin/ghx <org> <dir>   # live repo table with a Status column
    ./bin/ghx --help

Run twice against the same dir to see pulls. Piped output gets no progress.

## Pending question

None. Gary chose the table as permanent and asked for download-style marks.

## Decisions so far

- fang module path is `github.com/charmbracelet/fang` (v1.0.0); `charm.land/fang`
  serves a go.mod declaring the github path, so don't use the alias.
- Kong was considered and dropped: it changes code shape, not the experience.
- RepoDone fires once per repo with its primary result (wikis add none); an
  unusable name gets RepoDone with no RepoStarted; Stop is deferred after Start.
- fang's `man` and `completion` are kept; an org named `help`, `completion` or
  `man` must follow `--` (README and CHANGELOG say so).
- Under NO_COLOR, `execute` strips ANSI itself: fang still emits bold and reset.
- Table renderer: `ui.RepoLister` is (SetRepos, FinishTable); widths fixed at
  SetRepos (table_layout.go); finished rows stream above the live area in finish
  order; FinishTable closes the table (unfinished rows, rule, totals footer,
  border); footer, rule and border are hand-drawn from StyleLight's characters
  because go-pretty's merged footer wraps at the first column; run.go skips the
  archived-pass and --delete notes inside the table; `ui.PrintSummaryLists` under it.
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

- Re-plan after wave 3: wave 4 (s11-s12) at Gary's request; unplanned row.
- wave 3: automatic fix-up 3-fix for the review's CONCERNS (passed unattended: gate 1, fix-up 1 of 2).
- Re-plan after wave 2: wave 3 (s8-s10) added at Gary's request; unplanned row.
- Re-plan after wave 1: wave 2 (s6, s7) added at Gary's request; its Cost table row is unplanned (expected —).

Wave 1 follow-ups (testing.md items done in s7): `make vuln` not run on fang/mpb/pterm; no real-org or Windows
console run; `specs/testing.md` lacks the renderer smoke tests and has a stale cli
coverage figure (92% vs 94.1%); `waitFor` asserts off the test goroutine; pterm
never unregisters stopped bars (small leak, pterm renderer only).
