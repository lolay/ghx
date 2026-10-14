```
--- KICKOFF: begin orchestration at [deep] ---

  Status: 3/4 groups done | last review: wave-3-fix PASS | current: ghx s11-s12 [exec] | updated 2026-10-14 | over expected: ~$19.1 of ~$4.6

  mode: unattended | proposed unattended (harness=claude-code runner=cloud; CLAUDE_CODE_REMOTE=true, assigned branch claude/magical-ride-7h4rhi) | guard 3x min $50 | fixups 2 | confirmed 2026-10-13 session bd9c01f5-fe1c-5a92-ab0c-95720d491a10: unattended

  review: every-wave (log-only)

  Next model
    Cursor:      claude-opus-5-5[effort=high]
    Claude Code: /model opus                (/effort high)

  Prompt to paste into the next chat:
    In lolay/ghx, read specs/handoffs/plan-cli-ux-sparkling.md. The plan is already tagged.
    On branch claude/magical-ride-7h4rhi (task branch): subagents commit each finished step.
    Run in unattended mode.
    Run the personal-plan-orchestrate skill from the top: walk to
    each tier boundary, dispatch subagents per the skill's
    procedure, and pause only where the recorded mode stops. Do not
    execute plan work inline. Update plan progress after each wave
    returns per the skill's procedure. The mode line above is the
    kickoff answer. If the Status line shows BLOCKED, re-post that
    question and wait; otherwise record the mode, print the kickoff
    summary and begin dispatching.

---
```

**Cost (API-equiv, Claude Code models)**

| wave | expected tokens | expected $ | actual tokens | actual $ |
|---|---|---|---|---|
| 1 [exec] ghx s1-s5 | ~4.6M | ~$1.8 | ~11M (output est.) | ~$4.2 (output est.) |
| 2 [exec] ghx s6-s7 | — | — | ~6.2M (output est.) | ~$2.6 (output est.) |
| 3 [exec] ghx s8-s10 | — | — | ~8.9M (output est.) | ~$3.5 (output est.) |
| 3-fix [exec] ghx s8-s10 | — | — | ~1.3M (output est.) | ~$0.85 (output est.) |
| 4 [exec] ghx s11-s12 | — | — | — | — |
| orchestrator | ~3.1M | ~$2.8 | ~19M | ~$8.0 |
| **Total** | ~7.7M | ~$4.6 | ~47M | ~$19.1 |

Expected values are estimates, good to about 2-3× per wave.

# Spike nicer CLI experiences for ghx, then pick one

## Context

Gary prefers the Rust port's (`old/rust` branch) feel, especially progress that
updates in place, and wants a better experience rather than a clap/indicatif
clone. He chose to spike the candidates and judge them side by side by running the
branch himself:

- Help and errors: `github.com/charmbracelet/fang` v1.0.0 wrapping the existing
  cobra command (the GoReleaser look). Kong is out: it changes the code shape, not
  the experience. Use the `github.com/charmbracelet/fang` module path (the
  `charm.land/fang` alias serves a go.mod that declares the github path).
- Sync progress, three candidates plus today's baseline:
  - **buildx-style**: one live line per active worker (repo, clone/pull, timer),
    finished repos print above as `✓ foo pulled 1.2s`, overall bar last.
    `github.com/vbauerster/mpb/v8` (v8.16.2).
  - **indicatif-style**: one line redrawn in place: spinner, `[=>-]` bar, %,
    elapsed, current repo. Also mpb.
  - **pterm**: `github.com/pterm/pterm` (v0.12.84) multi-printer + progress bar.
  - **gopretty** (default): today's go-pretty tracker, unchanged.

Git output is captured (`internal/cloner/git.go:153`), so nothing interleaves
with a renderer.

## Design

Decouple cloning from rendering so the spikes are swappable and the losers delete
cleanly:

