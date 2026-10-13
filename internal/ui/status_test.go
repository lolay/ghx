package ui_test

import (
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pterm/pterm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lolay/ghx/internal/cloner"
	"github.com/lolay/ghx/internal/ui"
)

func TestStatusMark_ChoosesASymbolAndColourPerClassOfAction(t *testing.T) {
	tests := []struct {
		name   string
		action cloner.Action
		symbol string
		color  pterm.Color
	}{
		{"cloned is a green tick", cloner.ActionCloned, "✓", pterm.FgGreen},
		{"pulled is a green tick", cloner.ActionPulled, "✓", pterm.FgGreen},
		{"moved is a green tick", cloner.ActionMovedDeleted, "✓", pterm.FgGreen},
		{"fetched only is a yellow warning", cloner.ActionFetchedOnly, "⚠", pterm.FgYellow},
		{"skipped as too large is a yellow warning", cloner.ActionSkippedTooLarge, "⚠", pterm.FgYellow},
		{"an empty repo is a grey empty set", cloner.ActionSkippedEmpty, "∅", pterm.FgGray},
		{"a failed wiki is a red cross", cloner.ActionFailedWiki, "✗", pterm.FgRed},
		{"skipped for an unusable name is a yellow warning", cloner.ActionSkippedName, "⚠", pterm.FgYellow},
		{"failed is a red cross", cloner.ActionFailed, "✗", pterm.FgRed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			symbol, color := ui.StatusMark(tt.action)
			assert.Equal(t, tt.symbol, symbol)
			assert.Equal(t, tt.color, color)
		})
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
		{"fetched only ran git, so it shows its time", cloner.ActionFetchedOnly, 1200 * time.Millisecond, "fetched only 1.2s"},
		{"a skip has no time", cloner.ActionSkippedName, time.Second, string(cloner.ActionSkippedName)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ui.ActionText(tt.action, tt.elapsed))
		})
	}
}

// TestPtermProgress_LeadsEachFinishedRepoWithItsSymbol checks the lines the
// pterm renderer prints above its area: a symbol, then the name and action,
// and none of pterm's SUCCESS, WARNING or ERROR prefixes.
func TestPtermProgress_LeadsEachFinishedRepoWithItsSymbol(t *testing.T) {
	p, buf := useRenderer(t, "pterm")
	finished := []struct {
		name   string
		action cloner.Action
		symbol string
		line   string
	}{
		{"fresh", cloner.ActionCloned, "✓", "fresh cloned 1.2s"},
		{"dirty", cloner.ActionFetchedOnly, "⚠", "dirty fetched only 1.2s"},
		{"broken", cloner.ActionFailed, "✗", "broken failed 1.2s"},
	}

	within(t, func() {
		p.Start(len(finished))
		var wg sync.WaitGroup
		for _, f := range finished {
			wg.Add(1)
			go func() {
				defer wg.Done()
				p.RepoDone(cloner.RepoResult{Name: f.name, Action: f.action}, 1200*time.Millisecond)
			}()
		}
		wg.Wait()
		p.Stop()
	})

	out := stripANSI(buf.String())
	for _, f := range finished {
		assert.Regexp(t, f.symbol+`\s+`+regexp.QuoteMeta(f.line), out)
	}
	for _, prefix := range []string{"SUCCESS", "WARNING", "ERROR"} {
		assert.NotContains(t, out, prefix)
	}
	require.True(t, strings.HasSuffix(out, "\n"), "the cursor is left on a fresh line")
}
