package cloner

import (
	"bytes"
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

	git, err := newGitRunner(ctx, opts)
	if err != nil {
		return summary, err
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
	url := resolveURL(repo, opts.UseSSH)

	if hasGitDir(dest) {
		git.setIdentity(ctx, dest, opts.GitAuthor, opts.GitEmail)
		dirty, status := git.status(ctx, dest)
		switch {
		case !status.Success():
			results = append(results, RepoResult{Name: repo.Name, Action: ActionFailed, Detail: status.Message()})
		case dirty:
			results = append(results, RepoResult{Name: repo.Name, Action: ActionSkippedDirty})
		default:
			if err := git.cleanOrigin(ctx, dest); err != nil {
				results = append(results, RepoResult{Name: repo.Name, Action: ActionFailed, Detail: err.Error()})
				break
			}
			// The header is scoped to the HTTPS clone URL's host even in SSH
			// mode, so a clone first made over HTTPS keeps pulling; an SSH
			// origin never sends it.
			r := git.fetch(ctx, dest, repo.CloneURL, "pull", "--ff-only")
			if r.Success() {
				results = append(results, RepoResult{Name: repo.Name, Action: ActionPulled})
			} else {
				results = append(results, RepoResult{Name: repo.Name, Action: ActionFailed, Detail: r.Message()})
			}
		}
	} else {
		args := slices.Concat([]string{"clone"}, platformCloneArgs, []string{"--branch", repo.DefaultBranch, url, dest})
		r := git.fetch(ctx, "", url, args...)
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
	wikiURL := resolveWikiURL(repo, opts.UseSSH)

	if hasGitDir(wikiDest) {
		git.setIdentity(ctx, wikiDest, opts.GitAuthor, opts.GitEmail)
		if dirty, status := git.status(ctx, wikiDest); status.Success() && !dirty {
			// A wiki that can't be cleaned or pulled adds no result, as before;
			// the next run tries again.
			if git.cleanOrigin(ctx, wikiDest) == nil {
				git.fetch(ctx, wikiDest, repo.CloneURL, "pull", "--ff-only")
			}
		}
		return nil
	}
	args := slices.Concat([]string{"clone"}, platformCloneArgs, []string{wikiURL, wikiDest})
	r := git.fetch(ctx, "", wikiURL, args...)
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
			if !e.IsDir() || !hasGitDir(dir) || !mayHoldUserInfo(dir) {
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
