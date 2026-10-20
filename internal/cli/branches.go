package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/lolay/ghx/internal/branches"
	"github.com/lolay/ghx/internal/config"
	"github.com/lolay/ghx/internal/ghapi"
	"github.com/lolay/ghx/internal/ui"
)

// now is the time branch ages are measured from; tests pin it.
var now = time.Now

// pacerSleep is how a rate-limit wait sleeps; nil is a real timer. Tests swap
// it so a wait takes no time.
var pacerSleep func(ctx context.Context, d time.Duration) error

// Report formats --format accepts.
const (
	formatText = "text"
	formatJSON = "json"
)

// branchesFlags is the raw flag state of ghb; see flags.
type branchesFlags struct {
	token           string
	repoType        string
	include         string
	exclude         string
	concurrency     int
	includeArchived bool
	staleDays       int
	expectedDefault string
	format          string
}

func newBranchesCmd() *cobra.Command {
	f := &branchesFlags{}
	cmd := &cobra.Command{
		Use:   "ghb <ORG>",
		Short: "Report every branch of every repo in a GitHub organization",
		Long: `Report every branch of every repo in a GitHub organization, read-only, from
the GitHub API: no clones, no output directory. Each branch but the default one
is merged (its PR merged and it was never deleted), stale (no commit for
--stale-days and no open PR), open-pr, or active.

Settings resolve flag, then environment (GITHUB_TOKEN), then ~/.ghx.toml.`,
		Example: `  # Every branch of every active repo in acme
  ghb acme

  # Stale after 30 days, and repos should default to trunk
  ghb --stale-days 30 --expected-default trunk acme

  # The report as JSON, for jq
  ghb --format json acme | jq '.totals'`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBranches(cmd, args[0], f)
		},
	}
	setVersion(cmd)

	fl := cmd.Flags()
	fl.StringVarP(&f.token, "token", "t", "", "GitHub Personal Access Token.")
	fl.StringVar(&f.repoType, "type", "", "Repo type: all|public|private|forks|sources.")
	fl.StringVar(&f.include, "include", "", "Regex to include only matching repo names.")
	fl.StringVar(&f.exclude, "exclude", "", "Regex to exclude matching repo names.")
	fl.IntVarP(&f.concurrency, "concurrency", "c", 0, "Repos read in parallel.")
	fl.BoolVar(&f.includeArchived, "include-archived", false, "Also report archived repos.")
	fl.IntVar(&f.staleDays, "stale-days", 0, "Days without a commit before a branch is stale (default 90).")
	fl.StringVar(&f.expectedDefault, "expected-default", "", "Default branch name repos should use (default main).")
	fl.StringVar(&f.format, "format", formatText, "Output format: text|json.")
	return cmd
}

// branchesSettings are ghb's resolved settings.
type branchesSettings struct {
	org             string
	token           string
	repoType        string
	include         string
	exclude         string
	concurrency     int
	includeArchived bool
	staleDays       int
	expectedDefault string
	rows            []ui.SettingRow
}