- `internal/cloner`: replace `Options.ProgressOut io.Writer` with
  `Options.Progress Progress`, an interface:

      // Progress receives sync events; implementations render them.
      type Progress interface {
          Start(total int)
          RepoStarted(name, action string) // action: "clone" or "pull"
          RepoDone(r RepoResult, elapsed time.Duration)
          Stop()
      }

  plus `NoProgress` (a no-op) used when `Options.Progress` is nil. Workers in
  `cloner.go` (the loop at about lines 57-122) call `RepoStarted` before and
  `RepoDone` after each repo (once per `RepoResult`, wikis included, or once per
  repo with its primary result: pick one and document it), and `Stop` replaces the
  `IsRenderInProgress` poll. Calls come from several goroutines, so every
  implementation must be safe for concurrent use. On context cancel, `Stop` must
  still be called and must leave the terminal on a fresh line.
- `internal/ui`: `NewProgress() cloner.Progress` returns `cloner.NoProgress{}`
  when `animate` is false (pipe, file, no ANSI), otherwise the renderer named by
  the temporary `GHX_PROGRESS` env var (`gopretty`, `buildx`, `indicatif`,
  `pterm`; unset or unknown means `gopretty`). Each renderer lives in its own file
  (`progress_gopretty.go`, `progress_buildx.go`, `progress_indicatif.go`,
  `progress_pterm.go`) and writes to `Out`. `ui` already imports `cloner`, so the
  dependency direction is unchanged.
- `internal/cli/run.go` (about line 215): pass `Progress: ui.NewProgress()`
  instead of `ProgressOut: ui.ProgressOut()`. Remove `ui.ProgressOut` once nothing
  uses it, and move its tests to `NewProgress` (a file or non-ANSI console gets
  `NoProgress`).

## Steps

Every step: follow `AGENTS.md` and `CONTRIBUTING.md` (black-box `_test` packages,
testify, table-driven, doc comments on exported identifiers, three import blocks,
`filepath` for paths, Windows-safe). Gate: `make ci` must pass before each commit
(see the environment notes in the handoff for the toolchain and lint). Update
`specs/testing.md` in the same commit when a seam it describes changes.

--- WAVE 1 [exec] ---

#### s1 - [exec] Put the go-pretty tracker behind a Progress interface (done)

Pure refactor per "Design": add `cloner.Progress` and `cloner.NoProgress`, move
the go-pretty tracker from `internal/cloner/cloner.go` into
`internal/ui/progress_gopretty.go` behind the interface, add `ui.NewProgress()`
(with `GHX_PROGRESS` read but only `gopretty` known yet), and switch
`internal/cli/run.go`. Terminal output on a TTY must look the same as before.

Acceptance: `make ci` passes; `cloner` tests run with `NoProgress` or a recording
fake that asserts `Start(n)`, one `RepoDone` per repo and exactly one `Stop`, also
when the context is cancelled; `ui` tests cover `NewProgress` returning
`NoProgress` for a non-terminal; the progress rows of `specs/testing.md` describe
the new seam; `internal/cloner` no longer imports go-pretty.

#### s2 - [exec] Wrap the root command with fang (done)

In `internal/cli/root.go` `Execute`, run the command through
`fang.Execute(ctx, cmd, ...)` with `fang.WithVersion(buildinfo.String())`, keeping
`Execute`'s exit-code contract (0 on success, 1 on error) and the
`ghx <release>+<sha> (<buildCode>)` version string. Let fang print errors (drop
the manual `Error:` print) unless that loses the error text. Keep the signal
context. Add an `Example:` block to the root command (dry run, `--ssh`,
`--include`). Decide on fang's `man` and `completion` subcommands: keep them only
if they work next to `cobra.ExactArgs(2)` on the root; otherwise pass
`fang.WithoutManpage()` / `fang.WithoutCompletions()`. Record which in the
artifact.

Acceptance: `make ci` passes; a test runs `--help` and asserts the flags and the
example appear, and one asserts an unknown flag returns a non-zero code; with
`NO_COLOR=1` or a non-terminal stdout the help has no ANSI escapes (test it if
fang allows, else verify by hand and say so). The artifact holds the plain-text
`--help` output and one error output.

#### s3 - [exec] Add the buildx-style renderer (done)

