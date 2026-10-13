package cloner_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lolay/ghx/internal/cloner"
	"github.com/lolay/ghx/internal/ghapi"
	"github.com/lolay/ghx/internal/gittest"
)

func TestFindGit(t *testing.T) {
	t.Run("finds git on PATH", func(t *testing.T) {
		path, err := cloner.FindGit(t.Context())
		require.NoError(t, err)
		assert.True(t, filepath.IsAbs(path), "an absolute path, not one relative to the working directory: %s", path)
	})

	t.Run("a PATH without git is a clear error", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())

		_, err := cloner.FindGit(t.Context())

		require.ErrorIs(t, err, cloner.ErrGitNotFound)
		assert.Contains(t, err.Error(), "install Git")
	})

	t.Run("a git in the current directory is refused", func(t *testing.T) {
		dir := t.TempDir()
		exe := "git"
		if runtime.GOOS == "windows" {
			exe = "git.exe"
		}
		require.NoError(t, os.WriteFile(filepath.Join(dir, exe), []byte("#!/bin/sh\n"), 0o755))
		t.Chdir(dir)
		t.Setenv("PATH", ".")

		_, err := cloner.FindGit(t.Context())

		require.ErrorIs(t, err, cloner.ErrGitNotFound)
		assert.Contains(t, err.Error(), "current directory")
	})

	t.Run("CloneRepos stops before cloning when it can't find git", func(t *testing.T) {
		f := newFixture(t)
		repo := f.repo(t, "app", "main")
		t.Setenv("PATH", t.TempDir())

		summary, err := cloner.CloneRepos(t.Context(), []ghapi.RepoInfo{repo}, f.opts())

		require.ErrorIs(t, err, cloner.ErrGitNotFound)
		assert.Empty(t, summary.Results)
		assert.NoDirExists(t, filepath.Join(f.out, "app"))
	})
}

func TestCheckGitVersion(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		wantErr string // "" means accepted
		tooOld  bool
	}{
		{"exactly 2.31", "git version 2.31.0\n", "", false},
		{"a newer release", "git version 2.56.0\n", "", false},
		{"Apple's git", "git version 2.39.3 (Apple Git-145)\n", "", false},
		{"Git for Windows", "git version 2.45.1.windows.1\r\n", "", false},
		{"a two-part version", "git version 2.40\n", "", false},
		{"a future major", "git version 3.0.0\n", "", false},
		{"2.30 is too old", "git version 2.30.9\n", "needs git 2.31 or later and found 2.30.9", true},
		{"1.x is too old", "git version 1.9.5\n", "needs git 2.31 or later", true},
		{"not git's output", "hello\n", "can't read git's version", false},
		{"no minor version", "git version 2\n", "can't read git's version", false},
		{"a version that isn't a number", "git version two.thirty\n", "can't read git's version", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := cloner.CheckGitVersion(tt.output)
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
			assert.Equal(t, tt.tooOld, errors.Is(err, cloner.ErrGitTooOld))
		})
	}
}

func TestHasChanges(t *testing.T) {
	tests := []struct {
		name      string
		porcelain string
		want      bool
	}{
		{"no output is clean", "", false},
		{"a bare LF is clean", "\n", false},
		{"a bare CRLF is clean", "\r\n", false},
		{"a modified file", " M README.md\n", true},
		{"a modified file with a CRLF ending", " M README.md\r\n", true},
		{"an untracked file", "?? scratch.txt\n", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, cloner.HasChanges(tt.porcelain))
		})
	}
}

func TestCloneRepos_ReportsAStatusThatFails(t *testing.T) {
	f := newFixture(t)
	repo := f.repo(t, "app", "main")
	// A .git directory git can't read: ghx can't tell whether the tree is
	// dirty, so it must not pull.
	require.NoError(t, os.MkdirAll(filepath.Join(f.out, "app", ".git"), 0o755))

	summary, err := cloner.CloneRepos(t.Context(), []ghapi.RepoInfo{repo}, f.opts())

	require.NoError(t, err)
	require.Len(t, summary.Results, 1)
	assert.Equal(t, cloner.ActionFailed, summary.Results[0].Action)
	assert.NotEmpty(t, summary.Results[0].Detail)
}

