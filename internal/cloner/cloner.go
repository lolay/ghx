package cloner

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jedib0t/go-pretty/v6/progress"

	"github.com/lolay/ghx/internal/ghapi"
)

// Options controls a single CloneRepos invocation.
type Options struct {
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
	// ProgressOut is the io.Writer the progress bar renders to.
	// Defaults to os.Stdout when nil.
	ProgressOut io.Writer
}

// CloneRepos clones or pulls every repo in repos concurrently, bounded
// by opts.Concurrency, and returns the aggregate SyncSummary. A single
// progress tracker advances as each repo finishes.
func CloneRepos(ctx context.Context, repos []ghapi.RepoInfo, opts Options) (SyncSummary, error) {
	summary := SyncSummary{}
	if err := os.MkdirAll(opts.OutputDir, 0o755); err != nil {
		return summary, fmt.Errorf("creating output dir: %w", err)
	}

	if len(repos) == 0 {
		return summary, nil
	}

	git := gitRunner{path: opts.GitPath}
	if git.path == "" {
		path, err := FindGit()
		if err != nil {
			return summary, err
		}
		git.path = path
	}

	concurrency := opts.Concurrency
	if concurrency < 1 {
		concurrency = 1
	}

	progressOut := opts.ProgressOut
	if progressOut == nil {
		progressOut = os.Stdout
	}

	pw := progress.NewWriter()
	pw.SetOutputWriter(progressOut)
	pw.SetAutoStop(true)
	pw.SetUpdateFrequency(100 * time.Millisecond)
	pw.SetTrackerLength(25)
	pw.SetNumTrackersExpected(1)
	pw.Style().Visibility.ETA = false
	pw.Style().Visibility.ETAOverall = false
	go pw.Render()

	tracker := &progress.Tracker{
		Message: "Syncing repos",
		Total:   int64(len(repos)),
		Units:   progress.UnitsDefault,
	}
	pw.AppendTracker(tracker)

	type job struct{ repo ghapi.RepoInfo }
	jobs := make(chan job)

	var mu sync.Mutex
	var wg sync.WaitGroup

	for range concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				results := processRepo(ctx, git, j.repo, opts)
				mu.Lock()
				summary.Results = append(summary.Results, results...)
				mu.Unlock()
				tracker.Increment(1)
			}
		}()
	}

	for _, r := range repos {
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			tracker.MarkAsErrored()
			return summary, ctx.Err()
		case jobs <- job{repo: r}:
		}
	}
	close(jobs)
	wg.Wait()

	// Give the renderer a moment to flush the final frame.
	for pw.IsRenderInProgress() {
		time.Sleep(50 * time.Millisecond)
	}

	return summary, nil
}

func processRepo(ctx context.Context, git gitRunner, repo ghapi.RepoInfo, opts Options) []RepoResult {
	if err := CheckRepoName(repo.Name); err != nil {
		return []RepoResult{{Name: repo.Name, Action: ActionSkippedName, Detail: err.Error()}}
	}

	var results []RepoResult
	dest := filepath.Join(opts.OutputDir, repo.Name)
	url := resolveURL(repo, opts.Token, opts.UseSSH)

	if hasGitDir(dest) {
		git.setIdentity(ctx, dest, opts.GitAuthor, opts.GitEmail)
		dirty, status := git.status(ctx, dest)
		switch {
		case !status.Success():
			results = append(results, RepoResult{Name: repo.Name, Action: ActionFailed, Detail: status.Message()})
		case dirty:
			results = append(results, RepoResult{Name: repo.Name, Action: ActionSkippedDirty})
		default:
			r := git.run(ctx, dest, "pull", "--ff-only")
			if r.Success() {
				results = append(results, RepoResult{Name: repo.Name, Action: ActionPulled})
			} else {
				results = append(results, RepoResult{Name: repo.Name, Action: ActionFailed, Detail: r.Message()})
			}
		}
	} else {
		args := slices.Concat([]string{"clone"}, platformCloneArgs, []string{"--branch", repo.DefaultBranch, url, dest})
		r := git.run(ctx, "", args...)
		if r.Success() {
			git.setIdentity(ctx, dest, opts.GitAuthor, opts.GitEmail)
			results = append(results, RepoResult{Name: repo.Name, Action: ActionCloned})
		} else {
			results = append(results, RepoResult{Name: repo.Name, Action: ActionFailed, Detail: r.Message()})
		}
	}

	if opts.CloneWiki && repo.HasWiki && results[len(results)-1].Action != ActionFailed {
		results = append(results, processWiki(ctx, git, repo, opts)...)
	}
	return results
}

func processWiki(ctx context.Context, git gitRunner, repo ghapi.RepoInfo, opts Options) []RepoResult {
	wikiDest := filepath.Join(opts.OutputDir, repo.Name+".wiki")
	wikiURL := resolveWikiURL(repo, opts.Token, opts.UseSSH)

	if hasGitDir(wikiDest) {
		git.setIdentity(ctx, wikiDest, opts.GitAuthor, opts.GitEmail)
		if dirty, status := git.status(ctx, wikiDest); status.Success() && !dirty {
			git.run(ctx, wikiDest, "pull", "--ff-only")
		}
		return nil
	}
	args := slices.Concat([]string{"clone"}, platformCloneArgs, []string{wikiURL, wikiDest})
	r := git.run(ctx, "", args...)
	if r.Success() {
		git.setIdentity(ctx, wikiDest, opts.GitAuthor, opts.GitEmail)
		return []RepoResult{{Name: repo.Name, Action: ActionClonedWiki}}
	}
	return []RepoResult{{Name: repo.Name, Action: ActionSkippedWiki}}
}

func hasGitDir(repoDir string) bool {
	info, err := os.Stat(filepath.Join(repoDir, ".git"))
	return err == nil && info.IsDir()
}

func resolveURL(repo ghapi.RepoInfo, token string, useSSH bool) string {
	if useSSH {
		return repo.SSHURL
	}
	return authenticatedHTTPS(repo.CloneURL, token)
}

func resolveWikiURL(repo ghapi.RepoInfo, token string, useSSH bool) string {
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
	return authenticatedHTTPS(wikiURL, token)
}

func authenticatedHTTPS(url, token string) string {
	const prefix = "https://"
	if strings.HasPrefix(url, prefix) {
		return prefix + token + "@" + strings.TrimPrefix(url, prefix)
	}
	return url
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
