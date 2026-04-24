package cloner

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"time"
)

// gitTimeout bounds each git subprocess; mirrors Python's timeout=300.
const gitTimeout = 5 * time.Minute

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

func runGit(ctx context.Context, cwd string, args ...string) gitResult {
	cctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, "git", args...)
	if cwd != "" {
		cmd.Dir = cwd
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	res := gitResult{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}
	if err != nil {
		res.Err = err
	}
	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	}
	return res
}

func isDirty(ctx context.Context, repoDir string) bool {
	r := runGit(ctx, repoDir, "status", "--porcelain")
	return strings.TrimSpace(r.Stdout) != ""
}

func setGitIdentity(ctx context.Context, repoDir, author, email string) {
	if author != "" {
		runGit(ctx, repoDir, "config", "user.name", author)
	}
	if email != "" {
		runGit(ctx, repoDir, "config", "user.email", email)
	}
}
