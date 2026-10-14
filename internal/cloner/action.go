// Package cloner handles concurrent cloning and pulling of repos, plus
// moving or deleting repo directories that have been removed or archived.
package cloner

// Action is the outcome of processing a single repo. Most string values
// match the Python Action enum. The Go port has since diverged: it added
// ActionSkippedName, and the fetch-first sync replaced "skipped (local
// changes)" with ActionFetchedOnly, added ActionSkippedEmpty and
// ActionFailedWiki, and split ActionUpToDate out of ActionPulled. Reconciling
// clones with their repo's archived state added ActionMovedUnarchived,
// ActionSkippedOrigin and ActionConflict.
type Action string

const (
	ActionCloned Action = "cloned"
	// ActionPulled is an existing clone whose working tree moved to new
	// commits; ActionUpToDate is one that already had them.
	ActionPulled   Action = "pulled"
	ActionUpToDate Action = "up to date"
	// ActionFetchedOnly is a repo whose remote refs were brought up to date
	// but whose working tree was left alone, because moving it could lose
	// local work (or git refused to); Detail says why.
	ActionFetchedOnly     Action = "fetched only"
	ActionSkippedArchived Action = "skipped (archived)"
	ActionMovedDeleted    Action = "moved (deleted)"
	ActionMovedArchived   Action = "moved (archived)"
	// ActionMovedUnarchived is a clone moved back out of the archived
	// directory because its repo is no longer archived.
	ActionMovedUnarchived Action = "moved (unarchived)"
	// ActionSkippedOrigin is a directory where a clone of the repo would be
	// moved or deleted from, left alone because its origin URL doesn't name
	// the repo (or it has none); Detail gives the path and the URL found.
	ActionSkippedOrigin Action = "skipped (origin mismatch)"
	// ActionConflict is a clone found both where its repo belongs and where
	// it would be moved from; both are left in place and Detail names them.
	ActionConflict        Action = "conflict"
	ActionDeleted         Action = "deleted"
	ActionFailed          Action = "failed"
	ActionClonedWiki      Action = "cloned wiki"
	ActionSkippedWiki     Action = "skipped wiki (not found)"
	ActionSkippedTooLarge Action = "skipped (too large)"
	// ActionSkippedName is a repo whose name can't be a directory here (see
	// CheckRepoName); Detail says why.
	ActionSkippedName Action = "skipped (unusable name)"
	// ActionSkippedEmpty is a repo with no branches yet. A new clone of it is
	// removed, so the next run tries again.
	ActionSkippedEmpty Action = "skipped (empty)"
	// ActionFailedWiki is a wiki that exists but couldn't be cloned or synced;
	// Detail says why.
	ActionFailedWiki Action = "failed wiki"
)

// AllActions is used for stable iteration in the summary table.
var AllActions = []Action{
	ActionCloned,
	ActionPulled,
	ActionUpToDate,
	ActionFetchedOnly,
	ActionSkippedArchived,
	ActionMovedDeleted,
	ActionMovedArchived,
	ActionMovedUnarchived,
	ActionDeleted,
	ActionFailed,
	ActionClonedWiki,
	ActionSkippedWiki,
	ActionSkippedTooLarge,
	ActionSkippedName,
	ActionSkippedEmpty,
	ActionFailedWiki,
	ActionSkippedOrigin,
	ActionConflict,
}

// RepoResult records the outcome for a single repo (or its wiki).
type RepoResult struct {
	Name   string
	Action Action
	Detail string
}

// SyncSummary collects RepoResults across a sync run and provides a
// counter for each action value.
type SyncSummary struct {
	Results []RepoResult
}

// Count returns the number of results with the given action.
func (s *SyncSummary) Count(a Action) int {
	n := 0
	for _, r := range s.Results {
		if r.Action == a {
			n++
		}
	}
	return n
}

// Append adds a single result (helper used by the run flow).
func (s *SyncSummary) Append(r RepoResult) {
	s.Results = append(s.Results, r)
}

// Extend appends every result from another summary.
func (s *SyncSummary) Extend(other SyncSummary) {
	s.Results = append(s.Results, other.Results...)
}
