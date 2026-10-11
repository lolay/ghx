package cloner

import (
	"os/exec"
	"slices"
	"sync"
	"testing"
)

// These aliases give the black-box tests in package cloner_test the
// unexported helpers, which have no exported caller, without widening the
// package API.
var (
	ResolveURL      = resolveURL
	ResolveWikiURL  = resolveWikiURL
	CheckRepoNameOn = checkRepoName
	CheckGitVersion = checkGitVersion
	HasChanges      = hasChanges
	StripUserInfo   = stripUserInfo
	RetryWithin     = retryWithin
	InterruptGit    = interruptGit
	IsTransient     = isTransient
)

// GitCall is one git command ghx ran: its arguments (without the executable)
// and the environment it was given, nil when it inherited ghx's own.
type GitCall struct {
	Args []string
	Env  []string
}

// GitLog collects the git commands a test's code runs.
type GitLog struct {
	mu    sync.Mutex
	calls []GitCall
}

// Calls returns the commands run so far.
func (l *GitLog) Calls() []GitCall {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.calls)
}

// RecordGit logs every git command ghx runs for the rest of the test. With a
// nil fake each command then runs for real; otherwise fake runs instead, and
// can write to the command's Stdout and Stderr. It swaps a package variable,
// so the test can't run in parallel.
func RecordGit(t *testing.T, fake func(*exec.Cmd) error) *GitLog {
	t.Helper()
	log := &GitLog{}
	orig := runCommand
	runCommand = func(cmd *exec.Cmd) error {
		log.mu.Lock()
		log.calls = append(log.calls, GitCall{Args: slices.Clone(cmd.Args[1:]), Env: slices.Clone(cmd.Env)})
		log.mu.Unlock()
		if fake != nil {
			return fake(cmd)
		}
		return orig(cmd)
	}
	t.Cleanup(func() { runCommand = orig })
	return log
}
