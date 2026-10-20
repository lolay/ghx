package ui

import (
	"fmt"
	"time"

	"github.com/lolay/ghx/internal/cloner"
	"github.com/lolay/ghx/internal/ghapi"
)

// NewProgress returns the Progress for a sync: cloner.NoProgress when nothing
// can draw (a pipe, a file, a console without ANSI support: see
// SetupTerminal), otherwise the live repo table, which is also a RepoLister.
// It writes to Out as Out is now.
func NewProgress() cloner.Progress {
	if !animate {
		return cloner.NoProgress{}
	}
	return newTableProgress(syncSpec, Out)
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
// table itself, growing as the sync runs, in place of the static table
// PrintRepoTable prints. The run flow gives it the repos before the first
// CloneRepos and closes the table once the last pass is over. The table is
// one piece across the passes, so the run flow prints nothing of its own
// between them.
type RepoLister interface {
	// SetRepos gives the repos the sync will cover, active and archived, in
	// the order the table lists them. It starts a new table: every repo is
	// queued.
	SetRepos(repos []ghapi.RepoInfo)
	// FinishTable closes the table: a row for each repo that never finished
	// (queued, or interrupted), then the bottom border and a footer with the
	// totals. It does nothing when SetRepos has not been called, a live area
	// is still on screen, or the table was already closed since the last
	// SetRepos, so calling it again is harmless.
	FinishTable()
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
