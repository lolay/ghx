package cloner

import "time"

// Actions RepoStarted reports: what CloneRepos is about to do to a repo.
const (
	// ProgressClone is the action for a repo that has no local clone yet.
	ProgressClone = "clone"
	// ProgressPull is the action for a repo that already has one.
	ProgressPull = "pull"
)

// Progress receives sync events from CloneRepos; implementations render them.
// CloneRepos calls Start once with the number of repos it will process, then
// RepoStarted and RepoDone from several goroutines at once, and Stop exactly
// once when it is finished, whether it completed or the context ended, so an
// implementation must be safe for concurrent use and must leave the terminal
// on a fresh line when Stop returns.
//
// RepoDone is called once per repo, with the repo's primary result (the first
// of its results: the wiki's, when CloneWiki adds one, is not reported on its
// own). A repo whose name is unusable is skipped before git runs, so its
// RepoDone arrives without a RepoStarted, and an implementation must accept
// that. A Progress may be reused: the run flow calls CloneRepos for the active
// and then the archived repos with one value, so Start follows an earlier Stop.
// After the context ends, repos not yet handed to a worker get neither.
type Progress interface {
	// Start announces a sync of total repos.
	Start(total int)
	// RepoStarted reports that work on the named repo began; action is
	// ProgressClone or ProgressPull.
	RepoStarted(name, action string)
	// RepoDone reports the primary result for a repo and how long it took.
	RepoDone(r RepoResult, elapsed time.Duration)
	// Stop ends the sync's display and returns once it is flushed.
	Stop()
}

// NoProgress is the Progress that shows nothing. CloneRepos uses it when
// Options.Progress is nil.
type NoProgress struct{}

// Start does nothing.
func (NoProgress) Start(int) {}

// RepoStarted does nothing.
func (NoProgress) RepoStarted(string, string) {}

// RepoDone does nothing.
func (NoProgress) RepoDone(RepoResult, time.Duration) {}

// Stop does nothing.
func (NoProgress) Stop() {}
