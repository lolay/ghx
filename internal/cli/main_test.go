package cli_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/jedib0t/go-pretty/v6/text"

	"github.com/lolay/ghx/internal/ui"
)

// TestMain points the home directory at an empty temporary directory, clears
// GITHUB_TOKEN and turns off ANSI colors for the whole package, so a test that
// forgets its own setup still never reads the developer's ~/.ghx.toml or
// token, and output compares as plain text on any terminal or runner. It also
// makes the run non-terminal (standard output is a file, as in a pipe), so the
// run-flow tests see the static table and the full summary; a test that wants
// the live table says so with terminal.
func TestMain(m *testing.M) {
	os.Exit(runIsolated(m))
}

func runIsolated(m *testing.M) int {
	home, err := os.MkdirTemp("", "ghx-cli-home-")
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "creating temp home:", err)
		return 1
	}
	defer func() { _ = os.RemoveAll(home) }()

	for _, key := range homeEnvKeys {
		if err := os.Setenv(key, home); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, "setting", key+":", err)
			return 1
		}
	}
	if err := os.Unsetenv("GITHUB_TOKEN"); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "clearing GITHUB_TOKEN:", err)
		return 1
	}
	text.DisableColors()

	stdoutFile, err = os.CreateTemp(home, "stdout-")
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "creating a stand-in for stdout:", err)
		return 1
	}
	defer func() { _ = stdoutFile.Close() }()
	ui.SetupTerminal(stdoutFile)
	return m.Run()
}

// stdoutFile is a regular file, which ui.SetupTerminal takes for a pipe or a
// redirect: the package's default, and what terminal puts back.
var stdoutFile *os.File

// homeEnvKeys are the variables os.UserHomeDir reads: HOME on Unix and
// macOS, USERPROFILE on Windows.
var homeEnvKeys = []string{"HOME", "USERPROFILE"}
