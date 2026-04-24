package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/garyrudolph/ghx/internal/cloner"
	"github.com/garyrudolph/ghx/internal/config"
	"github.com/garyrudolph/ghx/internal/ghapi"
	"github.com/garyrudolph/ghx/internal/manifest"
	"github.com/garyrudolph/ghx/internal/ui"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"
)

func run(cmd *cobra.Command, args []string, f *flags) error {
	ctx := cmd.Context()
	org := args[0]
	outputDir := args[1]

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	homeCfg, localCfg, msgs, err := config.Load(outputDir)
	if err != nil {
		return err
	}
	for _, m := range msgs {
		fmt.Fprintln(ui.Out, text.Faint.Sprint(m))
	}

	// --ssh is a shortcut that forces protocol=ssh even if --protocol
	// was not given. Applied before option resolution so the flag
	// source is reported as "option".
	var protocolFlag *string
	if cmd.Flags().Changed("protocol") {
		v := f.protocol
		protocolFlag = &v
	} else if f.ssh {
		v := "ssh"
		protocolFlag = &v
	}

	token, tokenSrc, err := config.ResolveToken(f.token, homeCfg, localCfg)
	if err != nil {
		if errors.Is(err, config.ErrNoToken) {
			return errors.New(config.NoTokenMessage)
		}
		return err
	}

	protocol, protocolSrc := config.ResolveString(protocolFlag, homeCfg, localCfg, "protocol", "https")
	repoType, typeSrc := config.ResolveString(flagPtr(cmd, "type", f.repoType), homeCfg, localCfg, "type", "all")
	concurrency, _ := config.ResolveInt(flagIntPtr(cmd, "concurrency", f.concurrency), homeCfg, localCfg, "concurrency", 4)
	dryRun, _ := config.ResolveBool(flagBoolPtr(cmd, "dry-run", f.dryRun), homeCfg, localCfg, "dry-run", false)
	cloneWiki, wikiSrc := config.ResolveBool(flagBoolPtr(cmd, "clone-wiki", f.cloneWiki), homeCfg, localCfg, "clone-wiki", false)
	include, includeSrc, includeSet := config.ResolveOptionalString(flagPtr(cmd, "include", f.include), homeCfg, localCfg, "include")
	exclude, excludeSrc, excludeSet := config.ResolveOptionalString(flagPtr(cmd, "exclude", f.exclude), homeCfg, localCfg, "exclude")
	gitAuthor, authorSrc, _ := config.ResolveOptionalString(flagPtr(cmd, "git-author", f.gitAuthor), homeCfg, localCfg, "git-author")
	gitEmail, emailSrc, _ := config.ResolveOptionalString(flagPtr(cmd, "git-email", f.gitEmail), homeCfg, localCfg, "git-email")
	shouldDelete, _ := config.ResolveBool(flagBoolPtr(cmd, "delete", f.shouldDelete), homeCfg, localCfg, "delete", false)
	deletedDir, _ := config.ResolveString(flagPtr(cmd, "deleted-dir", f.deletedDir), homeCfg, localCfg, "deleted-dir", "DELETED")
	archivedDir, _ := config.ResolveString(flagPtr(cmd, "archived-dir", f.archivedDir), homeCfg, localCfg, "archived-dir", "ARCHIVED")
	maxSize, maxSizeSrc, maxSizeSet := config.ResolveOptionalInt(flagIntPtr(cmd, "max-size", f.maxSize), homeCfg, localCfg, "max-size")

	rows := []ui.SettingRow{
		{Label: "Organization:", Value: org, Source: string(config.SourceArgument)},
		{Label: "Output:", Value: outputDir, Source: string(config.SourceArgument)},
		{Label: "Protocol:", Value: protocol, Source: string(protocolSrc)},
		{Label: "Token:", Value: ui.ObfuscateToken(token), Source: string(tokenSrc)},
		{Label: "Type:", Value: repoType, Source: string(typeSrc)},
		{Label: "Clone wiki:", Value: boolYesNo(cloneWiki), Source: string(wikiSrc)},
	}
	if includeSet && include != "" {
		rows = append(rows, ui.SettingRow{Label: "Include:", Value: include, Source: string(includeSrc)})
	}
	if excludeSet && exclude != "" {
		rows = append(rows, ui.SettingRow{Label: "Exclude:", Value: exclude, Source: string(excludeSrc)})
	}
	if maxSizeSet {
		rows = append(rows, ui.SettingRow{
			Label: "Max size:", Value: fmt.Sprintf("%d MB", maxSize), Source: string(maxSizeSrc),
		})
	}
	rows = append(rows,
		ui.SettingRow{Label: "Git author:", Value: orDash(gitAuthor), Source: string(authorSrc)},
		ui.SettingRow{Label: "Git email:", Value: orDash(gitEmail), Source: string(emailSrc)},
	)
	ui.PrintSettings(rows)

	fmt.Fprintln(ui.Out, text.Faint.Sprint("Authenticating..."))
	username, err := ghapi.ValidateToken(ctx, token)
	if err != nil {
		return err
	}
	fmt.Fprintf(ui.Out, "Authenticated as %s\n", text.Bold.Sprint(username))

	fmt.Fprintf(ui.Out, "Fetching repos for %s...\n", text.Bold.Sprint(org))
	allRepos, err := ghapi.ListOrgRepos(ctx, token, org)
	if err != nil {
		return err
	}
	repos, err := ghapi.FilterRepos(allRepos, repoType, include, exclude)
	if err != nil {
		return err
	}
	fmt.Fprintf(ui.Out, "Found %s repos\n", text.Bold.Sprint(len(repos)))

	var skippedLargeResults []cloner.RepoResult
	if maxSizeSet {
		maxSizeKB := maxSize * 1024
		var tooLarge, kept []ghapi.RepoInfo
		for _, r := range repos {
			if r.SizeKB > maxSizeKB {
				tooLarge = append(tooLarge, r)
			} else {
				kept = append(kept, r)
			}
		}
		repos = kept
		if len(tooLarge) > 0 {
			fmt.Fprintf(ui.Out,
				"Skipping %s repo(s) exceeding %d MB\n",
				text.Bold.Sprint(len(tooLarge)), maxSize,
			)
			for _, r := range tooLarge {
				skippedLargeResults = append(skippedLargeResults, cloner.RepoResult{
					Name: r.Name, Action: cloner.ActionSkippedTooLarge,
				})
			}
		}
	}

	m, msg, err := manifest.Read(outputDir)
	if err != nil {
		return err
	}
	if msg != "" {
		fmt.Fprintln(ui.Out, text.Faint.Sprint(msg))
	}
	var previousRepos []manifest.Repo
	if m != nil {
		previousRepos = m.Repos
	}

	ui.PrintRepoTable(repos)

	var removalSummary cloner.SyncSummary
	if len(previousRepos) > 0 {
		removalSummary = handleRemovedAndArchived(
			outputDir, allRepos, previousRepos,
			shouldDelete, deletedDir, archivedDir, dryRun,
		)
		if len(removalSummary.Results) > 0 {
			fmt.Fprintln(ui.Out)
			fmt.Fprintln(ui.Out, text.Bold.Sprint("Changes for removed/archived repos:"))
			for _, r := range removalSummary.Results {
				fmt.Fprintf(ui.Out, "  %s: %s\n", r.Name, r.Action)
			}
		}
	}

	if len(skippedLargeResults) > 0 {
		fmt.Fprintln(ui.Out)
		fmt.Fprintln(ui.Out, text.Bold.Sprint("Repos skipped (too large):"))
		for _, r := range skippedLargeResults {
			fmt.Fprintf(ui.Out, "  %s: %s\n", r.Name, r.Action)
		}
	}

	if dryRun {
		fmt.Fprintln(ui.Out)
		fmt.Fprintln(ui.Out, text.Faint.Sprint("Dry run complete. No changes made."))
		return nil
	}

	var activeRepos, archivedRepos []ghapi.RepoInfo
	for _, r := range repos {
		if r.Archived {
			archivedRepos = append(archivedRepos, r)
		} else {
			activeRepos = append(activeRepos, r)
		}
	}

	baseOpts := cloner.Options{
		Token:       token,
		Concurrency: concurrency,
		CloneWiki:   cloneWiki,
		UseSSH:      protocol == "ssh",
		GitAuthor:   gitAuthor,
		GitEmail:    gitEmail,
		ProgressOut: ui.Out,
	}

	activeOpts := baseOpts
	activeOpts.OutputDir = outputDir
	summary, err := cloner.CloneRepos(ctx, activeRepos, activeOpts)
	if err != nil {
		return err
	}

	if len(archivedRepos) > 0 {
		if shouldDelete {
			fmt.Fprintf(ui.Out, "Skipping %d archived repo(s) (--delete)\n", len(archivedRepos))
		} else {
			fmt.Fprintf(ui.Out, "Cloning %d archived repo(s) into %s/...\n",
				len(archivedRepos), archivedDir)
			archivedOpts := baseOpts
			archivedOpts.OutputDir = filepath.Join(outputDir, archivedDir)
			archivedSummary, err := cloner.CloneRepos(ctx, archivedRepos, archivedOpts)
			if err != nil {
				return err
			}
			summary.Extend(archivedSummary)
		}
	}

	summary.Extend(removalSummary)
	summary.Results = append(summary.Results, skippedLargeResults...)

	if err := manifest.Write(outputDir, org, allRepos); err != nil {
		return fmt.Errorf("writing manifest: %w", err)
	}
	ui.PrintSummary(summary)
	return nil
}

