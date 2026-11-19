package cli_test

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lolay/ghx/internal/config"
	"github.com/lolay/ghx/internal/ghapi/ghapitest"
	"github.com/lolay/ghx/internal/gittest"
)

func TestRun_AnArchivedRepoWithNoManifestIsMovedNotDuplicated(t *testing.T) {
	w := newWorld(t)
	w.addRepo("alpha")
	w.addRepo("beta")
	w.mustRun()
	// A mirror whose manifest is gone, as after a manual delete or for a tree
	// cloned some other way: only the disk says where beta is.
	require.NoError(t, os.Remove(filepath.Join(w.out, config.ManifestFilename)))
	w.archiveRepo("beta")

	out := w.mustRun()

	assert.Equal(t, []string{"ARCHIVED/beta", "alpha"}, repoDirs(t, w.out), "moved, with no copy left behind")
	assert.Contains(t, out, "beta: moved (archived)")
	assert.NotContains(t, out, "cloned", "nothing was cloned again")
}

func TestRun_AnUnarchivedRepoMovesBackAndIsPulled(t *testing.T) {
	w := newWorld(t)
	w.addRepo("alpha")
	w.addRepo("beta", func(r *ghapitest.Repo) { r.Archived = true })
	w.mustRun()
	require.Equal(t, []string{"ARCHIVED/beta", "alpha"}, repoDirs(t, w.out))

	w.unarchiveRepo("beta")
	w.pushCommit("beta", "NEWS.md")
	out := w.mustRun()

	assert.Equal(t, []string{"alpha", "beta"}, repoDirs(t, w.out))
	assert.Contains(t, out, "beta: moved (unarchived)")
	assert.FileExists(t, filepath.Join(w.out, "beta", "NEWS.md"), "the moved clone was then pulled")
}

func TestRun_ADryRunReportsAnUnarchiveAndMovesNothing(t *testing.T) {
	w := newWorld(t)
	w.addRepo("beta", func(r *ghapitest.Repo) { r.Archived = true })
	w.mustRun()
	w.unarchiveRepo("beta")
	before := snapshot(t, w.out)

	out := w.mustRun("--dry-run")

	assert.Equal(t, before, snapshot(t, w.out))
	assert.Contains(t, out, "beta: moved (unarchived) (dry-run)")
}

func TestRun_AWikiMovesWithItsRepo(t *testing.T) {
	w := newWorld(t)
	w.addRepo("alpha", func(r *ghapitest.Repo) { r.HasWiki = true })
	gittest.NewBareRepo(t, w.upstream, "alpha.wiki", "master")
	w.mustRun("--clone-wiki")
	require.Equal(t, []string{"alpha", "alpha.wiki"}, repoDirs(t, w.out))

	w.archiveRepo("alpha")
	out := w.mustRun("--clone-wiki")

	assert.Equal(t, []string{"ARCHIVED/alpha", "ARCHIVED/alpha.wiki"}, repoDirs(t, w.out))
	assert.Contains(t, out, "alpha.wiki: moved (archived)")
}

func TestRun_AnExcludedArchivedRepoStaysUntilItIsSynced(t *testing.T) {
	w := newWorld(t)
	w.addRepo("alpha")
	w.addRepo("beta")
	w.mustRun()
	w.archiveRepo("beta")

	// Excluded, beta isn't synced, so it isn't moved where no sync reaches it.
	w.mustRun("--exclude", "^beta$")
	require.Equal(t, []string{"alpha", "beta"}, repoDirs(t, w.out))
	require.Contains(t, manifestNames(readManifest(t, w.out)), "beta", "the manifest now records beta as archived")

	// The manifest already says archived; the disk says the move is owed.
	out := w.mustRun()

	assert.Equal(t, []string{"ARCHIVED/beta", "alpha"}, repoDirs(t, w.out))
	assert.Contains(t, out, "beta: moved (archived)")
}

func TestRun_ACloneWhoseOriginNamesAnotherRepoIsLeftAlone(t *testing.T) {
	tests := []struct {
		name   string
		change func(w *world)
		args   []string
	}{
		{"archived", func(w *world) { w.archiveRepo("beta") }, nil},
		{"archived with --delete", func(w *world) { w.archiveRepo("beta") }, []string{"--delete"}},
		{"removed", func(w *world) { w.removeRepo("beta") }, nil},
		{"removed with --delete", func(w *world) { w.removeRepo("beta") }, []string{"--delete"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newWorld(t)
			w.addRepo("alpha")
			w.addRepo("beta")
			w.mustRun()
			// The directory is named beta but is a clone of alpha.
			beta := filepath.Join(w.out, "beta")
			gittest.Git(t, beta, "remote", "set-url", "origin", w.bare("alpha"))
			tt.change(w)

			out := w.mustRun(tt.args...)

			assert.DirExists(t, filepath.Join(beta, ".git"), "neither moved nor deleted")
			assert.Contains(t, out, "beta: skipped (origin mismatch)")
			assert.Contains(t, out, "has origin "+w.bare("alpha"), "the detail names the URL found")
			assert.Contains(t, out, "Skipped (origin mismatch):")
		})
	}
}

