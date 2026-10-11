package manifest_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lolay/ghx/internal/config"
	"github.com/lolay/ghx/internal/ghapi"
	"github.com/lolay/ghx/internal/manifest"
)

// goldenDir holds a .ghx.json written by the Python implementation's own
// code path (json.dump(manifest, f, indent=2) plus a trailing newline), so
// the golden test pins byte compatibility with what Python wrote.
var goldenDir = filepath.Join("testdata", "golden")

// goldenRepos are the repos the golden manifest was written from. Fields the
// manifest doesn't store are set so the test shows they are dropped.
var goldenRepos = []ghapi.RepoInfo{
	{Name: "ghx", DefaultBranch: "main", HasWiki: true, Private: true, SizeKB: 2048, Language: "Go"},
	{Name: "old-tool", DefaultBranch: "master", Archived: true, Fork: true, CloneURL: "https://example.invalid/old-tool.git"},
}

func readManifestFile(t *testing.T, dir string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, config.ManifestFilename))
	require.NoError(t, err)
	return data
}

func exportedAt(t *testing.T, data []byte) string {
	t.Helper()
	var m manifest.Manifest
	require.NoError(t, json.Unmarshal(data, &m))
	return m.ExportedAt
}

func TestWrite_MatchesPythonGolden(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, manifest.Write(dir, "lolay", goldenRepos))

	golden := readManifestFile(t, goldenDir)
	got := readManifestFile(t, dir)
	// exported_at is the one field that differs by design: Go writes RFC 3339
	// seconds with "Z", Python wrote isoformat() with microseconds and
	// "+00:00". Swap in the golden value, then compare every byte.
	got = bytes.Replace(got, []byte(exportedAt(t, got)), []byte(exportedAt(t, golden)), 1)
	assert.Equal(t, string(golden), string(got))
}

func TestWrite_StampsExportedAtInUTC(t *testing.T) {
	dir := t.TempDir()
	before := time.Now().UTC().Truncate(time.Second)

	require.NoError(t, manifest.Write(dir, "lolay", nil))

	after := time.Now().UTC()
	stamp := exportedAt(t, readManifestFile(t, dir))
	parsed, err := time.Parse(time.RFC3339, stamp)
	require.NoError(t, err)
	assert.Equal(t, time.UTC, parsed.Location(), "exported_at %q is not UTC", stamp)
	assert.False(t, parsed.Before(before), "exported_at %s before %s", parsed, before)
	assert.False(t, parsed.After(after), "exported_at %s after %s", parsed, after)
}

func TestWrite_NoReposWritesEmptyArray(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, manifest.Write(dir, "lolay", nil))

	assert.Contains(t, string(readManifestFile(t, dir)), `"repos": []`)
}

func TestWrite_ReturnsErrorWhenDirectoryIsMissing(t *testing.T) {
	err := manifest.Write(filepath.Join(t.TempDir(), "missing"), "lolay", goldenRepos)
	require.Error(t, err)
}

func TestRead_PythonGolden(t *testing.T) {
	m, msg, err := manifest.Read(goldenDir)
	require.NoError(t, err)

	assert.Empty(t, msg)
	assert.Equal(t, &manifest.Manifest{
		Org:        "lolay",
		ExportedAt: "2026-04-01T09:30:00.123456+00:00",
		Repos: []manifest.Repo{
			{Name: "ghx", DefaultBranch: "main", HasWiki: true},
			{Name: "old-tool", DefaultBranch: "master", Archived: true},
		},
	}, m)
}

func TestWriteRead_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, manifest.Write(dir, "lolay", goldenRepos))

	m, msg, err := manifest.Read(dir)
	require.NoError(t, err)

	assert.Empty(t, msg)
	require.NotNil(t, m)
	assert.Equal(t, "lolay", m.Org)
	assert.NotEmpty(t, m.ExportedAt)
	assert.Equal(t, []manifest.Repo{
		{Name: "ghx", DefaultBranch: "main", HasWiki: true},
		{Name: "old-tool", DefaultBranch: "master", Archived: true},
	}, m.Repos)
}

func TestRead_MissingFileReturnsNil(t *testing.T) {
	m, msg, err := manifest.Read(t.TempDir())
	require.NoError(t, err)
	assert.Nil(t, m)
	assert.Empty(t, msg)
}

func TestRead_MigratesLegacyManifest(t *testing.T) {
	dir := t.TempDir()
	legacy := []byte(`{"org": "lolay", "exported_at": "x", "repos": [{"name": "a", "default_branch": "main", "archived": false, "has_wiki": false}]}`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, config.LegacyManifestFilename), legacy, 0o600))

	m, msg, err := manifest.Read(dir)
	require.NoError(t, err)

	assert.Contains(t, msg, "Renamed legacy")
	require.NotNil(t, m)
	assert.Equal(t, []manifest.Repo{{Name: "a", DefaultBranch: "main"}}, m.Repos)
	assert.NoFileExists(t, filepath.Join(dir, config.LegacyManifestFilename))
	assert.FileExists(t, filepath.Join(dir, config.ManifestFilename))
}

func TestRead_BothManifestsExistKeepsLegacyAndReadsNew(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, config.LegacyManifestFilename), []byte(`{"org": "old"}`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, config.ManifestFilename), []byte(`{"org": "new"}`), 0o600))

	m, msg, err := manifest.Read(dir)
	require.NoError(t, err)

	assert.Contains(t, msg, "already exists; leaving legacy file in place")
	require.NotNil(t, m)
	assert.Equal(t, "new", m.Org)
	assert.FileExists(t, filepath.Join(dir, config.LegacyManifestFilename))
}

func TestRead_Errors(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T, path string)
		wantErr string
	}{
		{
			name: "invalid JSON",
			setup: func(t *testing.T, path string) {
				require.NoError(t, os.WriteFile(path, []byte("{not json"), 0o600))
			},
			wantErr: "parsing",
		},
		{
			name: "manifest path is a directory",
			setup: func(t *testing.T, path string) {
				require.NoError(t, os.Mkdir(path, 0o755))
			},
			wantErr: "reading",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, config.ManifestFilename)
			tt.setup(t, path)

			m, _, err := manifest.Read(dir)

			require.ErrorContains(t, err, tt.wantErr)
			assert.ErrorContains(t, err, path)
			assert.Nil(t, m)
		})
	}
}