func TestCloneRepos_SkipsANameThatIsNotADirectory(t *testing.T) {
	f := newFixture(t)
	repo := f.repo(t, "app", "main")
	repo.Name = "../escape"
	repo.HasWiki = true
	opts := f.opts()
	opts.CloneWiki = true

	summary, err := cloner.CloneRepos(t.Context(), []ghapi.RepoInfo{repo}, opts)

	require.NoError(t, err)
	require.Len(t, summary.Results, 1, "no wiki result either")
	assert.Equal(t, cloner.ActionSkippedName, summary.Results[0].Action)
	assert.Contains(t, summary.Results[0].Detail, "path separator")
	assert.NoDirExists(t, filepath.Join(filepath.Dir(f.out), "escape"))
}

func TestRetryWithin(t *testing.T) {
	errBusy := errors.New("busy")
	errFatal := errors.New("fatal")
	transient := func(err error) bool { return errors.Is(err, errBusy) }

	t.Run("retries a transient failure until it succeeds", func(t *testing.T) {
		calls := 0
		err := cloner.RetryWithin(func() error {
			calls++
			if calls < 3 {
				return errBusy
			}
			return nil
		}, transient, time.Second)
		require.NoError(t, err)
		assert.Equal(t, 3, calls)
	})

	t.Run("returns any other failure at once", func(t *testing.T) {
		calls := 0
		err := cloner.RetryWithin(func() error { calls++; return errFatal }, transient, time.Second)
		require.ErrorIs(t, err, errFatal)
		assert.Equal(t, 1, calls)
	})

	t.Run("gives up when the budget runs out", func(t *testing.T) {
		calls := 0
		err := cloner.RetryWithin(func() error { calls++; return errBusy }, transient, 20*time.Millisecond)
		require.ErrorIs(t, err, errBusy)
		assert.Greater(t, calls, 1, "it tried more than once")
	})
}

func TestDeleteRepo_RemovesReadOnlyFiles(t *testing.T) {
	// Windows git marks its object files read-only, and Windows won't delete a
	// read-only file until the attribute is cleared; os.Chmod sets it.
	dir := filepath.Join(t.TempDir(), "app")
	objects := filepath.Join(dir, ".git", "objects", "ab")
	require.NoError(t, os.MkdirAll(objects, 0o755))
	file := filepath.Join(objects, "cdef")
	require.NoError(t, os.WriteFile(file, []byte("blob"), 0o644))
	require.NoError(t, os.Chmod(file, 0o444))

	require.NoError(t, cloner.DeleteRepo(dir))
	assert.NoDirExists(t, dir)
}

func TestMoveRepo_ReplacesADestinationWithReadOnlyFiles(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "app")
	require.NoError(t, os.MkdirAll(src, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(src, "new.txt"), []byte("new"), 0o644))
	stale := filepath.Join(root, "DELETED", "app")
	require.NoError(t, os.MkdirAll(stale, 0o755))
	old := filepath.Join(stale, "old.txt")
	require.NoError(t, os.WriteFile(old, []byte("old"), 0o644))
	require.NoError(t, os.Chmod(old, 0o444))

	require.NoError(t, cloner.MoveRepo(src, filepath.Join(root, "DELETED")))

	assert.NoFileExists(t, old)
	assert.FileExists(t, filepath.Join(stale, "new.txt"))
}

// gitPath finds the git the tests run, and isolates it from the runner's
// config, as newFixture does.
func gitPath(t *testing.T) string {
	t.Helper()
	gittest.Isolate(t)
	path, err := cloner.FindGit(t.Context())
	require.NoError(t, err)
	return path
}

// commitFile writes content to file in the repo at dir and commits it.
func commitFile(t *testing.T, dir, file, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, file), []byte(content), 0o644))
	gittest.Git(t, dir, "add", file)
	gittest.Git(t, dir, "commit", "-m", "Set "+file+" to "+content)
}

