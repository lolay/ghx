package cloner

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/lolay/ghx/internal/ghapi"
)

// Options controls a single CloneRepos invocation.
type Options struct {
	// Token authenticates HTTPS clones and pulls. git gets it through its
	// environment for each command (see credential), never in a URL.
	Token       string
	OutputDir   string
	Concurrency int
	CloneWiki   bool
	UseSSH      bool
	GitAuthor   string
	GitEmail    string
	// GitPath is the git executable, normally from FindGit. When empty,
	// CloneRepos calls FindGit itself.
	GitPath string
	// Progress receives sync events for display. Nil means NoProgress.
	Progress Progress
}

// CloneRepos clones or pulls every repo in repos concurrently, bounded
// by opts.Concurrency, and returns the aggregate SyncSummary. opts.Progress
// hears about each repo as it starts and finishes.
func CloneRepos(ctx context.Context, repos []ghapi.RepoInfo, opts Options) (SyncSummary, error) {
	summary := SyncSummary{}
	if err := os.MkdirAll(opts.OutputDir, 0o755); err != nil {
		return summary, fmt.Errorf("creating output dir: %w", err)
	}

	if len(repos) == 0 {
		return summary, nil
	}

	git, err := newGitRunner(ctx, opts)
	if err != nil {
		return summary, err
	}

	concurrency := opts.Concurrency
	if concurrency < 1 {
		concurrency = 1
	}

	prog := opts.Progress
	if prog == nil {
		prog = NoProgress{}
	}
	prog.Start(len(repos))
	// Stop runs on every path out, including a cancelled context, so the
	// renderer always releases the terminal.
	defer prog.Stop()

	type job struct{ repo ghapi.RepoInfo }
	jobs := make(chan job)

	var mu sync.Mutex
	var wg sync.WaitGroup

	for range concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				start := time.Now()
				if CheckRepoName(j.repo.Name) == nil {
					prog.RepoStarted(j.repo.Name, plannedAction(opts, j.repo))
				}
				results := processRepo(ctx, git, j.repo, opts)
				mu.Lock()
				summary.Results = append(summary.Results, results...)
				mu.Unlock()
				prog.RepoDone(results[0], time.Since(start))
			}
		}()
	}

	for _, r := range repos {
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return summary, ctx.Err()
		case jobs <- job{repo: r}:
		}
	}
	close(jobs)
	wg.Wait()

	return summary, nil
}

// plannedAction is what processRepo will do to repo: pull when a clone is
// already there, clone otherwise.
func plannedAction(opts Options, repo ghapi.RepoInfo) string {
	if HasGitDir(filepath.Join(opts.OutputDir, repo.Name)) {
		return ProgressPull
	}
	return ProgressClone
}

func processRepo(ctx context.Context, git gitRunner, repo ghapi.RepoInfo, opts Options) []RepoResult {
	if err := CheckRepoName(repo.Name); err != nil {
		return []RepoResult{{Name: repo.Name, Action: ActionSkippedName, Detail: err.Error()}}
	}

	dest := filepath.Join(opts.OutputDir, repo.Name)
	var result RepoResult
	if HasGitDir(dest) {
		result = syncExisting(ctx, git, dest, repo, opts)
	} else {
		result = cloneNew(ctx, git, dest, repo, opts)
	}

	results := []RepoResult{result}
	if opts.CloneWiki && repo.HasWiki && result.Action != ActionFailed {
		results = append(results, processWiki(ctx, git, repo, opts)...)
	}
	return results
}

