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

	"github.com/garyrudolph/ghx/internal/cloner"
	"github.com/garyrudolph/ghx/internal/ghapi"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
)

// Out is the writer used for all UI output. Tests can override it.
var Out io.Writer = os.Stdout

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
	fmt.Fprintln(Out)
	fmt.Fprintln(Out, text.Bold.Sprint("Settings:"))
	for _, r := range rows {
		padding := maxVal - len(r.Value) + 2
		fmt.Fprintf(Out,
			"  %-15s%s%s%s\n",
			r.Label, r.Value, spaces(padding),
			text.Faint.Sprintf("(%s)", r.Source),
		)
	}
	fmt.Fprintln(Out)
}

// ObfuscateToken returns a short redacted form of tok for display
// (first and last 4 chars, or "****" if too short).
func ObfuscateToken(tok string) string {
	if len(tok) <= 8 {
		return "****"
	}
	return tok[:4] + "****" + tok[len(tok)-4:]
}

// PrintRepoTable renders the "Repos to export" table shown before
// cloning begins.
func PrintRepoTable(repos []ghapi.RepoInfo) {
	tw := table.NewWriter()
	tw.SetOutputMirror(Out)
	tw.SetTitle("Repos to export")
	tw.AppendHeader(table.Row{
		"Name", "Visibility", "Language", "Size (MB)",
		"Default Branch", "Archived", "Last Pushed",
	})
	for _, r := range repos {
		visibility := "public"
		if r.Private {
			visibility = "private"
		}
		language := r.Language
		if language == "" {
			language = "-"
		}
		pushed := r.PushedAt
		if pushed == "" {
			pushed = "-"
		}
		archived := "no"
		if r.Archived {
			archived = "yes"
		}
		tw.AppendRow(table.Row{
			text.FgCyan.Sprint(r.Name),
			visibility,
			language,
			fmt.Sprintf("%.1f", float64(r.SizeKB)/1024),
			r.DefaultBranch,
			archived,
			pushed,
		})
	}
	tw.SetColumnConfigs([]table.ColumnConfig{
		{Number: 4, Align: text.AlignRight},
	})
	tw.SetStyle(table.StyleLight)

	fmt.Fprintln(Out)
	tw.Render()
	fmt.Fprintf(Out, "\nTotal: %s repos\n", text.Bold.Sprint(len(repos)))
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

	fmt.Fprintln(Out)
	tw.Render()
	fmt.Fprintln(Out)

	var failed, dirty []cloner.RepoResult
	for _, r := range summary.Results {
		switch r.Action {
		case cloner.ActionFailed:
			failed = append(failed, r)
		case cloner.ActionSkippedDirty:
			dirty = append(dirty, r)
		}
	}
	if len(failed) > 0 {
		fmt.Fprintln(Out, text.FgRed.Sprint(text.Bold.Sprint("Failed repos:")))
		for _, r := range failed {
			fmt.Fprintf(Out, "  %s: %s\n", r.Name, r.Detail)
		}
	}
	if len(dirty) > 0 {
		fmt.Fprintln(Out, text.FgYellow.Sprint(text.Bold.Sprint("Skipped (local changes):")))
		for _, r := range dirty {
			fmt.Fprintf(Out, "  %s\n", r.Name)
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