`internal/ui/progress_buildx.go`, selected by `GHX_PROGRESS=buildx`, on
`github.com/vbauerster/mpb/v8`: an overall bar at the bottom (`Syncing repos`,
count, percentage, elapsed); one live line per repo in flight showing a spinner,
the repo name, `cloning`/`pulling` and its own elapsed time; when a repo finishes
its line is removed and a one-line log entry prints above the bars
(`✓ name pulled 1.2s`, `✗ name failed 0.4s`, `- name skipped (local changes)`),
coloured with go-pretty `text` like the rest of `ui`. Writes go to `Out`.

Acceptance: `make ci` passes; a smoke test drives the renderer with
`Start/RepoStarted/RepoDone/Stop` from several goroutines into a buffer and
asserts it returns without deadlock and the buffer names every repo. No change
outside `internal/ui`, `go.mod` and `go.sum`.

#### s4 - [exec] Add the indicatif-style renderer (done)

`internal/ui/progress_indicatif.go`, selected by `GHX_PROGRESS=indicatif`, on
mpb: a single line redrawn in place, `<spinner> Syncing repos [=====>----]
42% (3s) <last repo started>`, using mpb's bar style
(`Lbound("[").Filler("=").Tip(">").Padding("-").Rbound("]")`).

Acceptance: as s3 (smoke test, `make ci`, scope).

#### s5 - [exec] Add the pterm renderer (done)

