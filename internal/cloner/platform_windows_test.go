//go:build windows

package cloner_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lolay/ghx/internal/cloner"
	"github.com/lolay/ghx/internal/ghapi"
)

func TestInterruptGit_KillsTheProcessOnWindows(t *testing.T) {
	cmd := startWaiter(t)

	require.NoError(t, cloner.InterruptGit(cmd.Process))

	_ = cmd.Wait()
	assert.NotEqual(t, exitInterrupted, cmd.ProcessState.ExitCode(), "Windows can't deliver os.Interrupt, so the child was killed")
}

// holdOpen creates dir/name and keeps it open until the test (or release) closes it.
func holdOpen(t *testing.T, dir, name string) (release func()) {
	t.Helper()
	f, err := os.Create(filepath.Join(dir, name))
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })
	return func() { _ = f.Close() }
}

func TestIsTransient_AFileHeldOpenBlocksARename(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "app")
	require.NoError(t, os.Mkdir(src, 0o755))
	holdOpen(t, src, "pack.idx")

	err := os.Rename(src, filepath.Join(root, "moved"))

	require.Error(t, err)
	assert.True(t, cloner.IsTransient(err), "%v", err)
}

func TestMoveRepo_WaitsForAFileHeldBrieflyOpen(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "app")
	require.NoError(t, os.Mkdir(src, 0o755))
	release := holdOpen(t, src, "pack.idx")
	// A scanner that lets go well inside MoveRepo's retry budget.
	time.AfterFunc(100*time.Millisecond, release)

	require.NoError(t, cloner.MoveRepo(src, filepath.Join(root, "DELETED")))
	assert.DirExists(t, filepath.Join(root, "DELETED", "app"))
}

func TestCloneRepos_TurnsOnCoreLongpathsOnWindows(t *testing.T) {
	f := newFixture(t)
	_, err := cloner.CloneRepos(t.Context(), []ghapi.RepoInfo{f.repo(t, "app", "main")}, f.opts())
	require.NoError(t, err)

	assert.Equal(t, "true", strings.TrimSpace(localConfig(t, filepath.Join(f.out, "app"), "core.longpaths")))
}

func TestCloneRepos_SkipsWindowsDeviceNames(t *testing.T) {
	f := newFixture(t)
	// No upstream is needed: the name is refused before git runs.
	repo := ghapi.RepoInfo{Name: "CON", CloneURL: filepath.Join(f.upstream, "app.git"), DefaultBranch: "main"}

	summary, err := cloner.CloneRepos(t.Context(), []ghapi.RepoInfo{repo}, f.opts())

	require.NoError(t, err)
	require.Len(t, summary.Results, 1)
	assert.Equal(t, cloner.ActionSkippedName, summary.Results[0].Action)
	assert.Contains(t, summary.Results[0].Detail, "reserved device name")
}

func TestFindGit_RefusesAGitOlderThan231(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "git.bat"), []byte("@echo git version 2.30.2.windows.1\r\n"), 0o755))
	t.Setenv("PATH", dir)

	_, err := cloner.FindGit(t.Context())

	require.ErrorIs(t, err, cloner.ErrGitTooOld)
	assert.Contains(t, err.Error(), "found 2.30.2")
}

func TestFindGit_ReportsAGitThatFails(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "git.bat"), []byte("@echo broken install 1>&2\r\n@exit /b 1\r\n"), 0o755))
	t.Setenv("PATH", dir)

	_, err := cloner.FindGit(t.Context())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "broken install")
}
