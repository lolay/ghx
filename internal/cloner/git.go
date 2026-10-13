package cloner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/lolay/ghx/internal/redact"
)

// gitTimeout bounds each local git subprocess; mirrors Python's timeout=300.
const gitTimeout = 5 * time.Minute

// networkTimeout bounds a git command that talks to the remote (clone, fetch,
// pull). A big repo over a slow link legitimately takes far longer than a
// local command, so the stall check below, not this, catches a dead
// connection; this only stops a transfer that crawls forever.
const networkTimeout = 60 * time.Minute

// networkArgs make git give up on a connection that has moved fewer than 1000
// bytes a second for 60 seconds, so a stalled transfer fails (and may be
// retried) in a minute instead of holding a worker until networkTimeout.
var networkArgs = []string{"-c", "http.lowSpeedLimit=1000", "-c", "http.lowSpeedTime=60"}

// retryDelay is how long fetch waits before its one retry. It is a variable so
// the tests can set it to 0 (export_test.go).
var retryDelay = 2 * time.Second

// transientMarkers are the pieces of git's stderr that mark a failure worth
// one more try: a dropped or refused connection, a DNS blip, a rate limit or a
// server error. The list is fixed on purpose. Anything not on it, such as
// "Repository not found", an authentication failure or a 4xx other than 429,
// would fail the same way again and is reported at once.
var transientMarkers = []string{
	"RPC failed",
	"early EOF",
	"the remote end hung up unexpectedly",
	"Could not resolve host",
	"Connection timed out",
	"Connection reset",
	"HTTP 429",
	"The requested URL returned error: 5",
	"unexpected disconnect",
}

// withTimeout bounds a git command's context. It is a variable so the tests
// can see the timeout each command got (export_test.go).
var withTimeout = context.WithTimeout

// gitWaitDelay is how long a git process that was asked to stop (Ctrl-C, or
// gitTimeout) gets to clean up before it is killed.
const gitWaitDelay = 10 * time.Second

// ErrGitNotFound is returned by FindGit when there is no usable git on PATH.
var ErrGitNotFound = errors.New("git not found")

// ErrGitTooOld is returned by FindGit when git predates GIT_CONFIG_COUNT,
// which ghx needs to hand git the token (see credential).
var ErrGitTooOld = errors.New("git is too old")

// minGitMajor and minGitMinor are the oldest git ghx runs: 2.31 added
// GIT_CONFIG_COUNT.
const (
	minGitMajor = 2
	minGitMinor = 31
)

// runCommand runs a git command ghx built. It is a variable so the tests can
// see each command's arguments and environment (export_test.go).
var runCommand = (*exec.Cmd).Run

// FindGit returns the path of the git executable on PATH (git.exe on
// Windows) after checking it is git 2.31 or later, so a sync looks it up once
// and a missing or old git stops the run before it starts, rather than failing
// once per repo.
func FindGit(ctx context.Context) (string, error) {
	path, err := exec.LookPath("git")
	if errors.Is(err, exec.ErrDot) {
		// Running a git.exe that happens to sit in the current directory is how
		// a crafted checkout would run its own code, so it doesn't count.
		return "", fmt.Errorf("%w on PATH: %s is relative to the current directory, which ghx won't run; install Git (https://git-scm.com/downloads) and put it on PATH", ErrGitNotFound, path)
	}
	if err != nil {
		return "", fmt.Errorf("%w on PATH: install Git (https://git-scm.com/downloads) and run ghx again", ErrGitNotFound)
	}
	res := gitRunner{path: path}.run(ctx, "", "version")
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !res.Success() {
		return "", fmt.Errorf("running %s version: %s", path, res.Message())
	}
	if err := checkGitVersion(res.Stdout); err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}
	return path, nil
}

