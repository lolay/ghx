package cli_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lolay/ghx/internal/config"
)

func TestRun_MoveDirectoriesMustStayInsideTheOutputDirectory(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"a parent deleted-dir", []string{"--deleted-dir", ".."}},
		{"a sibling deleted-dir", []string{"--deleted-dir", filepath.Join("..", "removed")}},
		{"the output directory itself", []string{"--archived-dir", "."}},
		{"an empty archived-dir", []string{"--archived-dir", ""}},
		{"an absolute archived-dir", []string{"--archived-dir", filepath.Join(t.TempDir(), "cold")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newWorld(t)
			w.addRepo("alpha")

			_, err := w.run(tt.args...)

			require.Error(t, err)
			assert.Contains(t, err.Error(), "must be a directory inside the output directory")
			assert.Empty(t, repoDirs(t, w.out))
			assert.Empty(t, w.api.Requests(), "nothing was asked of GitHub")
		})
	}
}

func TestRun_NestedMoveDirectoriesAreAllowed(t *testing.T) {
	w := newWorld(t)
	w.addRepo("alpha")
	w.addRepo("beta")
	w.mustRun()
	w.removeRepo("beta")

	w.mustRun("--deleted-dir", filepath.Join("old", "removed"))

	assert.Equal(t, []string{"alpha", "old/removed/beta"}, repoDirs(t, w.out))
}

func TestRun_AManifestNameOutsideTheOutputDirectoryIsNeverTouched(t *testing.T) {
	w := newWorld(t)
	w.addRepo("alpha")
	// A directory beside the mirror that an edited manifest points at.
	victim := filepath.Join(filepath.Dir(w.out), "victim")
	require.NoError(t, os.MkdirAll(filepath.Join(victim, ".git"), 0o755))
	require.NoError(t, os.MkdirAll(w.out, 0o755))
	m := `{"org":"acme","exported_at":"2025-01-01T00:00:00+00:00","repos":[` +
		`{"name":"alpha","default_branch":"main","archived":false,"has_wiki":false},` +
		`{"name":"../victim","default_branch":"main","archived":false,"has_wiki":false}]}` + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(w.out, config.ManifestFilename), []byte(m), 0o644))

	out := w.mustRun("--delete")

	assert.DirExists(t, filepath.Join(victim, ".git"), "a name from the manifest can't reach outside the mirror")
	assert.Contains(t, out, "../victim: skipped (unusable name)")
	assert.Equal(t, []string{"alpha"}, repoDirs(t, w.out))
}

func TestRun_WithoutGit(t *testing.T) {
	t.Run("a sync stops before asking GitHub", func(t *testing.T) {
		w := newWorld(t)
		w.addRepo("alpha")
		t.Setenv("PATH", t.TempDir())

		_, err := w.run()

		require.Error(t, err)
		assert.Contains(t, err.Error(), "git not found on PATH")
		assert.Empty(t, w.api.Requests(), "nothing was asked of GitHub")
	})

	t.Run("a dry run needs no git", func(t *testing.T) {
		w := newWorld(t)
		w.addRepo("alpha")
		t.Setenv("PATH", t.TempDir())

		out := w.mustRun("--dry-run")

		assert.Contains(t, out, "Dry run complete")
	})
}