// cloneNew clones repo into dest, which has no .git directory. It clones
// without --branch, so git follows the remote's HEAD. When that HEAD names a
// branch that doesn't exist (GitHub reports "main" for an empty repo, or for
// one whose pushes never made main), git clones every branch and checks out
// nothing, and cloneNew picks a branch itself.
func cloneNew(ctx context.Context, git gitRunner, dest string, repo ghapi.RepoInfo, opts Options) RepoResult {
	result := resultFor(repo.Name)
	if _, err := os.Lstat(dest); err == nil {
		// Someone else's files: clone would refuse a non-empty directory
		// anyway, and nothing here is ghx's to move or delete.
		return result(ActionFailed, dest+" exists and isn't a git clone; move it aside")
	}

	url := resolveURL(repo, opts.UseSSH)
	args := slices.Concat([]string{"clone"}, platformCloneArgs, []string{url, dest})
	r := git.fetch(ctx, "", url, args...)
	if !r.Success() {
		if HasGitDir(dest) && strings.Contains(r.Stderr, "Clone succeeded, but checkout failed") {
			// Every object is down, which is the backup; only the working tree
			// is missing (typically Git LFS without git-lfs installed).
			git.setIdentity(ctx, dest, opts.GitAuthor, opts.GitEmail)
			return result(ActionFetchedOnly, r.Message())
		}
		return result(ActionFailed, r.Message())
	}

	if _, born := git.revParse(ctx, dest, "HEAD"); born {
		git.setIdentity(ctx, dest, opts.GitAuthor, opts.GitEmail)
		return result(ActionCloned, "")
	}
	branches := git.remoteBranches(ctx, dest)
	if len(branches) == 0 {
		if err := ctx.Err(); err != nil {
			// The probes failed because the run stopped, not because the repo
			// is empty: keep the clone.
			return result(ActionFailed, err.Error())
		}
		// An empty clone holds nothing to back up; removing it keeps the
		// output tree to real repos, and the next run clones it again.
		if err := DeleteRepo(dest); err != nil {
			return result(ActionFailed, fmt.Sprintf("removing the clone of an empty repository: %v", err))
		}
		return result(ActionSkippedEmpty, "empty repository")
	}
	git.setIdentity(ctx, dest, opts.GitAuthor, opts.GitEmail)
	b := pickBranch(repo.DefaultBranch, branches)
	if sw := git.run(ctx, dest, "switch", "--track", "-c", b, originRef(b)); !sw.Success() {
		return result(ActionFetchedOnly, sw.Message())
	}
	return result(ActionCloned, fmt.Sprintf("remote HEAD names %s, which doesn't exist; checked out %s", repo.DefaultBranch, b))
}

// syncExisting brings an existing clone up to date. It always fetches first:
// a fetch never touches the working tree, so every remote branch is backed up
// each run, whatever state the clone is in. The checkout then moves only when
// no local work can be lost; otherwise the result is ActionFetchedOnly with
// the reason.
func syncExisting(ctx context.Context, git gitRunner, dest string, repo ghapi.RepoInfo, opts Options) RepoResult {
	result := resultFor(repo.Name)
	git.setIdentity(ctx, dest, opts.GitAuthor, opts.GitEmail)
	if err := git.cleanOrigin(ctx, dest); err != nil {
		return result(ActionFailed, err.Error())
	}

	cur, detached, curErr := git.currentBranch(ctx, dest)
	before := tipsBeforeFetch(ctx, git, dest, repo.DefaultBranch, cur)
	// The header is scoped to the HTTPS clone URL's host even in SSH mode, so
	// a clone first made over HTTPS keeps fetching; an SSH origin never sends
	// it.
	if r := git.fetch(ctx, dest, repo.CloneURL, "fetch", "--prune", "--quiet", "origin"); !r.Success() {
		return result(ActionFailed, r.Message())
	}

	branches := git.remoteBranches(ctx, dest)
	if len(branches) == 0 {
		return result(ActionSkippedEmpty, "empty repository")
	}
	b := pickBranch(repo.DefaultBranch, branches)
	// Local only: keeps origin/HEAD naming the branch ghx follows.
	git.run(ctx, dest, "symbolic-ref", "refs/remotes/origin/HEAD", originRef(b))

	// In this order: a rebase stopped on a conflict also detaches HEAD, and
	// "rebase in progress" is the reason that helps.
	if op := git.operationInProgress(ctx, dest); op != "" {
		return result(ActionFetchedOnly, op+" in progress")
	}
	switch {
	case curErr != nil:
		return result(ActionFailed, curErr.Error())
	case detached:
		return result(ActionFetchedOnly, "detached HEAD")
	}
	dirty, status := git.status(ctx, dest)
	if !status.Success() {
		return result(ActionFailed, status.Message())
	}

	if cur == b {
		if dirty {
			return result(ActionFetchedOnly, describeChanges(status.Stdout))
		}
		return result(updateTarget(ctx, git, dest, b, before[originRef(b)]))
	}
	return result(updateOther(ctx, git, dest, cur, b, dirty))
}