// gitFails runs git in dir, expecting it to stop partway, as a conflicting
// merge or rebase does, and leave its state behind.
func gitFails(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{
		"-c", "user.name=ghx test",
		"-c", "user.email=ghx-test@example.com",
		"-c", "commit.gpgsign=false",
	}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.Error(t, err, "git %v should stop on a conflict: %s", args, out)
}

// newRepo makes a non-bare repo at a new temporary directory with one commit,
// of README.md, on main.
func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gittest.Git(t, dir, "init", "--initial-branch", "main")
	commitFile(t, dir, "README.md", "one")
	return dir
}

// cloneOf clones the bare repo at upstream into a new temporary directory.
func cloneOf(t *testing.T, upstream string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "work")
	gittest.Git(t, "", "clone", upstream, dir)
	return dir
}

// conflictingBranches makes a repo whose main and side branches each set
// conflict.txt differently, with main checked out.
func conflictingBranches(t *testing.T) string {
	t.Helper()
	dir := newRepo(t)
	commitFile(t, dir, "conflict.txt", "base")
	gittest.Git(t, dir, "checkout", "-b", "side")
	commitFile(t, dir, "conflict.txt", "side")
	gittest.Git(t, dir, "checkout", "main")
	commitFile(t, dir, "conflict.txt", "main")
	return dir
}

func TestGitProbes_CurrentBranch(t *testing.T) {
	tests := []struct {
		name         string
		setup        func(t *testing.T) string
		wantName     string
		wantDetached bool
		wantErr      bool
	}{
		{"on a branch", func(t *testing.T) string {
			dir := newRepo(t)
			gittest.Git(t, dir, "checkout", "-b", "feature/x")
			return dir
		}, "feature/x", false, false},
		{"detached HEAD", func(t *testing.T) string {
			dir := newRepo(t)
			gittest.Git(t, dir, "checkout", "--detach")
			return dir
		}, "", true, false},
		{"unborn HEAD keeps the branch name", func(t *testing.T) string {
			dir := t.TempDir()
			gittest.Git(t, dir, "init", "--initial-branch", "trunk")
			return dir
		}, "trunk", false, false},
		{"not a repository", func(t *testing.T) string { return t.TempDir() }, "", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			git := cloner.NewGit(gitPath(t), "")
			dir := tt.setup(t)

			name, detached, err := git.CurrentBranch(t.Context(), dir)

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantName, name)
			assert.Equal(t, tt.wantDetached, detached)
		})
	}
}

func TestGitProbes_RevParse(t *testing.T) {
	git := cloner.NewGit(gitPath(t), "")
	dir := newRepo(t)
	head := strings.TrimSpace(gittest.Git(t, dir, "rev-parse", "HEAD"))
	gittest.Git(t, dir, "tag", "-a", "-m", "annotated", "v1")
	unborn := t.TempDir()
	gittest.Git(t, unborn, "init", "--initial-branch", "main")

	tests := []struct {
		name   string
		dir    string
		ref    string
		want   string
		wantOK bool
	}{
		{"HEAD", dir, "HEAD", head, true},
		{"a branch", dir, "main", head, true},
		{"an annotated tag resolves to its commit", dir, "v1", head, true},
		{"a ref that doesn't exist", dir, "nope", "", false},
		{"unborn HEAD", unborn, "HEAD", "", false},
		{"not a repository", t.TempDir(), "HEAD", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sha, ok := git.RevParse(t.Context(), tt.dir, tt.ref)

			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.want, sha)
		})
	}
}