// checkGitVersion reads `git version` output, such as "git version 2.39.3
// (Apple Git-145)" or "git version 2.45.1.windows.1", and fails below 2.31.
func checkGitVersion(output string) error {
	fields := strings.Fields(output)
	if len(fields) < 3 || fields[0] != "git" || fields[1] != "version" {
		return fmt.Errorf("can't read git's version from %q", strings.TrimSpace(output))
	}
	parts := strings.SplitN(fields[2], ".", 3)
	if len(parts) < 2 {
		return fmt.Errorf("can't read git's version from %q", fields[2])
	}
	major, errMajor := strconv.Atoi(parts[0])
	minor, errMinor := strconv.Atoi(parts[1])
	if errMajor != nil || errMinor != nil {
		return fmt.Errorf("can't read git's version from %q", fields[2])
	}
	if major < minGitMajor || major == minGitMajor && minor < minGitMinor {
		return fmt.Errorf("%w: ghx needs git %d.%d or later and found %s; update Git (https://git-scm.com/downloads)",
			ErrGitTooOld, minGitMajor, minGitMinor, fields[2])
	}
	return nil
}

// gitResult captures what we need out of a git invocation: exit code,
// stdout, stderr, and any error from running the process itself (e.g.
// command not found or timeout).
type gitResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
	Err      error
	// secrets are masked in Message: the token and its header value.
	secrets []string
}

// Success reports whether git exited cleanly.
func (r gitResult) Success() bool { return r.Err == nil && r.ExitCode == 0 }

// Message returns a user-facing failure string (stderr preferred), which
// becomes a failed repo's Detail. The token and its header value are masked
// whatever git printed: ghx no longer gives git the token in a URL, but a
// user's GIT_TRACE_CURL with GIT_TRACE_REDACT=0, or an old origin URL that a
// fatal: line quotes, could still bring it back.
func (r gitResult) Message() string {
	msg := lastFatalLine(r.Stderr)
	if msg == "" {
		msg = strings.TrimSpace(r.Stderr)
	}
	if msg == "" {
		msg = strings.TrimSpace(r.Stdout)
	}
	if msg == "" && r.Err != nil {
		msg = r.Err.Error()
	}
	return redact.String(msg, r.secrets...)
}

