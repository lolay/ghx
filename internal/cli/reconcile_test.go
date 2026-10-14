package cli_test

import (
	"os"
	"path/filepath"
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
	w := newWorld(t)
	w.addRepo("alpha")
	w.addRepo("beta")
	w.mustRun()
	// Work in the active clone that a move onto ARCHIVED/beta would keep, and
	// a copy already in ARCHIVED/ that a replace would lose.
	local := filepath.Join(w.out, "beta", "notes.txt")
	require.NoError(t, os.WriteFile(local, []byte("mine\n"), 0o644))
	archived := filepath.Join(w.out, "ARCHIVED", "beta")
	gittest.Git(t, "", "clone", "--quiet", w.bare("beta"), archived)
	kept := filepath.Join(archived, "kept.txt")
	require.NoError(t, os.WriteFile(kept, []byte("also mine\n"), 0o644))
	w.archiveRepo("beta")

	out := w.mustRun()

	assert.Equal(t, []string{"ARCHIVED/beta", "alpha", "beta"}, repoDirs(t, w.out))
	assert.FileExists(t, local)
	assert.FileExists(t, kept)
	assert.Contains(t, out, "beta: conflict")
	assert.Contains(t, out, "Conflicts (both copies left in place):")
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
