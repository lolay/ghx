package cloner_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lolay/ghx/internal/cloner"
	"github.com/lolay/ghx/internal/ghapi"
	"github.com/lolay/ghx/internal/gittest"
)

// fixture is a directory of upstream bare repos and an empty output directory.
type fixture struct {
	upstream string
	out      string
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	gittest.Isolate(t)
	return fixture{upstream: t.TempDir(), out: filepath.Join(t.TempDir(), "out")}
}

// repo makes a bare upstream repo and returns the RepoInfo ghx would list for it.
func (f fixture) repo(t *testing.T, name, branch string) ghapi.RepoInfo {
	t.Helper()
	bare := gittest.NewBareRepo(t, f.upstream, name, branch)
	return ghapi.RepoInfo{Name: name, CloneURL: bare, SSHURL: bare, DefaultBranch: branch}
}

func (f fixture) opts() cloner.Options {
	return cloner.Options{OutputDir: f.out, Concurrency: 1, ProgressOut: io.Discard}
}

// actionsByName groups a summary's actions by repo name; with concurrency
// above one the result order is not defined.
func actionsByName(s cloner.SyncSummary) map[string][]cloner.Action {
	out := map[string][]cloner.Action{}
	for _, r := range s.Results {
		out[r.Name] = append(out[r.Name], r.Action)
	}
	return out
}

func TestCloneRepos_ClonesThenPullsNewCommits(t *testing.T) {
	f := newFixture(t)
	repo := f.repo(t, "app", "trunk")
	dest := filepath.Join(f.out, "app")

	first, err := cloner.CloneRepos(t.Context(), []ghapi.RepoInfo{repo}, f.opts())
	require.NoError(t, err)
	assert.Equal(t, []cloner.Action{cloner.ActionCloned}, actionsByName(first)["app"])
	assert.FileExists(t, filepath.Join(dest, "README.md"))
	assert.Equal(t, "trunk", strings.TrimSpace(gittest.Git(t, dest, "branch", "--show-current")),
		"the clone follows the repo's default branch, not git's own default")

	gittest.AddCommit(t, repo.CloneURL, "trunk", "NEWS.md", "news\n")
	second, err := cloner.CloneRepos(t.Context(), []ghapi.RepoInfo{repo}, f.opts())
	require.NoError(t, err)
	assert.Equal(t, []cloner.Action{cloner.ActionPulled}, actionsByName(second)["app"])
	assert.FileExists(t, filepath.Join(dest, "NEWS.md"), "the second run pulled the new upstream commit")
}

func TestCloneRepos_SkipsADirtyWorkingTree(t *testing.T) {
	f := newFixture(t)
	repo := f.repo(t, "app", "main")
	_, err := cloner.CloneRepos(t.Context(), []ghapi.RepoInfo{repo}, f.opts())
	require.NoError(t, err)

	dest := filepath.Join(f.out, "app")
	require.NoError(t, os.WriteFile(filepath.Join(dest, "scratch.txt"), []byte("mine\n"), 0o644))
	gittest.AddCommit(t, repo.CloneURL, "main", "NEWS.md", "news\n")

	summary, err := cloner.CloneRepos(t.Context(), []ghapi.RepoInfo{repo}, f.opts())
	require.NoError(t, err)
	assert.Equal(t, []cloner.Action{cloner.ActionSkippedDirty}, actionsByName(summary)["app"])
	assert.NoFileExists(t, filepath.Join(dest, "NEWS.md"), "a dirty tree must not be pulled into")
	assert.FileExists(t, filepath.Join(dest, "scratch.txt"))
}

func TestCloneRepos_ReportsAFailedClone(t *testing.T) {
	f := newFixture(t)
	repo := ghapi.RepoInfo{Name: "ghost", CloneURL: filepath.Join(f.upstream, "ghost.git"), DefaultBranch: "main"}

	summary, err := cloner.CloneRepos(t.Context(), []ghapi.RepoInfo{repo}, f.opts())
	require.NoError(t, err)
	require.Len(t, summary.Results, 1)
	assert.Equal(t, cloner.ActionFailed, summary.Results[0].Action)
	assert.NotEmpty(t, summary.Results[0].Detail, "git's message explains the failure")
}

