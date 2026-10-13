// Package ui centralizes terminal output: the settings summary, the
// repo table shown before cloning, and the post-run summary table.
//
// Colors are kept restrained (ANSI bold + a handful of styled cells via
// go-pretty) so the output stays readable when the TTY doesn't support
// color or the user pipes stdout to a file.
package ui

import (
	"fmt"
	"io"
	"os"
	"slices"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"

	"github.com/lolay/ghx/internal/cloner"
	"github.com/lolay/ghx/internal/ghapi"
)

// Out is the writer used for all UI output. Tests can override it.
var Out io.Writer = os.Stdout

// glyphs marks each action that needs attention, in the summary's counts table
// and beside each repo it lists. The words always come with the glyph, so a
// font without it, or output piped to a file, still reads.
var glyphs = map[cloner.Action]string{
	cloner.ActionFailed:       "✗",
	cloner.ActionFailedWiki:   "✗",
	cloner.ActionFetchedOnly:  "⚠",
	cloner.ActionSkippedEmpty: "∅",
	cloner.ActionSkippedName:  "⚠",
}

// noteGlyph marks a repo that synced but has something to say.
const noteGlyph = "·"

// animate is whether progress bars draw on Out; SetupTerminal decides.
var animate = true

// SetupTerminal decides, once at startup, whether f (os.Stdout) can show the
// animated progress bar: it must be a terminal that takes ANSI escape codes,
// since the bar redraws itself with cursor movements. go-pretty has by now
// turned on virtual terminal processing for a Windows console, or turned
// colors off where it couldn't (text.ANSICodesSupported); a pipe or a file
// gets no bar, so a log isn't full of redraws.
func SetupTerminal(f *os.File) {
	animate = text.ANSICodesSupported && isTerminal(f)
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// Printf formats to Out and drops the write error: there is nothing useful to
// do when the terminal is gone.
func Printf(format string, args ...any) { _, _ = fmt.Fprintf(Out, format, args...) }

// Println writes args and a newline to Out, dropping the write error as Printf does.
func Println(args ...any) { _, _ = fmt.Fprintln(Out, args...) }

// SettingRow is one line of the settings block.
type SettingRow struct {
	Label  string
	Value  string
	Source string
}

// PrintSettings renders the aligned "Settings:" block, matching the
// Python output: fixed-width labels, value column padded so the
// "(source)" suffixes align.
func PrintSettings(rows []SettingRow) {
	if len(rows) == 0 {
		return
	}
	maxVal := 0
	for _, r := range rows {
		if len(r.Value) > maxVal {
			maxVal = len(r.Value)
		}
	}
	Println()
	Println(text.Bold.Sprint("Settings:"))
	for _, r := range rows {
		padding := maxVal - len(r.Value) + 2
		Printf(
			"  %-15s%s%s%s\n",
			r.Label, r.Value, spaces(padding),
			text.Faint.Sprintf("(%s)", r.Source),
		)
	}
	Println()
}

// ObfuscateToken returns a short redacted form of tok for display
// (first and last 4 chars, or "****" if too short).
func ObfuscateToken(tok string) string {
	if len(tok) <= 8 {
		return "****"
	}
	return tok[:4] + "****" + tok[len(tok)-4:]
}

// repoColumn is one column of the "Repos to export" table.
type repoColumn struct {
	title string
	cell  func(ghapi.RepoInfo) any
	right bool
}

// Indexes into repoColumns, for the columns the live table drops to fit.
const (
	columnLanguage = 2
	columnBranch   = 4
	columnArchived = 5
	columnPushed   = 6
)

// repoColumns lists the table's columns in the order they are shown.
var repoColumns = []repoColumn{
	{title: "Name", cell: func(r ghapi.RepoInfo) any { return text.FgCyan.Sprint(r.Name) }},
	{title: "Visibility", cell: func(r ghapi.RepoInfo) any {
		if r.Private {
			return "private"
		}
		return "public"
	}},
	{title: "Language", cell: func(r ghapi.RepoInfo) any { return orDash(r.Language) }},
	{title: "Size (MB)", right: true, cell: func(r ghapi.RepoInfo) any {
		return fmt.Sprintf("%.1f", float64(r.SizeKB)/1024)
	}},
	{title: "Default Branch", cell: func(r ghapi.RepoInfo) any { return r.DefaultBranch }},
	{title: "Archived", cell: func(r ghapi.RepoInfo) any {
		if r.Archived {
			return "yes"
		}
		return "no"
	}},
	{title: "Last Pushed", cell: func(r ghapi.RepoInfo) any { return orDash(r.PushedAt) }},
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// repoTable builds the "Repos to export" table from the columns of
// repoColumns that cols names, in that order. A non-nil status adds a Status
// column after them, one cell per repo. A non-nil widths fixes the width of
// each column (cols in order, then Status) and cuts a cell that is wider with
// "…", so what is drawn never changes the table's width.
func repoTable(cols []int, repos []ghapi.RepoInfo, status []string, widths []int) table.Writer {
	tw := table.NewWriter()
	tw.SetTitle("Repos to export")
	header := make(table.Row, 0, len(cols)+1)
	var configs []table.ColumnConfig
	fix := func(cfg table.ColumnConfig, n int) table.ColumnConfig {
		if widths != nil {
			cfg.WidthMin, cfg.WidthMax = widths[n], widths[n]
			// The header is laid out with no rows (see tableLayout), where
			// go-pretty has no cells to take its alignment from.
			cfg.AlignHeader = cfg.Align
			if cfg.Align == text.AlignDefault {
				cfg.AlignHeader = text.AlignLeft
			}
		}
		return cfg
	}
	for n, c := range cols {
		header = append(header, repoColumns[c].title)
		cfg := table.ColumnConfig{Number: n + 1}
		if repoColumns[c].right {
			cfg.Align = text.AlignRight
		}
		configs = append(configs, fix(cfg, n))
	}
	cell := func(v any, n int) any {
		if widths == nil {
			return v
		}
		return truncate(fmt.Sprint(v), widths[n])
	}
	if status != nil {
		header = append(header, "Status")
		configs = append(configs, fix(table.ColumnConfig{Number: len(cols) + 1}, len(cols)))
	}
	tw.AppendHeader(header)
	for i, r := range repos {
		row := make(table.Row, 0, len(cols)+1)
		for n, c := range cols {
			row = append(row, cell(repoColumns[c].cell(r), n))
		}
		if status != nil {
			row = append(row, cell(status[i], len(cols)))
		}
		tw.AppendRow(row)
	}
	tw.SetColumnConfigs(configs)
	tw.SetStyle(table.StyleLight)
	return tw
}

// allColumns is every column of the table, in order.
func allColumns() []int {
	cols := make([]int, len(repoColumns))
	for i := range cols {
		cols[i] = i
	}
	return cols
}

// PrintRepoTable renders the "Repos to export" table shown before
// cloning begins.
func PrintRepoTable(repos []ghapi.RepoInfo) {
	tw := repoTable(allColumns(), repos, nil, nil)
	tw.SetOutputMirror(Out)

	Println()
	tw.Render()
	Printf("\nTotal: %s repos\n", text.Bold.Sprint(len(repos)))
}

// PrintSummary renders the post-run summary: per-action counts, then a section
// for each kind of repo that needs a look: failures, repos fetched without
// updating their working tree, empty repos, unusable names, and notes on repos
// that synced with something to say. A section with no rows isn't printed.
func PrintSummary(summary cloner.SyncSummary) {
	tw := table.NewWriter()
	tw.SetOutputMirror(Out)
	tw.SetTitle("Sync Summary")
	tw.AppendHeader(table.Row{"Action", "Count"})

	for _, a := range cloner.AllActions {
		n := summary.Count(a)
		if n == 0 {
			continue
		}
		label := string(a)
		// The unusable-name rows keep their plain label, as the table's other
		// quiet skips do; their glyph is on the lines below.
		if g, ok := glyphs[a]; ok && a != cloner.ActionSkippedName {
			label = g + " " + label
		}
		tw.AppendRow(table.Row{text.FgCyan.Sprint(label), n})
	}
	tw.SetColumnConfigs([]table.ColumnConfig{
		{Number: 2, Align: text.AlignRight},
	})
	tw.SetStyle(table.StyleLight)

	Println()
	tw.Render()
	Println()

	printSummaryLists(summary)
}

// PrintSummaryLists prints only the sections that follow PrintSummary's counts,
// after a blank line, and nothing at all when none has a row. The table
// renderer's closing footer already holds the counts, so the run flow calls
// this in place of PrintSummary after it.
func PrintSummaryLists(summary cloner.SyncSummary) {
	if !slices.ContainsFunc(summary.Results, listed) {
		return
	}
	Println()
	printSummaryLists(summary)
}

// listed reports whether r has a line in one of the summary's sections.
func listed(r cloner.RepoResult) bool {
	switch r.Action {
	case cloner.ActionFailed, cloner.ActionFailedWiki, cloner.ActionFetchedOnly,
		cloner.ActionSkippedEmpty, cloner.ActionSkippedName:
		return true
	case cloner.ActionCloned, cloner.ActionPulled:
		return r.Detail != ""
	}
	return false
}

// printSummaryLists prints each section that has rows.
func printSummaryLists(summary cloner.SyncSummary) {
	printSection("Failed repos:", text.FgRed, summary.Results, "", cloner.ActionFailed, cloner.ActionFailedWiki)
	printSection("Fetched only (working tree not updated):", text.FgYellow, summary.Results, "", cloner.ActionFetchedOnly)
	printSection("Skipped (empty):", text.Faint, summary.Results, "empty repository, skipped", cloner.ActionSkippedEmpty)
	printSection("Skipped (unusable name):", text.FgYellow, summary.Results, "", cloner.ActionSkippedName)
	printNotes(summary.Results)
}

// printSection lists the results whose action is one of actions under a bold
// heading, one "  <glyph> <name>  <detail>" line each, the heading and glyph in
// color. A non-empty detail replaces each result's own.
func printSection(heading string, color text.Color, results []cloner.RepoResult, detail string, actions ...cloner.Action) {
	var rows []cloner.RepoResult
	for _, r := range results {
		if slices.Contains(actions, r.Action) {
			rows = append(rows, r)
		}
	}
	if len(rows) == 0 {
		return
	}
	Println(color.Sprint(text.Bold.Sprint(heading)))
	for _, r := range rows {
		d := r.Detail
		if detail != "" {
			d = detail
		}
		printRow(color.Sprint(glyphs[r.Action]), r.Name, d)
	}
}

// printNotes lists the clones and pulls that carry a detail (a branch
// fallback, a reset after a force-push, a switch after a rename), so none of
// them is silent.
func printNotes(results []cloner.RepoResult) {
	var rows []cloner.RepoResult
	for _, r := range results {
		if (r.Action == cloner.ActionCloned || r.Action == cloner.ActionPulled) && r.Detail != "" {
			rows = append(rows, r)
		}
	}
	if len(rows) == 0 {
		return
	}
	Println(text.Bold.Sprint("Notes:"))
	for _, r := range rows {
		printRow(noteGlyph, r.Name, r.Detail)
	}
}

func printRow(glyph, name, detail string) {
	Printf("  %s %s  %s\n", glyph, name, detail)
}

func spaces(n int) string {
	if n <= 0 {
		return ""
	}
	b := make([]byte, n)
	for i := range b {
		b[i] = ' '
	}
	return string(b)
}