// tipsBeforeFetch records, by full ref, where origin's copy of the default
// branch and of the current branch's upstream pointed before the fetch, so
// updateTarget can tell a force-push (the old tip is no longer in the new one)
// from local commits.
func tipsBeforeFetch(ctx context.Context, git gitRunner, dest, defaultBranch, cur string) map[string]string {
	var refs []string
	if defaultBranch != "" {
		refs = append(refs, originRef(defaultBranch))
	}
	if cur != "" {
		if upstream, _ := git.upstreamTrack(ctx, dest, cur); upstream != "" {
			refs = append(refs, upstream)
		}
	}
	tips := map[string]string{}
	for _, ref := range refs {
		if sha, ok := git.revParse(ctx, dest, ref); ok {
			tips[ref] = sha
		}
	}
	return tips
}

// updateTarget moves HEAD, which is on b with no tracked changes, to origin's
// b when nothing local can be lost. oldTip is origin/b before the fetch, "" when it wasn't there; the
// reflog of origin/b covers force-pushes an earlier fetch already saw.
func updateTarget(ctx context.Context, git gitRunner, dest, b, oldTip string) (Action, string) {
	target := originRef(b)
	head, born := git.revParse(ctx, dest, "HEAD")
	if !born {
		// An empty clone that has since gained commits. -c, not -C: it fails
		// if b exists, so a probe that misread HEAD can't move a branch.
		if r := git.run(ctx, dest, "switch", "--track", "-c", b, target); !r.Success() {
			return ActionFetchedOnly, r.Message()
		}
		return ActionPulled, ""
	}
	if git.isAncestor(ctx, dest, head, target) {
		if tip, _ := git.revParse(ctx, dest, target); tip == head {
			return ActionUpToDate, ""
		}
		// git refuses, and changes nothing, when the merge would overwrite an
		// untracked file or the index is locked.
		if r := git.run(ctx, dest, "merge", "--ff-only", target); !r.Success() {
			return ActionFetchedOnly, r.Message()
		}
		return ActionPulled, ""
	}
	// oldTip only sees a force-push this run's fetch brought in. The fork point
	// also sees one an earlier fetch brought in (a failed pull, a manual
	// fetch): HEAD being its own fork point means every commit on it was once
	// on origin, so nothing is unpushed.
	fp, hasFP := git.forkPoint(ctx, dest, target, head)
	if (hasFP && fp == head) ||
		(oldTip != "" && !git.isAncestor(ctx, dest, oldTip, target) && git.isAncestor(ctx, dest, head, oldTip)) {
		return followForcePush(ctx, git, dest, b, head)
	}
	if hasFP && !git.isAncestor(ctx, dest, fp, target) {
		// Commits origin dropped aren't the user's work, so only those after
		// the fork point are counted.
		if n, ok := countCommits(ctx, git, dest, fp+"..HEAD"); ok {
			return ActionFetchedOnly, fmt.Sprintf("%s local commit(s) on top of a force-pushed origin/%s", n, b)
		}
	}
	if n, ok := countCommits(ctx, git, dest, target+"..HEAD"); ok {
		return ActionFetchedOnly, fmt.Sprintf("%s local commit(s) not on origin/%s", n, b)
	}
	return ActionFetchedOnly, "local commits not on origin/" + b
}

