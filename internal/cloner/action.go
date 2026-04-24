// Package cloner handles concurrent cloning and pulling of repos, plus
// moving or deleting repo directories that have been removed or archived.
package cloner

// Action is the outcome of processing a single repo. The string values
// match the Python Action enum so the summary output is identical.
type Action string

const (
	ActionCloned          Action = "cloned"
	ActionPulled          Action = "pulled"
	ActionSkippedDirty    Action = "skipped (local changes)"
	ActionSkippedArchived Action = "skipped (archived)"
	ActionMovedDeleted    Action = "moved (deleted)"
	ActionMovedArchived   Action = "moved (archived)"
	ActionDeleted         Action = "deleted"
	ActionFailed          Action = "failed"
	ActionClonedWiki      Action = "cloned wiki"
	ActionSkippedWiki     Action = "skipped wiki (not found)"
	ActionSkippedTooLarge Action = "skipped (too large)"
)

// AllActions is used for stable iteration in the summary table.
var AllActions = []Action{
	ActionCloned,
	ActionPulled,
	ActionSkippedDirty,
	ActionSkippedArchived,
	ActionMovedDeleted,
	ActionMovedArchived,
	ActionDeleted,
	ActionFailed,
	ActionClonedWiki,
	ActionSkippedWiki,
	ActionSkippedTooLarge,
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
