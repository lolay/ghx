# Terminal output

How ghx shows a sync, and why it looks the way it does. Users: the Output bullet
under Platforms in [`README.md`](../README.md). Test seams: [`testing.md`](./testing.md).

## What a run shows

| Where stdout goes | Repo table | During the sync | After the sync |
|---|---|---|---|
| A terminal that takes ANSI codes | Live: drawn by the progress renderer | Finished rows print into scrollback; in-flight rows and a progress footer redraw at the bottom | The table closes with a totals footer, then the summary's sections without the counts table |
| A pipe, a file, or a console without ANSI | Static, printed once (`ui.PrintRepoTable`) | A note as each pass starts | The full summary: counts table, then its sections |
| `--dry-run` (either) | Static | Nothing runs | "Dry run complete. No changes made." |

`ui.SetupTerminal(os.Stdout)` decides which, once, at startup: the live table needs
a character device and go-pretty's `text.ANSICodesSupported`, since it moves the
cursor to redraw.

## The live repo table

The goal is the feel of a modern CLI (Claude Code, BuildKit): the screen updates
in place, nothing is printed twice, and you can scroll back through what finished
while the run goes on.

- **Columns.** Name, Visibility, Language, Size (MB), Default Branch, Last
  Pushed, and Status on the live table. Visibility reads `public`, `private` or
  `archived`, and archived wins, so an archived private repo shows `archived`:
  what matters about an archived repo is that it is read-only, and it takes the
  place of a separate Archived column, which no table has. The static table has
  the same columns without Status.
- **Fixed widths.** `newTableLayout` measures every column once, from the full repo
  list, when the renderer gets it (`SetRepos`). Status is as wide as its longest
  possible value. Widths never change mid-run, which is what lets rows printed
  one at a time line up as one table. If the terminal is too narrow at the start,
  columns drop in this order: Last Pushed, Default Branch, Language; then a
  long name is cut with `…`. A terminal resized mid-run keeps the start's
  widths.
- **Only the bottom is live.** The table's top (title, header) prints once as
  ordinary output. Rows print above the live area, into the terminal's
  scrollback, in list order: a finished row prints once every row above it has
  finished, so the scrollback reads in list order however the workers finish.
  The live area (pterm's `AreaPrinter`, redrawn every 100 ms) holds the rows not
  printed yet that are in flight or finished (held back behind a slower one),
  in list order, then the rule that closes their columns, the progress footer
  and the border.
- **A slow repo holds the rows after it.** Workers start repos in list order, so
  the live area is usually the concurrency plus a few lines. One slow repo (a
  large monorepo) holds back every row after it until it finishes. When the
  rows don't fit the terminal, the held ones give way first and the footer
  counts them, `+5 done, waiting on bigrepo`; then the last in-flight rows give
  way, counted as `+N more running`. When the slow repo finishes, it and the
  run of finished rows behind it print together.
- **Progress in the footer.** `Syncing 34/60 [====>---] 57% 12s · 4 running ·
  22 queued`, counted over every repo, across both passes.
- **Two passes, one table.** Active repos sync first, archived ones second, with
  the same renderer; the table stays open between them, so the run flow prints
  no "Cloning N archived repo(s)" line on a terminal, and the `--delete` note goes
  after the table's close. The table lists repos in pass order, each pass by
  name ignoring case (`ghapi.ListOrgRepos` sorts the list, and `run.go` puts the
  active repos before the archived ones), which is also the order the workers
  start them. An archived repo listed among the active ones would hold back the
  first pass's rows until the second pass reached it.
- **Closing.** `run.go` calls `FinishTable` after the last pass, and also through
  `defer` so a cancel or an error still closes it, once. Every row not printed
  yet prints in list order: finished ones held back with their result, and the
  ones that never finished as `queued` or `interrupted`. Then the totals footer, which wraps between
  counts if it is wider than the table:
  `60 repos · ✓ 3 cloned · ↓ 12 pulled · – 40 up to date · ⇣ 2 fetched only ·
  ⚠ 1 skipped · ∅ 1 empty · ✗ 1 failed · 1m12s`.
- **Footer drawing.** The footer, its rule and the closing border are drawn by
  hand from go-pretty's `StyleLight` box characters: go-pretty's merged footer row
  wraps at the first column's width.

### Status marks

`statusMark` gives one mark per action, shared with the summary's `glyphs` where
they overlap, so the table and the summary agree. Every mark must be one terminal
column wide (no emoji presentation), because the fixed widths depend on it; a
test checks every `cloner.Action`.

| Mark | Colour | Actions |
|---|---|---|
| `✓` | green | cloned, moved, deleted, cloned wiki |
| `↓` | green | pulled (new commits came in) |
| `–` | grey | up to date |
| `⇣` | yellow | fetched only: remote refs updated, working tree left alone |
| `⚠` | yellow | every skip, including an unusable name or an origin mismatch, and a conflict |
| `∅` | grey | skipped (empty) |
| `✗` | red | failed, failed wiki |

## The progress seam

`cloner.Progress` (`Start`, `RepoStarted`, `RepoDone`, `Stop`) is how `CloneRepos`
reports a sync; `cloner.NoProgress` is the no-op. `RepoDone` fires once per repo
with its primary result (a wiki adds none). `ui.NewProgress()` returns
`NoProgress` when the live table can't draw and the table renderer otherwise. The
renderer also implements `ui.RepoLister` (`SetRepos`, `FinishTable`), which is how
the run flow hands it the table instead of printing it; anything that isn't a
`RepoLister` gets the static table.

## Help and errors

The root command runs through [fang](https://github.com/charmbracelet/fang): styled
usage, examples and errors, and `ghx completion <shell>` and `ghx man`. The exit
code is 0 or 1 as before. Under `NO_COLOR` fang still emits bold and reset codes,
so `execute` strips ANSI itself. Because fang adds subcommands next to the root's
two positional arguments, an org named `help`, `completion` or `man` needs `--`
before it.

## Alternatives considered

- **Kong instead of cobra.** Closest to clap's derive API, but its default help is
  no nicer than cobra's, and switching would rewrite the flag-over-config
  precedence in `run.go`. fang gives the better experience on the existing cobra
  command.
- **A progress bar under a static table** (go-pretty's tracker, an indicatif-style
  single line on mpb, a BuildKit-style line per worker on mpb, pterm's bar and
  spinner). All were built and tried; the live table won because the repo list
  and the progress are one thing to read.
- **Redrawing the whole table.** The first live table did, and it can't work
  past one screen: rows that scroll off can't be redrawn, and every frame then
  leaves a copy behind. It also changed width as statuses changed.
- **A full-screen TUI** (bubbletea, an alternate screen). It can scroll inside
  itself, but takes over the terminal and leaves nothing in scrollback.
- **Rows in finish order.** Each row prints the moment its repo finishes, so the
  live area never holds a finished row, but the scrollback comes out shuffled
  and a repo is hard to find in a long run. Holding rows back keeps list order
  and still prints each row once; its cost is the `waiting on` stretch behind a
  slow repo.
- **Rows in list order by reprinting the table at the end.** Prints every row
  twice, and the first copy is in finish order anyway.

## Limits

- No run has been timed against a large real org; the 100 ms repaint and the
  per-row print are cheap, but a slow terminal over SSH is untested.
- On Windows the cursor package moves the console through its API, so the
  renderer tests that read cursor movement from a buffer skip there.
