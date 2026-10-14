package cloner_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

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
	return cloner.Options{OutputDir: f.out, Concurrency: 1, Progress: cloner.NoProgress{}}
}

// progressEvent is one call a recordingProgress heard.
type progressEvent struct {
	kind   string // "start", "started", "done" or "stop"
	name   string
	action string
	total  int
	result cloner.RepoResult
}

// recordingProgress is a cloner.Progress that logs each call in order. Its
// mutex is what the interface demands of every implementation.
type recordingProgress struct {
	// onStarted, when set, runs inside each RepoStarted call.
	onStarted func()

	mu     sync.Mutex
	events []progressEvent
}

func (r *recordingProgress) record(e progressEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func (r *recordingProgress) Start(total int) { r.record(progressEvent{kind: "start", total: total}) }

func (r *recordingProgress) RepoStarted(name, action string) {
	r.record(progressEvent{kind: "started", name: name, action: action})
	if r.onStarted != nil {
		r.onStarted()
	}
}

func (r *recordingProgress) RepoDone(res cloner.RepoResult, _ time.Duration) {
	r.record(progressEvent{kind: "done", name: res.Name, result: res})
}

func (r *recordingProgress) Stop() { r.record(progressEvent{kind: "stop"}) }

// kind returns the events of one kind, in the order they arrived.
func (r *recordingProgress) kind(k string) []progressEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []progressEvent
	for _, e := range r.events {
		if e.kind == k {
			out = append(out, e)
		}
	}
	return out
}

// requireBracketed fails unless the first event is Start and the last is the
// only Stop.
func (r *recordingProgress) requireBracketed(t *testing.T, total int) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	require.NotEmpty(t, r.events)
	assert.Equal(t, progressEvent{kind: "start", total: total}, r.events[0], "Start comes first, with the repo count")
	assert.Equal(t, "stop", r.events[len(r.events)-1].kind, "Stop comes last")
	stops := 0
	for _, e := range r.events {
		if e.kind == "stop" {
			stops++
		}
	}
	assert.Equal(t, 1, stops, "Stop is called exactly once")
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

func TestCloneRepos_ReportsAFailedClone(t *testing.T) {
	f := newFixture(t)
	repo := ghapi.RepoInfo{Name: "ghost", CloneURL: filepath.Join(f.upstream, "ghost.git"), DefaultBranch: "main"}

	summary, err := cloner.CloneRepos(t.Context(), []ghapi.RepoInfo{repo}, f.opts())
	require.NoError(t, err)
	require.Len(t, summary.Results, 1)
	assert.Equal(t, cloner.ActionFailed, summary.Results[0].Action)
	assert.NotEmpty(t, summary.Results[0].Detail, "git's message explains the failure")
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

func TestCloneRepos_ReportsProgress(t *testing.T) {
	tests := []struct {
		name        string
		concurrency int
	}{
		{"one worker", 1},
		{"several workers", 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			var repos []ghapi.RepoInfo
			for i := range 6 {
				repos = append(repos, f.repo(t, fmt.Sprintf("repo-%d", i), "main"))
			}
			opts := f.opts()
			opts.Concurrency = tt.concurrency
			rec := &recordingProgress{}
			opts.Progress = rec

			_, err := cloner.CloneRepos(t.Context(), repos, opts)
			require.NoError(t, err)
			rec.requireBracketed(t, len(repos))
			assert.Len(t, rec.kind("started"), len(repos))
			assert.Len(t, rec.kind("done"), len(repos), "one RepoDone per repo")
			for _, e := range rec.kind("started") {
				assert.Equal(t, cloner.ProgressClone, e.action, e.name)
			}
			for _, e := range rec.kind("done") {
				assert.Equal(t, cloner.ActionCloned, e.result.Action, e.name)
			}

			// The second run finds a clone of each and pulls; nothing new came
			// in, so each is up to date.
			again := &recordingProgress{}
			opts.Progress = again
			_, err = cloner.CloneRepos(t.Context(), repos, opts)
			require.NoError(t, err)
			again.requireBracketed(t, len(repos))
			for _, e := range again.kind("started") {
				assert.Equal(t, cloner.ProgressPull, e.action, e.name)
			}
			for _, e := range again.kind("done") {
				assert.Equal(t, cloner.ActionUpToDate, e.result.Action, e.name)
			}
		})
	}
}