func TestGitProbes_IsAncestor(t *testing.T) {
	git := cloner.NewGit(gitPath(t), "")
	dir := newRepo(t)
	first := strings.TrimSpace(gittest.Git(t, dir, "rev-parse", "HEAD"))
	commitFile(t, dir, "two.txt", "two")
	second := strings.TrimSpace(gittest.Git(t, dir, "rev-parse", "HEAD"))
	gittest.Git(t, dir, "checkout", "-b", "other", first)
	commitFile(t, dir, "other.txt", "other")
	other := strings.TrimSpace(gittest.Git(t, dir, "rev-parse", "HEAD"))

	tests := []struct {
		name string
		a, b string
		want bool
	}{
		{"an older commit is an ancestor", first, second, true},
		{"a commit is its own ancestor", second, second, true},
		{"a newer commit is not", second, first, false},
		{"diverged commits are not", second, other, false},
		{"a commit that doesn't exist", "deadbeef", second, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, git.IsAncestor(t.Context(), dir, tt.a, tt.b))
		})
	}
}

func TestGitProbes_ForkPoint(t *testing.T) {
	git := cloner.NewGit(gitPath(t), "")
	const ref = "refs/remotes/origin/main"
	// rewritten clones an upstream, force-pushes it, and fetches the rewrite,
	// returning the clone and its HEAD from before the force-push.
	rewritten := func(t *testing.T, keepReflog bool) (string, string) {
		bare := gittest.NewBareRepo(t, t.TempDir(), "up", "main")
		dir := cloneOf(t, bare)
		old := strings.TrimSpace(gittest.Git(t, dir, "rev-parse", "HEAD"))
		forcePush(t, bare, "main", "NEWS.md", "news\n")
		if !keepReflog {
			gittest.Git(t, dir, "config", "core.logAllRefUpdates", "false")
			require.NoError(t, os.RemoveAll(filepath.Join(dir, ".git", "logs", "refs", "remotes")))
		}
		gittest.Git(t, dir, "fetch", "origin")
		return dir, old
	}

	t.Run("a commit once on origin is its own fork point", func(t *testing.T) {
		dir, old := rewritten(t, true)
		sha, ok := git.ForkPoint(t.Context(), dir, ref, old)
		assert.True(t, ok)
		assert.Equal(t, old, sha)
	})
	t.Run("a local commit forks from the old tip", func(t *testing.T) {
		dir, old := rewritten(t, true)
		commitFile(t, dir, "mine.txt", "mine")
		sha, ok := git.ForkPoint(t.Context(), dir, ref, "HEAD")
		assert.True(t, ok)
		assert.Equal(t, old, sha)
	})
	t.Run("without a reflog the old tip is forgotten", func(t *testing.T) {
		dir, old := rewritten(t, false)
		sha, ok := git.ForkPoint(t.Context(), dir, ref, old)
		assert.NotEqual(t, old, sha)
		if ok {
			assert.NotEmpty(t, sha)
		}
	})
	t.Run("not a repository", func(t *testing.T) {
		sha, ok := git.ForkPoint(t.Context(), t.TempDir(), ref, "HEAD")
		assert.False(t, ok)
		assert.Empty(t, sha)
	})
}

func TestGitProbes_UpstreamTrack(t *testing.T) {
	tests := []struct {
		name         string
		setup        func(t *testing.T) (dir, branch string)
		wantUpstream string
		wantGone     bool
	}{
		{"a branch that tracks origin", func(t *testing.T) (string, string) {
			f := newFixture(t)
			f.repo(t, "app", "main")
			return cloneOf(t, filepath.Join(f.upstream, "app.git")), "main"
		}, "refs/remotes/origin/main", false},
		{"a branch whose upstream the remote deleted", func(t *testing.T) (string, string) {
			f := newFixture(t)
			f.repo(t, "app", "main")
			bare := filepath.Join(f.upstream, "app.git")
			gittest.Git(t, bare, "branch", "topic", "main")
			dir := cloneOf(t, bare)
			gittest.Git(t, dir, "checkout", "topic")
			gittest.Git(t, bare, "branch", "-D", "topic")
			gittest.Git(t, dir, "fetch", "--prune")
			return dir, "topic"
		}, "refs/remotes/origin/topic", true},
		{"a branch that tracks nothing", func(t *testing.T) (string, string) {
			return newRepo(t), "main"
		}, "", false},
		{"a branch that doesn't exist", func(t *testing.T) (string, string) {
			return newRepo(t), "nope"
		}, "", false},
		{"unborn HEAD", func(t *testing.T) (string, string) {
			dir := t.TempDir()
			gittest.Git(t, dir, "init", "--initial-branch", "main")
			return dir, "main"
		}, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			git := cloner.NewGit(gitPath(t), "")
			dir, branch := tt.setup(t)

			upstream, gone := git.UpstreamTrack(t.Context(), dir, branch)

			assert.Equal(t, tt.wantUpstream, upstream)
			assert.Equal(t, tt.wantGone, gone)
		})
	}
}