`internal/ui/progress_pterm.go`, selected by `GHX_PROGRESS=pterm`, on
`github.com/pterm/pterm`: a pterm progress bar for the overall count plus a
spinner (or multi-printer area) showing the repos in flight, and a one-line
success/failure entry per finished repo, in pterm's default look. Route pterm's
output to `Out` (pterm's `WithWriter`); make sure pterm doesn't change colours or
global state for the rest of `ui` (disable pterm's own output in tests if needed).

Acceptance: as s3 (smoke test, `make ci`, scope). The artifact says how Gary
tries each renderer: `make build`, then `GHX_PROGRESS=<name> ./bin/ghx <org>
<dir>` against a real org with a few dozen repos, plus `./bin/ghx --help`.

--- WAVE 2 [exec] ---

Re-plan after wave 1 (2026-10-13): Gary tried the renderers, likes pterm, and
asked for status symbols in it plus a new variant that updates the repo table in
place. Keep every existing renderer.

#### s6 - [exec] Status symbols in the pterm renderer (done)

In `internal/ui/progress_pterm.go`, replace pterm's `SUCCESS` / `WARNING` /
`ERROR` prefixes on the finished-repo lines with symbols: green `✓` for a success
(cloned, pulled, moved), yellow `⚠` for any `skipped (...)` action, red `✗` for
`failed`. Build them as the renderer's own `pterm.PrefixPrinter` values (prefix
text and style set on a copy), never by assigning `pterm.Success`,
`pterm.Warning` or `pterm.Error`, so pterm's globals stay untouched. Keep the
line text as it is (`name action 1.2s`, skipped lines without a time). Put the
symbol-and-colour choice for an action in one small helper in `internal/ui` that
s7 reuses.

Acceptance: `make ci` passes; the pterm smoke test (or a new table-driven test of
the helper) asserts each class of action gets its symbol; no assignment to a pterm
package-level printer anywhere in the repo. No change outside `internal/ui`.

#### s7 - [exec] Add the live repo-table renderer (done)

`internal/ui/progress_table.go`, selected by `GHX_PROGRESS=table`, built on pterm's
`AreaPrinter` like s5. It redraws the "Repos to export" table in place with a
Status column, and the overall pterm bar under the table.

- **Owning the table.** Add an optional interface in `internal/ui`, e.g.
  `RepoLister interface{ SetRepos([]ghapi.RepoInfo) }`. In `internal/cli/run.go`,
  create the progress once (`progress := ui.NewProgress()`) before the table is
  printed; when the run is not a dry run and `progress` implements the
  interface, call `SetRepos(repos)` instead of `ui.PrintRepoTable(repos)`.
  Otherwise print the table as today. A dry run, a pipe or a file (NoProgress)
  behaves exactly as now. Pass the same `progress` to `baseOpts.Progress`.
- **Rows and status.** Same columns, order and formatting as `PrintRepoTable`
  (reuse its row-building code: factor it into a helper rather than copying),
  plus a Status column: faint `queued`; a spinner frame plus `cloning` or
  `pulling` and the running time while in flight; then the s6 symbol with the
  action and time (`✓ pulled 1.2s`, `⚠ skipped (local changes)`, `✗ failed
  0.4s`). Repos never started keep `queued`.
- **Fitting the terminal.** Read the terminal size each repaint through a field
  the tests can set (default: pterm's or `golang.org/x/term`'s size of `Out`,
  falling back to 80x24). When the whole table plus the bar fits in height - 2
  lines, draw it all. Otherwise draw a window: the header, every in-flight row,
  then the most recent finished rows to fill the space, and a footer line like
  `… 41 done, 18 queued`. When the table is wider than the terminal, drop
  columns in this order until it fits: Last Pushed, Default Branch, Archived,
  Language. Never let a line wrap.
- **Two passes.** `run.go` calls `CloneRepos` for active repos, prints "Cloning N
  archived repo(s)…", then calls it again for archived ones with the same
  Progress. Keep every repo's row across both passes. Each `Stop` ends the live
  area. The full table (all rows given to `SetRepos`, current statuses, no
  window) prints once as ordinary output, at the first `Stop` after which no
  row is still `queued`; if a run ends with rows still queued (a cancel, or
  archived repos skipped under `--delete`), print it at that `Stop` anyway when
  the context is done, else let `run.go` print it: pick the simplest rule that
  prints it exactly once per run and document it. The sync summary that
  follows stays as it is.
- **Safety.** Same concurrency rules as the other renderers; `Stop` leaves the
  terminal on a fresh line, also after a cancel; pterm globals untouched (the
  cursor target only between Start and Stop, as s5 does).

Acceptance: `make ci` passes; tests drive the renderer from several goroutines
into a buffer with a fixed terminal size and assert (a) every repo name and its
final status appear in the final table, (b) a tall list shows the window footer
and not every row in the live frame, (c) a narrow width drops Last Pushed first;
a `cli` test shows a dry run and a non-terminal run still print the static table.
`specs/testing.md` gets a line for the renderer tests (the s3-s5 smoke tests and
this one) and its `internal/cli` coverage figure is brought up to date. The
artifact says how to try it: `GHX_PROGRESS=table ./bin/ghx <org> <dir>` in a tall
and in a short terminal window.

--- WAVE 3 [exec] ---

Re-plan after wave 2 (2026-10-13): Gary picked the table renderer and asked for
(1) column widths that don't change while it runs, (2) the progress inside a table
footer, (3) a footer that shows more is coming, and (4) scrolling up through the
table while it runs, as Claude's CLI allows. He accepted rows appearing in the
order repos finish. Change `GHX_PROGRESS=table` in place; leave the other
renderers alone.

#### s8 - [exec] Fixed column widths and a progress footer in the table renderer (done)

In `internal/ui/progress_table.go`:
- Measure every column once, when `SetRepos` gets the list: each column's width
  is its widest value over all repos and its header; Status is as wide as its
  longest possible value (the longest of the in-flight form `⠋ cloning 12m59s`,
  `⠋ pulling 12m59s`, `interrupted`, `queued`, and `<symbol> <action> <time>` for
  every `cloner.Action`). Fix those widths on the go-pretty columns
  (`WidthMin` = `WidthMax`) so no frame changes the table's width. Decide which
  columns to drop for the terminal's width once, at that point, in the existing
  order (Last Pushed, Default Branch, Archived, Language); if the Name column
  still doesn't fit, cap it and cut long names with `…`. A terminal resized
  mid-run keeps the widths chosen at the start.
- Replace the separate pterm progress bar with a table footer row merged across
  all columns (go-pretty `AppendFooter` with `table.RowConfig{AutoMerge: true}`,
  or the equivalent that renders one cell spanning the table): `Syncing 34/60
  [=========>------] 57% 12s · 4 running · 22 queued`. Count over every repo
  given to `SetRepos`, across both passes, not per pass. Draw the bar with plain
  characters sized to fit the footer.
- Keep the existing window behaviour for now (s9 replaces it).

Acceptance: `make ci` passes; a test renders frames at several points of a run
(some queued, some in flight with different elapsed times, some finished with
the longest action) and asserts every frame's lines have the same width; a test
asserts the footer text and counts across two `Start`/`Stop` passes; the narrow
width test still drops Last Pushed first. No change outside `internal/ui`.

#### s9 - [exec] Stream finished rows into scrollback (done)

Rework the table renderer so only the bottom of the table is live, the way
Claude's CLI keeps finished output in the terminal's scrollback:
- On the first `Start` after `SetRepos`, print the table's top: its title, top
  border, header row and header separator, as ordinary output (not in the live
  area).
- When a repo finishes, print its row line as ordinary output above the live
  area (clear the area, print, start the area again, as the pterm renderer does
  for its log lines). With s8's fixed widths, rows printed one at a time line up
  as one continuous table. Rows appear in the order repos finish.
- The live area holds only the in-flight rows, a separator and the s8 footer.
  That is at most the concurrency plus a few lines, so drop the window logic and
  its `… N done, M queued` footer; the progress footer carries the queued count.
  If the live area would still be taller than the terminal (huge concurrency),
  show the first in-flight rows that fit and `+N more running` in the footer.
- Between the two passes the live area may stay up or be cleared; either way the
  table must stay continuous, so when the renderer is a `ui.RepoLister`,
  `internal/cli/run.go` must not print "Cloning N archived repo(s) into …"
  between the passes (the archived rows show `yes` in Archived already; keep
  the message for the other renderers).
- `PrintTable` becomes the close of the table: print any rows never finished
  (`queued` or `interrupted`) as ordinary rows, then the bottom border with a
  final footer such as `60 repos · ✓ 52 · ⚠ 6 skipped · ✗ 2 failed · 1m12s`,
  once per `SetRepos`. Rename it (e.g. `FinishTable`) if that reads better, and
  update `run.go` and the `RepoLister` doc.
- Nothing is printed twice: the full table is not reprinted at the end.

Acceptance: `make ci` passes; a test drives a run into a buffer from several
goroutines and asserts, on the final output with the live-area frames removed
(or on a terminal emulation if one is already in the tests), that the header
appears once, every repo's row appears exactly once in completion order, the
bottom border and final footer close it, and every table line has the same width;
a test covers a cancel (unfinished rows printed as `queued`/`interrupted`); a cli
test shows the archived-pass message is absent with the table renderer and
present with another renderer.

#### s10 - [exec] Drop the duplicate summary counts under the table renderer (done)

When the table renderer drew the table, the final footer already has the counts,
so `ui.PrintSummary`'s "Sync Summary" counts table is a repeat. Under the table
renderer, print only the lists that follow it (Failed repos, Skipped (local
changes), Skipped (unusable name)); everything else prints the summary as now.
Choose the smallest API for this (a separate `ui` function for the lists, or an
option), and keep `PrintSummary`'s existing tests passing.

Acceptance: `make ci` passes; a cli test with the table renderer shows no "Sync
Summary" table but does show a failed repo's list entry; one with another
renderer shows the summary as before. Update `specs/testing.md` for any seam
these steps change.

--- WAVE 4 [exec] ---

Re-plan after wave 3 (2026-10-14): `origin/main` is merged twice (b62310c,
bb82990), bringing `ActionFetchedOnly`, `ActionSkippedEmpty`, `ActionFailedWiki`
and `ActionUpToDate` ("up to date", split out of pulled), and main's summary
`glyphs`. Gary asked for download-style marks and to make the table renderer
the permanent progress display.

#### s11 - [exec] Download-style status marks

In `internal/ui`, change the marks `statusMark` gives (and the summary's
`glyphs` where they overlap, so the table and the summary agree):
- cloned: `✓` green (unchanged); pulled (new commits came in): `↓` green;
  up to date: `–` (U+2013) faint/grey; fetched only: `⇣` (U+21E3, the dashed down
  arrow: "partly downloaded") yellow, in `glyphs` too so the summary's "Fetched
  only" section and counts row use it; failed and failed wiki `✗` red, empty
  `∅` grey, unusable name and every other skip `⚠` yellow, moves `✓` green: all
  unchanged.
- `actionText`: up to date ran git, so it keeps its time like pulled.
- The table's closing footer counts one class per mark, in this order, showing
  a class only when its count is non-zero except the first:
  `60 repos · ✓ 3 cloned · ↓ 12 pulled · – 40 up to date · ⇣ 2 fetched only ·
  ⚠ 1 skipped · ∅ 1 empty · ✗ 1 failed · 1m12s` (keep the interrupted and
  queued counts after them). Drop the "not updated" wording.
- Every mark must be one terminal column wide (no emoji presentation): assert
  it with go-runewidth (already a dependency) in a test, since the table's fixed
  widths depend on it.

Acceptance: `make ci` passes; `status_test.go` covers every `cloner.Action`
(table-driven over `cloner.AllActions`, so a new action without a mark fails);
the footer test asserts the new totals text; the summary's counts table and
"Fetched only" section show `⇣`; `CHANGELOG.md` Unreleased notes the marks.

#### s12 - [exec] Make the table the only progress display

- Delete `progress_gopretty.go`, `progress_buildx.go`, `progress_indicatif.go`,
  `progress_pterm.go` and their tests/smoke-test cases, and the `GHX_PROGRESS`
  switch: `ui.NewProgress()` returns `cloner.NoProgress{}` when `animate` is
  false (pipe, file, no ANSI) and the table renderer otherwise. Keep
  `ui.RepoLister` and the run flow's handling as they are (a non-terminal still
  prints the static table and the full summary).