func TestCloneRepos_ReportsEachRepoOnceWhateverItsResult(t *testing.T) {
	f := newFixture(t)
	good := f.repo(t, "app", "main")
	wiki := f.repo(t, "docs", "main")
	wiki.HasWiki = true
	gittest.NewBareRepo(t, f.upstream, "docs.wiki", "master")
	broken := ghapi.RepoInfo{Name: "ghost", CloneURL: filepath.Join(f.upstream, "ghost.git"), DefaultBranch: "main"}
	unusable := ghapi.RepoInfo{Name: "..", CloneURL: filepath.Join(f.upstream, "app.git"), DefaultBranch: "main"}
	opts := f.opts()
	opts.CloneWiki = true
	rec := &recordingProgress{}
	opts.Progress = rec

	_, err := cloner.CloneRepos(t.Context(), []ghapi.RepoInfo{good, wiki, broken, unusable}, opts)
	require.NoError(t, err)

	rec.requireBracketed(t, 4)
	done := map[string]cloner.Action{}
	for _, e := range rec.kind("done") {
		_, dup := done[e.name]
		assert.False(t, dup, "%s reported twice", e.name)
		done[e.name] = e.result.Action
	}
	assert.Equal(t, map[string]cloner.Action{
		"app":   cloner.ActionCloned,
		"docs":  cloner.ActionCloned, // the repo's own result; its wiki adds none of its own
		"ghost": cloner.ActionFailed,
		"..":    cloner.ActionSkippedName,
	}, done)
	var started []string
	for _, e := range rec.kind("started") {
		started = append(started, e.name)
	}
	assert.ElementsMatch(t, []string{"app", "docs", "ghost"}, started, "an unusable name is skipped before it starts")
}