func TestCloneRepos_ReportsAFailedPull(t *testing.T) {
	f := newFixture(t)
	repo := f.repo(t, "app", "main")
	_, err := cloner.CloneRepos(t.Context(), []ghapi.RepoInfo{repo}, f.opts())
	require.NoError(t, err)

	require.NoError(t, os.RemoveAll(repo.CloneURL), "the upstream disappears")
	summary, err := cloner.CloneRepos(t.Context(), []ghapi.RepoInfo{repo}, f.opts())
	require.NoError(t, err)
	require.Len(t, summary.Results, 1)
	assert.Equal(t, cloner.ActionFailed, summary.Results[0].Action)
	assert.NotEmpty(t, summary.Results[0].Detail)
}

func TestCloneRepos_WritesGitIdentity(t *testing.T) {
	tests := []struct {
		name      string
		author    string
		email     string
		wantName  string
		wantEmail string
	}{
		{"both set", "Ada Lovelace", "ada@example.com", "Ada Lovelace", "ada@example.com"},
		{"only the author", "Ada Lovelace", "", "Ada Lovelace", ""},
		{"only the email", "", "ada@example.com", "", "ada@example.com"},
		{"neither", "", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			repo := f.repo(t, "app", "main")
			opts := f.opts()
			opts.GitAuthor, opts.GitEmail = tt.author, tt.email

			_, err := cloner.CloneRepos(t.Context(), []ghapi.RepoInfo{repo}, opts)
			require.NoError(t, err)

			// --local so the runner's own identity can't leak into the answer.
			dest := filepath.Join(f.out, "app")
			get := func(key string) string {
				return strings.TrimSpace(localConfig(t, dest, key))
			}
			assert.Equal(t, tt.wantName, get("user.name"))
			assert.Equal(t, tt.wantEmail, get("user.email"))
		})
	}
}

// localConfig reads a key from the repo's own config file; an unset key is "".
func localConfig(t *testing.T, dir, key string) string {
	t.Helper()
	return gittest.Git(t, dir, "config", "--local", "--default", "", "--get", key)
}

func TestCloneRepos_ReappliesIdentityOnAnExistingClone(t *testing.T) {
	f := newFixture(t)
	repo := f.repo(t, "app", "main")
	_, err := cloner.CloneRepos(t.Context(), []ghapi.RepoInfo{repo}, f.opts())
	require.NoError(t, err)

	opts := f.opts()
	opts.GitAuthor = "Grace Hopper"
	_, err = cloner.CloneRepos(t.Context(), []ghapi.RepoInfo{repo}, opts)
	require.NoError(t, err)

	assert.Equal(t, "Grace Hopper", strings.TrimSpace(localConfig(t, filepath.Join(f.out, "app"), "user.name")))
}

func TestCloneRepos_RunsRepoJobsConcurrently(t *testing.T) {
	f := newFixture(t)
	var repos []ghapi.RepoInfo
	for i := range 6 {
		repos = append(repos, f.repo(t, fmt.Sprintf("repo-%d", i), "main"))
	}
	opts := f.opts()
	opts.Concurrency = 4

	summary, err := cloner.CloneRepos(t.Context(), repos, opts)
	require.NoError(t, err)

	got := actionsByName(summary)
	require.Len(t, got, len(repos))
	for _, r := range repos {
		assert.Equal(t, []cloner.Action{cloner.ActionCloned}, got[r.Name], r.Name)
		assert.DirExists(t, filepath.Join(f.out, r.Name, ".git"))
	}
}

func TestCloneRepos_TreatsConcurrencyBelowOneAsOne(t *testing.T) {
	f := newFixture(t)
	opts := f.opts()
	opts.Concurrency = 0

	summary, err := cloner.CloneRepos(t.Context(), []ghapi.RepoInfo{f.repo(t, "app", "main")}, opts)
	require.NoError(t, err)
	assert.Equal(t, 1, summary.Count(cloner.ActionCloned))
}

func TestCloneRepos_NoReposStillCreatesTheOutputDir(t *testing.T) {
	f := newFixture(t)

	summary, err := cloner.CloneRepos(t.Context(), nil, f.opts())
	require.NoError(t, err)
	assert.Empty(t, summary.Results)
	assert.DirExists(t, f.out)
}

