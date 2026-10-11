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

func TestRun_FirstSyncClonesEveryRepoAndWritesTheManifest(t *testing.T) {
	w := newWorld(t)
	w.addRepo("alpha")
	w.addRepo("beta")

	out := w.mustRun()

	assert.Equal(t, []string{"alpha", "beta"}, repoDirs(t, w.out))
	assert.FileExists(t, filepath.Join(w.out, "alpha", "README.md"))
	assert.FileExists(t, filepath.Join(w.out, "beta", "README.md"))

	m := readManifest(t, w.out)
	assert.Equal(t, testOrg, m.Org)
	assert.Equal(t, []string{"alpha", "beta"}, manifestNames(m))
	for _, r := range m.Repos {
		assert.Equal(t, "main", r.DefaultBranch, r.Name)
		assert.False(t, r.Archived, r.Name)
	}

	assert.Contains(t, out, "Authenticated as octocat")
	assert.Contains(t, out, "Found 2 repos")
}

func TestRun_SecondSyncPullsNewCommits(t *testing.T) {
	w := newWorld(t)
	w.addRepo("alpha")
	w.addRepo("beta")
	w.mustRun()

	w.pushCommit("alpha", "NEWS.md")
	w.mustRun()

	assert.FileExists(t, filepath.Join(w.out, "alpha", "NEWS.md"), "the second sync pulled the new commit")
	assert.Equal(t, []string{"alpha", "beta"}, repoDirs(t, w.out))
}

func TestRun_RepoRemovedUpstreamMovesToDeleted(t *testing.T) {
	w := newWorld(t)
	w.addRepo("alpha")
	w.addRepo("beta")
	w.mustRun()

	w.removeRepo("beta")
	out := w.mustRun()

	assert.Equal(t, []string{"DELETED/beta", "alpha"}, repoDirs(t, w.out))
	assert.FileExists(t, filepath.Join(w.out, "DELETED", "beta", "README.md"), "the clone moved intact")
	assert.Equal(t, []string{"alpha"}, manifestNames(readManifest(t, w.out)))
	assert.Contains(t, out, "beta: moved (deleted)")
}

func TestRun_ArchivedRepoMovesToArchived(t *testing.T) {
	w := newWorld(t)
	w.addRepo("alpha")
	w.addRepo("beta")
	w.mustRun()

	w.archiveRepo("beta")
	out := w.mustRun()

	assert.Equal(t, []string{"ARCHIVED/beta", "alpha"}, repoDirs(t, w.out))
	assert.Contains(t, out, "beta: moved (archived)")

	m := readManifest(t, w.out)
	assert.Equal(t, []string{"alpha", "beta"}, manifestNames(m))
	for _, r := range m.Repos {
		assert.Equal(t, r.Name == "beta", r.Archived, r.Name)
	}

	// A later sync keeps updating the archived clone where it now lives.
	w.pushCommit("beta", "LAST.md")
	w.mustRun()
	assert.FileExists(t, filepath.Join(w.out, "ARCHIVED", "beta", "LAST.md"))
}

func TestRun_ArchivedRepoOnAFirstSyncIsClonedIntoArchived(t *testing.T) {
	w := newWorld(t)
	w.addRepo("alpha")
	w.addRepo("old", func(r *ghapitest.Repo) { r.Archived = true })

	out := w.mustRun()

	assert.Equal(t, []string{"ARCHIVED/old", "alpha"}, repoDirs(t, w.out))
	assert.Contains(t, out, "Cloning 1 archived repo(s) into ARCHIVED/")
}

func TestRun_CustomDirectoriesReplaceDeletedAndArchived(t *testing.T) {
	w := newWorld(t)
	w.addRepo("alpha")
	w.addRepo("beta")
	w.addRepo("gamma")
	w.mustRun()

	w.removeRepo("beta")
	w.archiveRepo("gamma")
	w.mustRun("--deleted-dir", "GONE", "--archived-dir", "COLD")

	assert.Equal(t, []string{"COLD/gamma", "GONE/beta", "alpha"}, repoDirs(t, w.out))
}

func TestRun_DeleteRemovesRemovedAndArchivedRepos(t *testing.T) {
	w := newWorld(t)
	w.addRepo("alpha")
	w.addRepo("beta")
	w.addRepo("gamma")
	w.mustRun()

	w.removeRepo("beta")
	w.archiveRepo("gamma")
	out := w.mustRun("--delete")

	assert.Equal(t, []string{"alpha"}, repoDirs(t, w.out))
	assert.NoDirExists(t, filepath.Join(w.out, "DELETED"))
	assert.NoDirExists(t, filepath.Join(w.out, "ARCHIVED"))
	assert.Contains(t, out, "beta: deleted")
	assert.Contains(t, out, "gamma: deleted")
	assert.Contains(t, out, "Skipping 1 archived repo(s) (--delete)")
}