func TestRun_ACloneWithNoOriginIsLeftAlone(t *testing.T) {
	w := newWorld(t)
	w.addRepo("beta")
	w.mustRun()
	beta := filepath.Join(w.out, "beta")
	gittest.Git(t, beta, "remote", "remove", "origin")
	w.archiveRepo("beta")

	out := w.mustRun()

	assert.DirExists(t, filepath.Join(beta, ".git"))
	assert.Contains(t, out, "beta: skipped (origin mismatch)")
	assert.Contains(t, out, "has no origin URL")
}

func TestRun_AnOriginNamesTheRepoWhateverItsHostOwnerOrCase(t *testing.T) {
	w := newWorld(t)
	w.addRepo("beta")
	w.mustRun()
	gittest.Git(t, filepath.Join(w.out, "beta"), "remote", "set-url", "origin", "git@ghe-alias:renamed-org/BETA.git")
	w.archiveRepo("beta")

	out := w.mustRun("--dry-run")

	assert.Contains(t, out, "beta: moved (archived) (dry-run)")
}

func TestRun_AClonePresentInBothPlacesIsAConflict(t *testing.T) {
	tests := []struct {
		name  string
		flags []string
		then  func(out string) string
	}{
		{"synced as usual", nil, func(out string) string { return "; syncing " + filepath.Join(out, "ARCHIVED", "beta") }},
		{"under --delete", []string{"--delete"}, func(string) string { return "; deleting neither" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newWorld(t)
			w.addRepo("alpha")
			w.addRepo("beta")
			w.mustRun()
			// Work in the active clone that a move onto ARCHIVED/beta would
			// keep, and a copy already in ARCHIVED/ that a replace would lose.
			local := filepath.Join(w.out, "beta", "notes.txt")
			require.NoError(t, os.WriteFile(local, []byte("mine\n"), 0o644))
			archived := filepath.Join(w.out, "ARCHIVED", "beta")
			gittest.Git(t, "", "clone", "--quiet", w.bare("beta"), archived)
			kept := filepath.Join(archived, "kept.txt")
			require.NoError(t, os.WriteFile(kept, []byte("also mine\n"), 0o644))
			w.archiveRepo("beta")

			out := w.mustRun(tt.flags...)

			assert.Equal(t, []string{"ARCHIVED/beta", "alpha", "beta"}, repoDirs(t, w.out))
			assert.FileExists(t, local)
			assert.FileExists(t, kept)
			assert.Contains(t, out, "beta: conflict")
			assert.Contains(t, out, tt.then(w.out))
			assert.Contains(t, out, "Conflicts (both copies left in place):")
		})
	}
}

func TestRun_AMoveThatFailedIsRetriedByTheNextRun(t *testing.T) {
	w := newWorld(t)
	w.addRepo("alpha")
	w.addRepo("beta")
	w.mustRun()
	w.archiveRepo("beta")
	// A dangling symlink where the archive directory belongs. Lstat of a path
	// under it says "not found", so the reconcile sees no conflict and goes on
	// to MoveRepo, whose MkdirAll then fails. A plain file there would make the
	// check itself fail on Unix (not a directory), which is a conflict instead.
	squatter := filepath.Join(w.out, "ARCHIVED")
	if err := os.Symlink(filepath.Join(w.out, "nowhere"), squatter); err != nil {
		t.Skipf("can't make a symlink here: %v", err)
	}

	out, _ := w.run()

	assert.Contains(t, out, "beta: failed", "the move was tried and failed, not reported as a conflict")
	assert.NotContains(t, out, "conflict")
	assert.NotContains(t, out, "moved (archived)", "nothing moved")
	assert.DirExists(t, filepath.Join(w.out, "beta", ".git"), "the clone stays where it was")

	require.NoError(t, os.Remove(squatter))
	out = w.mustRun()

	assert.Equal(t, []string{"ARCHIVED/beta", "alpha"}, repoDirs(t, w.out), "the next run finishes the move")
	assert.Contains(t, out, "beta: moved (archived)")
}

func TestRun_ArchivingUnarchivingAndArchivingAgainKeepsOneCloneAndItsCommits(t *testing.T) {
	w := newWorld(t)
	w.addRepo("alpha")
	w.addRepo("beta")
	w.mustRun()
	// Work that exists only in the clone: a second copy cloned fresh from
	// upstream would not have it.
	gittest.Git(t, filepath.Join(w.out, "beta"), "commit", "--allow-empty", "-m", "Local work")
	head := gittest.Git(t, filepath.Join(w.out, "beta"), "rev-parse", "HEAD")

	steps := []struct {
		name   string
		change func()
		want   []string
		clone  string
	}{
		{"archived", func() { w.archiveRepo("beta") }, []string{"ARCHIVED/beta", "alpha"}, "ARCHIVED/beta"},
		{"unarchived", func() { w.unarchiveRepo("beta") }, []string{"alpha", "beta"}, "beta"},
		{"archived again", func() { w.archiveRepo("beta") }, []string{"ARCHIVED/beta", "alpha"}, "ARCHIVED/beta"},
	}
	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			step.change()

			out := w.mustRun()

			assert.Equal(t, step.want, repoDirs(t, w.out), "exactly one clone of beta")
			assert.NotContains(t, out, "beta: cloned", "moved, never cloned again")
			assert.Equal(t, head, gittest.Git(t, filepath.Join(w.out, filepath.FromSlash(step.clone)), "rev-parse", "HEAD"),
				"the local commit survived")
		})
	}
}

