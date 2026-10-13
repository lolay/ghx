package ui

import (
	"fmt"
	"os"
	"time"

	"github.com/lolay/ghx/internal/cloner"
)

// progressEnv names the temporary variable that picks a progress renderer
// while the candidates are compared; the losers are deleted afterwards.
const progressEnv = "GHX_PROGRESS"

// NewProgress returns the Progress for a sync: cloner.NoProgress when the
// progress bar can't draw (a pipe, a file, a console without ANSI support:
// see SetupTerminal), otherwise the renderer GHX_PROGRESS names. Unset or an
// unknown name means "gopretty". It writes to Out as Out is now.
func NewProgress() cloner.Progress {
	if !animate {
		return cloner.NoProgress{}
	}
	switch os.Getenv(progressEnv) {
	case "buildx":
		return newBuildxProgress(Out)
	case "indicatif":
		return newIndicatifProgress(Out)
	default:
		return newGoPrettyProgress(Out)
	}
}

// formatSeconds shows d as tenths of a second ("1.2s"), or as minutes and
// seconds from a minute on ("1m15s").
func formatSeconds(d time.Duration) string {
	if d >= time.Minute {
		return d.Round(time.Second).String()
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}