- `run.go` and its tests: drop the "other renderer" branches that only existed
  for the spike, but keep the non-terminal path and its tests; the
  "Cloning N archived repo(s)" and "--delete" notes now print only on the
  non-terminal path (verify the tests say so).
- `go mod tidy`: `github.com/vbauerster/mpb/v8` and anything only it pulled in
  should leave `go.mod`; pterm stays (the table draws through its AreaPrinter).
- Docs in the same commit: `specs/testing.md` (progress rows describe one
  renderer, no `GHX_PROGRESS`), `README.md` (one short paragraph on what a sync
  looks like on a terminal versus a pipe, if the README describes output at all),
  `CHANGELOG.md` Unreleased (the live repo table replaces the progress bar on a
  terminal: fixed columns, finished rows in scrollback, progress in the footer,
  totals at the close).
- Leave the `.scratch/` demo and `specs/handoffs/` alone.

Acceptance: `make ci` passes; `grep -rn GHX_PROGRESS` finds nothing outside
`specs/handoffs/`; `go.mod` has no mpb; a cli test shows a terminal-like run
(the renderer forced on, as the table tests do now) draws the live table without
any env var, and a non-terminal run still prints the static table and the full
summary; check the real binary's run in a pty with the scratch demo and put the
final screen in the artifact.