// countCommits returns how many commits rangeSpec holds, as git prints it.
func countCommits(ctx context.Context, git gitRunner, dest, rangeSpec string) (string, bool) {
	r := git.run(ctx, dest, "rev-list", "--count", rangeSpec)
	n := strings.TrimSpace(r.Stdout)
	return n, r.Success() && n != ""
}

// followForcePush moves b, checked out at head with no local changes or
// unpushed commits, to origin's rewritten b. The old tip is kept under
// refs/ghx/forced/, since a backup shouldn't lose history GitHub dropped.
func followForcePush(ctx context.Context, git gitRunner, dest, b, head string) (Action, string) {
	backup := "refs/ghx/forced/" + b + "/" + time.Now().UTC().Format("20060102T150405Z")
	// The empty old value makes update-ref refuse to overwrite a backup.
	if r := git.run(ctx, dest, "update-ref", backup, head, ""); !r.Success() {
		return ActionFetchedOnly, r.Message()
	}
	// --keep, not --hard: with no tracked changes they move the same files,
	// but --keep refuses to overwrite an untracked file the new tip adds.
	if r := git.run(ctx, dest, "reset", "--keep", originRef(b)); !r.Success() {
		git.run(ctx, dest, "update-ref", "-d", backup, head)
		return ActionFetchedOnly, r.Message()
	}
	return ActionPulled, fmt.Sprintf("origin/%s was force-pushed; old tip kept at %s", b, backup)
}

// updateOther handles HEAD on cur, a branch other than the target b. When the
// remote renamed its default branch (cur's upstream is gone or was never set)
// and nothing can be lost, it does what GitHub documents after a rename:
// switch to b and delete cur. Otherwise the working tree stays on cur, and a
// local b is fast-forwarded without checking it out.
func updateOther(ctx context.Context, git gitRunner, dest, cur, b string, dirty bool) (Action, string) {
	target := originRef(b)
	_, haveLocal := git.revParse(ctx, dest, "refs/heads/"+b)
	upstream, gone := git.upstreamTrack(ctx, dest, cur)
	if !haveLocal && !dirty && (upstream == "" || gone) {
		head, born := git.revParse(ctx, dest, "HEAD")
		if !born || git.isAncestor(ctx, dest, head, target) {
			if r := git.run(ctx, dest, "switch", "--track", "-c", b, target); !r.Success() {
				return ActionFetchedOnly, r.Message()
			}
			if born {
				// -d refuses a branch holding unmerged work; that refusal
				// just keeps the branch.
				git.run(ctx, dest, "branch", "-d", cur)
			}
			return ActionPulled, fmt.Sprintf("switched from %s to %s", cur, b)
		}
	}
	if haveLocal {
		// fetch refuses anything but a fast-forward; a refusal leaves b as it
		// was.
		git.run(ctx, dest, "fetch", ".", target+":refs/heads/"+b)
	}
	return ActionFetchedOnly, "on branch " + cur
}

// resultFor returns a function that makes name's result from an action and its
// detail, so a helper's (Action, string) return can be passed straight to it.
func resultFor(name string) func(Action, string) RepoResult {
	return func(a Action, detail string) RepoResult {
		return RepoResult{Name: name, Action: a, Detail: detail}
	}
}

// originRef is the full name of origin's copy of branch, which can't be
// mistaken for a tag or a local branch that happens to be named origin/x.
func originRef(branch string) string {
	return "refs/remotes/origin/" + branch
}

