package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/pterm/pterm"

	"github.com/lolay/ghx/internal/cloner"
)

// statusMark is the symbol and colour that mark a finished repo: a green tick
// for a success (cloned, pulled, moved), a yellow warning sign for any
// "skipped (...)" action, and a red cross for a failure. The pterm and table
// renderers share it, so the two look alike.
func statusMark(action cloner.Action) (symbol string, color pterm.Color) {
	switch {
	case action == cloner.ActionFailed:
		return "✗", pterm.FgRed
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