func TestRun_AWikiStaysBesideItsRepoThroughArchiveAndUnarchive(t *testing.T) {
	w := newWorld(t)
	w.addRepo("alpha", func(r *ghapitest.Repo) { r.HasWiki = true })
	gittest.NewBareRepo(t, w.upstream, "alpha.wiki", "master")
	w.mustRun("--clone-wiki")
	require.Equal(t, []string{"alpha", "alpha.wiki"}, repoDirs(t, w.out))

	w.archiveRepo("alpha")
	out := w.mustRun("--clone-wiki")

	assert.Equal(t, []string{"ARCHIVED/alpha", "ARCHIVED/alpha.wiki"}, repoDirs(t, w.out), "after the archive")
	assert.Contains(t, out, "alpha.wiki: moved (archived)")

	w.unarchiveRepo("alpha")
	out = w.mustRun("--clone-wiki")

	assert.Equal(t, []string{"alpha", "alpha.wiki"}, repoDirs(t, w.out), "after the unarchive")
	assert.Contains(t, out, "alpha.wiki: moved (unarchived)")
}

func TestRun_DeleteRemovesAnArchivedReposWikiWithIt(t *testing.T) {
	w := newWorld(t)
	w.addRepo("alpha", func(r *ghapitest.Repo) { r.HasWiki = true })
	gittest.NewBareRepo(t, w.upstream, "alpha.wiki", "master")
	w.mustRun("--clone-wiki")
	require.Equal(t, []string{"alpha", "alpha.wiki"}, repoDirs(t, w.out))

	w.archiveRepo("alpha")
	out := w.mustRun("--clone-wiki", "--delete")

	assert.Empty(t, repoDirs(t, w.out), "the wiki went with its repo")
	assert.Contains(t, out, "alpha: deleted")
	assert.Contains(t, out, "alpha.wiki: deleted")
}

func TestRun_ARemovedReposWikiGoesWithIt(t *testing.T) {
	tests := []struct {
		name  string
		flags []string
		want  []string
		says  string
	}{
		{"moved to DELETED", nil, []string{"DELETED/alpha", "DELETED/alpha.wiki"}, "alpha.wiki: moved (deleted)"},
		{"deleted under --delete", []string{"--delete"}, nil, "alpha.wiki: deleted"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newWorld(t)
			w.addRepo("alpha", func(r *ghapitest.Repo) { r.HasWiki = true })
			gittest.NewBareRepo(t, w.upstream, "alpha.wiki", "master")
			w.mustRun("--clone-wiki")
			require.Equal(t, []string{"alpha", "alpha.wiki"}, repoDirs(t, w.out))

			w.removeRepo("alpha")
			out := w.mustRun(append([]string{"--clone-wiki"}, tt.flags...)...)

			assert.Equal(t, tt.want, repoDirs(t, w.out))
			assert.Contains(t, out, tt.says)
		})
	}
}

func TestRun_ARepoRemovedTwiceKeepsBothClonesInDeleted(t *testing.T) {
	w := newWorld(t)
	w.addRepo("alpha")
	w.addRepo("beta", func(r *ghapitest.Repo) { r.HasWiki = true })
	gittest.NewBareRepo(t, w.upstream, "beta.wiki", "master")
	beta := w.repos[1]
	w.mustRun("--clone-wiki")

	w.removeRepo("beta")
	w.mustRun("--clone-wiki")
	require.Equal(t, []string{"DELETED/beta", "DELETED/beta.wiki", "alpha"}, repoDirs(t, w.out))
	first := filepath.Join(w.out, "DELETED", "beta", "first.txt")
	require.NoError(t, os.WriteFile(first, []byte("first removal\n"), 0o644))

	// The repo comes back, is cloned again, and is removed again.
	w.repos = append(w.repos, beta)
	w.publish()
	w.mustRun("--clone-wiki")
	w.removeRepo("beta")
	out := w.mustRun("--clone-wiki")

	assert.FileExists(t, first, "the first removal's clone is kept")
	moved, err := filepath.Glob(filepath.Join(w.out, "DELETED", "beta.*"))
	require.NoError(t, err)
	var stamped []string
	for _, m := range moved {
		if filepath.Base(m) != "beta.wiki" {
			stamped = append(stamped, filepath.Base(m))
		}
	}
	require.Len(t, stamped, 2, "the second clone and its wiki get a stamped name: %v", moved)
	repo, wiki := stamped[0], stamped[1]
	assert.Regexp(t, `^beta\.\d{8}T\d{6}Z$`, repo)
	assert.Equal(t, repo+".wiki", wiki, "the wiki keeps its repo's name")
	assert.DirExists(t, filepath.Join(w.out, "DELETED", repo, ".git"))
	assert.NoDirExists(t, filepath.Join(w.out, "beta"))
	assert.Contains(t, out, "beta: moved (deleted)")
	assert.Contains(t, out, "exists; moved to")
}