## After the run (not part of the orchestrated waves)

Gary tries the renderers and picks one. A follow-up then keeps the winner as the
only renderer, deletes the others and `GHX_PROGRESS`, runs `go mod tidy`, updates
`specs/`, and restyles the tables if they clash. This plan and its handoff are
removed or promoted before the branch merges.

## Review log

review wave-1 (ghx s1,s2,s3,s4,s5) 56d21d8..33a21fd: PASS - make ci green (lint 0 issues, race tests x3); interface, fang wrap and three renderers match spec; minor: waitFor asserts off the test goroutine, testing.md cli coverage says ~92% (now 94.1%) - 2026-10-13
review wave-2 (ghx s6,s7) 7ee0a3c..0592684: PASS - make ci green (race x3 on ui and cli); symbols via copies of pterm.Success, no pterm globals assigned; table renderer windows, drops columns, prints the full table once from run.go; removed/too-large notes now print before the live table - 2026-10-13
review wave-3 (ghx s8,s9,s10) c7bf5b6..4a5e995: CONCERNS - live area box drawing is wrong: the ┴ rule sits above the in-flight rows (closing columns the rows still use) instead of between them and the footer, and on the first frame it sits right under the header separator (two separators in a row); progress_table.go frameLocked concatenates [l.rule, rows..., footer, bottom] and progress_table_test.go:385-395 locks that order in; the rule belongs directly above the merged footer - 2026-10-13
review wave-3-fix (ghx s8,s9,s10) c7bf5b6..683d367: PASS - live area is now in-flight rows, ┴ rule, footer, border; rule omitted when nothing sits above it; tests lock the order; make ci green; checked in a pty at 70x14; passed unattended: gate 1 (fix-up 1 of 2) - 2026-10-13

