package cloner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/lolay/ghx/internal/redact"
)

// gitTimeout bounds each git subprocess; mirrors Python's timeout=300.
const gitTimeout = 5 * time.Minute

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
	msg := strings.TrimSpace(r.Stderr)
	if msg == "" {
		msg = strings.TrimSpace(r.Stdout)
	}
	if msg == "" && r.Err != nil {
		msg = r.Err.Error()
	}
	return redact.String(msg, r.secrets...)
}

// gitRunner runs the one git executable a sync found, with the token for the
// commands that fetch.
type gitRunner struct {
	path string
	cred credential
}

// run runs a git command that needs no credential.
func (g gitRunner) run(ctx context.Context, dir string, args ...string) gitResult {
	return g.exec(ctx, dir, nil, args)
}

// fetch runs a git command that talks to the remote (clone, pull) with the
// token in its environment, scoped to scopeURL's scheme and host.
func (g gitRunner) fetch(ctx context.Context, dir, scopeURL string, args ...string) gitResult {
	return g.exec(ctx, dir, g.cred.env(scopeURL), args)
}

func (g gitRunner) exec(ctx context.Context, dir string, env, args []string) gitResult {
	cctx, cancel := context.WithTimeout(ctx, gitTimeout)
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

// status runs git status --porcelain and reports whether the working tree has
// changes. The result is returned too: a failed status means ghx can't tell,
// and must not pull.
func (g gitRunner) status(ctx context.Context, repoDir string) (dirty bool, res gitResult) {
	res = g.run(ctx, repoDir, "status", "--porcelain")
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
