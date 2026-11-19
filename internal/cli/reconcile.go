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
	check  *originCheck
	dryRun bool
	// stamp suffixes a name already taken in the deleted directory; one per
	// run, so a repo and its wiki share it.
	stamp   string
	summary cloner.SyncSummary
	// failed holds the names whose move or delete failed, so a removal that
	// didn't finish can be kept for the next run to retry.
	failed map[string]bool
}

// deletedStampLayout formats the UTC time that tells two clones of the same
// removed repo apart in the deleted directory. It has no colons, which
// Windows doesn't allow in a file name.
const deletedStampLayout = "20060102T150405Z"

func (r *reconciler) add(name string, action cloner.Action, detail string) {
	r.summary.Append(cloner.RepoResult{Name: name, Action: action, Detail: detail})
	if action == cloner.ActionFailed {
		if r.failed == nil {
			r.failed = map[string]bool{}
		}
		r.failed[name] = true
	}
}

// reconcileArchived puts each repo's clone where its archived state says it
// belongs, <out>/<archived-dir>/x for an archived repo and <out>/x otherwise,
// whatever the previous manifest said: a clone found in the other place is
// moved over (deleted instead, for an archived repo under --delete), and its
// wiki follows it either way. That covers a repo archived since the last
// run, one unarchived since, a mirror with no manifest, and a move an earlier
// run failed to make. repos is the filtered list, so a repo the run doesn't
// sync is never moved somewhere it won't be synced. listed is every name in
// the unfiltered listing, lowercased: a wiki is left alone when a repo of its
// name is listed, since the origin check can't tell the two apart.
func reconcileArchived(
	check *originCheck,
	outputDir, archivedDir string,
	repos []ghapi.RepoInfo,
	listed map[string]bool,
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
		moved := r.place(repo.Name, other, want, action, deleteIt)
		// The wiki goes where its repo went, or with it under --delete: left
		// behind, it would never be synced again, and the sync would clone a
		// second one. That includes a wiki an earlier move left behind (an
		// older ghx, or --clone-wiki turned on later), once its repo is no
		// longer at other; a repo still there after a conflict or an origin
		// mismatch keeps its wiki beside it.
		wiki := repo.Name + ".wiki"
		if (moved || !cloner.HasGitDir(other)) && !listed[strings.ToLower(wiki)] {
			r.place(wiki, other+".wiki", want+".wiki", action, deleteIt)
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
		// Under --delete an archived repo isn't synced, so want only stays.
		then := "syncing " + want
		if deleteIt {
			then = "deleting neither"
		}
		r.add(name, cloner.ActionConflict, fmt.Sprintf("%s and %s both exist; %s", other, want, then))
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

// retire moves the clone of a removed repo at dir into deletedRoot as base,
// or deletes it under deleteIt, and reports the name it moved to and whether
// it moved or deleted it (or in a dry run, would). Nothing missing, nothing
// whose origin doesn't name the repo, and nothing already in deletedRoot is
// touched: when base is taken, the clone goes to base.<stamp> beside it, so
// an earlier removal's clone, and any work in it, is kept.
func (r *reconciler) retire(name, dir, deletedRoot, base string, deleteIt bool) (string, bool) {
	if _, err := os.Stat(dir); err != nil {
		// Only a clone that isn't there is done with. Anything else (a
		// directory that can't be read) is a failure, so the removal is kept
		// and retried once the path can be checked.
		if !errors.Is(err, fs.ErrNotExist) {
			r.add(name, cloner.ActionFailed, err.Error())
		}
		return "", false
	}
	if detail := r.check.mismatch(dir, name); detail != "" {
		r.add(name, cloner.ActionSkippedOrigin, detail)
		return "", false
	}
	if deleteIt {
		if !r.dryRun {
			if err := cloner.DeleteRepo(dir); err != nil {
				r.add(name, cloner.ActionFailed, err.Error())
				return "", false
			}
		}
		r.add(name, cloner.ActionDeleted, dryRunDetail("", r.dryRun))
		return "", true
	}
	dest, detail, err := r.freeName(deletedRoot, base)
	if err != nil {
		r.add(name, cloner.ActionFailed, err.Error())
		return "", false
	}
	if !r.dryRun {
		if err := cloner.MoveRepoTo(dir, filepath.Join(deletedRoot, dest)); err != nil {
			r.add(name, cloner.ActionFailed, err.Error())
			return "", false
		}
	}
	r.add(name, cloner.ActionMovedDeleted, dryRunDetail(detail, r.dryRun))
	return dest, true
}

// freeName returns base when nothing in root has that name, and otherwise
// base.<stamp>, or base.<stamp>-2 and so on, with a detail saying why. A name
// that can't be checked (root is a file, or can't be traversed) is an error:
// counting it as taken would try suffixes forever.
func (r *reconciler) freeName(root, base string) (name, detail string, err error) {
	isTaken, err := taken(filepath.Join(root, base))
	if err != nil || !isTaken {
		return base, "", err
	}
	name = base + "." + r.stamp
	for n := 2; ; n++ {
		if isTaken, err = taken(filepath.Join(root, name)); err != nil {
			return "", "", err
		}
		if !isTaken {
			break
		}
		name = fmt.Sprintf("%s.%s-%d", base, r.stamp, n)
	}
	return name, fmt.Sprintf("%s exists; moved to %s", filepath.Join(root, base), filepath.Join(root, name)), nil
}

// taken reports whether anything is at path, and an error when that can't
// be told.
func taken(path string) (bool, error) {
	_, err := os.Lstat(path)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	default:
		return false, err
	}
}

func dryRunDetail(detail string, dryRun bool) string {
	switch {
	case !dryRun:
		return detail
	case detail == "":
		return "dry-run"
	default:
		return detail + "; dry-run"
	}
}

// exists reports whether anything is at path. A path that can't be checked
// counts as there, so nothing is moved onto it.
func exists(path string) bool {
	_, err := os.Lstat(path)
	return !errors.Is(err, fs.ErrNotExist)
}
