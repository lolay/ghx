package ui_test

import (
	"testing"
	"time"

	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/mattn/go-runewidth"
	"github.com/pterm/pterm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lolay/ghx/internal/cloner"
	"github.com/lolay/ghx/internal/ui"
)

// marks is the symbol and colour of every action. TestStatusMark_ChecksEveryAction
// walks cloner.AllActions against it, so an action added without a mark fails
// until it has a row here.
var marks = map[cloner.Action]struct {
	symbol string
	color  pterm.Color
}{
	cloner.ActionCloned:          {"✓", pterm.FgGreen},
	cloner.ActionPulled:          {"↓", pterm.FgGreen},
	cloner.ActionUpToDate:        {"–", pterm.FgGray},
	cloner.ActionFetchedOnly:     {"⇣", pterm.FgYellow},
	cloner.ActionSkippedArchived: {"⚠", pterm.FgYellow},
	cloner.ActionMovedDeleted:    {"✓", pterm.FgGreen},
	cloner.ActionMovedArchived:   {"✓", pterm.FgGreen},
	cloner.ActionDeleted:         {"✓", pterm.FgGreen},
	cloner.ActionFailed:          {"✗", pterm.FgRed},
	cloner.ActionClonedWiki:      {"✓", pterm.FgGreen},
	cloner.ActionSkippedWiki:     {"⚠", pterm.FgYellow},
	cloner.ActionSkippedTooLarge: {"⚠", pterm.FgYellow},
	cloner.ActionSkippedName:     {"⚠", pterm.FgYellow},
	cloner.ActionSkippedEmpty:    {"∅", pterm.FgGray},
	cloner.ActionFailedWiki:      {"✗", pterm.FgRed},
}

func TestStatusMark_ChecksEveryAction(t *testing.T) {
	for _, action := range cloner.AllActions {
		t.Run(string(action), func(t *testing.T) {
			want, ok := marks[action]
			require.True(t, ok, "%q has no mark in the test: give it one", action)
			symbol, color := ui.StatusMark(action)
			assert.Equal(t, want.symbol, symbol)
			assert.Equal(t, want.color, color)
		})
	}
	assert.Len(t, marks, len(cloner.AllActions), "the test lists an action that no longer exists")
}

func TestStatusMark_EveryMarkIsOneTerminalColumn(t *testing.T) {
	// The table's Status column is as wide as its longest cell, counted in
	// terminal columns, so a mark that drew two (an emoji presentation) would
	// push its row out of line.
	condition := runewidth.NewCondition()
	condition.EastAsianWidth = false
	for _, action := range cloner.AllActions {
		symbol, _ := ui.StatusMark(action)
		assert.Equal(t, 1, condition.StringWidth(symbol), "%q for %q", symbol, action)
		assert.Equal(t, 1, runewidth.StringWidth(symbol), "%q for %q under this locale", symbol, action)
		assert.Equal(t, 1, text.StringWidthWithoutEscSequences(symbol), "%q for %q as go-pretty counts it", symbol, action)
		assert.NotContains(t, symbol, "\ufe0f", "no emoji presentation selector")
	}
}

func TestStatusMark_TheSummaryAndTheTableAgree(t *testing.T) {
	buf := captureOut(t)
	results := []cloner.RepoResult{
		{Name: "a", Action: cloner.ActionFetchedOnly, Detail: "local changes"},
		{Name: "b", Action: cloner.ActionFailed, Detail: "boom"},
		{Name: "c", Action: cloner.ActionSkippedEmpty},
		{Name: "d", Action: cloner.ActionSkippedName, Detail: "reserved"},
	}

	ui.PrintSummary(cloner.SyncSummary{Results: results})

	out := buf.String()
	for _, r := range results {
		symbol, _ := ui.StatusMark(r.Action)
		assert.Contains(t, out, "  "+symbol+" "+r.Name, "the summary's line for %s uses the table's mark", r.Name)
	}
}

func TestActionText_ShowsTheTimeOnlyWhenGitRan(t *testing.T) {
	tests := []struct {
		name    string
		action  cloner.Action
		elapsed time.Duration
		want    string
	}{
		{"a success shows its time", cloner.ActionPulled, 1200 * time.Millisecond, "pulled 1.2s"},
		{"a failure shows its time", cloner.ActionFailed, 400 * time.Millisecond, "failed 0.4s"},
		{"up to date ran git, so it shows its time", cloner.ActionUpToDate, 300 * time.Millisecond, "up to date 0.3s"},
		{"fetched only ran git, so it shows its time", cloner.ActionFetchedOnly, 1200 * time.Millisecond, "fetched only 1.2s"},
		{"a skip has no time", cloner.ActionSkippedName, time.Second, string(cloner.ActionSkippedName)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ui.ActionText(tt.action, tt.elapsed))
		})
	}
}
