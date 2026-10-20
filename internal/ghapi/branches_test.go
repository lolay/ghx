package ghapi_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lolay/ghx/internal/ghapi"
	"github.com/lolay/ghx/internal/ghapi/ghapitest"
)

func TestListBranches(t *testing.T) {
	t.Run("reads every field of a branch", func(t *testing.T) {
		srv := newAPI(t)
		srv.SetBranches("acme", "api", "main", []ghapitest.Branch{
			{Name: "main", OID: "a1", CommittedDate: "2026-10-01T12:00:00Z", AuthorName: "Ada", AuthorLogin: "ada", Protected: true},
			{
				Name: "feature", OID: "b2", CommittedDate: "2026-09-01T08:30:00Z", AuthorName: "Bob",
				OpenPRs: 2, Merged: &ghapitest.MergedPR{Number: 7, MergedAt: "2026-09-02T00:00:00Z", HeadRefOID: "b1"},
			},
		})

		got, err := ghapi.ListBranches(t.Context(), testToken, "acme", "api", ghapi.BranchOptions{})
		require.NoError(t, err)

		assert.Equal(t, ghapi.RepoBranches{
			DefaultBranch: "main",
			Branches: []ghapi.Branch{
				{
					Name: "main", CommitOID: "a1", CommittedAt: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC),
					AuthorName: "Ada", AuthorLogin: "ada", Protected: true,
				},
				{
					Name: "feature", CommitOID: "b2", CommittedAt: time.Date(2026, 9, 1, 8, 30, 0, 0, time.UTC),
					AuthorName: "Bob", OpenPRs: 2,
					MergedPR: &ghapi.MergedPR{Number: 7, MergedAt: time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC), HeadOID: "b1"},
				},
			},
		}, normalize(got))
	})

	tests := []struct {
		name          string
		defaultBranch string
		count         int
		wantGraphQL   int
	}{
		{"one page", "main", 3, 1},
		{"exactly one full page", "main", 100, 1},
		{"pages past 100 branches", "main", 250, 3},
		{"a default branch that isn't main", "trunk", 2, 1},
		{"an empty repository", "", 0, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newAPI(t)
			srv.SetBranches("acme", "api", tt.defaultBranch, numbered(tt.count))
			var pages []int

			got, err := ghapi.ListBranches(t.Context(), testToken, "acme", "api", ghapi.BranchOptions{OnPage: func(p int) { pages = append(pages, p) }})
			require.NoError(t, err)

			assert.Equal(t, tt.defaultBranch, got.DefaultBranch)
			require.Len(t, got.Branches, tt.count)
			for i, b := range got.Branches {
				assert.Equal(t, fmt.Sprintf("b%03d", i), b.Name, "in order, none lost or repeated")
			}
			assert.Len(t, pages, tt.wantGraphQL)
			assert.Equal(t, tt.wantGraphQL, countPath(srv, "/graphql"))
			assert.Equal(t, 1, countPath(srv, "/repos/acme/api/branches"), "protection is one REST listing")
		})
	}
}

func TestListBranches_MarksOnlyTheProtectedBranches(t *testing.T) {
	srv := newAPI(t)
	srv.SetPageSize(2)
	srv.SetBranches("acme", "api", "main", []ghapitest.Branch{
		{Name: "main", Protected: true}, {Name: "release", Protected: true}, {Name: "topic"}, {Name: "v1", Protected: true},
	})

	got, err := ghapi.ListBranches(t.Context(), testToken, "acme", "api", ghapi.BranchOptions{})
	require.NoError(t, err)

	protected := map[string]bool{}
	for _, b := range got.Branches {
		protected[b.Name] = b.Protected
	}
	assert.Equal(t, map[string]bool{"main": true, "release": true, "topic": false, "v1": true}, protected)
	assert.Equal(t, 2, countPath(srv, "/repos/acme/api/branches"), "the protected listing pages too")
}

