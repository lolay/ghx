// Package gittest builds local git repositories for tests that exercise
// ghx's git handling without a network: a bare repo with one commit on a
// named default branch, extra upstream commits, and a thin wrapper to run git.
//
// Every command sets its own identity and default branch with -c, and Isolate
// points git away from the runner's global config, so a result never depends
// on the machine the tests run on.
package gittest

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// Isolate keeps git from reading the runner's system and global config for
// the rest of the test, so settings like pull.rebase or commit.gpgsign can't
// change the behaviour under test. It uses t.Setenv, so the calling test can't
// run in parallel.
func Isolate(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_TERMINAL_PROMPT", "0")
}

// RewriteURL makes git fetch any URL that starts with prefix, such as
// "https://github.com/acme/", from the directory dir instead, for the rest of
// the test: "https://github.com/acme/app.git" becomes dir/app.git. It writes
// url.<dir>/.insteadOf to a global config file of the test's own, so a test
// can clone and pull a real HTTPS GitHub URL with no network, while the clone
// records the HTTPS URL as its origin. Call it after Isolate, whose
// GIT_CONFIG_GLOBAL it replaces.
func RewriteURL(t *testing.T, prefix, dir string) {
	t.Helper()
	global := filepath.Join(t.TempDir(), "gitconfig")
	// Forward slashes: Git for Windows reads C:/x/y as a path, and the key is
	// split on its first and last dots, so dots inside the path are fine.
	Git(t, "", "config", "--file", global, "url."+filepath.ToSlash(dir)+"/.insteadOf", prefix)
	t.Setenv("GIT_CONFIG_GLOBAL", global)
}

// Git runs git in dir with args and returns its stdout. The command carries a
// fixed identity and default branch (-c) and fails the test on a non-zero exit.
func Git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{
		"-c", "user.name=ghx test",
		"-c", "user.email=ghx-test@example.com",
		"-c", "init.defaultBranch=main",
		"-c", "commit.gpgsign=false",
	}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	require.NoError(t, cmd.Run(), "git %v in %s: %s", args, dir, stderr.String())
	return stdout.String()
}

// NewBareRepo creates <parent>/<name>.git as a bare repository whose only
// commit, a README.md, sits on branch, and returns its path. The path works
// as a clone URL: ghx hands anything that isn't https:// to git unchanged.
func NewBareRepo(t *testing.T, parent, name, branch string) string {
	t.Helper()
	bare := filepath.Join(parent, name+".git")
	require.NoError(t, os.MkdirAll(bare, 0o755))
	// -b sets HEAD, so a clone with no --branch lands on the same branch.
	Git(t, bare, "init", "--bare", "--initial-branch", branch)

	seed := t.TempDir()
	Git(t, seed, "init", "--initial-branch", branch)
	require.NoError(t, os.WriteFile(filepath.Join(seed, "README.md"), []byte("# "+name+"\n"), 0o644))
	Git(t, seed, "add", "README.md")
	Git(t, seed, "commit", "-m", "Initial commit")
	Git(t, seed, "push", bare, "HEAD:refs/heads/"+branch)
	return bare
}

// AddCommit pushes a commit that creates file with content to branch of the
// bare repository at bare, as a second developer would.
func AddCommit(t *testing.T, bare, branch, file, content string) {
	t.Helper()
	work := t.TempDir()
	Git(t, work, "clone", "--branch", branch, bare, ".")
	require.NoError(t, os.WriteFile(filepath.Join(work, file), []byte(content), 0o644))
	Git(t, work, "add", file)
	Git(t, work, "commit", "-m", "Add "+file)
	Git(t, work, "push", "origin", branch)
}