func TestRun_AnArchivedRepoRemovedUpstreamLeavesArchived(t *testing.T) {
	tests := []struct {
		name  string
		flags []string
		want  []string
		says  []string
	}{
		{"moved to DELETED", nil, []string{"DELETED/alpha", "DELETED/alpha.wiki"},
			[]string{"alpha: moved (deleted)", "alpha.wiki: moved (deleted)"}},
		{"deleted under --delete", []string{"--delete"}, nil,
			[]string{"alpha: deleted", "alpha.wiki: deleted"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newWorld(t)
			w.addRepo("alpha", func(r *ghapitest.Repo) { r.Archived = true; r.HasWiki = true })
			gittest.NewBareRepo(t, w.upstream, "alpha.wiki", "master")
			w.mustRun("--clone-wiki")
			require.Equal(t, []string{"ARCHIVED/alpha", "ARCHIVED/alpha.wiki"}, repoDirs(t, w.out))

			w.removeRepo("alpha")
			out := w.mustRun(append([]string{"--clone-wiki"}, tt.flags...)...)

			assert.Equal(t, tt.want, repoDirs(t, w.out))
			for _, s := range tt.says {
				assert.Contains(t, out, s)
			}
		})
	}
}

func TestRun_ADryRunReportsARemovedArchivedRepoAndMovesNothing(t *testing.T) {
	w := newWorld(t)
	w.addRepo("alpha", func(r *ghapitest.Repo) { r.Archived = true })
	w.mustRun()
	w.removeRepo("alpha")
	before := snapshot(t, w.out)

	out := w.mustRun("--dry-run")

	assert.Equal(t, before, snapshot(t, w.out))
	assert.Contains(t, out, "alpha: moved (deleted) (dry-run)")
}

func TestRun_DeleteLeavesEarlierRemovalsInDeleted(t *testing.T) {
	w := newWorld(t)
	w.addRepo("alpha")
	w.addRepo("beta")
	w.mustRun()
	w.removeRepo("alpha")
	w.mustRun()
	require.Equal(t, []string{"DELETED/alpha", "beta"}, repoDirs(t, w.out))

	w.removeRepo("beta")
	out := w.mustRun("--delete")

	assert.Equal(t, []string{"DELETED/alpha"}, repoDirs(t, w.out), "only the repo removed since the last run is deleted")
	assert.Contains(t, out, "beta: deleted")
	assert.NotContains(t, out, "alpha: deleted")
}

func TestRun_AWikiLeftBehindByAnEarlierMoveFollowsItsRepo(t *testing.T) {
	tests := []struct {
		name     string
		archived bool
		// from and to are where the repo is moved by hand, as an older ghx
		// that didn't move wikis would have done, before the state flips.
		from, to string
		want     []string
		says     string
	}{
		{"archived", false, "alpha", "ARCHIVED/alpha",
			[]string{"ARCHIVED/alpha", "ARCHIVED/alpha.wiki"}, "alpha.wiki: moved (archived)"},
		{"unarchived", true, "ARCHIVED/alpha", "alpha",
			[]string{"alpha", "alpha.wiki"}, "alpha.wiki: moved (unarchived)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newWorld(t)
			w.addRepo("alpha", func(r *ghapitest.Repo) { r.Archived = tt.archived; r.HasWiki = true })
			gittest.NewBareRepo(t, w.upstream, "alpha.wiki", "master")
			w.mustRun("--clone-wiki")
			to := filepath.Join(w.out, filepath.FromSlash(tt.to))
			require.NoError(t, os.MkdirAll(filepath.Dir(to), 0o755))
			require.NoError(t, os.Rename(filepath.Join(w.out, filepath.FromSlash(tt.from)), to))
			w.setArchived("alpha", !tt.archived)

			out := w.mustRun("--clone-wiki")

			assert.Equal(t, tt.want, repoDirs(t, w.out), "one wiki, beside its repo")
			assert.Contains(t, out, tt.says)
		})
	}
}

func TestRun_AWikiStaysWithItsRepoInAConflict(t *testing.T) {
	w := newWorld(t)
	w.addRepo("alpha", func(r *ghapitest.Repo) { r.HasWiki = true })
	gittest.NewBareRepo(t, w.upstream, "alpha.wiki", "master")
	w.mustRun("--clone-wiki")
	gittest.Git(t, "", "clone", "--quiet", w.bare("alpha"), filepath.Join(w.out, "ARCHIVED", "alpha"))
	w.archiveRepo("alpha")

	out := w.mustRun("--clone-wiki")

	assert.Contains(t, out, "alpha: conflict")
	assert.DirExists(t, filepath.Join(w.out, "alpha.wiki", ".git"), "the wiki stays beside the repo left in place")
	assert.NotContains(t, out, "alpha.wiki: moved")
}

func TestRun_ARemovalThatFailedIsRetriedByTheNextRun(t *testing.T) {
	tests := []struct {
		name     string
		archived bool
		at       string
	}{
		{"active", false, "alpha"},
		{"archived", true, "ARCHIVED/alpha"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newWorld(t)
			w.addRepo("alpha", func(r *ghapitest.Repo) { r.Archived = tt.archived })
			w.addRepo("beta")
			w.mustRun()
			w.removeRepo("alpha")
			// A dangling symlink where DELETED/ belongs: the move's MkdirAll
			// fails, as an unwritable directory or a held file would.
			squatter := filepath.Join(w.out, "DELETED")
			if err := os.Symlink(filepath.Join(w.out, "nowhere"), squatter); err != nil {
				t.Skipf("can't make a symlink here: %v", err)
			}

			out, _ := w.run()

			assert.Contains(t, out, "alpha: failed")
			assert.DirExists(t, filepath.Join(w.out, filepath.FromSlash(tt.at), ".git"), "the clone stays where it was")
			assert.Equal(t, []string{"alpha", "beta"}, manifestNames(readManifest(t, w.out)), "the failed removal is kept for the next run")

			require.NoError(t, os.Remove(squatter))
			out = w.mustRun()

			assert.Equal(t, []string{"DELETED/alpha", "beta"}, repoDirs(t, w.out), "the next run finishes the move")
			assert.Contains(t, out, "alpha: moved (deleted)")
			assert.Equal(t, []string{"beta"}, manifestNames(readManifest(t, w.out)), "done, so no longer kept")
		})
	}
}