func TestListBranches_RateLimits(t *testing.T) {
	tests := []struct {
		name    string
		failure ghapitest.Failure
		want    ghapi.RateLimitError
		wantMsg string
	}{
		{
			"secondary limit with Retry-After", ghapitest.SecondaryLimit,
			ghapi.RateLimitError{Secondary: true, RetryAfter: time.Minute},
			"Failed to list branches of acme/api: GitHub secondary rate limit hit; retry after 1m0s",
		},
		{
			"primary limit over REST", ghapitest.PrimaryLimit,
			ghapi.RateLimitError{Reset: ghapitest.RateLimitReset},
			"Failed to list branches of acme/api: GitHub rate limit hit; it resets at 14:05 UTC",
		},
		{
			"GraphQL RATE_LIMITED", ghapitest.RateLimited,
			ghapi.RateLimitError{Reset: ghapitest.RateLimitReset},
			"Failed to list branches of acme/api: GitHub rate limit hit; it resets at 14:05 UTC",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newAPI(t)
			srv.SetBranches("acme", "api", "main", numbered(3))
			srv.Fail("acme", "api", tt.failure, 0)

			got, err := ghapi.ListBranches(t.Context(), testToken, "acme", "api", ghapi.BranchOptions{})
			require.Error(t, err)
			assert.Empty(t, got.Branches, "no partial result")
			assert.ErrorIs(t, err, ghapi.ErrRateLimited)
			var rl *ghapi.RateLimitError
			require.ErrorAs(t, err, &rl)
			assert.Equal(t, tt.want.Secondary, rl.Secondary)
			assert.Equal(t, tt.want.RetryAfter, rl.RetryAfter)
			assert.True(t, tt.want.Reset.Equal(rl.Reset), "reset %v, want %v", rl.Reset, tt.want.Reset)
			assert.Equal(t, tt.wantMsg, err.Error())
		})
	}
}

func TestRateLimitError_WithNothingToGoOn(t *testing.T) {
	assert.Equal(t, "GitHub rate limit hit; try again later", (&ghapi.RateLimitError{}).Error())
}

func TestListBranches_Errors(t *testing.T) {
	t.Run("an unknown repository names it", func(t *testing.T) {
		newAPI(t)

		_, err := ghapi.ListBranches(t.Context(), testToken, "acme", "gone", ghapi.BranchOptions{})
		require.Error(t, err)
		assert.Equal(t, "Failed to list branches of acme/gone: Could not resolve to a Repository with the name 'acme/gone'.", err.Error())
		assert.NotErrorIs(t, err, ghapi.ErrRateLimited)
	})

	t.Run("a rejected token never shows the token", func(t *testing.T) {
		newAPI(t)
		const secret = "ghp_secret_never_shown"

		_, err := ghapi.ListBranches(t.Context(), secret, "acme", "api", ghapi.BranchOptions{})
		require.Error(t, err)
		assert.Equal(t, "Failed to list branches of acme/api (401): Bad credentials", err.Error())
		assert.NotContains(t, err.Error(), secret)
	})

	t.Run("a cancelled context stays a cancellation", func(t *testing.T) {
		srv := newAPI(t)
		srv.SetBranches("acme", "api", "main", numbered(1))
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		_, err := ghapi.ListBranches(ctx, testToken, "acme", "api", ghapi.BranchOptions{})
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})
}

// numbered returns n branches named b000, b001, ... in order.
func numbered(n int) []ghapitest.Branch {
	out := make([]ghapitest.Branch, n)
	for i := range out {
		out[i] = ghapitest.Branch{Name: fmt.Sprintf("b%03d", i), OID: fmt.Sprintf("%040d", i), CommittedDate: "2026-01-01T00:00:00Z"}
	}
	return out
}

func countPath(srv *ghapitest.Server, path string) int {
	n := 0
	for _, r := range srv.Requests() {
		if r.Path == path {
			n++
		}
	}
	return n
}

// normalize puts every time in UTC, so assert.Equal compares instants the
// same way whatever zone the JSON decoder chose.
func normalize(rb ghapi.RepoBranches) ghapi.RepoBranches {
	for i, b := range rb.Branches {
		rb.Branches[i].CommittedAt = b.CommittedAt.UTC()
		if b.MergedPR != nil {
			m := *b.MergedPR
			m.MergedAt = m.MergedAt.UTC()
			rb.Branches[i].MergedPR = &m
		}
	}
	return rb
}
