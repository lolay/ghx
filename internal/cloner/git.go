package cloner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"time"
)

// gitTimeout bounds each git subprocess; mirrors Python's timeout=300.
const gitTimeout = 5 * time.Minute

// gitWaitDelay is how long a git process that was asked to stop (Ctrl-C, or
// gitTimeout) gets to clean up before it is killed.
const gitWaitDelay = 10 * time.Second

// ErrGitNotFound is returned by FindGit when there is no usable git on PATH.
var ErrGitNotFound = errors.New("git not found")

// runCommand runs a git command ghx built. It is a variable so the tests can
// see each command's arguments and environment (export_test.go).
var runCommand = (*exec.Cmd).Run

// FindGit returns the path of the git executable on PATH (git.exe on Windows),
// so a sync looks it up once and a missing git stops the run before it starts,
// rather than failing once per repo.
func FindGit() (string, error) {
	path, err := exec.LookPath("git")
	if errors.Is(err, exec.ErrDot) {
		// Running a git.exe that happens to sit in the current directory is how
		// a crafted checkout would run its own code, so it doesn't count.
		return "", fmt.Errorf("%w on PATH: %s is relative to the current directory, which ghx won't run; install Git (https://git-scm.com/downloads) and put it on PATH", ErrGitNotFound, path)
	}
	if err != nil {
		return "", fmt.Errorf("%w on PATH: install Git (https://git-scm.com/downloads) and run ghx again", ErrGitNotFound)
	}
	return path, nil
}

// gitResult captures what we need out of a git invocation: exit code,
// stdout, stderr, and any error from running the process itself (e.g.
// command not found or timeout).
type gitResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
	Err      error
}

// Success reports whether git exited cleanly.
func (r gitResult) Success() bool { return r.Err == nil && r.ExitCode == 0 }

// Message returns a user-facing failure string (stderr preferred).
func (r gitResult) Message() string {
	msg := strings.TrimSpace(r.Stderr)
	if msg == "" {
		msg = strings.TrimSpace(r.Stdout)
	}
	if msg == "" && r.Err != nil {
		msg = r.Err.Error()
	}
	return msg
}

// gitRunner runs the one git executable a sync found.
type gitRunner struct {
	path string
}

func (g gitRunner) run(ctx context.Context, dir string, args ...string) gitResult {
	cctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, g.path, slices.Concat(platformGitArgs, args)...)
	cmd.Cancel = func() error { return interruptGit(cmd.Process) }
	cmd.WaitDelay = gitWaitDelay
	if dir != "" {
		cmd.Dir = dir
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := runCommand(cmd)

	res := gitResult{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
		Err:    err,
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

func (g gitRunner) setIdentity(ctx context.Context, repoDir, author, email string) {
	if author != "" {
		g.run(ctx, repoDir, "config", "user.name", author)
	}
	if email != "" {
		g.run(ctx, repoDir, "config", "user.email", email)
	}
}