// lastFatalLine returns the last line of stderr that starts "fatal:" or
// "error:", or "" when there is none. Git buries the reason for a failure
// under progress output and multi-line "hint:" blocks, and a repo's detail
// should be the reason alone.
func lastFatalLine(stderr string) string {
	lines := strings.Split(stderr, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimRight(lines[i], "\r")
		if strings.HasPrefix(line, "fatal:") || strings.HasPrefix(line, "error:") {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

// isTransientFailure reports whether git's stderr names a failure that a
// second attempt may get past.
func isTransientFailure(stderr string) bool {
	return slices.ContainsFunc(transientMarkers, func(m string) bool {
		return strings.Contains(stderr, m)
	})
}

// gitRunner runs the one git executable a sync found, with the token for the
// commands that fetch.
type gitRunner struct {
	path string
	cred credential
}

// run runs a local git command that needs no credential.
func (g gitRunner) run(ctx context.Context, dir string, args ...string) gitResult {
	return g.exec(ctx, dir, nil, gitTimeout, args)
}

// fetch runs a git command that talks to the remote (clone, pull) with the
// token in its environment, scoped to scopeURL's scheme and host. It gives the
// command the network timeout and the stall check, and tries it once more
// after retryDelay when it failed in a way transientMarkers lists. It is here,
// not in each caller, so every network command gets all three.
func (g gitRunner) fetch(ctx context.Context, dir, scopeURL string, args ...string) gitResult {
	env := g.cred.env(scopeURL)
	full := slices.Concat(networkArgs, args)

	dest, isClone := cloneDestination(dir, args)
	existed := false
	if isClone {
		_, err := os.Lstat(dest)
		existed = err == nil
	}

	res := g.exec(ctx, dir, env, networkTimeout, full)
	if res.Success() || ctx.Err() != nil || !isTransientFailure(res.Stderr) {
		return res
	}
	if isClone && !existed {
		// Git removes a clone it fails to finish, but a killed or crashed
		// one can leave a partial directory, which would make the retry fail
		// with "already exists". A destination that was there before the
		// first attempt isn't ours to delete.
		if err := os.RemoveAll(dest); err != nil {
			return res
		}
	}
	timer := time.NewTimer(retryDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return res
	case <-timer.C:
	}
	return g.exec(ctx, dir, env, networkTimeout, full)
}

// cloneDestination returns where a git clone in args puts its repo, and
// whether args is a clone at all. The destination is clone's last argument; dir
// is the directory the command runs in, "" for the current one.
func cloneDestination(dir string, args []string) (string, bool) {
	for i := 0; i < len(args); i++ {
		if args[i] == "-c" {
			i++
			continue
		}
		if args[i] != "clone" || i == len(args)-1 {
			return "", false
		}
		dest := args[len(args)-1]
		if dir != "" && !filepath.IsAbs(dest) {
			dest = filepath.Join(dir, dest)
		}
		return dest, true
	}
	return "", false
}

func (g gitRunner) exec(ctx context.Context, dir string, env []string, timeout time.Duration, args []string) gitResult {
	cctx, cancel := withTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, g.path, slices.Concat(platformGitArgs, args)...)
	cmd.Cancel = func() error { return interruptGit(cmd.Process) }
	cmd.WaitDelay = gitWaitDelay
	cmd.Env = env
	if dir != "" {
		cmd.Dir = dir
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := runCommand(cmd)

	res := gitResult{
		Stdout:  stdout.String(),
		Stderr:  stderr.String(),
		Err:     err,
		secrets: g.cred.secrets(),
	}
	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	}
	return res
}

// status runs git status --porcelain and reports whether tracked files have
// changes. Untracked files don't count: git refuses, safely, to overwrite one
// with an incoming file, and otherwise leaves them alone. The result is
// returned too: a failed status means ghx can't tell, and must not update the
// working tree.
func (g gitRunner) status(ctx context.Context, repoDir string) (dirty bool, res gitResult) {
	res = g.run(ctx, repoDir, "status", "--porcelain", "--untracked-files=no")
	return hasChanges(res.Stdout), res
}

// hasChanges reads git status --porcelain output: any line means a change.
// Trimming covers the CRLF line ends a Windows git could print.
func hasChanges(porcelain string) bool {
	return strings.TrimSpace(porcelain) != ""
}

// cleanOrigin rewrites an HTTPS origin URL that carries user info, as clones
// made by ghx before the token moved to git's environment do, to the same URL
// without it. SSH and local-path origins, and a repo with no origin, are left
// alone.
func (g gitRunner) cleanOrigin(ctx context.Context, repoDir string) error {
	got := g.run(ctx, repoDir, "remote", "get-url", "origin")
	if !got.Success() {
		return nil
	}
	clean, ok := stripUserInfo(strings.TrimSpace(got.Stdout))
	if !ok {
		return nil
	}
	if set := g.run(ctx, repoDir, "remote", "set-url", "origin", clean); !set.Success() {
		return fmt.Errorf("removing the credential from the origin URL: %s", set.Message())
	}
	return nil
}

func (g gitRunner) setIdentity(ctx context.Context, repoDir, author, email string) {
	if author != "" {
		g.run(ctx, repoDir, "config", "user.name", author)
	}
	if email != "" {
		g.run(ctx, repoDir, "config", "user.email", email)
	}
}

// currentBranch returns the branch HEAD points at. detached is true, with no
// error, when HEAD points at a commit instead (symbolic-ref -q exits 1 and
// prints nothing). An unborn branch, in a repo with no commits yet, still has
// its name.
func (g gitRunner) currentBranch(ctx context.Context, dir string) (name string, detached bool, err error) {
	res := g.run(ctx, dir, "symbolic-ref", "--short", "-q", "HEAD")
	name = strings.TrimSpace(res.Stdout)
	switch {
	case res.Success():
		return name, false, nil
	case ctx.Err() == nil && res.ExitCode == 1 && name == "":
		return "", true, nil
	}
	return "", false, fmt.Errorf("reading the current branch: %s", res.Message())
}

// revParse resolves ref to the SHA of the commit it names. ok is false when
// there is no such ref or it names nothing that is a commit.
func (g gitRunner) revParse(ctx context.Context, dir, ref string) (sha string, ok bool) {
	res := g.run(ctx, dir, "rev-parse", "-q", "--verify", ref+"^{commit}")
	if !res.Success() {
		return "", false
	}
	return strings.TrimSpace(res.Stdout), true
}

// forkPoint returns where commit forked from any version of ref its reflog
// remembers, so a branch that was once all on origin still reads that way
// after an earlier fetch saw origin rewritten. ok is false when there's no
// such point, such as when reflogs are off.
func (g gitRunner) forkPoint(ctx context.Context, dir, ref, commit string) (sha string, ok bool) {
	res := g.run(ctx, dir, "merge-base", "--fork-point", ref, commit)
	sha = strings.TrimSpace(res.Stdout)
	if !res.Success() || sha == "" {
		return "", false
	}
	return sha, true
}

// isAncestor reports whether commit a is reachable from commit b (or is b).
// Any failure, such as a commit that doesn't exist, reads as false.
func (g gitRunner) isAncestor(ctx context.Context, dir, a, b string) bool {
	return g.run(ctx, dir, "merge-base", "--is-ancestor", a, b).Success()
}

// upstreamTrack returns the full ref a local branch tracks, such as
// refs/remotes/origin/main, and whether that ref is gone (the remote deleted
// the branch and a fetch --prune removed the tracking ref). upstream is ""
// when the branch tracks nothing or doesn't exist.
func (g gitRunner) upstreamTrack(ctx context.Context, dir, branch string) (upstream string, gone bool) {
	res := g.run(ctx, dir, "for-each-ref", "--format=%(upstream)%00%(upstream:track)", "refs/heads/"+branch)
	if !res.Success() {
		return "", false
	}
	line := strings.TrimRight(strings.SplitN(res.Stdout, "\n", 2)[0], "\r")
	upstream, track, _ := strings.Cut(line, "\x00")
	return upstream, strings.TrimSpace(track) == "[gone]"
}

// remoteBranches lists the branches origin has, newest commit first, as the
// names after refs/remotes/origin/ (so feature/x keeps its slash). origin's
// HEAD pointer is not a branch and is left out.
func (g gitRunner) remoteBranches(ctx context.Context, dir string) []string {
	res := g.run(ctx, dir, "for-each-ref", "--sort=-committerdate", "--format=%(refname:lstrip=3)", "refs/remotes/origin")
	if !res.Success() {
		return nil
	}
	var branches []string
	for line := range strings.Lines(res.Stdout) {
		name := strings.TrimSpace(line)
		if name != "" && name != "HEAD" {
			branches = append(branches, name)
		}
	}
	return branches
}

// operationInProgress names the merge, rebase, cherry-pick or revert a repo is
// in the middle of, or returns "" when there is none (or git can't say). Such
// a repo must not be switched or pulled. git has no command for this, so it
// looks for the state files git leaves in the git directory. ctx is the first
// argument, as for the other probes, so a Ctrl-C stops the git call.
func (g gitRunner) operationInProgress(ctx context.Context, dir string) string {
	res := g.run(ctx, dir, "rev-parse", "--git-dir")
	if !res.Success() {
		return ""
	}
	gitDir := strings.TrimSpace(res.Stdout)
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(dir, gitDir)
	}
	states := []struct {
		operation string
		paths     []string
	}{
		{"merge", []string{"MERGE_HEAD"}},
		{"rebase", []string{"rebase-merge", "rebase-apply"}},
		{"cherry-pick", []string{"CHERRY_PICK_HEAD"}},
		{"revert", []string{"REVERT_HEAD"}},
	}
	for _, s := range states {
		for _, p := range s.paths {
			if _, err := os.Lstat(filepath.Join(gitDir, p)); err == nil {
				return s.operation
			}
		}
	}
	return ""
}

// pickBranch chooses the branch to put a repo on: the default branch when
// origin has it, else main, else master, else the one with the newest commit
// (branches is newest first, as remoteBranches returns it), else "" when
// origin has no branches.
func pickBranch(defaultBranch string, branches []string) string {
	for _, want := range []string{defaultBranch, "main", "master"} {
		if want != "" && slices.Contains(branches, want) {
			return want
		}
	}
	if len(branches) > 0 {
		return branches[0]
	}
	return ""
}
