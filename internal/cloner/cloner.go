package cloner

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/garyrudolph/ghx/internal/ghapi"
	"github.com/jedib0t/go-pretty/v6/progress"
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
				results := processRepo(ctx, j.repo, opts)
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

func processRepo(ctx context.Context, repo ghapi.RepoInfo, opts Options) []RepoResult {
	var results []RepoResult
	dest := filepath.Join(opts.OutputDir, repo.Name)
	url := resolveURL(repo, opts.Token, opts.UseSSH)

	if hasGitDir(dest) {
		setGitIdentity(ctx, dest, opts.GitAuthor, opts.GitEmail)
		if isDirty(ctx, dest) {
			results = append(results, RepoResult{Name: repo.Name, Action: ActionSkippedDirty})
		} else {
			r := runGit(ctx, dest, "pull", "--ff-only")
			if r.Success() {
				results = append(results, RepoResult{Name: repo.Name, Action: ActionPulled})
			} else {
				results = append(results, RepoResult{Name: repo.Name, Action: ActionFailed, Detail: r.Message()})
			}
		}
	} else {
		r := runGit(ctx, "", "clone", "--branch", repo.DefaultBranch, url, dest)
		if r.Success() {
			setGitIdentity(ctx, dest, opts.GitAuthor, opts.GitEmail)
			results = append(results, RepoResult{Name: repo.Name, Action: ActionCloned})
		} else {
			results = append(results, RepoResult{Name: repo.Name, Action: ActionFailed, Detail: r.Message()})
		}
	}

	if opts.CloneWiki && repo.HasWiki && results[len(results)-1].Action != ActionFailed {
		results = append(results, processWiki(ctx, repo, opts)...)
	}
	return results
}

func processWiki(ctx context.Context, repo ghapi.RepoInfo, opts Options) []RepoResult {
	wikiDest := filepath.Join(opts.OutputDir, repo.Name+".wiki")
	wikiURL := resolveWikiURL(repo, opts.Token, opts.UseSSH)

	if hasGitDir(wikiDest) {
		setGitIdentity(ctx, wikiDest, opts.GitAuthor, opts.GitEmail)
		if !isDirty(ctx, wikiDest) {
			runGit(ctx, wikiDest, "pull", "--ff-only")
		}
		return nil
	}
	r := runGit(ctx, "", "clone", wikiURL, wikiDest)
	if r.Success() {
		setGitIdentity(ctx, wikiDest, opts.GitAuthor, opts.GitEmail)
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
// missing). If the destination already exists it is removed first.
func MoveRepo(repoDir, targetDir string) error {
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return err
	}
	dest := filepath.Join(targetDir, filepath.Base(repoDir))
	if _, err := os.Stat(dest); err == nil {
		if err := os.RemoveAll(dest); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.Rename(repoDir, dest)
}

// DeleteRepo removes repoDir. Missing directories are treated as success.
func DeleteRepo(repoDir string) error {
	if _, err := os.Stat(repoDir); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return os.RemoveAll(repoDir)
}
