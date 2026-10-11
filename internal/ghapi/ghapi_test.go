package ghapi_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/go-github/v67/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lolay/ghx/internal/ghapi"
	"github.com/lolay/ghx/internal/ghapi/ghapitest"
)

const testToken = "ghp_test_token"

// newAPI starts a fake GitHub and points the client at it. Every test goes
// through here, so none can reach api.github.com.
func newAPI(t *testing.T) *ghapitest.Server {
	t.Helper()
	srv := ghapitest.NewServer(t, testToken, "octocat")
	require.True(t, strings.HasPrefix(srv.URL(), "http://127.0.0.1:"), "the fake must be on loopback: %s", srv.URL())
	ghapi.SetBaseURL(t, srv.URL())
	return srv
}

func TestValidateToken(t *testing.T) {
	t.Run("returns the login for a valid token", func(t *testing.T) {
		srv := newAPI(t)

		login, err := ghapi.ValidateToken(t.Context(), testToken)
		require.NoError(t, err)
		assert.Equal(t, "octocat", login)

		reqs := srv.Requests()
		require.Len(t, reqs, 1)
		assert.Equal(t, "/user", reqs[0].Path)
		assert.Equal(t, "Bearer "+testToken, reqs[0].Authorization)
	})

	t.Run("reports a rejected token with its status", func(t *testing.T) {
		newAPI(t)

		login, err := ghapi.ValidateToken(t.Context(), "wrong")
		require.Error(t, err)
		assert.Empty(t, login)
		assert.Equal(t, "Token validation failed (401): Bad credentials", err.Error())
	})

	t.Run("reports a network failure without a status", func(t *testing.T) {
		srv := newAPI(t)
		srv.Close()

		_, err := ghapi.ValidateToken(t.Context(), testToken)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Token validation failed: ")
		var urlErr *url.Error
		assert.ErrorAs(t, err, &urlErr, "the cause stays reachable through the wrapped error")
	})

	t.Run("rejects a response with no login", func(t *testing.T) {
		stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{}`))
		}))
		t.Cleanup(stub.Close)
		ghapi.SetBaseURL(t, stub.URL)

		_, err := ghapi.ValidateToken(t.Context(), testToken)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "empty login")
	})
}

func TestListOrgRepos(t *testing.T) {
	t.Run("follows pagination across two pages", func(t *testing.T) {
		srv := newAPI(t)
		srv.SetPageSize(2)
		srv.SetRepos("acme", []ghapitest.Repo{
			{Name: "one"}, {Name: "two"}, {Name: "three"},
		})

		repos, err := ghapi.ListOrgRepos(t.Context(), testToken, "acme")
		require.NoError(t, err)

		var names []string
		for _, r := range repos {
			names = append(names, r.Name)
		}
		assert.Equal(t, []string{"one", "two", "three"}, names)

		var pages []string
		for _, req := range srv.Requests() {
			if req.Path == "/orgs/acme/repos" {
				pages = append(pages, req.Query.Get("page"))
			}
		}
		assert.Equal(t, []string{"", "2"}, pages, "one request per page, the second asking for page 2")
	})

	t.Run("asks for every repo type and the largest page", func(t *testing.T) {
		srv := newAPI(t)
		srv.SetRepos("acme", []ghapitest.Repo{{Name: "one"}})

		_, err := ghapi.ListOrgRepos(t.Context(), testToken, "acme")
		require.NoError(t, err)

		reqs := srv.Requests()
		require.Len(t, reqs, 1)
		assert.Equal(t, "all", reqs[0].Query.Get("type"), "type filtering happens in FilterRepos, not on the wire")
		assert.Equal(t, "100", reqs[0].Query.Get("per_page"))
		assert.Equal(t, "Bearer "+testToken, reqs[0].Authorization)
	})

	t.Run("maps the repository fields", func(t *testing.T) {
		srv := newAPI(t)
		srv.SetRepos("acme", []ghapitest.Repo{{
			Name:          "app",
			CloneURL:      "https://github.com/acme/app.git",
			SSHURL:        "git@github.com:acme/app.git",
			DefaultBranch: "trunk",
			Archived:      true,
			Private:       true,
			Fork:          true,
			Size:          2048,
			Language:      "Go",
			HasWiki:       true,
			PushedAt:      "2026-03-04T05:06:07Z",
		}})

		repos, err := ghapi.ListOrgRepos(t.Context(), testToken, "acme")
		require.NoError(t, err)
		require.Len(t, repos, 1)
		assert.Equal(t, ghapi.RepoInfo{
			Name:          "app",
			CloneURL:      "https://github.com/acme/app.git",
			SSHURL:        "git@github.com:acme/app.git",
			DefaultBranch: "trunk",
			Archived:      true,
			Private:       true,
			Fork:          true,
			SizeKB:        2048,
			Language:      "Go",
			HasWiki:       true,
			PushedAt:      "2026-03-04T05:06:07Z",
		}, repos[0])
	})

	t.Run("returns an empty list for an org with no repos", func(t *testing.T) {
		srv := newAPI(t)
		srv.SetRepos("acme", nil)

		repos, err := ghapi.ListOrgRepos(t.Context(), testToken, "acme")
		require.NoError(t, err)
		assert.Empty(t, repos)
	})

	t.Run("names the org when it cannot be read", func(t *testing.T) {
		newAPI(t)

		_, err := ghapi.ListOrgRepos(t.Context(), testToken, "ghost")
		require.Error(t, err)
		assert.Equal(t, `Failed to access organization "ghost" (404): Not Found`, err.Error())
	})

	t.Run("reports a rejected token", func(t *testing.T) {
		newAPI(t)

		_, err := ghapi.ListOrgRepos(t.Context(), "wrong", "acme")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "(401)")
	})

	t.Run("stops on a failure partway through the pages", func(t *testing.T) {
		calls := 0
		stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			if calls == 1 {
				w.Header().Set("Link", fmt.Sprintf(`<http://%s/orgs/acme/repos?page=2>; rel="next"`, r.Host))
				_, _ = w.Write([]byte(`[{"name":"one"}]`))
				return
			}
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"boom"}`))
		}))
		t.Cleanup(stub.Close)
		ghapi.SetBaseURL(t, stub.URL)

		repos, err := ghapi.ListOrgRepos(t.Context(), testToken, "acme")
		require.Error(t, err)
		assert.Nil(t, repos, "a partial list is never returned")
		assert.Contains(t, err.Error(), "(500): boom")
	})
}

func TestToRepoInfo(t *testing.T) {
	str := func(s string) *string { return &s }
	flag := func(b bool) *bool { return &b }
	num := func(n int) *int { return &n }
	// A zone other than UTC proves the timestamp is normalised.
	zone := time.FixedZone("UTC+2", 2*60*60)

	tests := []struct {
		name string
		in   *github.Repository
		want ghapi.RepoInfo
	}{
		{
			"an empty repository has no fields and defaults to main",
			&github.Repository{},
			ghapi.RepoInfo{DefaultBranch: "main"},
		},
		{
			"every field is copied",
			&github.Repository{
				Name: str("app"), CloneURL: str("https://x/app.git"), SSHURL: str("git@x:app.git"),
				DefaultBranch: str("trunk"), Archived: flag(true), Private: flag(true), Fork: flag(true),
				Size: num(7), Language: str("Go"), HasWiki: flag(true),
				PushedAt: &github.Timestamp{Time: time.Date(2026, 1, 2, 5, 4, 5, 0, zone)},
			},
			ghapi.RepoInfo{
				Name: "app", CloneURL: "https://x/app.git", SSHURL: "git@x:app.git",
				DefaultBranch: "trunk", Archived: true, Private: true, Fork: true,
				SizeKB: 7, Language: "Go", HasWiki: true, PushedAt: "2026-01-02T03:04:05Z",
			},
		},
		{
			"a zero push time stays empty",
			&github.Repository{Name: str("app"), PushedAt: &github.Timestamp{}},
			ghapi.RepoInfo{Name: "app", DefaultBranch: "main"},
		},
		{
			"an empty default branch falls back to main",
			&github.Repository{Name: str("app"), DefaultBranch: str("")},
			ghapi.RepoInfo{Name: "app", DefaultBranch: "main"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ghapi.ToRepoInfo(tt.in))
		})
	}
}

func TestFormatAPIError(t *testing.T) {
	withStatus := func(status int, message string) error {
		return &github.ErrorResponse{
			Response: &http.Response{StatusCode: status},
			Message:  message,
		}
	}
	plain := errors.New("connection refused")

	tests := []struct {
		name string
		err  error
		want string
	}{
		{"a github error shows its status and message", withStatus(404, "Not Found"), "Prefix (404): Not Found"},
		{
			"an empty message falls back to the error text",
			withStatus(403, ""), "Prefix (403): " + withStatus(403, "").Error(),
		},
		{
			"a github error with no response shows status 0",
			&github.ErrorResponse{Message: "odd"}, "Prefix (0): odd",
		},
		{"any other error is wrapped as is", plain, "Prefix: connection refused"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ghapi.FormatAPIError("Prefix", tt.err, testToken)
			assert.Equal(t, tt.want, got.Error())
		})
	}

	t.Run("a wrapped plain error stays reachable", func(t *testing.T) {
		assert.ErrorIs(t, ghapi.FormatAPIError("Prefix", plain, testToken), plain)
	})

	t.Run("the token is masked wherever it appears", func(t *testing.T) {
		echo := ghapi.FormatAPIError("Prefix", withStatus(401, "Bad credentials: "+testToken), testToken)
		assert.Equal(t, "Prefix (401): Bad credentials: ***", echo.Error())

		cause := fmt.Errorf("dial https://%s@proxy.example: refused", testToken)
		plainEcho := ghapi.FormatAPIError("Prefix", cause, testToken)
		assert.Equal(t, "Prefix: dial https://***@proxy.example: refused", plainEcho.Error())
		assert.ErrorIs(t, plainEcho, cause)
	})
}

func TestValidateToken_NeverEchoesTheToken(t *testing.T) {
	// A server that quotes the credential back, as a misbehaving proxy might.
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprintf(w, `{"message":"rejected %s"}`, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	}))
	t.Cleanup(stub.Close)
	ghapi.SetBaseURL(t, stub.URL)

	_, err := ghapi.ValidateToken(t.Context(), testToken)

	require.Error(t, err)
	assert.NotContains(t, err.Error(), testToken)
	assert.Contains(t, err.Error(), "rejected ***")
}