func TestGitProbes_RemoteBranches(t *testing.T) {
	git := cloner.NewGit(gitPath(t), "")
	f := newFixture(t)
	f.repo(t, "app", "main")
	bare := filepath.Join(f.upstream, "app.git")
	work := cloneOf(t, bare)
	// main's commit is from now (2026 or later); the others straddle it.
	for _, b := range []struct{ name, date string }{
		{"ancient", "2000-01-01T00:00:00Z"},
		{"feature/x", "2099-01-01T00:00:00Z"},
		{"recent", "2098-01-01T00:00:00Z"},
	} {
		t.Setenv("GIT_COMMITTER_DATE", b.date)
		gittest.Git(t, work, "checkout", "-b", b.name, "main")
		gittest.Git(t, work, "commit", "--allow-empty", "-m", b.name)
		gittest.Git(t, work, "push", "origin", b.name)
	}
	t.Setenv("GIT_COMMITTER_DATE", "")
	require.NoError(t, os.Unsetenv("GIT_COMMITTER_DATE"))
	dir := cloneOf(t, bare)

	t.Run("newest commit first, without HEAD", func(t *testing.T) {
		assert.Equal(t, []string{"feature/x", "recent", "main", "ancient"}, git.RemoteBranches(t.Context(), dir))
	})

	t.Run("a repo with no origin has none", func(t *testing.T) {
		assert.Empty(t, git.RemoteBranches(t.Context(), newRepo(t)))
	})

	t.Run("not a repository", func(t *testing.T) {
		assert.Empty(t, git.RemoteBranches(t.Context(), t.TempDir()))
	})
}

func TestGitProbes_OperationInProgress(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T) string
		want  string
	}{
		{"a clean repo", newRepo, ""},
		{"not a repository", func(t *testing.T) string { return t.TempDir() }, ""},
		{"a merge with conflicts", func(t *testing.T) string {
			dir := conflictingBranches(t)
			gitFails(t, dir, "merge", "side")
			return dir
		}, "merge"},
		{"a rebase with conflicts", func(t *testing.T) string {
			dir := conflictingBranches(t)
			gittest.Git(t, dir, "checkout", "side")
			gitFails(t, dir, "rebase", "main")
			return dir
		}, "rebase"},
		{"an apply-style rebase with conflicts", func(t *testing.T) string {
			dir := conflictingBranches(t)
			gittest.Git(t, dir, "checkout", "side")
			gitFails(t, dir, "rebase", "--apply", "main")
			return dir
		}, "rebase"},
		{"a cherry-pick with conflicts", func(t *testing.T) string {
			dir := conflictingBranches(t)
			gitFails(t, dir, "cherry-pick", "side")
			return dir
		}, "cherry-pick"},
		{"a revert with conflicts", func(t *testing.T) string {
			dir := newRepo(t)
			commitFile(t, dir, "conflict.txt", "one")
			commitFile(t, dir, "conflict.txt", "two")
			commitFile(t, dir, "conflict.txt", "three")
			gitFails(t, dir, "revert", "HEAD~1")
			return dir
		}, "revert"},
		{"a merge, asked from a subdirectory", func(t *testing.T) string {
			dir := conflictingBranches(t)
			sub := filepath.Join(dir, "sub")
			require.NoError(t, os.Mkdir(sub, 0o755))
			gitFails(t, dir, "merge", "side")
			return sub
		}, "merge"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			git := cloner.NewGit(gitPath(t), "")
			dir := tt.setup(t)

			assert.Equal(t, tt.want, git.OperationInProgress(t.Context(), dir))
		})
	}
}

