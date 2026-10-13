package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/pterm/pterm"

	"github.com/lolay/ghx/internal/cloner"
)

// statusMark is the symbol and colour that mark a finished repo. An action the
// summary marks (glyphs) keeps that glyph: a red cross for a failure, a yellow
// warning sign for a clone fetched without updating its working tree or a name
// that can't be used, a faint empty set for an empty repo. Any other skip gets
// the warning sign, and everything else (cloned, pulled, moved) a green tick.
// The pterm and table renderers share it, so they look alike.
func statusMark(action cloner.Action) (symbol string, color pterm.Color) {
	g, marked := glyphs[action]
	switch {
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
// and, when git ran, how long it took. A skipped repo never ran git, so it has
// no time.
func actionText(action cloner.Action, elapsed time.Duration) string {
	if strings.HasPrefix(string(action), "skipped") {
		return string(action)
	}
	return fmt.Sprintf("%s %s", action, formatSeconds(elapsed))
}
