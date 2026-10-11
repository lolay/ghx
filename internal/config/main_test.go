package config_test

import (
	"fmt"
	"os"
	"testing"
)

// TestMain points the home directory at an empty temporary directory and
// clears GITHUB_TOKEN for the whole package, so a test that forgets its own
// isolateHome or t.Setenv still never reads the developer's ~/.ghx.toml or
// token. Tests that need a specific home or token set their own with
// t.Setenv, which restores this baseline afterwards.
func TestMain(m *testing.M) {
	os.Exit(runIsolated(m))
}

func runIsolated(m *testing.M) int {
	home, err := os.MkdirTemp("", "ghx-config-home-")
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
	return m.Run()
}

// homeEnvKeys are the variables os.UserHomeDir reads: HOME on Unix and
// macOS, USERPROFILE on Windows. Setting both keeps the tests portable.
var homeEnvKeys = []string{"HOME", "USERPROFILE"}

// isolateHome gives the test its own empty home directory and returns it.
func isolateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	for _, key := range homeEnvKeys {
		t.Setenv(key, home)
	}
	return home
}
