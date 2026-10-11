package cli_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lolay/ghx/internal/ghapi/ghapitest"
	"github.com/lolay/ghx/internal/gittest"
)

const githubPrefix = "https://github.com/acme/"

// httpsURL lists a repo with its real GitHub HTTPS clone URL;
// gittest.RewriteURL serves that URL from the world's upstream directory.
func httpsURL(r *ghapitest.Repo) { r.CloneURL = githubPrefix + r.Name + ".git" }

// gitConfigs returns the contents of every .git/config under root.
func gitConfigs(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && d.Name() == "config" && filepath.Base(filepath.Dir(path)) == ".git" {
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			out[path] = string(data)
		}
		return nil
	})
	require.NoError(t, err)
	return out
}

func origin(t *testing.T, dir string) string {
	t.Helper()
	return strings.TrimSpace(gittest.Git(t, dir, "config", "--local", "--get", "remote.origin.url"))
}

func TestRun_HTTPSSyncLeavesNoTokenInAnyGitConfig(t *testing.T) {
	w := newWorld(t)
	gittest.RewriteURL(t, githubPrefix, w.upstream)
	w.addRepo("alpha", httpsURL, func(r *ghapitest.Repo) { r.HasWiki = true })
	gittest.NewBareRepo(t, w.upstream, "alpha.wiki", "master")
	w.addRepo("beta", httpsURL)
	w.addRepo("gamma", httpsURL)

	w.mustRun("--clone-wiki")

	require.Len(t, gitConfigs(t, w.out), 4)
	for path, cfg := range gitConfigs(t, w.out) {
		assert.NotContains(t, cfg, testToken, path)
	}
	assert.Equal(t, githubPrefix+"alpha.git", origin(t, filepath.Join(w.out, "alpha")))
	assert.Equal(t, githubPrefix+"alpha.wiki.git", origin(t, filepath.Join(w.out, "alpha.wiki")))
}

func TestRun_CleansTokenBearingOriginsLeftByAnOlderGhx(t *testing.T) {
	w := newWorld(t)
	gittest.RewriteURL(t, githubPrefix, w.upstream)
	w.addRepo("alpha", httpsURL, func(r *ghapitest.Repo) { r.HasWiki = true })
	gittest.NewBareRepo(t, w.upstream, "alpha.wiki", "master")
	w.addRepo("beta", httpsURL)
	w.addRepo("gamma", httpsURL)
	w.mustRun("--clone-wiki")

	// What an older ghx left behind: the token in every origin URL, and a clone
	// under DELETED/ that no sync pulls any more.
	old := "https://" + testToken + "@github.com/acme/"
	for _, name := range []string{"alpha", "alpha.wiki", "beta", "gamma"} {
		gittest.Git(t, filepath.Join(w.out, name), "remote", "set-url", "origin", old+name+".git")
	}
	older := filepath.Join(w.out, "DELETED", "older")
	gittest.Git(t, "", "clone", "--quiet", w.bare("beta"), older)
	gittest.Git(t, older, "remote", "set-url", "origin", old+"older.git")
	w.removeRepo("beta")
	w.archiveRepo("gamma")
	w.pushCommit("alpha", "NEWS.md")

	w.mustRun("--clone-wiki", "--dry-run")
	assert.Equal(t, old+"older.git", origin(t, older), "a dry run changes nothing")

	out := w.mustRun("--clone-wiki")

	assert.Equal(t, []string{"ARCHIVED/gamma", "DELETED/beta", "DELETED/older", "alpha", "alpha.wiki"}, repoDirs(t, w.out))
	configs := gitConfigs(t, w.out)
	require.Len(t, configs, 5)
	for path, cfg := range configs {
		assert.NotContains(t, cfg, testToken, path)
	}
	assert.Equal(t, githubPrefix+"alpha.git", origin(t, filepath.Join(w.out, "alpha")))
	assert.Equal(t, githubPrefix+"alpha.wiki.git", origin(t, filepath.Join(w.out, "alpha.wiki")))
	assert.Equal(t, githubPrefix+"beta.git", origin(t, filepath.Join(w.out, "DELETED", "beta")))
	assert.Equal(t, githubPrefix+"gamma.git", origin(t, filepath.Join(w.out, "ARCHIVED", "gamma")))
	assert.Equal(t, githubPrefix+"older.git", origin(t, older))
	assert.FileExists(t, filepath.Join(w.out, "alpha", "NEWS.md"), "the pull ran on the cleaned URL")
	assert.NotContains(t, out, testToken)
}
