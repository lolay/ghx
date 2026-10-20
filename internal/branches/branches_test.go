package branches_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/lolay/ghx/internal/branches"
	"github.com/lolay/ghx/internal/ghapi"
)

var now = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

const staleAfter = 90 * 24 * time.Hour

func daysAgo(n int) time.Time { return now.Add(-time.Duration(n) * 24 * time.Hour) }

func merged(at time.Time, headOID string) *ghapi.MergedPR {
	return &ghapi.MergedPR{Number: 1, MergedAt: at, HeadOID: headOID}
}

func TestClassify(t *testing.T) {
	tests := []struct {
		name string
		b    ghapi.Branch
		want branches.Status
	}{
		{"merged, tip is the PR head", ghapi.Branch{CommitOID: "abc", CommittedAt: daysAgo(5), MergedPR: merged(daysAgo(4), "abc")}, branches.Merged},
		{"merged by squash, tip older than the merge", ghapi.Branch{CommitOID: "abc", CommittedAt: daysAgo(5), MergedPR: merged(daysAgo(4), "zzz")}, branches.Merged},
		{"merged, the tip's date is after the merge but it is the PR head", ghapi.Branch{CommitOID: "abc", CommittedAt: daysAgo(3), MergedPR: merged(daysAgo(4), "abc")}, branches.Merged},
		{"merged long ago is merged, not stale", ghapi.Branch{CommitOID: "abc", CommittedAt: daysAgo(400), MergedPR: merged(daysAgo(399), "abc")}, branches.Merged},
		{"merged, tip committed at the merge instant", ghapi.Branch{CommitOID: "abc", CommittedAt: daysAgo(4), MergedPR: merged(daysAgo(4), "zzz")}, branches.Merged},
		{"reused after the merge, recent commits", ghapi.Branch{CommitOID: "new", CommittedAt: daysAgo(2), MergedPR: merged(daysAgo(10), "old")}, branches.Active},
		{"reused after the merge, then left", ghapi.Branch{CommitOID: "new", CommittedAt: daysAgo(100), MergedPR: merged(daysAgo(200), "old")}, branches.Stale},
		{"reused after the merge, with an open PR", ghapi.Branch{CommitOID: "new", CommittedAt: daysAgo(2), OpenPRs: 1, MergedPR: merged(daysAgo(10), "old")}, branches.OpenPR},
		{"merged PR and an open one is open-pr", ghapi.Branch{CommitOID: "abc", CommittedAt: daysAgo(5), OpenPRs: 1, MergedPR: merged(daysAgo(4), "abc")}, branches.OpenPR},
		{"stale", ghapi.Branch{CommittedAt: daysAgo(91)}, branches.Stale},
		{"exactly at the threshold is not stale", ghapi.Branch{CommittedAt: daysAgo(90)}, branches.Active},
		{"old with an open PR is open-pr", ghapi.Branch{CommittedAt: daysAgo(400), OpenPRs: 2}, branches.OpenPR},
		{"open-pr", ghapi.Branch{CommittedAt: daysAgo(1), OpenPRs: 1}, branches.OpenPR},
		{"active", ghapi.Branch{CommittedAt: daysAgo(1)}, branches.Active},
		{"no commit date is never stale", ghapi.Branch{}, branches.Active},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, branches.Classify(tt.b, now, staleAfter))
		})
	}
}

func TestClassifyRepo(t *testing.T) {
	opts := branches.Options{Now: now, StaleAfter: staleAfter, ExpectedDefault: "main"}
	tests := []struct {
		name           string
		rb             ghapi.RepoBranches
		wantStatus     map[string]branches.Status
		wantCounts     branches.Counts
		wantUnexpected bool
	}{
		{
			name: "the default branch is never classified, even when it is old and merged",
			rb: ghapi.RepoBranches{DefaultBranch: "main", Branches: []ghapi.Branch{
				{Name: "main", CommittedAt: daysAgo(500), MergedPR: merged(daysAgo(499), "")},
				{Name: "gone", CommittedAt: daysAgo(500)},
				{Name: "pr", CommittedAt: daysAgo(1), OpenPRs: 1},
				{Name: "done", CommittedAt: daysAgo(3), MergedPR: merged(daysAgo(2), "")},
				{Name: "wip", CommittedAt: daysAgo(1)},
			}},
			wantStatus: map[string]branches.Status{
				"main": branches.Default, "gone": branches.Stale, "pr": branches.OpenPR, "done": branches.Merged, "wip": branches.Active,
			},
			wantCounts: branches.Counts{Branches: 4, Merged: 1, Stale: 1, OpenPR: 1, Active: 1},
		},
		{
			name:           "a default branch that isn't the expected name",
			rb:             ghapi.RepoBranches{DefaultBranch: "trunk", Branches: []ghapi.Branch{{Name: "trunk", CommittedAt: daysAgo(1)}}},
			wantStatus:     map[string]branches.Status{"trunk": branches.Default},
			wantUnexpected: true,
		},
		{
			name:       "an empty repository has no default to flag",
			rb:         ghapi.RepoBranches{},
			wantStatus: map[string]branches.Status{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := branches.ClassifyRepo("api", tt.rb, opts)

			status := map[string]branches.Status{}
			for _, b := range got.Branches {
				status[b.Name] = b.Status
			}
			assert.Equal(t, "api", got.Name)
			assert.Equal(t, tt.rb.DefaultBranch, got.DefaultBranch)
			assert.Equal(t, tt.wantStatus, status)
			assert.Equal(t, tt.wantCounts, got.Counts)
			assert.Equal(t, tt.wantUnexpected, got.UnexpectedDefault)
		})
	}
}

func TestTotal(t *testing.T) {
	repos := []branches.Repo{
		{Counts: branches.Counts{Branches: 4, Merged: 1, Stale: 1, OpenPR: 1, Active: 1}},
		{Counts: branches.Counts{Branches: 2, Merged: 2}, UnexpectedDefault: true},
		{},
	}
	assert.Equal(t, branches.Totals{
		Repos:             3,
		Counts:            branches.Counts{Branches: 6, Merged: 3, Stale: 1, OpenPR: 1, Active: 1},
		UnexpectedDefault: 1,
	}, branches.Total(repos))
}