func TestCloneRepos_StopsProgressWhenTheContextEndsMidRun(t *testing.T) {
	f := newFixture(t)
	gitPath, err := cloner.FindGit(t.Context())
	require.NoError(t, err)
	var repos []ghapi.RepoInfo
	for i := range 20 {
		repos = append(repos, ghapi.RepoInfo{
			Name:          fmt.Sprintf("repo-%d", i),
			CloneURL:      filepath.Join(f.upstream, fmt.Sprintf("repo-%d.git", i)),
			DefaultBranch: "main",
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	opts := f.opts()
	opts.GitPath = gitPath
	opts.Concurrency = 3
	rec := &recordingProgress{onStarted: cancel}
	opts.Progress = rec

	_, err = cloner.CloneRepos(ctx, repos, opts)
	require.ErrorIs(t, err, context.Canceled)
	rec.requireBracketed(t, len(repos))
	assert.NotEmpty(t, rec.kind("started"))
	assert.Less(t, len(rec.kind("done")), len(repos), "repos not yet handed out are never reported")
}

func TestCloneRepos_NoReposNeverStartsProgress(t *testing.T) {
	f := newFixture(t)
	opts := f.opts()
	rec := &recordingProgress{}
	opts.Progress = rec

	_, err := cloner.CloneRepos(t.Context(), nil, opts)
	require.NoError(t, err)
	assert.Empty(t, rec.events, "with nothing to sync there is nothing to draw")
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
		cloner.RecordGit(t, func(cmd *exec.Cmd) error {
			if !slices.Contains(cmd.Args, "clone") || !strings.HasSuffix(cmd.Args[len(cmd.Args)-1], "app.wiki") {
				return cmd.Run()
			}
			_, _ = fmt.Fprintf(cmd.Stderr, "remote: Repository not found.\nfatal: repository 'https://github.com/acme/app.wiki.git/' not found\n")
			return errors.New("exit status 128")
		})

		summary, err := cloner.CloneRepos(t.Context(), []ghapi.RepoInfo{repo}, opts)
		require.NoError(t, err)
		assert.ElementsMatch(t, []cloner.Action{cloner.ActionCloned, cloner.ActionSkippedWiki}, actionsByName(summary)["app"])
		assert.NoDirExists(t, filepath.Join(f.out, "app.wiki"))
	})

	t.Run("reports any other wiki clone failure as failed wiki with git's message", func(t *testing.T) {
		f := newFixture(t)
		repo := f.repo(t, "app", "main")
		repo.HasWiki = true
		opts := f.opts()
		opts.CloneWiki = true
		cloner.RecordGit(t, func(cmd *exec.Cmd) error {
			if !slices.Contains(cmd.Args, "clone") || !strings.HasSuffix(cmd.Args[len(cmd.Args)-1], "app.wiki") {
				return cmd.Run()
			}
			_, _ = fmt.Fprintf(cmd.Stderr, "Cloning into 'app.wiki'...\nfatal: unable to access 'https://github.com/acme/app.wiki.git/': Could not resolve host: github.com\n")
			return errors.New("exit status 128")
		})

		summary, err := cloner.CloneRepos(t.Context(), []ghapi.RepoInfo{repo}, opts)
		require.NoError(t, err)
		assert.Equal(t, []cloner.Action{cloner.ActionCloned}, actionsByName(summary)["app"])
		require.Equal(t, []cloner.Action{cloner.ActionFailedWiki}, actionsByName(summary)["app.wiki"],
			"the failure is named for the wiki, not the repo")
		for _, r := range summary.Results {
			if r.Action == cloner.ActionFailedWiki {
				assert.Contains(t, r.Detail, "Could not resolve host")
			}
		}
	})

	t.Run("removes the clone of an empty wiki and reports nothing", func(t *testing.T) {
		f := newFixture(t)
		repo := f.repo(t, "app", "main")
		repo.HasWiki = true
		bare := filepath.Join(f.upstream, "app.wiki.git")
		gittest.Git(t, f.upstream, "init", "--bare", bare)
		opts := f.opts()
		opts.CloneWiki = true

		summary, err := cloner.CloneRepos(t.Context(), []ghapi.RepoInfo{repo}, opts)
		require.NoError(t, err)
		assert.Equal(t, []cloner.Action{cloner.ActionCloned}, actionsByName(summary)["app"])
		assert.NoDirExists(t, filepath.Join(f.out, "app.wiki"))
	})

	t.Run("reports an existing wiki that diverged as fetched only", func(t *testing.T) {
		f := newFixture(t)
		repo := f.repo(t, "app", "main")
		repo.HasWiki = true
		wiki := gittest.NewBareRepo(t, f.upstream, "app.wiki", "master")
		opts := f.opts()
		opts.CloneWiki = true
		_, err := cloner.CloneRepos(t.Context(), []ghapi.RepoInfo{repo}, opts)
		require.NoError(t, err)
		wikiDest := filepath.Join(f.out, "app.wiki")
		require.NoError(t, os.WriteFile(filepath.Join(wikiDest, "Mine.md"), []byte("mine\n"), 0o644))
		gittest.Git(t, wikiDest, "add", "Mine.md")
		gittest.Git(t, wikiDest, "commit", "-m", "Local page")
		gittest.AddCommit(t, wiki, "master", "Theirs.md", "theirs\n")

		summary, err := cloner.CloneRepos(t.Context(), []ghapi.RepoInfo{repo}, opts)
		require.NoError(t, err)
		var wikiResults []cloner.RepoResult
		for _, r := range summary.Results {
			if r.Name == "app.wiki" {
				wikiResults = append(wikiResults, r)
			}
		}
		require.Len(t, wikiResults, 1)
		assert.Equal(t, cloner.ActionFetchedOnly, wikiResults[0].Action)
		assert.Equal(t, "app.wiki", wikiResults[0].Name)
		assert.Contains(t, wikiResults[0].Detail, "local commit(s) not on origin/master")
		assert.NoFileExists(t, filepath.Join(wikiDest, "Theirs.md"), "the working tree is left alone")
		assert.Equal(t, rev(t, wiki, "refs/heads/master"), rev(t, wikiDest, "refs/remotes/origin/master"), "the fetch still backed it up")
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
		assert.Equal(t, []cloner.Action{cloner.ActionUpToDate}, actionsByName(summary)["app"],
			"an existing wiki adds no result of its own")
		assert.FileExists(t, filepath.Join(wikiDest, "Home.md"))

		require.NoError(t, os.WriteFile(filepath.Join(wikiDest, "Home.md"), []byte("wip\n"), 0o644))
		gittest.AddCommit(t, wiki, "master", "More.md", "more\n")
		summary, err = cloner.CloneRepos(t.Context(), []ghapi.RepoInfo{repo}, opts)
		require.NoError(t, err)
		assert.NoFileExists(t, filepath.Join(wikiDest, "More.md"), "a wiki with tracked changes is not pulled into")
		assert.ElementsMatch(t, []cloner.Action{cloner.ActionUpToDate, cloner.ActionFetchedOnly}, slices.Concat(actionsByName(summary)["app"], actionsByName(summary)["app.wiki"]),
			"the wiki that was left alone is listed")
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
			OutputDir: target, Concurrency: 1, Progress: cloner.NoProgress{},
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

// newEmptyBare creates <parent>/<name>.git as a bare repository with no
// commits, whose HEAD names main, as GitHub makes a new empty repository.
func newEmptyBare(t *testing.T, parent, name string) string {
	t.Helper()
	bare := filepath.Join(parent, name+".git")
	require.NoError(t, os.MkdirAll(bare, 0o755))
	gittest.Git(t, bare, "init", "--bare", "--initial-branch", "main")
	return bare
}

// pushCommit pushes a new root commit to branch of bare, which may be empty.
func pushCommit(t *testing.T, bare, branch string) {
	t.Helper()
	seed := t.TempDir()
	gittest.Git(t, seed, "init")
	commitFile(t, seed, "README.md", branch)
	gittest.Git(t, seed, "push", bare, "HEAD:refs/heads/"+branch)
}

// rev resolves ref in the repo at dir.
func rev(t *testing.T, dir, ref string) string {
	t.Helper()
	return strings.TrimSpace(gittest.Git(t, dir, "rev-parse", "--verify", ref))
}

// currentBranch names the branch checked out in dir.
func currentBranch(t *testing.T, dir string) string {
	t.Helper()
	return strings.TrimSpace(gittest.Git(t, dir, "branch", "--show-current"))
}

// syncOne runs CloneRepos for repo alone and returns its one result.
func syncOne(t *testing.T, f fixture, repo ghapi.RepoInfo) cloner.RepoResult {
	t.Helper()
	summary, err := cloner.CloneRepos(t.Context(), []ghapi.RepoInfo{repo}, f.opts())
	require.NoError(t, err)
	require.Len(t, summary.Results, 1)
	return summary.Results[0]
}

func TestCloneRepos_EmptyRepos(t *testing.T) {
	t.Run("a new clone is removed and skipped, then cloned once it has a commit", func(t *testing.T) {
		f := newFixture(t)
		bare := newEmptyBare(t, f.upstream, "app")
		repo := ghapi.RepoInfo{Name: "app", CloneURL: bare, DefaultBranch: "main"}
		dest := filepath.Join(f.out, "app")

		got := syncOne(t, f, repo)
		assert.Equal(t, cloner.ActionSkippedEmpty, got.Action)
		assert.Equal(t, "empty repository", got.Detail)
		assert.NoDirExists(t, dest, "an empty clone is not kept")

		pushCommit(t, bare, "main")
		got = syncOne(t, f, repo)
		assert.Equal(t, cloner.RepoResult{Name: "app", Action: cloner.ActionCloned}, got)
		assert.Equal(t, "main", currentBranch(t, dest))
		assert.FileExists(t, filepath.Join(dest, "README.md"))
	})

	t.Run("an existing clone of a repo that is still empty is skipped and kept", func(t *testing.T) {
		f := newFixture(t)
		bare := newEmptyBare(t, f.upstream, "app")
		dest := filepath.Join(f.out, "app")
		gittest.Git(t, "", "clone", bare, dest)

		got := syncOne(t, f, ghapi.RepoInfo{Name: "app", CloneURL: bare, DefaultBranch: "main"})
		assert.Equal(t, cloner.ActionSkippedEmpty, got.Action)
		assert.DirExists(t, filepath.Join(dest, ".git"))
	})

	t.Run("an existing empty clone checks out the first commit", func(t *testing.T) {
		f := newFixture(t)
		bare := newEmptyBare(t, f.upstream, "app")
		dest := filepath.Join(f.out, "app")
		gittest.Git(t, "", "clone", bare, dest)
		pushCommit(t, bare, "main")

		got := syncOne(t, f, ghapi.RepoInfo{Name: "app", CloneURL: bare, DefaultBranch: "main"})
		assert.Equal(t, cloner.RepoResult{Name: "app", Action: cloner.ActionPulled}, got)
		assert.Equal(t, rev(t, bare, "refs/heads/main"), rev(t, dest, "HEAD"))
		assert.Equal(t, "origin/main", strings.TrimSpace(gittest.Git(t, dest, "rev-parse", "--abbrev-ref", "main@{upstream}")))
	})
}

func TestCloneRepos_RemoteHEADNamesAMissingBranch(t *testing.T) {
	tests := []struct {
		name     string
		branches []string // oldest commit first
		want     string
	}{
		{"master is the fallback after main", []string{"master", "dev"}, "master"},
		{"otherwise the branch with the newest commit", []string{"feature", "dev"}, "dev"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			bare := newEmptyBare(t, f.upstream, "app") // HEAD names main
			for i, b := range tt.branches {
				t.Setenv("GIT_COMMITTER_DATE", fmt.Sprintf("20%02d-01-01T00:00:00Z", 10+i))
				pushCommit(t, bare, b)
			}
			dest := filepath.Join(f.out, "app")

			got := syncOne(t, f, ghapi.RepoInfo{Name: "app", CloneURL: bare, DefaultBranch: "main"})

			assert.Equal(t, cloner.ActionCloned, got.Action)
			assert.Equal(t, "remote HEAD names main, which doesn't exist; checked out "+tt.want, got.Detail)
			assert.Equal(t, tt.want, currentBranch(t, dest))
			assert.Equal(t, "origin/"+tt.want, strings.TrimSpace(gittest.Git(t, dest, "rev-parse", "--abbrev-ref", "@{upstream}")))
		})
	}
}

func TestCloneRepos_LeavesADestinationThatIsNotAClone(t *testing.T) {
	f := newFixture(t)
	repo := f.repo(t, "app", "main")
	dest := filepath.Join(f.out, "app")
	require.NoError(t, os.MkdirAll(dest, 0o755))
	notes := filepath.Join(dest, "notes.txt")
	require.NoError(t, os.WriteFile(notes, []byte("mine\n"), 0o644))
	log := cloner.RecordGit(t, nil)

	got := syncOne(t, f, repo)

	assert.Equal(t, cloner.ActionFailed, got.Action)
	assert.Equal(t, dest+" exists and isn't a git clone; move it aside", got.Detail)
	for _, c := range log.Calls() {
		assert.Equal(t, "version", subcommand(c), "git never runs against someone else's directory: %v", c.Args)
	}
	data, err := os.ReadFile(notes)
	require.NoError(t, err)
	assert.Equal(t, "mine\n", string(data))
	assert.NoDirExists(t, filepath.Join(dest, ".git"))
}

func TestCloneRepos_KeepsACloneWhoseCheckoutFailed(t *testing.T) {
	f := newFixture(t)
	repo := f.repo(t, "app", "main")
	gittest.AddCommit(t, repo.CloneURL, "main", ".gitattributes", "README.md filter=broken\n")
	// What a missing git-lfs does to an LFS repo: the smudge filter that
	// writes the working tree fails after every object has come down.
	t.Setenv("GIT_CONFIG_COUNT", "2")
	t.Setenv("GIT_CONFIG_KEY_0", "filter.broken.smudge")
	t.Setenv("GIT_CONFIG_VALUE_0", "exit 1")
	t.Setenv("GIT_CONFIG_KEY_1", "filter.broken.required")
	t.Setenv("GIT_CONFIG_VALUE_1", "true")
	dest := filepath.Join(f.out, "app")

	got := syncOne(t, f, repo)

	assert.Equal(t, cloner.ActionFetchedOnly, got.Action)
	assert.Regexp(t, `^fatal: [^\n]*smudge filter broken failed$`, got.Detail)
	tip := rev(t, repo.CloneURL, "refs/heads/main")
	assert.Equal(t, tip, rev(t, dest, "HEAD"))
	gittest.Git(t, dest, "cat-file", "-e", tip+"^{tree}") // the objects are there
}

func TestCloneRepos_ReportsAFailedFetch(t *testing.T) {
	tests := []struct {
		name    string
		breakIt func(t *testing.T, bare, dest string)
	}{
		{"the upstream disappears", func(t *testing.T, bare, _ string) {
			require.NoError(t, os.RemoveAll(bare))
		}},
		{"origin points nowhere", func(t *testing.T, _, dest string) {
			gittest.Git(t, dest, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "nowhere.git"))
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			repo := f.repo(t, "app", "main")
			_, err := cloner.CloneRepos(t.Context(), []ghapi.RepoInfo{repo}, f.opts())
			require.NoError(t, err)
			tt.breakIt(t, repo.CloneURL, filepath.Join(f.out, "app"))

			got := syncOne(t, f, repo)

			assert.Equal(t, cloner.ActionFailed, got.Action)
			assert.Regexp(t, `^fatal: [^\n]+$`, got.Detail, "git's one line of reason, without the hints")
		})
	}
}

// syncWorld is an upstream repo and ghx's clone of it, made by a first sync.
type syncWorld struct {
	f    fixture
	repo ghapi.RepoInfo
	bare string
	dest string
}

func newSyncWorld(t *testing.T, branch string) *syncWorld {
	t.Helper()
	f := newFixture(t)
	repo := f.repo(t, "app", branch)
	w := &syncWorld{f: f, repo: repo, bare: repo.CloneURL, dest: filepath.Join(f.out, "app")}
	require.Equal(t, cloner.ActionCloned, syncOne(t, f, repo).Action)
	return w
}

// upstream resolves branch in the upstream repo.
func (w *syncWorld) upstream(t *testing.T, branch string) string {
	t.Helper()
	return rev(t, w.bare, "refs/heads/"+branch)
}

// write puts content in file in the clone, without committing it.
func (w *syncWorld) write(t *testing.T, file, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(w.dest, file), []byte(content), 0o644))
}

