//go:build !windows

package cloner_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lolay/ghx/internal/cloner"
	"github.com/lolay/ghx/internal/ghapi"
)

func TestInterruptGit_SendsSIGINTSoGitCanCleanUp(t *testing.T) {
	cmd := startWaiter(t)

	require.NoError(t, cloner.InterruptGit(cmd.Process))

	_ = cmd.Wait()
	assert.Equal(t, exitInterrupted, cmd.ProcessState.ExitCode(), "the child saw os.Interrupt")
}

func TestIsTransient_NothingIsOnUnix(t *testing.T) {
	assert.False(t, cloner.IsTransient(&fs.PathError{Op: "remove", Path: "x", Err: syscall.EACCES}))
	assert.False(t, cloner.IsTransient(&os.LinkError{Op: "rename", Old: "a", New: "b", Err: syscall.EBUSY}))
}

func TestCloneRepos_LeavesCoreLongpathsAloneOnUnix(t *testing.T) {
	f := newFixture(t)
	_, err := cloner.CloneRepos(t.Context(), []ghapi.RepoInfo{f.repo(t, "app", "main")}, f.opts())
	require.NoError(t, err)

	assert.Empty(t, strings.TrimSpace(localConfig(t, filepath.Join(f.out, "app"), "core.longpaths")))
}

func TestCloneRepos_ClonesWindowsDeviceNamesOnUnix(t *testing.T) {
	f := newFixture(t)
	_, err := cloner.CloneRepos(t.Context(), []ghapi.RepoInfo{f.repo(t, "CON", "main")}, f.opts())
	require.NoError(t, err)

	assert.DirExists(t, filepath.Join(f.out, "CON", ".git"))
}

func TestFindGit_RefusesAGitOlderThan231(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\necho 'git version 2.30.2'\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755))
	t.Setenv("PATH", dir)

	_, err := cloner.FindGit(t.Context())

	require.ErrorIs(t, err, cloner.ErrGitTooOld)
	assert.Contains(t, err.Error(), "found 2.30.2")
}

func TestFindGit_ReportsAGitThatFails(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\necho 'broken install' >&2\nexit 1\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755))
	t.Setenv("PATH", dir)

	_, err := cloner.FindGit(t.Context())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "broken install")
}
