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
	assert.Len(t, cloner.AllActions, 11)
	assert.Contains(t, cloner.AllActions, cloner.ActionSkippedTooLarge)
}