func runBranches(cmd *cobra.Command, org string, f *branchesFlags) error {
	ctx := cmd.Context()
	if f.format != formatText && f.format != formatJSON {
		return fmt.Errorf("unknown format %q: use %s or %s", f.format, formatText, formatJSON)
	}
	isJSON := f.format == formatJSON
	// JSON goes to stdout alone, so anything said on the way goes to stderr.
	say := ui.Out
	if isJSON {
		say = cmd.ErrOrStderr()
	}

	s, err := resolveBranches(cmd, org, f, say)
	if err != nil {
		return err
	}
	if !isJSON {
		ui.PrintSettings(s.rows)
		ui.Println(text.Faint.Sprint("Authenticating..."))
	}
	username, err := ghapi.ValidateToken(ctx, s.token)
	if err != nil {
		return err
	}
	if !isJSON {
		ui.Printf("Authenticated as %s\n", text.Bold.Sprint(username))
		ui.Printf("Fetching repos for %s...\n", text.Bold.Sprint(org))
	}
	repos, err := branchRepos(ctx, s)
	if err != nil {
		return err
	}

	opts := branches.Options{
		Now:             now(),
		StaleAfter:      time.Duration(s.staleDays) * 24 * time.Hour,
		ExpectedDefault: s.expectedDefault,
	}
	if isJSON {
		report, err := scan(ctx, s, repos, opts, ui.NoBranchesProgress{}, say)
		if err != nil {
			return err
		}
		return writeJSON(ui.Out, s, opts, report)
	}

	ui.Printf("Found %s repos\n", text.Bold.Sprint(len(repos)))
	progress, live := ui.NewBranchesProgress(s.expectedDefault)
	if !live {
		ui.Printf("Reading the branches of %s repos...\n", text.Bold.Sprint(len(repos)))
	}
	var note io.Writer
	if !live {
		note = say
	}
	report, err := scan(ctx, s, repos, opts, progress, note)
	if err != nil {
		return err
	}
	if !live {
		ui.PrintBranchesRepoTable(repos, report, s.expectedDefault)
	}
	ui.PrintBranchTable(report, opts.Now)
	ui.PrintBranchTotals(branches.Total(report), s.expectedDefault)
	return nil
}

// resolveBranches resolves ghb's settings: flag, then environment (the token
// only), then ~/.ghx.toml. There is no local config: ghb has no directory.
func resolveBranches(cmd *cobra.Command, org string, f *branchesFlags, say io.Writer) (branchesSettings, error) {
	homeCfg, _, notices, err := config.Load("")
	if err != nil {
		return branchesSettings{}, err
	}
	for _, n := range notices {
		style := text.Faint
		if n.IsWarning {
			style = text.FgYellow
		}
		_, _ = fmt.Fprintln(say, style.Sprint(n.Text))
	}
	// ghb has no local config file; ResolveString and friends take one.
	noLocal := config.Config{}

	token, tokenSrc, err := config.ResolveToken(f.token, homeCfg, noLocal)
	if err != nil {
		if errors.Is(err, config.ErrNoToken) {
			return branchesSettings{}, errors.New(config.NoTokenMessage) //nolint:staticcheck // ST1005: multi-line, user-facing sentence, not a wrapped error
		}
		return branchesSettings{}, err
	}
	s := branchesSettings{org: org, token: token}
	var typeSrc, concurrencySrc, archivedSrc, staleSrc, expectedSrc, includeSrc, excludeSrc config.Source
	var includeSet, excludeSet bool
	s.repoType, typeSrc = config.ResolveString(flagPtr(cmd, "type", f.repoType), homeCfg, noLocal, "type", "all")
	s.include, includeSrc, includeSet = config.ResolveOptionalString(flagPtr(cmd, "include", f.include), homeCfg, noLocal, "include")
	s.exclude, excludeSrc, excludeSet = config.ResolveOptionalString(flagPtr(cmd, "exclude", f.exclude), homeCfg, noLocal, "exclude")
	s.concurrency, concurrencySrc = config.ResolveInt(flagIntPtr(cmd, "concurrency", f.concurrency), homeCfg, noLocal, "concurrency", 4)
	s.includeArchived, archivedSrc = config.ResolveBool(flagBoolPtr(cmd, "include-archived", f.includeArchived), homeCfg, noLocal, "include-archived", false)
	s.staleDays, staleSrc = config.ResolveInt(flagIntPtr(cmd, "stale-days", f.staleDays), homeCfg, noLocal, "stale-days", 90)
	s.expectedDefault, expectedSrc = config.ResolveString(flagPtr(cmd, "expected-default", f.expectedDefault), homeCfg, noLocal, "expected-default", "main")
	if s.staleDays < 1 {
		return branchesSettings{}, fmt.Errorf("stale days must be at least 1, got %d", s.staleDays)
	}
	s.concurrency = max(s.concurrency, 1)

	s.rows = []ui.SettingRow{
		{Label: "Organization:", Value: org, Source: string(config.SourceArgument)},
		{Label: "Token:", Value: ui.ObfuscateToken(token), Source: string(tokenSrc)},
		{Label: "Type:", Value: s.repoType, Source: string(typeSrc)},
	}
	if includeSet && s.include != "" {
		s.rows = append(s.rows, ui.SettingRow{Label: "Include:", Value: s.include, Source: string(includeSrc)})
	}
	if excludeSet && s.exclude != "" {
		s.rows = append(s.rows, ui.SettingRow{Label: "Exclude:", Value: s.exclude, Source: string(excludeSrc)})
	}
	s.rows = append(s.rows,
		ui.SettingRow{Label: "Archived:", Value: map[bool]string{true: "included", false: "skipped"}[s.includeArchived], Source: string(archivedSrc)},
		ui.SettingRow{Label: "Stale after:", Value: fmt.Sprintf("%d days", s.staleDays), Source: string(staleSrc)},
		ui.SettingRow{Label: "Default name:", Value: s.expectedDefault, Source: string(expectedSrc)},
		ui.SettingRow{Label: "Concurrency:", Value: fmt.Sprint(s.concurrency), Source: string(concurrencySrc)},
	)
	return s, nil
}

