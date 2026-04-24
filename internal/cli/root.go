// Package cli wires the cobra command together with all of the package
// pieces. The public entry point is Execute, which runs the root command
// and returns the exit code.
package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
)

// Execute runs the ghx command with the given arguments (typically
// os.Args[1:]) and returns an exit code suitable for os.Exit.
func Execute() int {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	cmd := newRootCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	if err := cmd.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		return 1
	}
	return 0
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
		Use:   "ghx-go <ORG> <PATH>",
		Short: "Export and sync all GitHub repos for an organization",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(cmd, args, f)
		},
	}

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