// handleRemovedAndArchived compares the previous manifest against the
// current repo list and either moves or deletes repos that have been
// removed from the org or newly archived.
func handleRemovedAndArchived(
	outputDir string,
	currentRepos []ghapi.RepoInfo,
	previousRepos []manifest.Repo,
	shouldDelete bool,
	deletedDir, archivedDir string,
	dryRun bool,
) cloner.SyncSummary {
	summary := cloner.SyncSummary{}

	currentNames := map[string]struct{}{}
	currentArchived := map[string]struct{}{}
	for _, r := range currentRepos {
		currentNames[r.Name] = struct{}{}
		if r.Archived {
			currentArchived[r.Name] = struct{}{}
		}
	}

	previousNames := map[string]struct{}{}
	previouslyArchived := map[string]struct{}{}
	for _, r := range previousRepos {
		previousNames[r.Name] = struct{}{}
		if r.Archived {
			previouslyArchived[r.Name] = struct{}{}
		}
	}

	var removed, newlyArchived []string
	for name := range previousNames {
		if _, ok := currentNames[name]; !ok {
			removed = append(removed, name)
		}
	}
	for name := range currentArchived {
		if _, ok := previouslyArchived[name]; !ok {
			newlyArchived = append(newlyArchived, name)
		}
	}
	sort.Strings(removed)
	sort.Strings(newlyArchived)

	process := func(names []string, moveTarget string, moveAction cloner.Action) {
		for _, name := range names {
			repoDir := filepath.Join(outputDir, name)
			if _, err := os.Stat(repoDir); err != nil {
				continue
			}
			if dryRun {
				action := moveAction
				if shouldDelete {
					action = cloner.ActionDeleted
				}
				summary.Append(cloner.RepoResult{Name: name, Action: action, Detail: "dry-run"})
				continue
			}
			if shouldDelete {
				if err := cloner.DeleteRepo(repoDir); err != nil {
					summary.Append(cloner.RepoResult{Name: name, Action: cloner.ActionFailed, Detail: err.Error()})
					continue
				}
				summary.Append(cloner.RepoResult{Name: name, Action: cloner.ActionDeleted})
			} else {
				if err := cloner.MoveRepo(repoDir, filepath.Join(outputDir, moveTarget)); err != nil {
					summary.Append(cloner.RepoResult{Name: name, Action: cloner.ActionFailed, Detail: err.Error()})
					continue
				}
				summary.Append(cloner.RepoResult{Name: name, Action: moveAction})
			}
		}
	}

	process(removed, deletedDir, cloner.ActionMovedDeleted)
	process(newlyArchived, archivedDir, cloner.ActionMovedArchived)
	return summary
}

// flagPtr returns a pointer to s when the named flag was set on the
// command line, or nil otherwise. Used so ResolveString can treat
// "unset" and "set to empty" differently.
func flagPtr(cmd *cobra.Command, name, s string) *string {
	if cmd.Flags().Changed(name) {
		v := s
		return &v
	}
	return nil
}

func flagBoolPtr(cmd *cobra.Command, name string, b bool) *bool {
	if cmd.Flags().Changed(name) {
		v := b
		return &v
	}
	return nil
}

func flagIntPtr(cmd *cobra.Command, name string, n int) *int {
	if cmd.Flags().Changed(name) {
		v := n
		return &v
	}
	return nil
}

func boolYesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