// processWiki syncs repo's wiki clone, <repo>.wiki beside it. A wiki is
// synced like any repo, so a diverged or dirty one is fetched only rather than
// silently skipped. Its results are named after the repo, not the wiki
// directory, so the summary reads as it always has, except that a failure or a
// fetched-only wiki is named <repo>.wiki so it can't be mistaken for the repo.
func processWiki(ctx context.Context, git gitRunner, repo ghapi.RepoInfo, opts Options) []RepoResult {
	wikiDest := filepath.Join(opts.OutputDir, repo.Name+".wiki")
	if HasGitDir(wikiDest) {
		return syncWiki(ctx, git, wikiDest, repo, opts)
	}

	wikiURL := resolveWikiURL(repo, opts.UseSSH)
	args := slices.Concat([]string{"clone"}, platformCloneArgs, []string{wikiURL, wikiDest})
	r := git.fetch(ctx, "", wikiURL, args...)
	if !r.Success() {
		if wikiNotFound(r.Stderr) {
			return []RepoResult{{Name: repo.Name, Action: ActionSkippedWiki}}
		}
		return []RepoResult{{Name: repo.Name + ".wiki", Action: ActionFailedWiki, Detail: r.Message()}}
	}

	git.setIdentity(ctx, wikiDest, opts.GitAuthor, opts.GitEmail)
	if _, born := git.revParse(ctx, wikiDest, "HEAD"); !born {
		branches := git.remoteBranches(ctx, wikiDest)
		if len(branches) == 0 {
			if ctx.Err() == nil {
				// Nothing to back up, and the next run clones it again, so it
				// adds no row to the summary.
				_ = DeleteRepo(wikiDest)
			}
			return nil
		}
		b := pickBranch("", branches)
		git.run(ctx, wikiDest, "switch", "--track", "-c", b, originRef(b))
	}
	return []RepoResult{{Name: repo.Name, Action: ActionClonedWiki}}
}

// syncWiki brings an existing wiki clone up to date through syncExisting. A
// wiki has no default branch from the API, so it follows the branch origin/HEAD
// names (the clone set it) and falls back to main, master, then the newest
// branch. A quiet success stays quiet; fetched only and failures surface.
func syncWiki(ctx context.Context, git gitRunner, wikiDest string, repo ghapi.RepoInfo, opts Options) []RepoResult {
	wiki := repo
	wiki.DefaultBranch = ""
	if head := git.run(ctx, wikiDest, "symbolic-ref", "refs/remotes/origin/HEAD"); head.Success() {
		wiki.DefaultBranch = strings.TrimPrefix(strings.TrimSpace(head.Stdout), originRef(""))
	}
	result := syncExisting(ctx, git, wikiDest, wiki, opts)
	result.Name = repo.Name + ".wiki"
	switch result.Action {
	case ActionPulled, ActionUpToDate, ActionSkippedEmpty:
		return nil
	case ActionFailed:
		result.Action = ActionFailedWiki
	}
	return []RepoResult{result}
}

// wikiNotFound reports whether a wiki clone's stderr says the wiki doesn't
// exist, which is ordinary: GitHub's HasWiki is true for a repo that never had
// a page written. Any other failure is a real one.
func wikiNotFound(stderr string) bool {
	lower := strings.ToLower(stderr)
	return strings.Contains(lower, "not found") || strings.Contains(lower, "error: 404")
}

// HasGitDir reports whether repoDir holds a .git directory, which is how ghx
// tells a clone from any other directory. A .git file (a worktree or a
// submodule) doesn't count: ghx never makes one.
func HasGitDir(repoDir string) bool {
	info, err := os.Stat(filepath.Join(repoDir, ".git"))
	return err == nil && info.IsDir()
}

func resolveURL(repo ghapi.RepoInfo, useSSH bool) string {
	if useSSH {
		return repo.SSHURL
	}
	return repo.CloneURL
}

func resolveWikiURL(repo ghapi.RepoInfo, useSSH bool) string {
	if useSSH {
		ssh := repo.SSHURL
		if strings.HasSuffix(ssh, ".git") {
			return strings.TrimSuffix(ssh, ".git") + ".wiki.git"
		}
		return ssh + ".wiki"
	}
	wikiURL := strings.Replace(repo.CloneURL, ".git", ".wiki.git", 1)
	if !strings.HasSuffix(wikiURL, ".wiki.git") {
		wikiURL += ".wiki.git"
	}
	return wikiURL
}

