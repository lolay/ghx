package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lolay/ghx/internal/config"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

func TestLoad_ReadsHomeAndLocalAndNormalizesKeys(t *testing.T) {
	home := isolateHome(t)
	local := t.TempDir()
	writeFile(t, home, config.ConfigFilename, `
token = "home-token"
clone_wiki = true
max_size = 50
`)
	writeFile(t, local, config.ConfigFilename, `
dry_run = true
git_author = "Ada"
protocol = "ssh"
`)

	homeCfg, localCfg, msgs, err := config.Load(local)
	require.NoError(t, err)

	assert.Empty(t, msgs)
	assert.Equal(t, config.Config{
		"token":      "home-token",
		"clone-wiki": true,
		"max-size":   int64(50),
	}, homeCfg)
	assert.Equal(t, config.Config{
		"dry-run":    true,
		"git-author": "Ada",
		"protocol":   "ssh",
	}, localCfg)
}

func TestLoad_MissingFilesReturnEmptyConfigs(t *testing.T) {
	isolateHome(t)

	homeCfg, localCfg, msgs, err := config.Load(t.TempDir())
	require.NoError(t, err)

	assert.NotNil(t, homeCfg)
	assert.NotNil(t, localCfg)
	assert.Empty(t, homeCfg)
	assert.Empty(t, localCfg)
	assert.Empty(t, msgs)
}

func TestLoad_EmptyLocalDirReadsOnlyHome(t *testing.T) {
	home := isolateHome(t)
	writeFile(t, home, config.ConfigFilename, `type = "sources"`)

	homeCfg, localCfg, msgs, err := config.Load("")
	require.NoError(t, err)

	assert.Equal(t, config.Config{"type": "sources"}, homeCfg)
	assert.Empty(t, localCfg)
	assert.Empty(t, msgs)
}

func TestLoad_MigratesLegacyFilesInHomeAndLocal(t *testing.T) {
	home := isolateHome(t)
	local := t.TempDir()
	writeFile(t, home, config.LegacyConfigFilename, `token = "legacy-home"`)
	writeFile(t, local, config.LegacyConfigFilename, `token = "legacy-local"`)

	homeCfg, localCfg, msgs, err := config.Load(local)
	require.NoError(t, err)

	assert.Equal(t, config.Config{"token": "legacy-home"}, homeCfg)
	assert.Equal(t, config.Config{"token": "legacy-local"}, localCfg)
	require.Len(t, msgs, 2)
	assert.Contains(t, msgs[0].Text, "Renamed legacy")
	assert.Contains(t, msgs[1].Text, "Renamed legacy")
	assert.False(t, msgs[0].IsWarning)
	assert.False(t, msgs[1].IsWarning)
	for _, dir := range []string{home, local} {
		assert.NoFileExists(t, filepath.Join(dir, config.LegacyConfigFilename))
		assert.FileExists(t, filepath.Join(dir, config.ConfigFilename))
	}
}

func TestLoad_BothFilesExistKeepsLegacyAndReadsNew(t *testing.T) {
	isolateHome(t)
	local := t.TempDir()
	legacy := writeFile(t, local, config.LegacyConfigFilename, `token = "old"`)
	writeFile(t, local, config.ConfigFilename, `token = "new"`)

	_, localCfg, msgs, err := config.Load(local)
	require.NoError(t, err)

	assert.Equal(t, config.Config{"token": "new"}, localCfg)
	require.Len(t, msgs, 1)
	assert.Contains(t, msgs[0].Text, "already exists; leaving legacy file in place")
	assert.False(t, msgs[0].IsWarning)
	assert.Equal(t, `token = "old"`, readFile(t, legacy))
}

func TestLoad_InvalidTOMLReturnsErrorNamingTheFile(t *testing.T) {
	tests := []struct {
		name    string
		inHome  bool
		content string
	}{
		{"bad home file", true, "token = = nope"},
		{"bad local file", false, "[unterminated"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := isolateHome(t)
			local := t.TempDir()
			dir := local
			if tt.inHome {
				dir = home
			}
			path := writeFile(t, dir, config.ConfigFilename, tt.content)

			_, _, _, err := config.Load(local)

			require.Error(t, err)
			assert.Contains(t, err.Error(), path)
		})
	}
}

