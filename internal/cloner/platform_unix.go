//go:build !windows

package cloner

import (
	"os"
)

// platformGitArgs go before every git subcommand ghx runs. Unix needs none.
var platformGitArgs []string

// platformCloneArgs are extra options for git clone. Unix needs none.
var platformCloneArgs []string

// interruptGit stops a git process whose context ended. SIGINT, unlike the
// SIGKILL exec.CommandContext sends by default, lets git remove a clone it
// had only half written, so the next run doesn't mistake it for a clone.
func interruptGit(p *os.Process) error {
	return p.Signal(os.Interrupt)
}

// isTransient reports whether a file operation failed only because another
// process briefly held a file open. Unix lets a directory be renamed or
// removed while files in it are open, so nothing is transient here.
func isTransient(error) bool {
	return false
}