func TestRun_AWikiLeftBehindByARemovalFollowsItsRepo(t *testing.T) {
	w := newWorld(t)
	w.addRepo("alpha", func(r *ghapitest.Repo) { r.HasWiki = true })
	gittest.NewBareRepo(t, w.upstream, "alpha.wiki", "master")
	w.mustRun("--clone-wiki")
	w.removeRepo("alpha")
	// An earlier run moved the repo and failed on its wiki.
	require.NoError(t, os.MkdirAll(filepath.Join(w.out, "DELETED"), 0o755))
	require.NoError(t, os.Rename(filepath.Join(w.out, "alpha"), filepath.Join(w.out, "DELETED", "alpha")))

	out := w.mustRun("--clone-wiki")

	assert.Equal(t, []string{"DELETED/alpha", "DELETED/alpha.wiki"}, repoDirs(t, w.out))
	assert.Contains(t, out, "alpha.wiki: moved (deleted)")
}

func TestRun_ARemovedRepoWithAnOriginMismatchIsReportedOnce(t *testing.T) {
	w := newWorld(t)
	w.addRepo("alpha")
	w.addRepo("beta")
	w.mustRun()
	gittest.Git(t, filepath.Join(w.out, "alpha"), "remote", "set-url", "origin", w.bare("beta"))
	w.removeRepo("alpha")

	out := w.mustRun()

	assert.Contains(t, out, "alpha: skipped (origin mismatch)")
	assert.Equal(t, []string{"beta"}, manifestNames(readManifest(t, w.out)), "not ghx's clone, so not retried")

	out = w.mustRun()

	assert.NotContains(t, out, "alpha:")
	assert.DirExists(t, filepath.Join(w.out, "alpha", ".git"), "left alone")
}

func TestRun_AListedRepoNamedLikeAWikiIsNeverMovedAsOne(t *testing.T) {
	tests := []struct {
		name   string
		change func(w *world)
		want   []string
	}{
		{"alpha removed", func(w *world) { w.removeRepo("alpha") }, []string{"DELETED/alpha", "alpha.wiki"}},
		{"alpha archived", func(w *world) { w.archiveRepo("alpha") }, []string{"ARCHIVED/alpha", "alpha.wiki"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newWorld(t)
			w.addRepo("alpha")
			w.addRepo("alpha.wiki")
			w.mustRun()
			require.Equal(t, []string{"alpha", "alpha.wiki"}, repoDirs(t, w.out))
			marker := filepath.Join(w.out, "alpha.wiki", "mine.txt")
			require.NoError(t, os.WriteFile(marker, []byte("local work\n"), 0o644))
			tt.change(w)

			out := w.mustRun()

			assert.Equal(t, tt.want, repoDirs(t, w.out))
			assert.FileExists(t, marker, "the listed repo's clone stays, with its work")
			assert.NotContains(t, out, "alpha.wiki: moved")
		})
	}
}

func TestRun_ARemovalWhosePlaceCantBeCheckedIsKept(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory the test user can't traverse")
	}
	w := newWorld(t)
	w.addRepo("alpha", func(r *ghapitest.Repo) { r.Archived = true })
	w.addRepo("beta")
	w.mustRun()
	w.removeRepo("alpha")
	archived := filepath.Join(w.out, "ARCHIVED")
	require.NoError(t, os.Chmod(archived, 0o000))
	t.Cleanup(func() { _ = os.Chmod(archived, 0o755) })

	out, _ := w.run()

	assert.Contains(t, out, "alpha: failed")
	assert.Equal(t, []string{"alpha", "beta"}, manifestNames(readManifest(t, w.out)), "kept for the next run")

	require.NoError(t, os.Chmod(archived, 0o755))
	out = w.mustRun()

	assert.Equal(t, []string{"DELETED/alpha", "beta"}, repoDirs(t, w.out))
	assert.Contains(t, out, "alpha: moved (deleted)")
}

func TestRun_ARepoRenamedOnlyInCaseIsNotRemoved(t *testing.T) {
	w := newWorld(t)
	w.addRepo("alpha", func(r *ghapitest.Repo) { r.Archived = true })
	w.addRepo("beta")
	w.mustRun()
	require.Equal(t, []string{"ARCHIVED/alpha", "beta"}, repoDirs(t, w.out))
	w.repos[0].Name = "Alpha"
	w.publish()

	out := w.mustRun("--delete")

	assert.DirExists(t, filepath.Join(w.out, "ARCHIVED", "alpha", ".git"), "still the org's repo, so its clone stays")
	assert.NotContains(t, out, "alpha: deleted")
	assert.NotContains(t, out, "alpha: moved (deleted)")
}

