package cli_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lolay/ghx/internal/buildinfo"
	"github.com/lolay/ghx/internal/cli"
)

func TestVersion_PrintsTheBuildVersion(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		version   string
		sha       string
		buildCode string
		want      string
	}{
		{"stamped from a clean tree", []string{"--version"}, "0.1.0", "abc1234", "42", "ghx 0.1.0+abc1234 (42)\n"},
		{"stamped from a dirty tree", []string{"--version"}, "0.1.0", "abc1234.dirty", "42", "ghx 0.1.0+abc1234.dirty (42)\n"},
		{"unstamped", []string{"--version"}, "dev", "", "", "ghx dev\n"},
		{"the -v shorthand", []string{"-v"}, "0.1.0", "abc1234", "42", "ghx 0.1.0+abc1234 (42)\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stamp(t, tt.version, tt.sha, tt.buildCode)

			var out bytes.Buffer
			cmd := cli.NewRootCmd()
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			// No ORG or PATH: --version must not need them.
			cmd.SetArgs(tt.args)

			require.NoError(t, cmd.ExecuteContext(t.Context()))
			assert.Equal(t, tt.want, out.String())
		})
	}
}

// stamp sets the buildinfo variables as -ldflags -X would, for one test.
// newRootCmd reads them when it builds the command, so call stamp first.
func stamp(t *testing.T, version, sha, buildCode string) {
	t.Helper()
	saved := [3]string{buildinfo.Version, buildinfo.Sha, buildinfo.BuildCode}
	t.Cleanup(func() {
		buildinfo.Version, buildinfo.Sha, buildinfo.BuildCode = saved[0], saved[1], saved[2]
	})
	buildinfo.Version, buildinfo.Sha, buildinfo.BuildCode = version, sha, buildCode
}
