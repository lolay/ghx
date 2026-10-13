package ui

import (
	"fmt"
	"os"
	"time"

	"github.com/lolay/ghx/internal/cloner"
	"github.com/lolay/ghx/internal/ghapi"
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
	case "pterm":
		return newPtermProgress(Out)
	case "table":
		return newTableProgress(Out)
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

// RepoLister is implemented by a Progress that draws the "Repos to export"
// table itself, live, in place of the static table PrintRepoTable prints. The
// run flow gives it the repos before the first CloneRepos and asks it to print
// the table once the last pass is over.
type RepoLister interface {
	// SetRepos gives the repos the sync will cover, active and archived, in
	// the order the table lists them. It starts a new table: every repo is
	// queued.
	SetRepos(repos []ghapi.RepoInfo)
	// PrintTable prints the table with each repo's final status as ordinary
	// output, unless SetRepos has not been called, a live area is still on
	// screen, or it was already printed since the last SetRepos. Calling it
	// more than once is harmless.
	PrintTable()
}

// progressVerb is what RepoStarted's action looks like on a live line.
func progressVerb(action string) string {
	switch action {
	case cloner.ProgressClone:
		return "cloning"
	case cloner.ProgressPull:
		return "pulling"
	}
	return action
}