func TestRun_ARemovalWithADeletedDirThatIsAFileFailsAndIsKept(t *testing.T) {
	w := newWorld(t)
	w.addRepo("alpha")
	w.addRepo("beta")
	w.mustRun()
	w.removeRepo("alpha")
	squatter := filepath.Join(w.out, "DELETED")
	require.NoError(t, os.WriteFile(squatter, []byte("not a directory\n"), 0o644))

	out, _ := w.run()

	assert.Contains(t, out, "alpha: failed", "reported, not retried under endless suffixes")
	assert.DirExists(t, filepath.Join(w.out, "alpha", ".git"))
	assert.Equal(t, []string{"alpha", "beta"}, manifestNames(readManifest(t, w.out)), "kept for the next run")

	require.NoError(t, os.Remove(squatter))
	out = w.mustRun()

	assert.Equal(t, []string{"DELETED/alpha", "beta"}, repoDirs(t, w.out))
	assert.Contains(t, out, "alpha: moved (deleted)")
}

// stampedIn lists the names in dir/ that are name.<stamp>, the shape a
// removal takes in a shared archived and deleted directory.
func stampedIn(t *testing.T, dir, name string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, name+".*"))
	require.NoError(t, err)
	var stamped []string
	for _, m := range matches {
		base := filepath.Base(m)
		if regexp.MustCompile(`^` + regexp.QuoteMeta(name) + `\.\d{8}T\d{6}Z(-\d+)?$`).MatchString(base) {
			stamped = append(stamped, base)
		}
	}
	return stamped
}

func TestRun_ASharedArchivedAndDeletedDirectoryKeepsBackupsApart(t *testing.T) {
	w := newWorld(t)
	w.addRepo("alpha")
	w.addRepo("beta", func(r *ghapitest.Repo) { r.Archived = true })
	w.addRepo("gamma")
	alpha := w.repos[0]
	flags := []string{"--archived-dir", "old", "--deleted-dir", "old"}
	run := func(extra ...string) string { return w.mustRun(append(slices.Clone(flags), extra...)...) }
	shared := filepath.Join(w.out, "old")

	run()
	require.Equal(t, []string{"alpha", "gamma", "old/beta"}, repoDirs(t, w.out))

	// Removed, active and archived alike: both take a stamped name, so a
	// plain name in old/ is only ever an archived clone.
	w.removeRepo("alpha")
	w.removeRepo("beta")
	out := run()
	require.Len(t, stampedIn(t, shared, "alpha"), 1, "alpha was moved in under a stamped name")
	require.Len(t, stampedIn(t, shared, "beta"), 1, "beta was renamed in place to a stamped name")
	assert.NoDirExists(t, filepath.Join(shared, "alpha"))
	assert.NoDirExists(t, filepath.Join(shared, "beta"))
	assert.Contains(t, out, "the archived and deleted directories are the same")
	backup := filepath.Join(shared, stampedIn(t, shared, "alpha")[0])
	marker := filepath.Join(backup, "first-life.txt")
	require.NoError(t, os.WriteFile(marker, []byte("kept\n"), 0o644))

	// The repo comes back and is archived: its clone moves to old/alpha,
	// beside the backup, which nothing touches.
	w.repos = append(w.repos, alpha)
	w.publish()
	run()
	w.archiveRepo("alpha")
	out = run()
	assert.DirExists(t, filepath.Join(shared, "alpha", ".git"))
	assert.Contains(t, out, "alpha: moved (archived)")
	assert.FileExists(t, marker, "the backup was never mistaken for the archived clone")

	// Removed again under --delete: the archived clone goes, the backup stays.
	w.removeRepo("alpha")
	out = run("--delete")
	assert.NoDirExists(t, filepath.Join(shared, "alpha"))
	assert.Contains(t, out, "alpha: deleted")
	assert.FileExists(t, marker, "--delete never touches an earlier removal")
	assert.Equal(t, []string{"gamma"}, slices.DeleteFunc(repoDirs(t, w.out), func(d string) bool {
		return strings.HasPrefix(d, "old/")
	}))
}

func TestRun_ASharedDirectoryStampsWikisWithTheirRepos(t *testing.T) {
	w := newWorld(t)
	w.addRepo("alpha", func(r *ghapitest.Repo) { r.Archived = true; r.HasWiki = true })
	w.addRepo("beta", func(r *ghapitest.Repo) { r.HasWiki = true })
	w.addRepo("gamma")
	gittest.NewBareRepo(t, w.upstream, "alpha.wiki", "master")
	gittest.NewBareRepo(t, w.upstream, "beta.wiki", "master")
	flags := []string{"--clone-wiki", "--archived-dir", "old", "--deleted-dir", "old"}
	shared := filepath.Join(w.out, "old")
	w.mustRun(flags...)
	require.Equal(t, []string{"beta", "beta.wiki", "gamma", "old/alpha", "old/alpha.wiki"}, repoDirs(t, w.out))
	// beta's repo went on an earlier run; its wiki was left in PATH.
	require.NoError(t, os.Rename(filepath.Join(w.out, "beta"), filepath.Join(shared, "beta.20200101T000000Z")))

	w.removeRepo("alpha")
	w.removeRepo("beta")
	out := w.mustRun(flags...)

	alpha := stampedIn(t, shared, "alpha")
	require.Len(t, alpha, 1)
	assert.DirExists(t, filepath.Join(shared, alpha[0]+".wiki", ".git"), "the wiki takes its repo's stamped name")
	assert.NoDirExists(t, filepath.Join(shared, "alpha.wiki"))
	assert.NoDirExists(t, filepath.Join(w.out, "beta.wiki"))
	wikis, err := filepath.Glob(filepath.Join(shared, "beta.*.wiki"))
	require.NoError(t, err)
	assert.Len(t, wikis, 1, "a wiki retired on its own is stamped too: %v", wikis)
	assert.Contains(t, out, "beta.wiki: moved (deleted)")
}