func TestRun_DryRunChangesNothingOnDisk(t *testing.T) {
	w := newWorld(t)
	w.addRepo("alpha")
	w.addRepo("beta")
	w.addRepo("gamma")
	w.mustRun()

	// Everything a real sync would act on: a removal, an archive, a new repo
	// and a new upstream commit.
	w.removeRepo("beta")
	w.archiveRepo("gamma")
	w.addRepo("delta")
	w.pushCommit("alpha", "NEWS.md")
	before := snapshot(t, w.out)

	out := w.mustRun("--dry-run")

	assert.Equal(t, before, snapshot(t, w.out), "a dry run must not touch the mirror")
	assert.Contains(t, out, "beta: moved (deleted)")
	assert.Contains(t, out, "gamma: moved (archived)")
	assert.Contains(t, out, "Dry run complete")
}

func TestRun_DryRunOnAFreshDirectoryClonesNothing(t *testing.T) {
	w := newWorld(t)
	w.addRepo("alpha")

	out := w.mustRun("--dry-run")

	assert.Empty(t, repoDirs(t, w.out))
	assert.NoFileExists(t, filepath.Join(w.out, config.ManifestFilename))
	assert.Contains(t, out, "Dry run complete")
}

func TestRun_DryRunWithDeleteReportsDeletionsButKeepsTheClones(t *testing.T) {
	w := newWorld(t)
	w.addRepo("alpha")
	w.addRepo("beta")
	w.mustRun()
	w.removeRepo("beta")

	out := w.mustRun("--dry-run", "--delete")

	assert.Equal(t, []string{"alpha", "beta"}, repoDirs(t, w.out))
	assert.Contains(t, out, "beta: deleted")
}

func TestRun_IncludeAndExcludeSelectWhichReposAreSynced(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{"include keeps only matches", []string{"--include", "^(alpha|beta)$"}, []string{"alpha", "beta"}},
		{"exclude drops matches", []string{"--exclude", "^gamma$"}, []string{"alpha", "beta"}},
		{"exclude wins over include", []string{"--include", "^(alpha|beta)$", "--exclude", "beta"}, []string{"alpha"}},
		{"a pattern matching nothing syncs nothing", []string{"--include", "^zzz$"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newWorld(t)
			w.addRepo("alpha")
			w.addRepo("beta")
			w.addRepo("gamma")

			w.mustRun(tt.args...)

			assert.Equal(t, tt.want, repoDirs(t, w.out))
		})
	}
}

func TestRun_TypeFilterSyncsOnlyMatchingRepos(t *testing.T) {
	w := newWorld(t)
	w.addRepo("open")
	w.addRepo("secret", func(r *ghapitest.Repo) { r.Private = true })

	w.mustRun("--type", "private")

	assert.Equal(t, []string{"secret"}, repoDirs(t, w.out))
}

func TestRun_MaxSizeSkipsLargeRepos(t *testing.T) {
	w := newWorld(t)
	w.addRepo("small", func(r *ghapitest.Repo) { r.Size = 100 })
	w.addRepo("huge", func(r *ghapitest.Repo) { r.Size = 5 * 1024 })

	out := w.mustRun("--max-size", "1")

	assert.Equal(t, []string{"small"}, repoDirs(t, w.out))
	assert.Contains(t, out, "huge: skipped (too large)")
}

func TestRun_SSHFlagClonesFromTheSSHURL(t *testing.T) {
	w := newWorld(t)
	w.addRepo("alpha", func(r *ghapitest.Repo) {
		// Only the ssh URL works: --ssh must be the one that picks it.
		r.CloneURL = filepath.Join(w.upstream, "no-such-repo.git")
	})

	w.mustRun("--ssh")

	assert.Equal(t, []string{"alpha"}, repoDirs(t, w.out))
}

func TestRun_CloneWikiClonesTheWikiBesideTheRepo(t *testing.T) {
	w := newWorld(t)
	w.addRepo("alpha", func(r *ghapitest.Repo) { r.HasWiki = true })
	gittest.NewBareRepo(t, w.upstream, "alpha.wiki", "master")

	w.mustRun("--clone-wiki")

	assert.Equal(t, []string{"alpha", "alpha.wiki"}, repoDirs(t, w.out))
}

func TestRun_GitIdentityFlagsReachTheClones(t *testing.T) {
	w := newWorld(t)
	w.addRepo("alpha")

	w.mustRun("--git-author", "Ada Lovelace", "--git-email", "ada@example.com", "--concurrency", "2")

	dir := filepath.Join(w.out, "alpha")
	assert.Equal(t, "Ada Lovelace\n", gittest.Git(t, dir, "config", "--local", "user.name"))
	assert.Equal(t, "ada@example.com\n", gittest.Git(t, dir, "config", "--local", "user.email"))
}

