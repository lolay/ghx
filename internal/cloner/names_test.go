package cloner_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lolay/ghx/internal/cloner"
)

func TestCheckRepoName(t *testing.T) {
	tests := []struct {
		name    string
		repo    string
		unix    string // the reason on Linux and macOS; "" means usable
		windows string // the reason on Windows; "" means usable
	}{
		{"a plain name", "app", "", ""},
		{"dots, dashes and underscores", "acme.github.io_v-2", "", ""},
		{"a name that only starts like a device", "console", "", ""},
		{"a device name inside a longer name", "my-con", "", ""},
		{"COM with no digit", "COMx", "", ""},
		{"empty", "", "empty", "empty"},
		{"dot", ".", "not a directory name", "not a directory name"},
		{"dot dot", "..", "not a directory name", "not a directory name"},
		{"a parent path", "../outside", "path separator", "path separator"},
		{"a nested path", "a/b", "path separator", "path separator"},
		{"a backslash", `a\b`, "path separator", "path separator"},
		{"a control character", "a\nb", "control character", "control character"},
		{"CON", "CON", "", "reserved device name"},
		{"lower-case nul", "nul", "", "reserved device name"},
		{"a device name with an extension", "aux.wiki", "", "reserved device name"},
		{"a device name with trailing spaces before the dot", "PRN  .txt", "", "reserved device name"},
		{"COM1", "COM1", "", "reserved device name"},
		{"LPT9", "lpt9", "", "reserved device name"},
		{"COM with a superscript digit", "COM¹", "", "reserved device name"},
		{"CONIN$", "CONIN$", "", "reserved device name"},
		{"a trailing dot", "app.", "", "ends in a dot or a space"},
		{"a trailing space", "app ", "", "ends in a dot or a space"},
		{"a colon", "a:b", "", "character Windows doesn't allow"},
		{"a question mark", "what?", "", "character Windows doesn't allow"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for goos, want := range map[string]string{"linux": tt.unix, "darwin": tt.unix, "windows": tt.windows} {
				err := cloner.CheckRepoNameOn(tt.repo, goos)
				if want == "" {
					assert.NoError(t, err, goos)
					continue
				}
				require.ErrorIs(t, err, cloner.ErrUnusableName, goos)
				assert.Contains(t, err.Error(), want, goos)
			}
		})
	}
}

func TestCheckRepoName_UsesThisOS(t *testing.T) {
	require.NoError(t, cloner.CheckRepoName("app"))
	require.ErrorIs(t, cloner.CheckRepoName(".."), cloner.ErrUnusableName)
}
