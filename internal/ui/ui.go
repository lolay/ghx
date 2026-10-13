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

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"

	"github.com/lolay/ghx/internal/cloner"
	"github.com/lolay/ghx/internal/ghapi"
)

// Out is the writer used for all UI output. Tests can override it.
var Out io.Writer = os.Stdout

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
// column after them, one cell per repo.
func repoTable(cols []int, repos []ghapi.RepoInfo, status []string) table.Writer {
	tw := table.NewWriter()
	tw.SetTitle("Repos to export")
	header := make(table.Row, 0, len(cols)+1)
	var configs []table.ColumnConfig
	for n, c := range cols {
		header = append(header, repoColumns[c].title)
		if repoColumns[c].right {
			configs = append(configs, table.ColumnConfig{Number: n + 1, Align: text.AlignRight})
		}
	}
	if status != nil {
		header = append(header, "Status")
		// A floor keeps the column from jumping as "queued" gives way to a
		// spinner and a time.
		configs = append(configs, table.ColumnConfig{Number: len(cols) + 1, WidthMin: statusWidth})
	}
	tw.AppendHeader(header)
	for i, r := range repos {
		row := make(table.Row, 0, len(cols)+1)
		for _, c := range cols {
			row = append(row, repoColumns[c].cell(r))
		}
		if status != nil {
			row = append(row, status[i])
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
	tw := repoTable(allColumns(), repos, nil)
	tw.SetOutputMirror(Out)

	Println()
	tw.Render()
	Printf("\nTotal: %s repos\n", text.Bold.Sprint(len(repos)))
}

// PrintSummary renders the post-run summary: per-action counts, the
// list of failed repos, and the list of skipped dirty repos.
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
		tw.AppendRow(table.Row{text.FgCyan.Sprint(string(a)), n})
	}
	tw.SetColumnConfigs([]table.ColumnConfig{
		{Number: 2, Align: text.AlignRight},
	})
	tw.SetStyle(table.StyleLight)

	Println()
	tw.Render()
	Println()

	var failed, dirty, unusable []cloner.RepoResult
	for _, r := range summary.Results {
		switch r.Action {
		case cloner.ActionFailed:
			failed = append(failed, r)
		case cloner.ActionSkippedDirty:
			dirty = append(dirty, r)
		case cloner.ActionSkippedName:
			unusable = append(unusable, r)
		}
	}
	if len(failed) > 0 {
		Println(text.FgRed.Sprint(text.Bold.Sprint("Failed repos:")))
		for _, r := range failed {
			Printf("  %s: %s\n", r.Name, r.Detail)
		}
	}
	if len(dirty) > 0 {
		Println(text.FgYellow.Sprint(text.Bold.Sprint("Skipped (local changes):")))
		for _, r := range dirty {
			Printf("  %s\n", r.Name)
		}
	}
	if len(unusable) > 0 {
		Println(text.FgYellow.Sprint(text.Bold.Sprint("Skipped (unusable name):")))
		for _, r := range unusable {
			Printf("  %s: %s\n", r.Name, r.Detail)
		}
	}
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