func TestRun_TokenSources(t *testing.T) {
	t.Run("the flag beats the environment", func(t *testing.T) {
		w := newWorld(t)
		w.addRepo("alpha")
		t.Setenv("GITHUB_TOKEN", "ghp_wrong")

		out := w.mustRun("--token", testToken)

		assert.Equal(t, []string{"alpha"}, repoDirs(t, w.out))
		assert.Contains(t, out, "flag", "the settings table names the source")
	})

	t.Run("a local .ghx.toml supplies the token and options", func(t *testing.T) {
		w := newWorld(t)
		w.addRepo("alpha")
		w.addRepo("beta")
		t.Setenv("GITHUB_TOKEN", "")
		require.NoError(t, os.MkdirAll(w.out, 0o755))
		cfg := "token = \"" + testToken + "\"\ninclude = \"^beta$\"\n"
		require.NoError(t, os.WriteFile(filepath.Join(w.out, config.ConfigFilename), []byte(cfg), 0o600))

		w.mustRun()

		assert.Equal(t, []string{"beta"}, repoDirs(t, w.out))
	})

	t.Run("a missing token explains where to put one", func(t *testing.T) {
		w := newWorld(t)
		w.addRepo("alpha")
		t.Setenv("GITHUB_TOKEN", "")

		_, err := w.run()

		require.Error(t, err)
		assert.Contains(t, err.Error(), "No GitHub token provided")
		assert.Empty(t, repoDirs(t, w.out))
	})
}

func TestRun_Failures(t *testing.T) {
	t.Run("a rejected token stops before touching the mirror", func(t *testing.T) {
		w := newWorld(t)
		w.addRepo("alpha")

		_, err := w.run("--token", "ghp_wrong")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "Token validation failed (401)")
		assert.Empty(t, repoDirs(t, w.out))
		assert.NoFileExists(t, filepath.Join(w.out, config.ManifestFilename))
	})

	t.Run("an unknown org is reported by name", func(t *testing.T) {
		w := newWorld(t)

		_, err := runCLI(t, "ghost-org", w.out)

		require.Error(t, err)
		assert.Contains(t, err.Error(), `Failed to access organization "ghost-org" (404)`)
	})

	t.Run("an invalid include pattern is rejected", func(t *testing.T) {
		w := newWorld(t)
		w.addRepo("alpha")

		_, err := w.run("--include", "(")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid --include regex")
		assert.Empty(t, repoDirs(t, w.out))
	})

	t.Run("the org and path are both required", func(t *testing.T) {
		newWorld(t)

		_, err := runCLI(t, testOrg)

		require.Error(t, err)
	})

	t.Run("an output path under a file cannot be created", func(t *testing.T) {
		w := newWorld(t)
		blocker := filepath.Join(t.TempDir(), "file")
		require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o644))

		_, err := runCLI(t, testOrg, filepath.Join(blocker, "mirror"))

		require.Error(t, err)
		assert.Contains(t, err.Error(), "creating output directory")
		assert.Empty(t, w.api.Requests(), "nothing was asked of GitHub")
	})
}

func TestRun_ProtocolFlagSelectsSSH(t *testing.T) {
	w := newWorld(t)
	w.addRepo("alpha", func(r *ghapitest.Repo) {
		r.CloneURL = filepath.Join(w.upstream, "no-such-repo.git")
	})

	w.mustRun("--protocol", "ssh")

	assert.Equal(t, []string{"alpha"}, repoDirs(t, w.out))
}

func TestRun_LegacyFilesAreMigratedInPlace(t *testing.T) {
	w := newWorld(t)
	w.addRepo("alpha")
	require.NoError(t, os.MkdirAll(w.out, 0o755))
	// The previous run knew a repo that is gone upstream and was never cloned
	// here, so there is nothing to move.
	legacyManifest := `{"org":"acme","exported_at":"2025-01-01T00:00:00+00:00","repos":[` +
		`{"name":"alpha","default_branch":"main","archived":false,"has_wiki":false},` +
		`{"name":"vanished","default_branch":"main","archived":false,"has_wiki":false}]}` + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(w.out, config.LegacyManifestFilename), []byte(legacyManifest), 0o644))
	legacyConfig := "include = \"^alpha$\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(w.out, config.LegacyConfigFilename), []byte(legacyConfig), 0o644))

	out := w.mustRun()

	assert.Contains(t, out, "Renamed legacy")
	assert.NoFileExists(t, filepath.Join(w.out, config.LegacyManifestFilename))
	assert.NoFileExists(t, filepath.Join(w.out, config.LegacyConfigFilename))
	assert.FileExists(t, filepath.Join(w.out, config.ConfigFilename))
	assert.Equal(t, []string{"alpha"}, manifestNames(readManifest(t, w.out)))
	assert.Equal(t, []string{"alpha"}, repoDirs(t, w.out))
	assert.NoDirExists(t, filepath.Join(w.out, "DELETED"))
}

func TestRun_AnUnreadableLocalConfigStopsTheRun(t *testing.T) {
	w := newWorld(t)
	w.addRepo("alpha")
	require.NoError(t, os.MkdirAll(w.out, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(w.out, config.ConfigFilename), []byte("token = [unclosed\n"), 0o600))

	_, err := w.run()

	require.Error(t, err)
	assert.Empty(t, repoDirs(t, w.out))
	assert.Empty(t, w.api.Requests(), "nothing was asked of GitHub")
}
