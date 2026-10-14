package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/pterm/pterm"

	"github.com/lolay/ghx/internal/cloner"
)

// statusMark is the symbol and colour that mark a finished repo. The symbols
// read as how much of the repo came down: a green tick for a clone (or a move),
// a green down arrow for a pull that brought in commits, a grey dash for a
// clone that was already up to date, and a yellow dashed down arrow for a
// clone that was only fetched, so it is partly downloaded. An action the
// summary marks (glyphs) keeps that glyph: a red cross for a failure, a faint
// empty set for an empty repo, a yellow warning sign for a name that can't be
// used. Any other skip gets the warning sign too. The pterm and table
// renderers share it, so they look alike, and every symbol is one terminal
// column wide: the table's fixed Status width counts on it.
func statusMark(action cloner.Action) (symbol string, color pterm.Color) {
	g, marked := glyphs[action]
	switch {
	case action == cloner.ActionPulled:
		return "↓", pterm.FgGreen
	case action == cloner.ActionUpToDate:
		return "–", pterm.FgGray
	case marked && strings.HasPrefix(string(action), "failed"):
		return g, pterm.FgRed
	case action == cloner.ActionSkippedEmpty:
		return g, pterm.FgGray
	case marked:
		return g, pterm.FgYellow
	case strings.HasPrefix(string(action), "skipped"):
		return "⚠", pterm.FgYellow
	default:
		return "✓", pterm.FgGreen
	}
}

// actionText is what a finished repo's line says after its name: the action
// and, when git ran, how long it took (an up to date clone fetched, so it has
// one). A skipped repo never ran git, so it has no time.
func actionText(action cloner.Action, elapsed time.Duration) string {
	if strings.HasPrefix(string(action), "skipped") {
		return string(action)
	}
	return fmt.Sprintf("%s %s", action, formatSeconds(elapsed))
}