func TestPickBranch(t *testing.T) {
	tests := []struct {
		name          string
		defaultBranch string
		branches      []string
		want          string
	}{
		{"the default branch when origin has it", "trunk", []string{"newest", "main", "trunk"}, "trunk"},
		{"main when the default branch is gone", "trunk", []string{"newest", "master", "main"}, "main"},
		{"master when there is no main", "trunk", []string{"newest", "master"}, "master"},
		{"the newest branch when none of those exist", "trunk", []string{"newest", "older"}, "newest"},
		{"main when the default is unknown", "", []string{"newest", "main"}, "main"},
		{"an empty default never matches an empty name", "", []string{"newest"}, "newest"},
		{"no branches", "main", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, cloner.PickBranch(tt.defaultBranch, tt.branches))
		})
	}
}

// hasRun reports whether args holds want as consecutive elements.
func hasRun(args, want []string) bool {
	for i := 0; i+len(want) <= len(args); i++ {
		if slices.Equal(args[i:i+len(want)], want) {
			return true
		}
	}
	return false
}

func TestGitNetworkCommands_GetTheStallCheckAndTheLongTimeout(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, upstream string) (dir string, args []string)
		sub   string
	}{
		{"clone", func(t *testing.T, upstream string) (string, []string) {
			return "", []string{"clone", upstream, filepath.Join(t.TempDir(), "work")}
		}, "clone"},
		{"pull", func(t *testing.T, upstream string) (string, []string) {
			return cloneOf(t, upstream), []string{"pull", "--ff-only"}
		}, "pull"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := gitPath(t)
			f := newFixture(t)
			f.repo(t, "app", "main")
			dir, args := tt.setup(t, filepath.Join(f.upstream, "app.git"))
			log := cloner.RecordGit(t, nil)

			_, ok, msg := cloner.NewGit(path, "").Fetch(t.Context(), dir, "", args...)

			require.True(t, ok, msg)
			calls := log.Calls()
			require.Len(t, calls, 1)
			assert.True(t, hasRun(calls[0].Args, append(cloner.NetworkArgs(), tt.sub)),
				"the -c options come right before the subcommand: %v", calls[0].Args)
			assert.Equal(t, []time.Duration{cloner.NetworkTimeout}, log.Timeouts())
		})
	}

	t.Run("the options are the documented ones", func(t *testing.T) {
		assert.Equal(t, []string{"-c", "http.lowSpeedLimit=1000", "-c", "http.lowSpeedTime=60"}, cloner.NetworkArgs())
		assert.Equal(t, time.Hour, cloner.NetworkTimeout)
	})

	t.Run("local commands keep the short timeout and no stall check", func(t *testing.T) {
		git := cloner.NewGit(gitPath(t), "")
		dir := newRepo(t)
		log := cloner.RecordGit(t, nil)

		_, ok := git.RevParse(t.Context(), dir, "HEAD")

		require.True(t, ok)
		require.Len(t, log.Calls(), 1)
		for _, arg := range log.Calls()[0].Args {
			assert.NotContains(t, arg, "http.lowSpeed")
		}
		assert.Equal(t, []time.Duration{cloner.GitTimeout}, log.Timeouts())
		assert.Equal(t, 5*time.Minute, cloner.GitTimeout)
	})
}

// failWith returns a fake git for RecordGit that writes stderr and fails, for
// as long as fail says to.
func failWith(stderr string, fail func(attempt int) bool) (fake func(*exec.Cmd) error, attempts func() int) {
	n := 0
	return func(cmd *exec.Cmd) error {
			n++
			if fail(n) {
				_, _ = fmt.Fprintln(cmd.Stderr, stderr)
				return errors.New("exit status 128")
			}
			return cmd.Run()
		}, func() int {
			return n
		}
}

