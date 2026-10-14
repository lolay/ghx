package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/lolay/ghx/internal/cloner"
	"github.com/lolay/ghx/internal/ghapi"
)

// originCheck confirms that a directory is a clone of the repo it is named
// for before ghx moves or deletes it. The directory's name matches the repo by
// construction, so it proves nothing; the origin URL does.
type originCheck struct {
	ctx context.Context
	// gitPath is the git the run found; a dry run, which doesn't look git up,
	// finds it on first use.
	gitPath string
	gitErr  error
	looked  bool
}

// mismatch returns "" when dir's origin URL names the repo name, and
// otherwise the detail of a skipped (origin mismatch) result: the path and
// the URL found, or why there is none. Host and owner aren't compared, so a
// renamed org, an Enterprise host or an SSH host alias still matches; a
// missing or unreadable origin is a mismatch, which only ever leaves the
// directory alone.
func (c *originCheck) mismatch(dir, name string) string {
	if c.gitPath == "" && !c.looked {
		c.looked = true
		c.gitPath, c.gitErr = cloner.FindGit(c.ctx)
	}
	if c.gitErr != nil {
		return fmt.Sprintf("%s: can't read its origin: %v", dir, c.gitErr)
	}
	origin, err := cloner.ReadOrigin(c.ctx, c.gitPath, dir)
	switch {
	case errors.Is(err, cloner.ErrNoOrigin):
		return fmt.Sprintf("%s has no origin URL", dir)
	case err != nil:
		return fmt.Sprintf("%s: can't read its origin: %v", dir, err)
	}
	if got, ok := cloner.OriginRepoName(origin); ok && strings.EqualFold(got, name) {
		return ""
	}
	return fmt.Sprintf("%s has origin %s", dir, cloner.DisplayURL(origin))
}

// reconciler records what the moves and deletes before a sync did, or in a
// dry run would do.
type reconciler struct {
	check   *originCheck
	dryRun  bool
	summary cloner.SyncSummary
}

func (r *reconciler) add(name string, action cloner.Action, detail string) {
	r.summary.Append(cloner.RepoResult{Name: name, Action: action, Detail: detail})
}

// reconcileArchived puts each repo's clone where its archived state says it
// belongs, <out>/<archived-dir>/x for an archived repo and <out>/x otherwise,
// whatever the previous manifest said: a clone found in the other place is
// moved over (deleted instead, for an archived repo under --delete), and its
// wiki follows it. That covers a repo archived since the last run, one
// unarchived since, a mirror with no manifest, and a move an earlier run
// failed to make. repos is the filtered list, so a repo the run doesn't sync
// is never moved somewhere it won't be synced.
func reconcileArchived(
	check *originCheck,
	outputDir, archivedDir string,
	repos []ghapi.RepoInfo,
	shouldDelete, dryRun bool,
) cloner.SyncSummary {
	r := reconciler{check: check, dryRun: dryRun}
	archivedRoot := filepath.Join(outputDir, archivedDir)
	for _, repo := range repos {
		// CloneRepos reports a name that can't be a directory.
		if cloner.CheckRepoName(repo.Name) != nil {
			continue
		}
		want := filepath.Join(outputDir, repo.Name)
		other := filepath.Join(archivedRoot, repo.Name)
		action := cloner.ActionMovedUnarchived
		if repo.Archived {
			want, other = other, want
			action = cloner.ActionMovedArchived
		}
		deleteIt := repo.Archived && shouldDelete
		if r.place(repo.Name, other, want, action, deleteIt) && !deleteIt {
			wiki := repo.Name + ".wiki"
			r.place(wiki, other+".wiki", want+".wiki", action, false)
		}
	}
	return r.summary
}

// place moves the clone of name at other to want, or deletes it when
// deleteIt is set, and reports whether it did (or in a dry run, would). A
// directory that isn't a clone is no business of ghx's; one whose origin
// doesn't name the repo is left and reported; and nothing is moved onto an
// existing want, which the sync then syncs as usual, since MoveRepo would
// replace it and lose any work in it.
func (r *reconciler) place(name, other, want string, action cloner.Action, deleteIt bool) bool {
	if !cloner.HasGitDir(other) {
		return false
	}
	if detail := r.check.mismatch(other, name); detail != "" {
		r.add(name, cloner.ActionSkippedOrigin, detail)
		return false
	}
	if exists(want) {
		r.add(name, cloner.ActionConflict, fmt.Sprintf("%s and %s both exist; syncing %s", other, want, want))
		return false
	}
	if deleteIt {
		action = cloner.ActionDeleted
	}
	if r.dryRun {
		r.add(name, action, "dry-run")
		return true
	}
	var err error
	if deleteIt {
		err = cloner.DeleteRepo(other)
	} else {
		err = cloner.MoveRepo(other, filepath.Dir(want))
	}
	if err != nil {
		r.add(name, cloner.ActionFailed, err.Error())
		return false
	}
	r.add(name, action, "")
	return true
}

// exists reports whether anything is at path. A path that can't be checked
// counts as there, so nothing is moved onto it.
func exists(path string) bool {
	_, err := os.Lstat(path)
	return !errors.Is(err, fs.ErrNotExist)
}