## Token log

**Counting header (Claude Code)**

- Line, one per model a chat ran, appended below: `tokens <row> <group-id> (<model>): input ~X / cache read ~R / cache write ~W / output ~Y | ~$C API-equiv`. `<row>` is `wave-N`, `review-wave-N`, or `wave-N-fix` and `review-wave-N-fix` for a fix-up wave; `<group-id>` is the wave's group id with hyphens (`ghx-s1-s5`); `<model>` is `message.model` without a date suffix. Round counts to two significant figures with `k` or `M`.
- Usage: `~/.claude/projects/<slug>/$CLAUDE_CODE_SESSION_ID.jsonl` (`<slug>` is the working directory with every character but a letter or digit turned into `-`, matched by prefix when long; without the id, the newest `.jsonl` there), plus its subagents' `<session-id>/subagents/**/agent-*.jsonl` in the same directory. Sum `message.usage` over assistant lines once per `message.id`, from the line with `stop_reason`: input `input_tokens`, cache read `cache_read_input_tokens`, cache write `cache_creation.ephemeral_5m_input_tokens` (with `ephemeral_1h_input_tokens` too, `cache write ~40k 5m + ~8k 1h`), output `output_tokens`. A call with no `stop_reason` line keeps its input-side counts, takes output as about 1,000, and its line ends `(output est.) session <id>`.
- Rates by `<model>`, $ per Mtok input / cached / output: `claude-opus-5-5` 4.00 / 0.20 / 20.00; `claude-sonnet-5-5` 2.00 / 0.20 / 10.00.
- `$C` = (input × in + cache read × cached + 5m write × in × 1.25 + 1h write × in × 2.00 + output × out) / 1M.
- In another harness, or on a model not listed here, count and price per plan-execution.md "Token accounting" and "Model price table" instead.

tokens orchestrator-kickoff plan-cli-ux-sparkling (claude-opus-5-5): input ~96 / cache read ~6.6M / cache write ~0 5m + ~170k 1h / output ~37k | ~$3.39 API-equiv
tokens wave-1 ghx-s1-s2-s3-s4-s5 (claude-sonnet-5-5): input ~130 / cache read ~9.8M / cache write ~250k / output ~63k | ~$3.20 API-equiv (output est.) session bd9c01f5-fe1c-5a92-ab0c-95720d491a10
tokens review-wave-1 ghx-s1-s2-s3-s4-s5 (claude-opus-5-5): input ~40 / cache read ~960k / cache write ~70k / output ~21k | ~$0.97 API-equiv (output est.) session bd9c01f5-fe1c-5a92-ab0c-95720d491a10
tokens orchestrator-wave-1 ghx-s1-s2-s3-s4-s5 (claude-opus-5-5): input ~54 / cache read ~6.4M / cache write ~0 5m + ~70k 1h / output ~27k | ~$2.37 API-equiv
tokens wave-2 ghx-s6-s7 (claude-sonnet-5-5): input ~76 / cache read ~5.4M / cache write ~190k / output ~38k | ~$1.95 API-equiv (output est.) session bd9c01f5-fe1c-5a92-ab0c-95720d491a10
tokens review-wave-2 ghx-s6-s7 (claude-opus-5-5): input ~26 / cache read ~500k / cache write ~53k / output ~14k | ~$0.64 API-equiv (output est.) session bd9c01f5-fe1c-5a92-ab0c-95720d491a10
tokens orchestrator-wave-2 ghx-s6-s7 (claude-opus-5-5): input ~20 / cache read ~2.7M / cache write ~0 5m + ~41k 1h / output ~17k | ~$1.20 API-equiv
tokens wave-3 ghx-s8-s9-s10 (claude-sonnet-5-5): input ~100 / cache read ~8.5M / cache write ~240k / output ~51k | ~$2.79 API-equiv (output est.) session bd9c01f5-fe1c-5a92-ab0c-95720d491a10
tokens review-wave-3 ghx-s8-s9-s10 (claude-opus-5-5): input ~30 / cache read ~630k / cache write ~68k / output ~14k | ~$0.75 API-equiv (output est.) session bd9c01f5-fe1c-5a92-ab0c-95720d491a10
tokens orchestrator-wave-3 ghx-s8-s9-s10 (claude-opus-5-5): input ~22 / cache read ~2.9M / cache write ~0 5m + ~26k 1h / output ~11k | ~$1.00 API-equiv
tokens wave-3-fix ghx-s8-s9-s10 (claude-sonnet-5-5): input ~28 / cache read ~1M / cache write ~47k / output ~14k | ~$0.47 API-equiv (output est.) session bd9c01f5-fe1c-5a92-ab0c-95720d491a10
tokens review-wave-3-fix ghx-s8-s9-s10 (claude-opus-5-5): input ~20 / cache read ~220k / cache write ~27k / output ~10k | ~$0.38 API-equiv (output est.) session bd9c01f5-fe1c-5a92-ab0c-95720d491a10

