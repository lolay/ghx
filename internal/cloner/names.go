package cloner

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
)

// ErrUnusableName marks a repo name that ghx won't turn into a directory: it
// would escape the output directory, or this OS can't hold it.
var ErrUnusableName = errors.New("unusable repo name")

// CheckRepoName returns an error wrapping ErrUnusableName when name can't be
// used as a directory directly under the output directory on this OS. Names
// come from the GitHub API and from the previous run's manifest, which a user
// can edit, so neither is trusted to be a single, local path element.
func CheckRepoName(name string) error {
	return checkRepoName(name, runtime.GOOS)
}

// checkRepoName takes the OS as an argument so the Windows rules can be
// tested on any machine.
func checkRepoName(name, goos string) error {
	if reason := unusableReason(name, goos == "windows"); reason != "" {
		return fmt.Errorf("%w %q: %s", ErrUnusableName, name, reason)
	}
	return nil
}

func unusableReason(name string, windows bool) string {
	switch {
	case name == "":
		return "empty"
	case name == "." || name == "..":
		return "not a directory name"
	case strings.ContainsAny(name, `/\`):
		return "contains a path separator"
	case strings.ContainsFunc(name, func(r rune) bool { return r < 0x20 || r == 0x7f }):
		return "contains a control character"
	}
	if !windows {
		return ""
	}
	switch {
	case strings.ContainsAny(name, `<>:"|?*`):
		return "contains a character Windows doesn't allow in file names"
	case strings.HasSuffix(name, ".") || strings.HasSuffix(name, " "):
		return "ends in a dot or a space, which Windows drops from file names"
	case isWindowsDeviceName(name):
		return "a reserved device name on Windows"
	}
	return ""
}

// isWindowsDeviceName reports whether Windows reads name as a device (CON,
// NUL, COM1 and so on), with or without an extension: Windows 10 treats
// "CON.wiki" as the console too, so a repo named CON is skipped with its wiki.
func isWindowsDeviceName(name string) bool {
	base, _, _ := strings.Cut(name, ".")
	base = strings.TrimRight(base, " ")
	switch strings.ToUpper(base) {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
		return true
	}
	if len(base) < 4 {
		return false
	}
	prefix := strings.ToUpper(base[:3])
	if prefix != "COM" && prefix != "LPT" {
		return false
	}
	switch base[3:] {
	case "0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "¹", "²", "³":
		return true
	}
	return false
}
