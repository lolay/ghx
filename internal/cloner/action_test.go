package cloner_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lolay/ghx/internal/cloner"
)

func TestSyncSummary(t *testing.T) {
	var a cloner.SyncSummary
	a.Append(cloner.RepoResult{Name: "one", Action: cloner.ActionCloned})
	a.Append(cloner.RepoResult{Name: "two", Action: cloner.ActionPulled})

	var b cloner.SyncSummary
	b.Append(cloner.RepoResult{Name: "three", Action: cloner.ActionCloned})

	a.Extend(b)

	assert.Len(t, a.Results, 3)
	assert.Equal(t, 2, a.Count(cloner.ActionCloned))
	assert.Equal(t, 1, a.Count(cloner.ActionPulled))
	assert.Equal(t, 0, a.Count(cloner.ActionFailed))
}

func TestAllActionsListsEveryAction(t *testing.T) {
	assert.Len(t, cloner.AllActions, 18)
	assert.Contains(t, cloner.AllActions, cloner.ActionUpToDate)
	assert.Contains(t, cloner.AllActions, cloner.ActionSkippedTooLarge)
	assert.Contains(t, cloner.AllActions, cloner.ActionSkippedName)
	assert.Contains(t, cloner.AllActions, cloner.ActionFetchedOnly)
	assert.Contains(t, cloner.AllActions, cloner.ActionSkippedEmpty)
	assert.Contains(t, cloner.AllActions, cloner.ActionFailedWiki)
	assert.Contains(t, cloner.AllActions, cloner.ActionMovedUnarchived)
	assert.Contains(t, cloner.AllActions, cloner.ActionSkippedOrigin)
	assert.Contains(t, cloner.AllActions, cloner.ActionConflict)
}