func TestCloneRepos_FailsWhenTheOutputDirCannotBeCreated(t *testing.T) {
	f := newFixture(t)
	blocker := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o644))
	opts := f.opts()
	opts.OutputDir = filepath.Join(blocker, "out") // a directory can't live under a file

	_, err := cloner.CloneRepos(t.Context(), []ghapi.RepoInfo{f.repo(t, "app", "main")}, opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "creating output dir")
}

func TestCloneRepos_StopsOnACancelledContext(t *testing.T) {
	f := newFixture(t)
	// None of these exist upstream: a cancelled run must not need them.
	var repos []ghapi.RepoInfo
	for i := range 50 {
		repos = append(repos, ghapi.RepoInfo{
			Name:          fmt.Sprintf("repo-%d", i),
			CloneURL:      filepath.Join(f.upstream, fmt.Sprintf("repo-%d.git", i)),
			DefaultBranch: "main",
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	summary, err := cloner.CloneRepos(ctx, repos, f.opts())
	require.ErrorIs(t, err, context.Canceled)
	assert.Less(t, len(summary.Results), len(repos), "a cancelled run stops handing out repos")
}

func TestCloneRepos_Wikis(t *testing.T) {
	t.Run("clones an existing wiki beside the repo", func(t *testing.T) {
		f := newFixture(t)
		repo := f.repo(t, "app", "main")
		repo.HasWiki = true
		gittest.NewBareRepo(t, f.upstream, "app.wiki", "master")
		opts := f.opts()
		opts.CloneWiki = true

		summary, err := cloner.CloneRepos(t.Context(), []ghapi.RepoInfo{repo}, opts)
		require.NoError(t, err)
		assert.ElementsMatch(t, []cloner.Action{cloner.ActionCloned, cloner.ActionClonedWiki}, actionsByName(summary)["app"])
		assert.DirExists(t, filepath.Join(f.out, "app.wiki", ".git"))
	})

	t.Run("reports a wiki that does not exist as skipped", func(t *testing.T) {
		f := newFixture(t)
		repo := f.repo(t, "app", "main")
		repo.HasWiki = true // GitHub says so, but the wiki was never created
		opts := f.opts()
		opts.CloneWiki = true

		summary, err := cloner.CloneRepos(t.Context(), []ghapi.RepoInfo{repo}, opts)
		require.NoError(t, err)
		assert.ElementsMatch(t, []cloner.Action{cloner.ActionCloned, cloner.ActionSkippedWiki}, actionsByName(summary)["app"])
		assert.NoDirExists(t, filepath.Join(f.out, "app.wiki"))
	})

	t.Run("pulls an existing wiki clone and leaves a dirty one alone", func(t *testing.T) {
		f := newFixture(t)
		repo := f.repo(t, "app", "main")
		repo.HasWiki = true
		wiki := gittest.NewBareRepo(t, f.upstream, "app.wiki", "master")
		opts := f.opts()
		opts.CloneWiki = true
		_, err := cloner.CloneRepos(t.Context(), []ghapi.RepoInfo{repo}, opts)
		require.NoError(t, err)
		wikiDest := filepath.Join(f.out, "app.wiki")

		gittest.AddCommit(t, wiki, "master", "Home.md", "home\n")
		summary, err := cloner.CloneRepos(t.Context(), []ghapi.RepoInfo{repo}, opts)
		require.NoError(t, err)
		assert.Equal(t, []cloner.Action{cloner.ActionPulled}, actionsByName(summary)["app"],
			"an existing wiki adds no result of its own")
		assert.FileExists(t, filepath.Join(wikiDest, "Home.md"))

		require.NoError(t, os.WriteFile(filepath.Join(wikiDest, "draft.md"), []byte("wip\n"), 0o644))
		gittest.AddCommit(t, wiki, "master", "More.md", "more\n")
		_, err = cloner.CloneRepos(t.Context(), []ghapi.RepoInfo{repo}, opts)
		require.NoError(t, err)
		assert.NoFileExists(t, filepath.Join(wikiDest, "More.md"), "a dirty wiki is not pulled into")
	})

	t.Run("ignores wikis unless asked", func(t *testing.T) {
		f := newFixture(t)
		repo := f.repo(t, "app", "main")
		repo.HasWiki = true
		gittest.NewBareRepo(t, f.upstream, "app.wiki", "master")

		summary, err := cloner.CloneRepos(t.Context(), []ghapi.RepoInfo{repo}, f.opts())
		require.NoError(t, err)
		assert.Equal(t, []cloner.Action{cloner.ActionCloned}, actionsByName(summary)["app"])
		assert.NoDirExists(t, filepath.Join(f.out, "app.wiki"))
	})

	t.Run("skips the wiki of a repo that failed", func(t *testing.T) {
		f := newFixture(t)
		repo := ghapi.RepoInfo{
			Name: "ghost", CloneURL: filepath.Join(f.upstream, "ghost.git"),
			DefaultBranch: "main", HasWiki: true,
		}
		opts := f.opts()
		opts.CloneWiki = true

		summary, err := cloner.CloneRepos(t.Context(), []ghapi.RepoInfo{repo}, opts)
		require.NoError(t, err)
		assert.Equal(t, []cloner.Action{cloner.ActionFailed}, actionsByName(summary)["ghost"])
	})
}

func TestMoveRepo(t *testing.T) {
	t.Run("moves a real clone into a new directory", func(t *testing.T) {
		f := newFixture(t)
		repo := f.repo(t, "app", "main")
		_, err := cloner.CloneRepos(t.Context(), []ghapi.RepoInfo{repo}, f.opts())
		require.NoError(t, err)
		src := filepath.Join(f.out, "app")
		target := filepath.Join(f.out, "DELETED")

		require.NoError(t, cloner.MoveRepo(src, target))

		assert.NoDirExists(t, src)
		assert.FileExists(t, filepath.Join(target, "app", "README.md"))
		assert.DirExists(t, filepath.Join(target, "app", ".git"))
	})

	t.Run("replaces a destination that already exists", func(t *testing.T) {
		f := newFixture(t)
		repo := f.repo(t, "app", "main")
		_, err := cloner.CloneRepos(t.Context(), []ghapi.RepoInfo{repo}, f.opts())
		require.NoError(t, err)
		src := filepath.Join(f.out, "app")
		target := filepath.Join(f.out, "ARCHIVED")

		// An earlier move left a clone behind, with its read-only git objects.
		stale := filepath.Join(target, "app")
		_, err = cloner.CloneRepos(t.Context(), []ghapi.RepoInfo{repo}, cloner.Options{
			OutputDir: target, Concurrency: 1, ProgressOut: io.Discard,
		})
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(stale, "old.txt"), []byte("old\n"), 0o644))

		require.NoError(t, cloner.MoveRepo(src, target))

		assert.NoDirExists(t, src)
		assert.NoFileExists(t, filepath.Join(stale, "old.txt"), "the old destination is gone")
		assert.FileExists(t, filepath.Join(stale, "README.md"))
	})

	t.Run("fails when the source is missing", func(t *testing.T) {
		dir := t.TempDir()
		require.Error(t, cloner.MoveRepo(filepath.Join(dir, "nope"), filepath.Join(dir, "DELETED")))
	})

	t.Run("fails when the target directory cannot be created", func(t *testing.T) {
		dir := t.TempDir()
		blocker := filepath.Join(dir, "file")
		require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o644))
		src := filepath.Join(dir, "app")
		require.NoError(t, os.Mkdir(src, 0o755))

		require.Error(t, cloner.MoveRepo(src, filepath.Join(blocker, "DELETED")))
		assert.DirExists(t, src, "a failed move leaves the source in place")
	})
}

func TestDeleteRepo(t *testing.T) {
	t.Run("removes a real clone including its read-only git objects", func(t *testing.T) {
		f := newFixture(t)
		repo := f.repo(t, "app", "main")
		_, err := cloner.CloneRepos(t.Context(), []ghapi.RepoInfo{repo}, f.opts())
		require.NoError(t, err)
		dest := filepath.Join(f.out, "app")

		require.NoError(t, cloner.DeleteRepo(dest))
		assert.NoDirExists(t, dest)
	})

	t.Run("treats a missing directory as success", func(t *testing.T) {
		require.NoError(t, cloner.DeleteRepo(filepath.Join(t.TempDir(), "nope")))
	})
}
