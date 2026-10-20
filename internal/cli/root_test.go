package cli_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/charmbracelet/fang"
	"github.com/spf13/cobra"
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
	assertFlagsAreConfigKeys(t, cli.NewRootCmd(), "ssh")
}

// assertFlagsAreConfigKeys fails for a flag of cmd that has no config key,
// but for help, version and the flags named in flagOnly.
func assertFlagsAreConfigKeys(t *testing.T, cmd *cobra.Command, flagOnly ...string) {
	t.Helper()
	skip := map[string]bool{"help": true, "version": true}
	for _, name := range flagOnly {
		skip[name] = true
	}
	seen := 0

	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if skip[f.Name] {
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

// An org named like one of the subcommands cobra and fang add (completion,
// help, man) is reached by putting -- before it: everything after -- is a
// positional argument, so the sync runs and fails on the missing token.
func TestRun_DoubleDashSyncsAnOrgNamedLikeASubcommand(t *testing.T) {
	for _, org := range []string{"completion", "help", "man"} {
		t.Run(org, func(t *testing.T) {
			home := t.TempDir()
			for _, key := range homeEnvKeys {
				t.Setenv(key, home)
			}
			t.Setenv("GITHUB_TOKEN", "")

			code, _, errOut := runRoot(t, "--", org, t.TempDir())
			assert.Equal(t, 1, code)
			assert.Contains(t, errOut, "No GitHub token provided")
		})
	}
}

// fang title-cases the first word of an error on a terminal; a config value
// error starts with a file path, which must come out as it went in, including
// a local path with no directory separator.
func TestErrorStyles_KeepsAConfigPathAsIs(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"unix path", invalidValue("/tmp/home/.ghx.toml"), `/tmp/home/.ghx.toml: concurrency must be an integer, got string "8"`},
		{"windows path", invalidValue(`C:\Users\me\.ghx.toml`), `C:\Users\me\.ghx.toml: concurrency must be an integer, got string "8"`},
		{"path with no separator", invalidValue(".ghx.toml"), `.ghx.toml: concurrency must be an integer, got string "8"`},
		{"wrapped further", fmt.Errorf("sync: %w", invalidValue(".ghx.toml")), `sync: .ghx.toml: concurrency must be an integer, got string "8"`},
		{"plain error is title-cased as before", errors.New("unknown flag: --nope"), "Unknown flag: --nope"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			styles := cli.ErrorStyles(fangStyles(t), tt.err)

			assert.Equal(t, tt.want, strings.TrimSpace(styles.ErrorText.UnsetMargins().UnsetWidth().Render(tt.err.Error())))
		})
	}
}

// valueError stands in for config.Load's error: that type is unexported, and
// its text has no sentinel suffix.
type valueError struct{ msg string }

func (e valueError) Error() string { return e.msg }

func (e valueError) Unwrap() error { return config.ErrInvalidValue }

// invalidValue is the error config.Load returns for a string given to the
// integer key concurrency in the file at path.
func invalidValue(path string) error {
	return valueError{path + `: concurrency must be an integer, got string "8"`}
}

// fangStyles returns the styles fang hands its error handler, which it builds
// from a colour scheme and doesn't export.
func fangStyles(t *testing.T) fang.Styles {
	t.Helper()
	var got fang.Styles
	cmd := &cobra.Command{
		Use:  "probe",
		RunE: func(*cobra.Command, []string) error { return errors.New("probe") },
	}
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs(nil)
	_ = fang.Execute(t.Context(), cmd, fang.WithErrorHandler(func(_ io.Writer, styles fang.Styles, _ error) {
		got = styles
	}))
	return got
}