func TestGitFetch_RetriesOnceOnATransientFailure(t *testing.T) {
	t.Run("each documented failure, then success", func(t *testing.T) {
		for _, marker := range cloner.TransientMarkers() {
			t.Run(marker, func(t *testing.T) {
				path := gitPath(t)
				cloner.SetRetryDelay(t, 0)
				f := newFixture(t)
				f.repo(t, "app", "main")
				dest := filepath.Join(t.TempDir(), "work")
				fake, attempts := failWith("fatal: "+marker+" (simulated)", func(n int) bool { return n == 1 })
				log := cloner.RecordGit(t, fake)

				_, ok, msg := cloner.NewGit(path, "").Fetch(t.Context(), "", "",
					"clone", filepath.Join(f.upstream, "app.git"), dest)

				require.True(t, ok, msg)
				assert.Equal(t, 2, attempts())
				assert.Len(t, log.Calls(), 2)
				assert.DirExists(t, dest)
			})
		}
	})

	t.Run("RPC failed on a pull, then success", func(t *testing.T) {
		path := gitPath(t)
		cloner.SetRetryDelay(t, 0)
		f := newFixture(t)
		f.repo(t, "app", "main")
		dir := cloneOf(t, filepath.Join(f.upstream, "app.git"))
		gittest.AddCommit(t, filepath.Join(f.upstream, "app.git"), "main", "NEWS.md", "news\n")
		fake, attempts := failWith("error: RPC failed; curl 56 Recv failure", func(n int) bool { return n == 1 })
		cloner.RecordGit(t, fake)

		_, ok, msg := cloner.NewGit(path, "").Fetch(t.Context(), dir, "", "pull", "--ff-only")

		require.True(t, ok, msg)
		assert.Equal(t, 2, attempts())
		assert.FileExists(t, filepath.Join(dir, "NEWS.md"))
	})

	t.Run("a second failure is final: one retry, no more", func(t *testing.T) {
		path := gitPath(t)
		cloner.SetRetryDelay(t, 0)
		fake, attempts := failWith("fatal: early EOF", func(int) bool { return true })
		cloner.RecordGit(t, fake)

		_, ok, msg := cloner.NewGit(path, "").Fetch(t.Context(), t.TempDir(), "", "pull", "--ff-only")

		assert.False(t, ok)
		assert.Equal(t, 2, attempts())
		assert.Equal(t, "fatal: early EOF", msg)
	})
}

func TestGitFetch_NeverRetriesAFailureThatWouldRepeat(t *testing.T) {
	tests := []struct {
		name   string
		stderr string
	}{
		{"repository not found", "remote: Repository not found.\nfatal: repository 'https://github.com/acme/app.git/' not found"},
		{"authentication failed", "remote: Invalid username or token.\nfatal: Authentication failed for 'https://github.com/acme/app.git/'"},
		{"a 403", "fatal: unable to access 'https://github.com/acme/app.git/': The requested URL returned error: 403"},
		{"a 404", "fatal: unable to access 'https://github.com/acme/app.git/': The requested URL returned error: 404"},
		{"a diverged pull", "fatal: Not possible to fast-forward, aborting."},
		{"no output at all", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := gitPath(t)
			cloner.SetRetryDelay(t, 0)
			fake, attempts := failWith(tt.stderr, func(int) bool { return true })
			cloner.RecordGit(t, fake)

			_, ok, _ := cloner.NewGit(path, "").Fetch(t.Context(), "", "", "clone", "https://github.com/acme/app.git", filepath.Join(t.TempDir(), "w"))

			assert.False(t, ok)
			assert.Equal(t, 1, attempts())
		})
	}

	t.Run("a stopped run", func(t *testing.T) {
		path := gitPath(t)
		cloner.SetRetryDelay(t, 0)
		ctx, cancel := context.WithCancel(t.Context())
		attempts := 0
		cloner.RecordGit(t, func(cmd *exec.Cmd) error {
			attempts++
			cancel()
			_, _ = fmt.Fprintln(cmd.Stderr, "fatal: early EOF")
			return errors.New("exit status 128")
		})

		_, ok, _ := cloner.NewGit(path, "").Fetch(ctx, t.TempDir(), "", "pull", "--ff-only")

		assert.False(t, ok)
		assert.Equal(t, 1, attempts, "Ctrl-C ends the run rather than starting another attempt")
	})
}