// newGitRunner finds git, unless opts already names it, and carries the token.
func newGitRunner(ctx context.Context, opts Options) (gitRunner, error) {
	path := opts.GitPath
	if path == "" {
		found, err := FindGit(ctx)
		if err != nil {
			return gitRunner{}, err
		}
		path = found
	}
	return gitRunner{path: path, cred: newCredential(opts.Token)}, nil
}

// CleanOrigins removes the credential from the origin URL of every clone
// directly under each of parents, as processRepo does before a pull. The run
// flow calls it for the output directory, DELETED/ and ARCHIVED/, so clones
// that no sync pulls any more (moved, excluded, or a removed repo's wiki) are
// cleaned too. A clone's .git/config is read first and git is asked only when
// it holds an "@", so the pass costs a file read per clone that is already
// clean. Missing parents are skipped; each clone that can't be cleaned is a
// failed result named by its path. opts supplies GitPath and Token (for
// masking messages); with no GitPath it calls FindGit.
func CleanOrigins(ctx context.Context, opts Options, parents ...string) ([]RepoResult, error) {
	var git gitRunner
	var results []RepoResult
	for _, parent := range parents {
		entries, err := os.ReadDir(parent)
		if err != nil {
			continue
		}
		for _, e := range entries {
			dir := filepath.Join(parent, e.Name())
			if !e.IsDir() || !HasGitDir(dir) || !mayHoldUserInfo(dir) {
				continue
			}
			if git.path == "" {
				if git, err = newGitRunner(ctx, opts); err != nil {
					return results, err
				}
			}
			if err := git.cleanOrigin(ctx, dir); err != nil {
				results = append(results, RepoResult{Name: dir, Action: ActionFailed, Detail: err.Error()})
			}
		}
	}
	return results, nil
}

// mayHoldUserInfo reports whether repoDir's .git/config could hold a URL with
// user info. An unreadable file says yes, so git decides.
func mayHoldUserInfo(repoDir string) bool {
	data, err := os.ReadFile(filepath.Join(repoDir, ".git", "config"))
	return err != nil || bytes.Contains(data, []byte("@"))
}

// MoveRepo moves a repo directory into targetDir (creating targetDir if
// missing). If the destination already exists it is removed first. On
// Windows, a remove or rename that fails because another process briefly holds
// a file open is retried for up to retryBudget.
func MoveRepo(repoDir, targetDir string) error {
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return err
	}
	dest := filepath.Join(targetDir, filepath.Base(repoDir))
	if _, err := os.Stat(dest); err == nil {
		if err := retryTransient(func() error { return os.RemoveAll(dest) }); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	return retryTransient(func() error { return os.Rename(repoDir, dest) })
}

// DeleteRepo removes repoDir. Missing directories are treated as success.
// os.RemoveAll clears the read-only attribute Windows git gives its object
// files, so a clone deletes on every OS.
func DeleteRepo(repoDir string) error {
	if _, err := os.Stat(repoDir); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return retryTransient(func() error { return os.RemoveAll(repoDir) })
}

// retryBudget bounds how long MoveRepo and DeleteRepo retry a transient
// failure before reporting it.
const retryBudget = 2 * time.Second

func retryTransient(op func() error) error {
	return retryWithin(op, isTransient, retryBudget)
}

// retryWithin runs op until it succeeds, fails with an error transient
// doesn't accept, or budget runs out, backing off between attempts.
func retryWithin(op func() error, transient func(error) bool, budget time.Duration) error {
	deadline := time.Now().Add(budget)
	delay := 5 * time.Millisecond
	for {
		err := op()
		if err == nil || !transient(err) || time.Now().Add(delay).After(deadline) {
			return err
		}
		time.Sleep(delay)
		delay = min(2*delay, 250*time.Millisecond)
	}
}