func TestRun_ASharedDirectoryStampsAnUnstampedBackupFromAnEarlierGhx(t *testing.T) {
	w := newWorld(t)
	w.addRepo("alpha")
	w.addRepo("beta", func(r *ghapitest.Repo) { r.Archived = true })
	flags := []string{"--archived-dir", "old", "--deleted-dir", "old"}
	shared := filepath.Join(w.out, "old")
	w.mustRun(flags...)
	// An earlier ghx removed alpha into old/ under its plain name, and the
	// org has since recreated it: alpha is listed and its manifest entry
	// isn't archived.
	require.NoError(t, os.Rename(filepath.Join(w.out, "alpha"), filepath.Join(shared, "alpha")))
	marker := filepath.Join(shared, "alpha", "first-life.txt")
	require.NoError(t, os.WriteFile(marker, []byte("kept\n"), 0o644))

	out := w.mustRun(flags...)

	stamped := stampedIn(t, shared, "alpha")
	require.Len(t, stamped, 1, "the old backup took a stamped name")
	assert.FileExists(t, filepath.Join(shared, stamped[0], "first-life.txt"))
	assert.Contains(t, out, "not archived on the last run")
	assert.NotContains(t, out, "alpha: moved (unarchived)", "the backup wasn't taken for an archived clone")
	assert.DirExists(t, filepath.Join(w.out, "alpha", ".git"), "the recreated repo got a fresh clone")
	assert.DirExists(t, filepath.Join(shared, "beta", ".git"), "an archived clone the manifest lists is left alone")

	// Removed under --delete: the fresh clone goes, the backup stays.
	w.removeRepo("alpha")
	w.mustRun(append(flags, "--delete")...)
	assert.FileExists(t, filepath.Join(shared, stamped[0], "first-life.txt"))
}

func TestRun_ADryRunNamesTwoClonesOfOneRemovalApart(t *testing.T) {
	w := newWorld(t)
	w.addRepo("alpha")
	w.addRepo("beta")
	w.mustRun()
	// A second copy in ARCHIVED/, as a conflict an earlier run left.
	gittest.Git(t, "", "clone", "--quiet", w.bare("alpha"), filepath.Join(w.out, "ARCHIVED", "alpha"))
	w.removeRepo("alpha")

	out := w.mustRun("--dry-run")

	// The first copy takes DELETED/alpha; the second needs a stamped name,
	// as a real run would give it, though the dry run moved nothing.
	dests := regexp.MustCompile(`moved to (\S+?)[;)]`).FindAllStringSubmatch(out, -1)
	require.Len(t, dests, 1, "output:\n%s", out)
	assert.Regexp(t, `alpha\.\d{8}T\d{6}Z$`, dests[0][1])
}

// legacyWorld syncs alpha and beta with one directory for archived and
// deleted repos, then moves alpha's clone into it under its plain name, as an
// earlier ghx that didn't stamp would have left a removal: alpha is still
// listed, as if the org recreated it.
func legacyWorld(t *testing.T, name string) (w *world, flags []string, legacy string) {
	t.Helper()
	w = newWorld(t)
	w.addRepo(name)
	w.addRepo("beta")
	flags = []string{"--archived-dir", "old", "--deleted-dir", "old"}
	w.mustRun(flags...)
	legacy = filepath.Join(w.out, "old", name)
	require.NoError(t, os.MkdirAll(filepath.Dir(legacy), 0o755))
	require.NoError(t, os.Rename(filepath.Join(w.out, name), legacy))
	require.NoError(t, os.WriteFile(filepath.Join(legacy, "first-life.txt"), []byte("kept\n"), 0o644))
	return w, flags, legacy
}

func TestRun_ALegacyBackupWithAStampShapedRepoNameIsStamped(t *testing.T) {
	name := "alpha.20200101T000000Z"
	w, flags, _ := legacyWorld(t, name)

	out := w.mustRun(flags...)

	stamped := stampedIn(t, filepath.Join(w.out, "old"), name)
	require.Len(t, stamped, 1, "output:\n%s", out)
	assert.FileExists(t, filepath.Join(w.out, "old", stamped[0], "first-life.txt"))
	assert.NotContains(t, out, "moved (unarchived)")
	assert.NoFileExists(t, filepath.Join(w.out, name, "first-life.txt"), "the live clone is fresh")
}