// read returns file's content in the clone.
func (w *syncWorld) read(t *testing.T, file string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(w.dest, file))
	require.NoError(t, err)
	return string(data)
}

// backups lists the refs ghx kept under refs/ghx/forced/, with what each names.
func (w *syncWorld) backups(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for line := range strings.Lines(gittest.Git(t, w.dest, "for-each-ref", "--format=%(refname) %(objectname)", "refs/ghx/forced")) {
		ref, sha, _ := strings.Cut(strings.TrimSpace(line), " ")
		out[ref] = sha
	}
	return out
}

// forcePush rewrites the tip of branch upstream: the upstream commit is
// amended to also add file, and force-pushed.
func forcePush(t *testing.T, bare, branch, file, content string) {
	t.Helper()
	work := t.TempDir()
	gittest.Git(t, work, "clone", "--branch", branch, bare, ".")
	require.NoError(t, os.WriteFile(filepath.Join(work, file), []byte(content), 0o644))
	gittest.Git(t, work, "add", file)
	gittest.Git(t, work, "commit", "--amend", "-m", "Rewritten")
	gittest.Git(t, work, "push", "--force", "origin", branch)
}

// treeCommands are the git subcommands that move a working tree or a
// checked-out branch.
var treeCommands = []string{"switch", "checkout", "merge", "reset", "branch", "rebase", "pull"}

