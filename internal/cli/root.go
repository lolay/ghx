// Package cli wires the cobra command together with all of the package
// pieces. The public entry point is Execute, which runs the root command
// and returns the exit code.
package cli

import (
	"context"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/fang"
	"github.com/spf13/cobra"

	"github.com/lolay/ghx/internal/buildinfo"
	"github.com/lolay/ghx/internal/ui"
)

// Execute runs the ghx command with the process's arguments and returns an
// exit code suitable for os.Exit: 0 on success, 1 on any error.
func Execute() int {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	ui.SetupTerminal(os.Stdout)

	return execute(ctx, newRootCmd())
}

// execute runs cmd through fang, which styles the help and prints the error
// itself, so no caller prints it again. ctx carries the signal handling.
func execute(ctx context.Context, cmd *cobra.Command) int {
	// NO_COLOR asks for no colour, which fang's own detection reads as "keep
	// bold and resets", still escape codes; strip them all.
	if os.Getenv("NO_COLOR") != "" {
		cmd.SetOut(plainWriter(cmd.OutOrStdout()))
		cmd.SetErr(plainWriter(cmd.ErrOrStderr()))
	}
	if err := fang.Execute(ctx, cmd, fang.WithVersion(buildinfo.String()), fang.WithErrorHandler(errorHandler)); err != nil {
		return 1
	}
	return 0
}

// errorHandler is fang's default error handler with one exception: see
// errorStyles.
func errorHandler(w io.Writer, styles fang.Styles, err error) {
	fang.DefaultErrorHandler(w, errorStyles(styles, err), err)
}

// errorStyles returns styles unchanged unless err's first word is a path, as
// in a config error ("<path>: concurrency must be ..."). On a terminal fang
// title-cases the first word of an error, which would show a path that
// doesn't exist, so such errors keep their case.
func errorStyles(styles fang.Styles, err error) fang.Styles {
	first, _, _ := strings.Cut(strings.TrimSpace(err.Error()), " ")
	if strings.ContainsAny(first, `/\`) {
		styles.ErrorText = styles.ErrorText.UnsetTransform()
	}
	return styles
}

// plainWriter returns a writer that drops every ANSI escape sequence before
// writing to w.
func plainWriter(w io.Writer) io.Writer {
	return &colorprofile.Writer{Forward: w, Profile: colorprofile.NoTTY}
}

// flags is the raw flag state. Pointer-value helpers are used for
// booleans and integers so we can tell "flag not set" from "flag set to
// zero value" via cobra's Flags().Changed().
type flags struct {
	token        string
	repoType     string
	concurrency  int
	dryRun       bool
	cloneWiki    bool
	include      string
	exclude      string
	protocol     string
	ssh          bool
	gitAuthor    string
	gitEmail     string
	shouldDelete bool
	deletedDir   string
	archivedDir  string
	maxSize      int
}

func newRootCmd() *cobra.Command {
	f := &flags{}
	cmd := &cobra.Command{
		Use:   "ghx <ORG> <PATH>",
		Short: "Export and sync all GitHub repos for an organization",
		Example: `  # Preview what would be cloned, without touching the disk
  ghx --dry-run acme ~/src/acme

  # Clone over SSH instead of HTTPS
  ghx --ssh acme ~/src/acme

  # Only repos whose names match a pattern
  ghx --include '^(api|web)-' acme ~/src/acme`,
		Args: cobra.ExactArgs(2),
		// Setting Version gives the command --version (and -v); cobra handles
		// it before checking the two required arguments. fang sets it again
		// from the same string.
		Version: buildinfo.String(),
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(cmd, args, f)
		},
	}
	// The versioning standard's form, "ghx <release>+<sha> (<buildCode>)",
	// in place of cobra's "ghx version ...".
	cmd.SetVersionTemplate("{{.Name}} {{.Version}}\n")

	pf := cmd.Flags()
	pf.StringVarP(&f.token, "token", "t", "", "GitHub Personal Access Token.")
	pf.StringVar(&f.repoType, "type", "", "Repo type: all|public|private|forks|sources.")
	pf.IntVarP(&f.concurrency, "concurrency", "c", 0, "Parallel workers.")
	pf.BoolVar(&f.dryRun, "dry-run", false, "Preview changes without doing anything.")
	pf.BoolVar(&f.cloneWiki, "clone-wiki", false, "Also clone associated wikis.")
	pf.StringVar(&f.include, "include", "", "Regex to include only matching repo names.")
	pf.StringVar(&f.exclude, "exclude", "", "Regex to exclude matching repo names.")
	pf.StringVar(&f.protocol, "protocol", "", "Git transport protocol (https|ssh).")
	pf.BoolVar(&f.ssh, "ssh", false, "Shortcut for --protocol ssh.")
	pf.StringVar(&f.gitAuthor, "git-author", "", "Set git user.name in each cloned repo.")
	pf.StringVar(&f.gitEmail, "git-email", "", "Set git user.email in each cloned repo.")
	pf.BoolVar(&f.shouldDelete, "delete", false, "Permanently delete removed/archived repos.")
	pf.StringVar(&f.deletedDir, "deleted-dir", "", "Directory for removed repos.")
	pf.StringVar(&f.archivedDir, "archived-dir", "", "Directory for archived repos.")
	pf.IntVar(&f.maxSize, "max-size", 0, "Skip repos larger than this size in MB.")

	return cmd
}