func TestRun_ALegacyRenameThatFailsStopsTheRun(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory the test user can't write")
	}
	w, flags, legacy := legacyWorld(t, "alpha")
	shared := filepath.Dir(legacy)
	manifestPath := filepath.Join(w.out, config.ManifestFilename)
	before, err := os.ReadFile(manifestPath)
	require.NoError(t, err)
	require.NoError(t, os.Chmod(shared, 0o555))
	t.Cleanup(func() { _ = os.Chmod(shared, 0o755) })

	out, err := w.run(flags...)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "nothing was synced")
	assert.Contains(t, out, "alpha: failed")
	assert.NoDirExists(t, filepath.Join(w.out, "alpha"), "no fresh clone, no sync")
	after, err := os.ReadFile(manifestPath)
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after), "the manifest isn't rewritten, so the next run retries")

	require.NoError(t, os.Chmod(shared, 0o755))
	w.mustRun(flags...)
	assert.Len(t, stampedIn(t, shared, "alpha"), 1)
}

func TestRun_AnEmptyManifestStillStampsLegacyBackups(t *testing.T) {
	w, flags, legacy := legacyWorld(t, "alpha")
	m := `{"org":"acme","exported_at":"2025-01-01T00:00:00+00:00","repos":[]}` + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(w.out, config.ManifestFilename), []byte(m), 0o644))

	out := w.mustRun(flags...)

	assert.Len(t, stampedIn(t, filepath.Dir(legacy), "alpha"), 1, "output:\n%s", out)
	assert.NotContains(t, out, "alpha: moved (unarchived)")
}

func TestRun_ADryRunSeesALegacyBackupAsStamped(t *testing.T) {
	tests := []struct {
		name   string
		change func(w *world)
		flags  []string
		never  string
	}{
		{"recreated", func(*world) {}, nil, "alpha: moved (unarchived)"},
		{"removed under --delete", func(w *world) { w.removeRepo("alpha") }, []string{"--delete"}, "alpha: deleted"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, flags, _ := legacyWorld(t, "alpha")
			tt.change(w)
			before := snapshot(t, w.out)

			out := w.mustRun(append(append(flags, "--dry-run"), tt.flags...)...)

			assert.Equal(t, before, snapshot(t, w.out))
			assert.Contains(t, out, "not archived on the last run")
			assert.NotContains(t, out, tt.never, "the preview matches what a real run would do")
		})
	}
}

func TestRun_ASharedDirectoryThatCantBeListedStopsTheRun(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory the test user can enter but not list")
	}
	w, flags, legacy := legacyWorld(t, "alpha")
	shared := filepath.Dir(legacy)
	require.NoError(t, os.Chmod(shared, 0o311))
	t.Cleanup(func() { _ = os.Chmod(shared, 0o755) })

	out, err := w.run(flags...)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "nothing was synced")
	assert.Contains(t, out, ": failed")
	assert.NoDirExists(t, filepath.Join(w.out, "alpha"), "the backup wasn't moved out or synced over")
}

func TestRun_ALegacyBackupOfARepoNamedLikeAWikiIsStamped(t *testing.T) {
	w := newWorld(t)
	w.addRepo("alpha", func(r *ghapitest.Repo) { r.Archived = true })
	w.addRepo("alpha.wiki")
	flags := []string{"--archived-dir", "old", "--deleted-dir", "old"}
	shared := filepath.Join(w.out, "old")
	w.mustRun(flags...)
	require.Equal(t, []string{"alpha.wiki", "old/alpha"}, repoDirs(t, w.out))
	// An earlier ghx left the repo alpha.wiki's removal unstamped, and it
	// has since been recreated.
	legacy := filepath.Join(shared, "alpha.wiki")
	require.NoError(t, os.Rename(filepath.Join(w.out, "alpha.wiki"), legacy))
	require.NoError(t, os.WriteFile(filepath.Join(legacy, "first-life.txt"), []byte("kept\n"), 0o644))

	out := w.mustRun(flags...)

	stamped := stampedIn(t, shared, "alpha.wiki")
	require.Len(t, stamped, 1, "output:\n%s", out)
	assert.FileExists(t, filepath.Join(shared, stamped[0], "first-life.txt"))
	assert.NoFileExists(t, filepath.Join(w.out, "alpha.wiki", "first-life.txt"), "the live clone is fresh")
	assert.DirExists(t, filepath.Join(shared, "alpha", ".git"), "archived alpha is untouched")
}

func TestRun_LegacyStampingScansTheArchivedDirWhenNamesDifferByCase(t *testing.T) {
	probe := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(probe, "case"), nil, 0o644))
	if _, err := os.Stat(filepath.Join(probe, "CASE")); err == nil {
		t.Skip("needs a case-sensitive file system, where OLD and old are two directories")
	}
	w := newWorld(t)
	w.addRepo("alpha")
	w.addRepo("beta")
	flags := []string{"--archived-dir", "OLD", "--deleted-dir", "old"}
	w.mustRun(flags...)
	// An earlier setup pointed both flags at OLD and left alpha's removal
	// there unstamped; alpha has since been recreated.
	archived := filepath.Join(w.out, "OLD")
	require.NoError(t, os.MkdirAll(archived, 0o755))
	require.NoError(t, os.Rename(filepath.Join(w.out, "alpha"), filepath.Join(archived, "alpha")))

	out := w.mustRun(flags...)

	assert.Len(t, stampedIn(t, archived, "alpha"), 1, "output:\n%s", out)
	assert.NotContains(t, out, "alpha: moved (unarchived)")
}