func TestLoad_RejectsWrongTypes(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr []string
	}{
		{"string for int", `concurrency = "8"`, []string{`concurrency must be an integer, got string "8"`}},
		{"fractional float for int", `concurrency = 4.9`, []string{`concurrency must be an integer, got float 4.9`}},
		{"whole float for int", `max-size = 8.0`, []string{`max-size must be an integer, got float 8`}},
		{"int for bool", `dry-run = 1`, []string{`dry-run must be a boolean, got integer 1`}},
		{"bool for string", `token = true`, []string{`token must be a string, got boolean true`}},
		{"underscored key is checked too", `max_size = "big"`, []string{`max-size must be an integer, got string "big"`}},
		{
			"two bad keys are reported together, sorted",
			"dry-run = \"yes\"\nconcurrency = \"8\"\n",
			[]string{`concurrency must be an integer, got string "8"`, `dry-run must be a boolean, got string "yes"`},
		},
	}
	for _, where := range []string{"home", "local"} {
		for _, tt := range tests {
			t.Run(where+"/"+tt.name, func(t *testing.T) {
				homeDir := isolateHome(t)
				localDir := t.TempDir()
				dir := homeDir
				if where == "local" {
					dir = localDir
				}
				path := writeFile(t, dir, config.ConfigFilename, tt.content)

				_, _, _, err := config.Load(localDir)

				require.Error(t, err)
				assert.True(t, errors.Is(err, config.ErrInvalidValue), "err = %v", err)
				for _, want := range tt.wantErr {
					assert.Contains(t, err.Error(), path+": "+want)
				}
			})
		}
	}
}

func TestLoad_WarnsOnUnknownKeys(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantKey string
	}{
		{"a misspelled key", "concurency = 8\nprotocol = \"ssh\"\n", "concurency"},
		{"ssh, which is flag-only", "ssh = true\nprotocol = \"ssh\"\n", "ssh"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := isolateHome(t)
			path := writeFile(t, home, config.ConfigFilename, tt.content)

			homeCfg, _, notices, err := config.Load("")

			require.NoError(t, err)
			require.Len(t, notices, 1)
			assert.True(t, notices[0].IsWarning)
			assert.Equal(t, path+`: unknown key "`+tt.wantKey+`" ignored`, notices[0].Text)
			assert.Equal(t, "ssh", homeCfg["protocol"], "known values still load")
		})
	}
}

func TestIsKey(t *testing.T) {
	assert.True(t, config.IsKey("concurrency"))
	assert.True(t, config.IsKey("dry-run"))
	assert.False(t, config.IsKey("ssh"))
	assert.False(t, config.IsKey("concurency"))
}

func TestLoad_ReturnsErrorWhenHomeDirectoryIsUnset(t *testing.T) {
	for _, key := range homeEnvKeys {
		t.Setenv(key, "")
	}

	_, _, _, err := config.Load(t.TempDir())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolving home directory")
}

func TestMigrateLegacyFile(t *testing.T) {
	const legacyName, newName = ".old.toml", ".new.toml"
	tests := []struct {
		name         string
		legacy       string // content, empty means absent
		current      string // content, empty means absent
		wantMigrated bool
		wantMessage  string
		wantNew      string // content of newName afterwards, empty means absent
		wantLegacy   bool
	}{
		{
			name: "neither file exists",
		},
		{
			name:    "only the new file exists",
			current: "new",
			wantNew: "new",
		},
		{
			name:         "only the legacy file exists",
			legacy:       "old",
			wantMigrated: true,
			wantMessage:  "Renamed legacy",
			wantNew:      "old",
		},
		{
			name:        "both files exist",
			legacy:      "old",
			current:     "new",
			wantMessage: "already exists; leaving legacy file in place",
			wantNew:     "new",
			wantLegacy:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if tt.legacy != "" {
				writeFile(t, dir, legacyName, tt.legacy)
			}
			if tt.current != "" {
				writeFile(t, dir, newName, tt.current)
			}

			migrated, msg, err := config.MigrateLegacyFile(dir, legacyName, newName)
			require.NoError(t, err)

			assert.Equal(t, tt.wantMigrated, migrated)
			if tt.wantMessage == "" {
				assert.Empty(t, msg)
			} else {
				assert.Contains(t, msg, tt.wantMessage)
			}
			if tt.wantNew == "" {
				assert.NoFileExists(t, filepath.Join(dir, newName))
			} else {
				assert.Equal(t, tt.wantNew, readFile(t, filepath.Join(dir, newName)))
			}
			if tt.wantLegacy {
				assert.FileExists(t, filepath.Join(dir, legacyName))
			} else {
				assert.NoFileExists(t, filepath.Join(dir, legacyName))
			}
		})
	}
}