func TestGitFetch_ARetriedCloneStartsFromAnEmptyDestination(t *testing.T) {
	t.Run("a partial clone the failed attempt left is removed", func(t *testing.T) {
		path := gitPath(t)
		cloner.SetRetryDelay(t, 0)
		dest := filepath.Join(t.TempDir(), "work")
		var leftover bool
		attempt := 0
		cloner.RecordGit(t, func(cmd *exec.Cmd) error {
			attempt++
			if attempt == 1 {
				require.NoError(t, os.MkdirAll(filepath.Join(dest, ".git"), 0o755))
				_, _ = fmt.Fprintln(cmd.Stderr, "error: RPC failed; curl 56")
				return errors.New("exit status 128")
			}
			_, err := os.Lstat(dest)
			leftover = err == nil
			return nil
		})

		_, ok, msg := cloner.NewGit(path, "").Fetch(t.Context(), "", "", "clone", "https://github.com/acme/app.git", dest)

		require.True(t, ok, msg)
		assert.Equal(t, 2, attempt)
		assert.False(t, leftover, "the retry saw the half-made clone")
	})

	t.Run("a destination that was there before is left alone", func(t *testing.T) {
		path := gitPath(t)
		cloner.SetRetryDelay(t, 0)
		dest := t.TempDir()
		keep := filepath.Join(dest, "keep.txt")
		require.NoError(t, os.WriteFile(keep, []byte("mine"), 0o644))
		attempt := 0
		cloner.RecordGit(t, func(cmd *exec.Cmd) error {
			attempt++
			if attempt == 1 {
				_, _ = fmt.Fprintln(cmd.Stderr, "error: RPC failed; curl 56")
				return errors.New("exit status 128")
			}
			return nil
		})

		_, ok, msg := cloner.NewGit(path, "").Fetch(t.Context(), "", "", "clone", "https://github.com/acme/app.git", dest)

		require.True(t, ok, msg)
		assert.Equal(t, 2, attempt)
		assert.FileExists(t, keep)
	})
}

func TestGitResult_Message(t *testing.T) {
	const secret = "ghp_secret_value"
	tests := []struct {
		name   string
		stdout string
		stderr string
		err    error
		want   string
	}{
		{"fetch progress and hints drop away",
			"",
			"remote: Enumerating objects: 5, done.\nReceiving objects: 100% (5/5), done.\nhint: Diverging branches can't be fast-forwarded.\nhint: you need to specify how to reconcile them.\nfatal: Not possible to fast-forward, aborting.\n",
			nil, "fatal: Not possible to fast-forward, aborting."},
		{"the last fatal or error line wins",
			"",
			"error: first problem\nfatal: unable to access the remote\nerror: could not fetch origin\nhint: try again\n",
			nil, "error: could not fetch origin"},
		{"CRLF line ends", "", "Receiving objects: 100%\r\nfatal: early EOF\r\nhint: x\r\n", nil, "fatal: early EOF"},
		{"a line that only mentions fatal is not one", "", "hint: fatal: not at the start\nsomething broke\n", nil, "hint: fatal: not at the start\nsomething broke"},
		{"without a fatal or error line, stderr as it was", "", "  warning: odd thing\nand another\n", nil, "warning: odd thing\nand another"},
		{"stdout when stderr is empty", "  nothing to do\n", "", nil, "nothing to do"},
		{"the process error when both are empty", "", "", errors.New("signal: killed"), "signal: killed"},
		{"the token is masked in the chosen line",
			"",
			"remote: noise\nfatal: unable to access 'https://" + secret + "@github.com/acme/app.git/'\n",
			nil, "fatal: unable to access 'https://***@github.com/acme/app.git/'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, cloner.MessageOf(tt.stdout, tt.stderr, tt.err, secret))
		})
	}
}
