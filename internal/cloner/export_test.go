package cloner

import (
	"context"
	"os/exec"
	"slices"
	"sync"
	"testing"
	"time"
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
	PickBranch      = pickBranch
)

// Timeouts the two kinds of git command get.
const (
	GitTimeout     = gitTimeout
	NetworkTimeout = networkTimeout
)

// NetworkArgs is a copy of the -c options every network command starts with.
func NetworkArgs() []string { return slices.Clone(networkArgs) }

// TransientMarkers is a copy of the stderr fragments that earn a retry.
func TransientMarkers() []string { return slices.Clone(transientMarkers) }

// SetRetryDelay sets how long a network command waits before its retry for the
// rest of the test. It swaps a package variable, so the test can't run in
// parallel.
func SetRetryDelay(t *testing.T, d time.Duration) {
	t.Helper()
	orig := retryDelay
	retryDelay = d
	t.Cleanup(func() { retryDelay = orig })
}

// MessageOf is gitResult.Message for a result with the given output, process
// error and secrets.
func MessageOf(stdout, stderr string, err error, secrets ...string) string {
	return gitResult{Stdout: stdout, Stderr: stderr, Err: err, secrets: secrets}.Message()
}

// Git gives the black-box tests the gitRunner methods, which have no exported
// caller, over the git at path (see FindGit) with an optional token.
type Git struct{ g gitRunner }

// NewGit returns a Git that runs the git at path and hands it token for the
// commands that fetch.
func NewGit(path, token string) Git {
	return Git{gitRunner{path: path, cred: newCredential(token)}}
}

// Fetch runs a network command, as processRepo does, and returns what it
// printed, whether it succeeded, and its Message.
func (g Git) Fetch(ctx context.Context, dir, scopeURL string, args ...string) (stdout string, ok bool, message string) {
	res := g.g.fetch(ctx, dir, scopeURL, args...)
	return res.Stdout, res.Success(), res.Message()
}

// CurrentBranch calls gitRunner.currentBranch.
func (g Git) CurrentBranch(ctx context.Context, dir string) (string, bool, error) {
	return g.g.currentBranch(ctx, dir)
}

// RevParse calls gitRunner.revParse.
func (g Git) RevParse(ctx context.Context, dir, ref string) (string, bool) {
	return g.g.revParse(ctx, dir, ref)
}

// ForkPoint calls gitRunner.forkPoint.
func (g Git) ForkPoint(ctx context.Context, dir, ref, commit string) (string, bool) {
	return g.g.forkPoint(ctx, dir, ref, commit)
}

// IsAncestor calls gitRunner.isAncestor.
func (g Git) IsAncestor(ctx context.Context, dir, a, b string) bool {
	return g.g.isAncestor(ctx, dir, a, b)
}

// UpstreamTrack calls gitRunner.upstreamTrack.
func (g Git) UpstreamTrack(ctx context.Context, dir, branch string) (string, bool) {
	return g.g.upstreamTrack(ctx, dir, branch)
}

// RemoteBranches calls gitRunner.remoteBranches.
func (g Git) RemoteBranches(ctx context.Context, dir string) []string {
	return g.g.remoteBranches(ctx, dir)
}

// OperationInProgress calls gitRunner.operationInProgress.
func (g Git) OperationInProgress(ctx context.Context, dir string) string {
	return g.g.operationInProgress(ctx, dir)
}

// GitCall is one git command ghx ran: its arguments (without the executable)
// and the environment it was given, nil when it inherited ghx's own.
type GitCall struct {
	Args []string
	Env  []string
}

// GitLog collects the git commands a test's code runs.
type GitLog struct {
	mu       sync.Mutex
	calls    []GitCall
	timeouts []time.Duration
}

// Timeouts returns the timeout each command so far was given, in the order
// the commands ran. Tests that read it run their commands one at a time.
func (l *GitLog) Timeouts() []time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.timeouts)
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
	origTimeout := withTimeout
	withTimeout = func(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
		log.mu.Lock()
		log.timeouts = append(log.timeouts, d)
		log.mu.Unlock()
		return origTimeout(ctx, d)
	}
	runCommand = func(cmd *exec.Cmd) error {
		log.mu.Lock()
		log.calls = append(log.calls, GitCall{Args: slices.Clone(cmd.Args[1:]), Env: slices.Clone(cmd.Env)})
		log.mu.Unlock()
		if fake != nil {
			return fake(cmd)
		}
		return orig(cmd)
	}
	t.Cleanup(func() { runCommand = orig; withTimeout = origTimeout })
	return log
}
