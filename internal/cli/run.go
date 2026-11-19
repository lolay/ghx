package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"

	"github.com/lolay/ghx/internal/cloner"
	"github.com/lolay/ghx/internal/config"
	"github.com/lolay/ghx/internal/ghapi"
	"github.com/lolay/ghx/internal/manifest"
	"github.com/lolay/ghx/internal/ui"
)

func run(cmd *cobra.Command, args []string, f *flags) error {
	ctx := cmd.Context()
	org := args[0]
	outputDir := args[1]

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	homeCfg, localCfg, notices, err := config.Load(outputDir)
	if err != nil {
		return err
	}
	for _, n := range notices {
		if n.IsWarning {
			ui.Println(text.FgYellow.Sprint(n.Text))
		} else {
			ui.Println(text.Faint.Sprint(n.Text))
		}
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
			return errors.New(config.NoTokenMessage) //nolint:staticcheck // ST1005: multi-line, user-facing sentence, not a wrapped error
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

	if err := checkSubdir("deleted-dir", deletedDir); err != nil {
		return err
	}
	if err := checkSubdir("archived-dir", archivedDir); err != nil {
		return err
	}
	if err := checkApart(archivedDir, deletedDir); err != nil {
		return err
	}

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

	// A dry run never runs git, so only a real sync needs it; looking it up
	// here stops a run without git before it asks GitHub for anything.
	var gitPath string
	if !dryRun {
		if gitPath, err = cloner.FindGit(ctx); err != nil {
			return err
		}
	}

	ui.Println(text.Faint.Sprint("Authenticating..."))
	username, err := ghapi.ValidateToken(ctx, token)
	if err != nil {
		return err
	}
	ui.Printf("Authenticated as %s\n", text.Bold.Sprint(username))

	ui.Printf("Fetching repos for %s...\n", text.Bold.Sprint(org))
	allRepos, err := ghapi.ListOrgRepos(ctx, token, org, nil)
	if err != nil {
		return err
	}
	repos, err := ghapi.FilterRepos(allRepos, repoType, include, exclude)
	if err != nil {
		return err
	}
	ui.Printf("Found %s repos\n", text.Bold.Sprint(len(repos)))

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
			ui.Printf(
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
		ui.Println(text.Faint.Sprint(msg))
	}
	var previousRepos []manifest.Repo
	if m != nil {
		previousRepos = m.Repos
	}

	var activeRepos, archivedRepos []ghapi.RepoInfo
	for _, r := range repos {
		if r.Archived {
			archivedRepos = append(archivedRepos, r)
		} else {
			activeRepos = append(activeRepos, r)
		}
	}
	// The table lists the repos in the order the passes sync them. The live
	// table prints a row only once every row above it has finished, so an
	// archived repo listed among the active ones would hold the first pass's
	// rows back until the second pass reached it.
	tableRepos := slices.Concat(activeRepos, archivedRepos)

	// On a terminal the progress renderer draws the repo table itself, with a
	// Status column that follows each repo, so it gets the repos in place of the
	// static table. A dry run, a pipe and a file print the static table: there
	// is no sync to follow, or no terminal to follow it on.
	progress := ui.NewProgress()
	lister, drawsTable := progress.(ui.RepoLister)
	if drawsTable && !dryRun {
		lister.SetRepos(tableRepos)
		// The table is closed once the passes are over, here also when a
		// cancel or an error ends the run early: FinishTable does nothing the
		// second time.
		defer lister.FinishTable()
	} else {
		ui.PrintRepoTable(tableRepos)
	}

	// Before the dry-run return, so a dry run reports what would move. The
	// archived check runs on every sync, manifest or not: it looks at the
	// disk, not at what the last run recorded.
	check := &originCheck{ctx: ctx, gitPath: gitPath}
	p := newPlan()
	var removalSummary cloner.SyncSummary
	var pendingRemovals []manifest.Repo
	// Any manifest, even one with no repos, says what was archived, which is
	// what tells an unstamped backup from an archived clone.
	if m != nil && sameDir(archivedDir, deletedDir) {
		sharedRoot := filepath.Join(outputDir, deletedDir)
		legacy, ok := stampLegacy(check, sharedRoot, previousRepos, listedNames(allRepos), p, dryRun)
		removalSummary.Extend(legacy)
		if !ok && !dryRun {
			printReconcileChanges(removalSummary)
			return fmt.Errorf("couldn't rename every unstamped clone in %s to a stamped name, so nothing was synced; fix the failure above and run again", sharedRoot)
		}
	}
	if len(previousRepos) > 0 {
		removed, pending := handleRemoved(check, outputDir, archivedDir, deletedDir, allRepos, previousRepos, p, shouldDelete, dryRun)
		removalSummary.Extend(removed)
		pendingRemovals = pending
	}
	removalSummary.Extend(reconcileArchived(check, outputDir, archivedDir, repos, listedNames(allRepos), p, shouldDelete, dryRun))
	printReconcileChanges(removalSummary)

	if len(skippedLargeResults) > 0 {
		ui.Println()
		ui.Println(text.Bold.Sprint("Repos skipped (too large):"))
		for _, r := range skippedLargeResults {
			ui.Printf("  %s: %s\n", r.Name, r.Action)
		}
	}

	if dryRun {
		ui.Println()
		ui.Println(text.Faint.Sprint("Dry run complete. No changes made."))
		return nil
	}

	baseOpts := cloner.Options{
		Token:       token,
		Concurrency: concurrency,
		CloneWiki:   cloneWiki,
		UseSSH:      protocol == "ssh",
		GitAuthor:   gitAuthor,
		GitEmail:    gitEmail,
		GitPath:     gitPath,
		Progress:    progress,
	}

	// Clones made by ghx before the token moved to git's environment hold it
	// in their origin URL. CloneRepos cleans each repo it pulls; this pass
	// reaches the ones no sync pulls any more.
	cleanFailures, err := cloner.CleanOrigins(ctx, baseOpts,
		outputDir, filepath.Join(outputDir, deletedDir), filepath.Join(outputDir, archivedDir))
	if err != nil {
		return err
	}

	activeOpts := baseOpts
	activeOpts.OutputDir = outputDir
	summary, err := cloner.CloneRepos(ctx, activeRepos, activeOpts)
	if err != nil {
		return err
	}
	summary.Results = append(summary.Results, cleanFailures...)

	// The live table on a terminal is one piece from its top to its close, so
	// nothing is printed between the passes: the archived rows already read
	// "archived" under Visibility, and the note on skipping them waits for
	// the close.
	// Without a terminal the notes print where they happen.
	var afterTable string
	if len(archivedRepos) > 0 {
		if shouldDelete {
			note := fmt.Sprintf("Skipping %d archived repo(s) (--delete)", len(archivedRepos))
			if drawsTable {
				afterTable = note
			} else {
				ui.Println(note)
			}
		} else {
			if !drawsTable {
				ui.Printf("Cloning %d archived repo(s) into %s/...\n",
					len(archivedRepos), archivedDir)
			}
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

	// Before the summary, and before the manifest is written: the table shows
	// the sync, and the summary that follows counts it.
	if drawsTable && !dryRun {
		lister.FinishTable()
		if afterTable != "" {
			ui.Println()
			ui.Println(afterTable)
		}
	}

	if err := manifest.Write(outputDir, org, allRepos, pendingRemovals); err != nil {
		return fmt.Errorf("writing manifest: %w", err)
	}
	// The table's closing footer already holds the counts, so on a terminal
	// only the lists that follow them are left to print.
	if drawsTable && !dryRun {
		ui.PrintSummaryLists(summary)
	} else {
		ui.PrintSummary(summary)
	}
	return nil
}

// handleRemoved moves each repo the previous manifest lists and the org no
// longer has into deletedDir, or deletes it under shouldDelete, once its
// origin shows it is a clone of that repo; its wiki goes with it. A removed
// repo is absent from the listing, so the manifest is the only record of it.
// Its clone may sit in the output directory or, if it was archived, in
// archivedDir, and both places are retired: the disk decides, as in
// reconcileArchived. What is already in deletedDir is never touched, so a
// later --delete leaves earlier removals alone.
//
// It also returns the previous manifest's entries for the removed repos whose
// clone or wiki failed to move or delete, for the next manifest to keep: the
// manifest is the only record of a removal, so without them the next run
// would never retry. An origin mismatch isn't kept: that directory isn't a
// clone of the repo, so it isn't ghx's to retry.
func handleRemoved(
	check *originCheck,
	outputDir, archivedDir, deletedDir string,
	currentRepos []ghapi.RepoInfo,
	previousRepos []manifest.Repo,
	p *plan,
	shouldDelete, dryRun bool,
) (cloner.SyncSummary, []manifest.Repo) {
	r := reconciler{
		check:  check,
		dryRun: dryRun,
		stamp:  time.Now().UTC().Format(deletedStampLayout),
		shared: sameDir(archivedDir, deletedDir),
		plan:   p,
	}
	deletedRoot := filepath.Join(outputDir, deletedDir)
	parents := []string{outputDir, filepath.Join(outputDir, archivedDir)}
	listed := listedNames(currentRepos)

	var removed []string
	entries := map[string]manifest.Repo{}
	for _, repo := range previousRepos {
		// Without case: a repo renamed only in case is still the org's, and on
		// a case-insensitive file system its old directory is its live clone.
		if !listed[strings.ToLower(repo.Name)] {
			removed = append(removed, repo.Name)
			if _, seen := entries[repo.Name]; !seen {
				entries[repo.Name] = repo
			}
		}
	}
	slices.Sort(removed)
	removed = slices.Compact(removed)
	var pending []manifest.Repo

	for _, name := range removed {
		// The names come from the previous manifest, which anyone who can
		// write the output directory can edit: "../x" must not be moved or
		// deleted.
		if err := cloner.CheckRepoName(name); err != nil {
			r.add(name, cloner.ActionSkippedName, err.Error())
			continue
		}
		wiki := name + ".wiki"
		// A listed repo named like the wiki has an origin the check can't
		// tell from the wiki's, and it isn't removed.
		wikiIsListed := listed[strings.ToLower(wiki)]
		// A wiki retired on its own keeps the shared directory's rule: only a
		// stamped name there is a backup.
		wikiBase := wiki
		if r.shared {
			wikiBase = name + "." + r.stamp + ".wiki"
		}
		for _, parent := range parents {
			repoDir := filepath.Join(parent, name)
			dest, ok := r.retire(name, repoDir, deletedRoot, name, r.shared, shouldDelete)
			switch {
			case wikiIsListed:
			case ok:
				// The wiki goes where its repo went, under the same name, so the
				// pair stays together even when the repo's name was taken.
				r.retire(wiki, filepath.Join(parent, wiki), deletedRoot, dest+".wiki", false, shouldDelete)
			case !r.present(repoDir):
				// The repo went on an earlier run and its wiki failed to follow,
				// or never had a clone here: the wiki is retired on its own.
				r.retire(wiki, filepath.Join(parent, wiki), deletedRoot, wikiBase, false, shouldDelete)
			}
		}
		if r.failed[name] || r.failed[wiki] {
			pending = append(pending, entries[name])
		}
	}
	return r.summary, pending
}

// printReconcileChanges lists what the moves and deletes before the sync did,
// or in a dry run would do.
func printReconcileChanges(summary cloner.SyncSummary) {
	if len(summary.Results) == 0 {
		return
	}
	ui.Println()
	ui.Println(text.Bold.Sprint("Changes for removed, archived and unarchived repos:"))
	for _, r := range summary.Results {
		if r.Detail != "" {
			ui.Printf("  %s: %s (%s)\n", r.Name, r.Action, r.Detail)
		} else {
			ui.Printf("  %s: %s\n", r.Name, r.Action)
		}
	}
}

// listedNames is the set of repo names in the listing, lowercased: GitHub
// names are case-insensitive, and so are the default file systems on macOS
// and Windows.
func listedNames(repos []ghapi.RepoInfo) map[string]bool {
	names := make(map[string]bool, len(repos))
	for _, r := range repos {
		names[strings.ToLower(r.Name)] = true
	}
	return names
}

// checkApart rejects an --archived-dir and a --deleted-dir where one is inside
// the other: a repo named like the inner directory would collide with it.
// The same directory for both is allowed, and handleRemoved then stamps every
// removal so a backup can't be taken for an archived clone. Compared without
// case, so a case-insensitive file system's aliases count; on a case-sensitive
// one that only treats two names that differ by case as one directory, which
// at worst stamps removals that didn't need it.
func checkApart(archivedDir, deletedDir string) error {
	if sameDir(archivedDir, deletedDir) {
		return nil
	}
	if within(archivedDir, deletedDir) || within(deletedDir, archivedDir) {
		return fmt.Errorf("archived-dir %q and deleted-dir %q must be the same directory or separate ones, neither inside the other", archivedDir, deletedDir)
	}
	return nil
}

// sameDir reports whether a and b name the same directory, ignoring case.
func sameDir(a, b string) bool {
	return within(a, b) && within(b, a)
}

// within reports whether path is root or inside it, ignoring case.
func within(root, path string) bool {
	rel, err := filepath.Rel(strings.ToLower(filepath.Clean(root)), strings.ToLower(filepath.Clean(path)))
	return err == nil && (rel == "." || filepath.IsLocal(rel))
}

// checkSubdir rejects a --deleted-dir or --archived-dir that isn't a
// directory inside the output directory: ".." or an absolute path would move
// repos elsewhere, and "." would move each repo onto itself, which MoveRepo
// does by deleting the destination first.
func checkSubdir(key, dir string) error {
	if !filepath.IsLocal(dir) || filepath.Clean(dir) == "." {
		return fmt.Errorf("%s %q must be a directory inside the output directory, such as %q", key, dir, "DELETED")
	}
	return nil
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
