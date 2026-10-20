package cli_test

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lolay/ghx/internal/buildinfo"
	"github.com/lolay/ghx/internal/cli"
)

func TestVersion_PrintsTheBuildVersion(t *testing.T) {
	tests := []struct {
		name      string
		newCmd    func() *cobra.Command
		args      []string
		version   string
		sha       string
		buildCode string
		want      string
	}{
		{"stamped from a clean tree", cli.NewRootCmd, []string{"--version"}, "0.1.0", "abc1234", "42", "ghx 0.1.0+abc1234 (42)\n"},
		{"stamped from a dirty tree", cli.NewRootCmd, []string{"--version"}, "0.1.0", "abc1234.dirty", "42", "ghx 0.1.0+abc1234.dirty (42)\n"},
		{"unstamped", cli.NewRootCmd, []string{"--version"}, "dev", "", "", "ghx dev\n"},
		{"the -v shorthand", cli.NewRootCmd, []string{"-v"}, "0.1.0", "abc1234", "42", "ghx 0.1.0+abc1234 (42)\n"},
		{"ghb", cli.NewBranchesCmd, []string{"--version"}, "0.1.0", "abc1234", "42", "ghb 0.1.0+abc1234 (42)\n"},
		{"ghb unstamped", cli.NewBranchesCmd, []string{"-v"}, "dev", "", "", "ghb dev\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stamp(t, tt.version, tt.sha, tt.buildCode)

			var out bytes.Buffer
			cmd := tt.newCmd()
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			// No ORG or PATH: --version must not need them.
			cmd.SetArgs(tt.args)

			require.NoError(t, cmd.ExecuteContext(t.Context()))
			assert.Equal(t, tt.want, out.String())
		})
	}
}

func TestMan_PrintsAManPageForEachBinary(t *testing.T) {
	tests := []struct {
		name   string
		newCmd func() *cobra.Command
		want   string
	}{
		{"ghx", cli.NewRootCmd, ".TH GHX 1 "},
		{"ghb", cli.NewBranchesCmd, ".TH GHB 1 "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := tt.newCmd()
			cmd.SetArgs([]string{"man"})

			// fang adds the hidden man command, so this goes through Run, and
			// the command writes to os.Stdout rather than cmd's writer.
			var code int
			out := captureStdout(t, func() { code = cli.Run(t.Context(), cmd) })
			require.Equal(t, 0, code, "output:\n%s", out)
			assert.Contains(t, out, tt.want)
		})
	}
}

// captureStdout returns what fn writes to os.Stdout. It swaps a process-wide
// variable, so the calling test can't run in parallel.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	orig := os.Stdout
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = orig })

	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	fn()
	os.Stdout = orig
	require.NoError(t, w.Close())
	return <-done
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
