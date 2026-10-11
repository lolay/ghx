package ghapi_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lolay/ghx/internal/ghapi"
)

// sampleRepos covers each combination of private and fork.
var sampleRepos = []ghapi.RepoInfo{
	{Name: "api-server"},
	{Name: "web-app", Private: true},
	{Name: "forked-api", Fork: true},
	{Name: "private-fork", Private: true, Fork: true},
}

func names(repos []ghapi.RepoInfo) []string {
	out := make([]string, 0, len(repos))
	for _, r := range repos {
		out = append(out, r.Name)
	}
	return out
}

func TestFilterRepos(t *testing.T) {
	tests := []struct {
		name     string
		repoType string
		include  string
		exclude  string
		want     []string
	}{
		{"all keeps every repo", "all", "", "", []string{"api-server", "web-app", "forked-api", "private-fork"}},
		{"empty type is all", "", "", "", []string{"api-server", "web-app", "forked-api", "private-fork"}},
		{"unknown type is all", "bogus", "", "", []string{"api-server", "web-app", "forked-api", "private-fork"}},
		{"public", "public", "", "", []string{"api-server", "forked-api"}},
		{"private", "private", "", "", []string{"web-app", "private-fork"}},
		{"forks", "forks", "", "", []string{"forked-api", "private-fork"}},
		{"sources", "sources", "", "", []string{"api-server", "web-app"}},
		{"include is a substring match", "all", "api", "", []string{"api-server", "forked-api"}},
		{"include honors anchors", "all", "^api", "", []string{"api-server"}},
		{"exclude removes matches", "all", "", "fork", []string{"api-server", "web-app"}},
		{"include then exclude", "all", "api", "^forked", []string{"api-server"}},
		{"type, include and exclude together", "private", "-", "app", []string{"private-fork"}},
		{"include matching nothing", "all", "nomatch", "", []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ghapi.FilterRepos(sampleRepos, tt.repoType, tt.include, tt.exclude)
			require.NoError(t, err)
			assert.Equal(t, tt.want, names(got))
		})
	}
}

func TestFilterRepos_EmptyInputReturnsEmptySlice(t *testing.T) {
	got, err := ghapi.FilterRepos(nil, "all", "", "")
	require.NoError(t, err)
	assert.NotNil(t, got)
	assert.Empty(t, got)
}

func TestFilterRepos_RejectsBadRegex(t *testing.T) {
	tests := []struct {
		name    string
		include string
		exclude string
		wantErr string
	}{
		{"bad include", "(", "", "invalid --include regex"},
		{"bad exclude", "", "[a-", "invalid --exclude regex"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ghapi.FilterRepos(sampleRepos, "all", tt.include, tt.exclude)
			require.ErrorContains(t, err, tt.wantErr)
			assert.Nil(t, got)
		})
	}
}
