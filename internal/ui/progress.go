package ui

import (
	"os"

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
	default:
		return newGoPrettyProgress(Out)
	}
}
