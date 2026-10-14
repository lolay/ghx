package cli_test

import (
	"bytes"
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lolay/ghx/internal/cli"
	"github.com/lolay/ghx/internal/config"
)

// escape is the byte that starts every ANSI sequence.
const escape = "\x1b"

// runRoot runs the root command through fang with args and returns its exit
// code and what it wrote to stdout and stderr.
func runRoot(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	cmd := cli.NewRootCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(args)
	code = cli.Run(t.Context(), cmd)
	return code, out.String(), errOut.String()
}

// The README promises that every option but --ssh can be a config key; this
// fails when a flag is added without one.
func TestRootCmd_EveryFlagButSSHIsAConfigKey(t *testing.T) {
	flagOnly := map[string]bool{"ssh": true, "help": true, "version": true}
	seen := 0

	cli.NewRootCmd().Flags().VisitAll(func(f *pflag.Flag) {
		if flagOnly[f.Name] {
			return
		}
		seen++
		assert.True(t, config.IsKey(f.Name), "flag --%s has no config key", f.Name)
	})

	assert.Positive(t, seen, "the walk found no flags")
}

func TestRun_HelpListsTheFlagsAndExamples(t *testing.T) {
	code, out, errOut := runRoot(t, "--help")

	assert.Equal(t, 0, code)
	assert.Empty(t, errOut)
	for _, want := range []string{
		"ghx <ORG> <PATH>",
		"EXAMPLES",
		"ghx --dry-run acme ~/src/acme",
		"ghx --ssh acme ~/src/acme",
		"ghx --include '^(api|web)-' acme ~/src/acme",
		"--token", "--type", "--concurrency", "--dry-run", "--clone-wiki", "--include", "--exclude",
		"--protocol", "--ssh", "--git-author", "--git-email", "--delete", "--deleted-dir",
		"--archived-dir", "--max-size", "--version",
	} {
		assert.Contains(t, out, want)
	}
}

func TestRun_OutputIsPlainWhenItIsNotATerminal(t *testing.T) {
	tests := []struct {
		name    string
		noColor string
	}{
		{"a buffer, as a pipe or file is", ""},
		{"NO_COLOR set", "1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("NO_COLOR", tt.noColor)

			_, out, _ := runRoot(t, "--help")
			assert.NotContains(t, out, escape, "help has no ANSI escapes")

			_, _, errOut := runRoot(t, "--bogus")
			assert.NotContains(t, errOut, escape, "an error has no ANSI escapes")
		})
	}
}

func TestPlainWriter_StripsEveryEscapeSequence(t *testing.T) {
	var out bytes.Buffer
	_, err := cli.PlainWriter(&out).Write([]byte(escape + "[1mbold" + escape + "[m " + escape + "[31mred" + escape + "[0m\n"))
	require.NoError(t, err)
	assert.Equal(t, "bold red\n", out.String())
}

func TestRun_ExitCodes(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantCode int
		wantErr  string
	}{
		{"help is success", []string{"--help"}, 0, ""},
		{"an unknown flag fails", []string{"--bogus", "acme", "out"}, 1, "--bogus"},
		{"missing arguments fail with the reason", nil, 1, "Accepts 2 arg(s), received 0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, _, errOut := runRoot(t, tt.args...)
			assert.Equal(t, tt.wantCode, code)
			assert.Contains(t, errOut, tt.wantErr, "fang prints the error, so its text isn't lost")
		})
	}
}

func TestRun_PrintsTheBuildVersion(t *testing.T) {
	stamp(t, "0.1.0", "abc1234", "42")

	code, out, _ := runRoot(t, "--version")
	assert.Equal(t, 0, code)
	assert.Equal(t, "ghx 0.1.0+abc1234 (42)\n", out)
}

func TestRun_CompletionWorksNextToTheTwoArguments(t *testing.T) {
	code, out, _ := runRoot(t, "completion", "bash")
	assert.Equal(t, 0, code)
	assert.Contains(t, out, "bash completion V2 for ghx")
}
