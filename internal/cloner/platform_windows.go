//go:build windows

package cloner

import (
	"errors"
	"os"
	"syscall"
)

// platformGitArgs go before every git subcommand ghx runs. Git for Windows
// refuses paths over 260 characters unless core.longpaths is on, and a repo
// with deep paths inside a deep output directory crosses that easily.
var platformGitArgs = []string{"-c", "core.longpaths=true"}

// platformCloneArgs writes core.longpaths into each new clone's own config, so
// the user's own git commands in it work too, not only ghx's.
var platformCloneArgs = []string{"--config", "core.longpaths=true"}

// errorSharingViolation is ERROR_SHARING_VIOLATION, which package syscall
// doesn't name.
const errorSharingViolation syscall.Errno = 32

// interruptGit stops a git process whose context ended. Windows can't deliver
// os.Interrupt to another process, so it is killed; a Ctrl-C at the console
// still reaches git directly, because git shares ghx's console.
func interruptGit(p *os.Process) error {
	return p.Kill()
}

// isTransient reports whether a file operation failed only because another
// process (an antivirus scan, the search indexer, an editor) briefly held a
// file open. Windows refuses to rename or remove a directory while any file in
// it is open, and reports that as access denied or a sharing violation.
func isTransient(err error) bool {
	return errors.Is(err, syscall.ERROR_ACCESS_DENIED) || errors.Is(err, errorSharingViolation)
}