## Completion summary (wave 1)

- Wave 1 [exec] `ghx s1-s5`: 5 commits, 56d21d8..33a21fd, review PASS on the first pass. No fix-ups, retries or checkpoints.
- Cost: ~$8.0 API-equiv against ~$4.6 expected. The kickoff (planning, research, plan-mode round trips) cost $3.4 of it; the worker's 9.8M cache reads ran about 2x the anchor.
- Deviations from plan: none in scope. Worker decisions: RepoDone once per repo with its primary result; fang's `man` and `completion` kept (an org named `help`, `completion` or `man` needs `--`); `execute` strips ANSI itself under NO_COLOR because fang still emits bold and reset codes; pterm uses an AreaPrinter over private buffers.
- Follow-ups: `make vuln` not run on the new dependency trees (fang, mpb, pterm); no renderer tried on a real org or a Windows console; `specs/testing.md` lacks a line for the renderer smoke tests and has a stale cli coverage figure; `waitFor` should assert on the test goroutine. Most of these fold into the pick-and-clean-up follow-up.

## Completion summary (wave 2)

- Wave 2 [exec] `ghx s6-s7` (re-plan at Gary's request): 2 commits, 7ee0a3c..0592684, review PASS on the first pass. No fix-ups, retries or checkpoints.
- Cost: plan total ~$12.8 API-equiv against ~$4.6 expected; wave 2 itself ~$2.6. The parent's share grew with the debugging turn between waves.
- Decisions: `ui.RepoLister` (SetRepos, PrintTable); run.go prints the final full table once after the last pass (and via defer on error or cancel); Stop erases the live area; a row running at Stop shows `interrupted`; the bar counts per pass.
- Behaviour change to know: with `GHX_PROGRESS=table`, the "Changes for removed/archived repos" and "Repos skipped (too large)" notes print before the live table.
- Follow-ups: as wave 1's, minus the testing.md items (done in s7). Not run against a real org.

## Completion summary (wave 3)

- Wave 3 [exec] `ghx s8-s10` (re-plan): c7bf5b6..4a5e995, review CONCERNS (live-area rule placement); automatic fix-up 3-fix 683d367, review PASS. Range c7bf5b6..683d367.
- Cost: plan total ~$19.1 API-equiv against ~$4.6 expected (three re-plans).
- Decisions: widths fixed at SetRepos; finished rows stream into scrollback in finish order; FinishTable closes the table; footer and rules hand-drawn from StyleLight characters; `ui.PrintSummaryLists` under the table renderer; archived-pass and --delete notes kept out of the table.
- Next, outside the plan: merge `origin/main` (ee8d164) as Gary asked.