func TestCloneRepos_SyncsAnExistingClone(t *testing.T) {
	tests := []struct {
		name   string
		branch string // upstream's default branch; main when empty
		// setup changes the clone and the upstream, and returns what to check
		// afterwards.
		setup  func(t *testing.T, w *syncWorld) (check func(t *testing.T))
		want   cloner.Action
		detail string // a regular expression
		// refused is the one command that moves the working tree a fetched-only
		// case may run, because git is expected to refuse it.
		refused string
	}{
		{
			name: "a force-push is followed when nothing is unpushed",
			setup: func(t *testing.T, w *syncWorld) func(t *testing.T) {
				old := rev(t, w.dest, "HEAD")
				forcePush(t, w.bare, "main", "NEWS.md", "news\n")
				return func(t *testing.T) {
					assert.Equal(t, w.upstream(t, "main"), rev(t, w.dest, "HEAD"))
					assert.FileExists(t, filepath.Join(w.dest, "NEWS.md"))
					backups := w.backups(t)
					require.Len(t, backups, 1)
					for ref, sha := range backups {
						assert.Regexp(t, `^refs/ghx/forced/main/\d{8}T\d{6}Z$`, ref)
						assert.Equal(t, old, sha, "the old tip is kept")
					}
				}
			},
			want:   cloner.ActionPulled,
			detail: `^origin/main was force-pushed; old tip kept at refs/ghx/forced/main/\d{8}T\d{6}Z$`,
		},
		{
			name: "a force-push over an unpushed commit is only fetched",
			setup: func(t *testing.T, w *syncWorld) func(t *testing.T) {
				commitFile(t, w.dest, "mine.txt", "mine")
				local := rev(t, w.dest, "HEAD")
				forcePush(t, w.bare, "main", "NEWS.md", "news\n")
				return func(t *testing.T) {
					assert.Equal(t, local, rev(t, w.dest, "HEAD"), "the local commit is intact")
					assert.Equal(t, w.upstream(t, "main"), rev(t, w.dest, "refs/remotes/origin/main"))
					assert.Empty(t, w.backups(t))
				}
			},
			want: cloner.ActionFetchedOnly,
			// Only the local commit: the one the force-push dropped isn't the
			// user's.
			detail: `^1 local commit\(s\) on top of a force-pushed origin/main$`,
		},
		{
			name: "a force-push an earlier fetch saw is followed",
			setup: func(t *testing.T, w *syncWorld) func(t *testing.T) {
				old := rev(t, w.dest, "HEAD")
				forcePush(t, w.bare, "main", "NEWS.md", "news\n")
				// As the pull --ff-only of ghx before 0.3 did: the fetch moves
				// origin/main, then the fast-forward is refused.
				gittest.Git(t, w.dest, "fetch", "origin")
				return func(t *testing.T) {
					assert.Equal(t, w.upstream(t, "main"), rev(t, w.dest, "HEAD"))
					backups := w.backups(t)
					require.Len(t, backups, 1)
					for _, sha := range backups {
						assert.Equal(t, old, sha, "the old tip is kept")
					}
				}
			},
			want:   cloner.ActionPulled,
			detail: `^origin/main was force-pushed; old tip kept at refs/ghx/forced/main/\d{8}T\d{6}Z$`,
		},
		{
			name: "a force-push an earlier fetch saw, under a local commit, is only fetched",
			setup: func(t *testing.T, w *syncWorld) func(t *testing.T) {
				commitFile(t, w.dest, "mine.txt", "mine")
				local := rev(t, w.dest, "HEAD")
				forcePush(t, w.bare, "main", "NEWS.md", "news\n")
				gittest.Git(t, w.dest, "fetch", "origin")
				return func(t *testing.T) {
					assert.Equal(t, local, rev(t, w.dest, "HEAD"), "the local commit is intact")
					assert.Empty(t, w.backups(t))
				}
			},
			want:   cloner.ActionFetchedOnly,
			detail: `^1 local commit\(s\) on top of a force-pushed origin/main$`,
		},
		{
			name: "a force-push an earlier fetch saw, with no reflog to prove it, is only fetched",
			setup: func(t *testing.T, w *syncWorld) func(t *testing.T) {
				old := rev(t, w.dest, "HEAD")
				forcePush(t, w.bare, "main", "NEWS.md", "news\n")
				gittest.Git(t, w.dest, "config", "core.logAllRefUpdates", "false")
				require.NoError(t, os.RemoveAll(filepath.Join(w.dest, ".git", "logs", "refs", "remotes")))
				gittest.Git(t, w.dest, "fetch", "origin")
				return func(t *testing.T) {
					assert.Equal(t, old, rev(t, w.dest, "HEAD"))
					assert.Empty(t, w.backups(t))
				}
			},
			want:   cloner.ActionFetchedOnly,
			detail: `^1 local commit\(s\) not on origin/main$`,
		},
		{
			name: "a force-push that would overwrite an untracked file is only fetched",
			setup: func(t *testing.T, w *syncWorld) func(t *testing.T) {
				old := rev(t, w.dest, "HEAD")
				w.write(t, "NEWS.md", "mine\n")
				forcePush(t, w.bare, "main", "NEWS.md", "news\n")
				return func(t *testing.T) {
					assert.Equal(t, "mine\n", w.read(t, "NEWS.md"))
					assert.Equal(t, old, rev(t, w.dest, "HEAD"))
					assert.Empty(t, w.backups(t), "a backup for a reset that didn't happen is removed")
				}
			},
			want:    cloner.ActionFetchedOnly,
			detail:  `^(fatal|error): [^\n]+$`,
			refused: "reset",
		},
		{
			name: "local commits on a branch that moved on are only fetched",
			setup: func(t *testing.T, w *syncWorld) func(t *testing.T) {
				commitFile(t, w.dest, "mine.txt", "mine")
				local := rev(t, w.dest, "HEAD")
				gittest.AddCommit(t, w.bare, "main", "NEWS.md", "news\n")
				return func(t *testing.T) {
					assert.Equal(t, local, rev(t, w.dest, "HEAD"))
					assert.Equal(t, w.upstream(t, "main"), rev(t, w.dest, "refs/remotes/origin/main"))
				}
			},
			want:   cloner.ActionFetchedOnly,
			detail: `^1 local commit\(s\) not on origin/main$`,
		},
		{
			name: "a clone with nothing new is up to date",
			setup: func(t *testing.T, w *syncWorld) func(t *testing.T) {
				head := rev(t, w.dest, "HEAD")
				return func(t *testing.T) {
					assert.Equal(t, head, rev(t, w.dest, "HEAD"))
				}
			},
			want: cloner.ActionUpToDate,
		},
		{
			name: "a tracked change is kept and only fetched",
			setup: func(t *testing.T, w *syncWorld) func(t *testing.T) {
				w.write(t, "README.md", "edited\n")
				gittest.AddCommit(t, w.bare, "main", "NEWS.md", "news\n")
				return func(t *testing.T) {
					assert.Equal(t, "edited\n", w.read(t, "README.md"))
					assert.NoFileExists(t, filepath.Join(w.dest, "NEWS.md"))
					assert.Equal(t, w.upstream(t, "main"), rev(t, w.dest, "refs/remotes/origin/main"))
				}
			},
			want:   cloner.ActionFetchedOnly,
			detail: `^local changes: README\.md$`,
		},
		{
			name: "an untracked file doesn't block a fast-forward",
			setup: func(t *testing.T, w *syncWorld) func(t *testing.T) {
				w.write(t, "scratch.txt", "mine\n")
				gittest.AddCommit(t, w.bare, "main", "NEWS.md", "news\n")
				return func(t *testing.T) {
					assert.FileExists(t, filepath.Join(w.dest, "NEWS.md"))
					assert.Equal(t, "mine\n", w.read(t, "scratch.txt"))
				}
			},
			want:   cloner.ActionPulled,
			detail: `^$`,
		},
		{
			name: "an untracked file the incoming commit adds is kept and only fetched",
			setup: func(t *testing.T, w *syncWorld) func(t *testing.T) {
				old := rev(t, w.dest, "HEAD")
				w.write(t, "NEWS.md", "mine\n")
				gittest.AddCommit(t, w.bare, "main", "NEWS.md", "news\n")
				return func(t *testing.T) {
					assert.Equal(t, "mine\n", w.read(t, "NEWS.md"))
					assert.Equal(t, old, rev(t, w.dest, "HEAD"))
					assert.Equal(t, w.upstream(t, "main"), rev(t, w.dest, "refs/remotes/origin/main"))
				}
			},
			want:    cloner.ActionFetchedOnly,
			detail:  `^error: [^\n]+$`,
			refused: "merge",
		},
		{
			name: "a feature branch stays checked out and local main fast-forwards",
			setup: func(t *testing.T, w *syncWorld) func(t *testing.T) {
				gittest.Git(t, w.dest, "switch", "-c", "x")
				gittest.AddCommit(t, w.bare, "main", "NEWS.md", "news\n")
				return func(t *testing.T) {
					assert.Equal(t, "x", currentBranch(t, w.dest))
					assert.Equal(t, w.upstream(t, "main"), rev(t, w.dest, "refs/heads/main"))
					assert.NoFileExists(t, filepath.Join(w.dest, "NEWS.md"))
				}
			},
			want:   cloner.ActionFetchedOnly,
			detail: `^on branch x$`,
		},
		{
			name: "a detached HEAD is only fetched",
			setup: func(t *testing.T, w *syncWorld) func(t *testing.T) {
				gittest.Git(t, w.dest, "checkout", "--detach")
				old := rev(t, w.dest, "HEAD")
				gittest.AddCommit(t, w.bare, "main", "NEWS.md", "news\n")
				return func(t *testing.T) {
					assert.Equal(t, old, rev(t, w.dest, "HEAD"))
					assert.Equal(t, w.upstream(t, "main"), rev(t, w.dest, "refs/remotes/origin/main"))
				}
			},
			want:   cloner.ActionFetchedOnly,
			detail: `^detached HEAD$`,
		},
		{
			name: "a rebase stopped on a conflict is only fetched",
			setup: func(t *testing.T, w *syncWorld) func(t *testing.T) {
				commitFile(t, w.dest, "README.md", "local")
				gittest.AddCommit(t, w.bare, "main", "README.md", "upstream\n")
				gittest.Git(t, w.dest, "fetch", "origin")
				gitFails(t, w.dest, "rebase", "origin/main")
				return func(t *testing.T) {
					assert.Equal(t, "rebase", cloner.NewGit(gitPath(t), "").OperationInProgress(t.Context(), w.dest),
						"the rebase is still there to finish")
				}
			},
			want:   cloner.ActionFetchedOnly,
			detail: `^rebase in progress$`,
		},
		{
			name:   "a renamed default branch is switched to and the old one deleted",
			branch: "master",
			setup: func(t *testing.T, w *syncWorld) func(t *testing.T) {
				renameUpstream(t, w)
				return func(t *testing.T) {
					assert.Equal(t, "main", currentBranch(t, w.dest))
					assert.Equal(t, "origin/main", strings.TrimSpace(gittest.Git(t, w.dest, "rev-parse", "--abbrev-ref", "@{upstream}")))
					assert.Empty(t, gittest.Git(t, w.dest, "branch", "--list", "master"), "master is deleted")
				}
			},
			want:   cloner.ActionPulled,
			detail: `^switched from master to main$`,
		},
		{
			name:   "a renamed default branch with an unpushed commit keeps the old branch",
			branch: "master",
			setup: func(t *testing.T, w *syncWorld) func(t *testing.T) {
				commitFile(t, w.dest, "mine.txt", "mine")
				local := rev(t, w.dest, "HEAD")
				renameUpstream(t, w)
				return func(t *testing.T) {
					assert.Equal(t, "master", currentBranch(t, w.dest))
					assert.Equal(t, local, rev(t, w.dest, "refs/heads/master"))
					assert.Equal(t, w.upstream(t, "main"), rev(t, w.dest, "refs/remotes/origin/main"))
				}
			},
			want:   cloner.ActionFetchedOnly,
			detail: `^on branch master$`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			branch := tt.branch
			if branch == "" {
				branch = "main"
			}
			w := newSyncWorld(t, branch)
			check := tt.setup(t, w)
			log := cloner.RecordGit(t, nil)

			got := syncOne(t, w.f, w.repo)

			assert.Equal(t, tt.want, got.Action)
			assert.Regexp(t, tt.detail, got.Detail)
			check(t)
			// Neither moves the working tree.
			if tt.want == cloner.ActionFetchedOnly || tt.want == cloner.ActionUpToDate {
				for _, c := range log.Calls() {
					if sub := subcommand(c); sub != tt.refused {
						assert.NotContains(t, treeCommands, sub, "a fetched-only repo ran %v", c.Args)
					}
				}
			}
		})
	}
}

// renameUpstream renames the upstream's master to main, as GitHub's branch
// rename does, and tells ghx main is the default branch now.
func renameUpstream(t *testing.T, w *syncWorld) {
	t.Helper()
	gittest.Git(t, w.bare, "branch", "-m", "master", "main")
	gittest.Git(t, w.bare, "symbolic-ref", "HEAD", "refs/heads/main")
	w.repo.DefaultBranch = "main"
}
