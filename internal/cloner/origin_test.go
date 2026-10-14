package cloner_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lolay/ghx/internal/cloner"
	"github.com/lolay/ghx/internal/gittest"
)

func TestOriginRepoName(t *testing.T) {
	tests := []struct {
		name   string
		url    string
		want   string
		wantOK bool
	}{
		{"https", "https://github.com/acme/app.git", "app", true},
		{"https without .git", "https://github.com/acme/app", "app", true},
		{"https with user info", "https://user:ghp_tok@github.com/acme/app", "app", true},
		{"https with a token and .git", "https://ghp_tok@github.com/acme/app.git", "app", true},
		{"scp-style ssh", "git@github.com:acme/app.git", "app", true},
		{"scp-style ssh with no owner", "git@host:app.git", "app", true},
		{"ssh scheme with a trailing slash", "ssh://git@github.com/acme/app/", "app", true},
		{"ssh scheme with a port", "ssh://git@ghe.example.com:2222/acme/app.git", "app", true},
		{"a trailing slash after .git", "https://github.com/acme/app.git/", "app", true},
		{"several trailing slashes", "https://github.com/acme/app//", "app", true},
		{"case is kept", "https://github.com/Acme/App.GIT", "App", true},
		{"a wiki", "https://github.com/acme/app.wiki.git", "app.wiki", true},
		{"a posix path", "/srv/git/acme/app.git", "app", true},
		{"a windows path", `C:\Users\ada\upstream\app.git`, "app", true},
		{"a windows path with forward slashes", "C:/Users/ada/upstream/app.git", "app", true},
		{"surrounding whitespace", "  https://github.com/acme/app.git\n", "app", true},
		{"only .git dropped once", "https://github.com/acme/app.git.git", "app.git", true},
		{"empty", "", "", false},
		{"only separators", "/:\\", "", false},
		{"only .git", "https://github.com/acme/.git", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := cloner.OriginRepoName(tt.url)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDisplayURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{"a token is dropped", "https://ghp_tok@github.com/acme/app.git", "https://github.com/acme/app.git"},
		{"a user and password are dropped", "https://user:pw@github.com/acme/app", "https://github.com/acme/app"},
		{"a clean url is kept", "https://github.com/acme/app.git", "https://github.com/acme/app.git"},
		{"an scp-style address is kept", "git@github.com:acme/app.git", "git@github.com:acme/app.git"},
		{"a path is kept", "/srv/git/app.git", "/srv/git/app.git"},
		{"a windows path is kept", `C:\git\app.git`, `C:\git\app.git`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, cloner.DisplayURL(tt.url))
		})
	}
}

// initRepo makes an empty repository at dir, with origin set when it isn't "".
func initRepo(t *testing.T, dir, origin string) {
	t.Helper()
	gittest.Git(t, "", "init", "--quiet", dir)
	if origin != "" {
		gittest.Git(t, dir, "remote", "add", "origin", origin)
	}
}

func TestReadOrigin(t *testing.T) {
	path := gitPath(t)

	t.Run("returns the origin as the config holds it", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "app")
		initRepo(t, dir, "git@github.com:acme/app.git")

		got, err := cloner.ReadOrigin(t.Context(), path, dir)

		require.NoError(t, err)
		assert.Equal(t, "git@github.com:acme/app.git", got)
	})

	t.Run("an insteadOf rewrite is not applied", func(t *testing.T) {
		upstream := t.TempDir()
		gittest.RewriteURL(t, "https://github.com/acme/", upstream)
		dir := filepath.Join(t.TempDir(), "app")
		initRepo(t, dir, "https://github.com/acme/app.git")

		got, err := cloner.ReadOrigin(t.Context(), path, dir)

		require.NoError(t, err)
		assert.Equal(t, "https://github.com/acme/app.git", got)
	})

	t.Run("finds git itself when given no path", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "app")
		initRepo(t, dir, "https://github.com/acme/app.git")

		got, err := cloner.ReadOrigin(t.Context(), "", dir)

		require.NoError(t, err)
		assert.Equal(t, "https://github.com/acme/app.git", got)
	})

	t.Run("a clone without an origin", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "app")
		initRepo(t, dir, "")

		_, err := cloner.ReadOrigin(t.Context(), path, dir)

		require.ErrorIs(t, err, cloner.ErrNoOrigin)
	})

	t.Run("a broken .git doesn't borrow the origin of a repo around it", func(t *testing.T) {
		outer := t.TempDir()
		initRepo(t, outer, "https://github.com/acme/app.git")
		inner := filepath.Join(outer, "app")
		require.NoError(t, os.MkdirAll(filepath.Join(inner, ".git"), 0o755))

		got, err := cloner.ReadOrigin(t.Context(), path, inner)

		require.Error(t, err)
		assert.Empty(t, got)
	})

	t.Run("without git", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "app")
		initRepo(t, dir, "https://github.com/acme/app.git")
		t.Setenv("PATH", t.TempDir())

		_, err := cloner.ReadOrigin(t.Context(), "", dir)

		require.ErrorIs(t, err, cloner.ErrGitNotFound)
	})
}