// branchRepos lists the org's repos the report covers: filtered, and without
// the archived ones unless they are asked for.
func branchRepos(ctx context.Context, s branchesSettings) ([]ghapi.RepoInfo, error) {
	all, err := ghapi.ListOrgRepos(ctx, s.token, s.org)
	if err != nil {
		return nil, err
	}
	repos, err := ghapi.FilterRepos(all, s.repoType, s.include, s.exclude)
	if err != nil {
		return nil, err
	}
	if s.includeArchived {
		return repos, nil
	}
	kept := repos[:0]
	for _, r := range repos {
		if !r.Archived {
			kept = append(kept, r)
		}
	}
	return kept, nil
}

// scan reads and classifies the branches of every repo, s.concurrency at a
// time, and returns them in the order of repos. A rate limit pauses every
// worker until it lifts, shown on progress or, when note is set, as a line
// there; the scan then goes on. Any other failure cancels the rest and is the
// error: a report is whole or not at all.
func scan(ctx context.Context, s branchesSettings, repos []ghapi.RepoInfo, opts branches.Options, progress ui.BranchesProgress, note io.Writer) ([]branches.Repo, error) {
	progress.SetRepos(repos)
	progress.Start()
	// The table is closed after Stop, here also when an error ends the scan.
	defer progress.FinishTable()
	defer progress.Stop()

	pacer := &ghapi.Pacer{
		Sleep: pacerSleep,
		OnPause: func(until time.Time, limit *ghapi.RateLimitError) {
			progress.Paused(until, limit.Limit())
			if note != nil {
				_, _ = fmt.Fprintf(note, "%s hit; waiting %s, until %s. Ctrl-C to stop.\n",
					limit.Limit(), time.Until(until).Round(time.Second), until.UTC().Format("15:04:05 UTC"))
			}
		},
	}

	out := make([]branches.Repo, len(repos))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(s.concurrency)
	for i, r := range repos {
		g.Go(func() error {
			if gctx.Err() != nil {
				return gctx.Err()
			}
			progress.RepoStarted(r.Name)
			rb, err := ghapi.ListBranches(gctx, s.token, s.org, r.Name, ghapi.BranchOptions{
				OnPage: func(page int) { progress.RepoPage(r.Name, page) },
				Pacer:  pacer,
			})
			if err != nil {
				// A repo stopped because another failed stays in flight, and
				// Stop shows it as interrupted.
				if gctx.Err() == nil || !errors.Is(err, context.Canceled) {
					progress.RepoFailed(r.Name)
				}
				return err
			}
			out[i] = branches.ClassifyRepo(r.Name, rb, opts)
			progress.RepoDone(out[i])
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return out, nil
}
