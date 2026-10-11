package cloner_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lolay/ghx/internal/cloner"
	"github.com/lolay/ghx/internal/ghapi"
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
