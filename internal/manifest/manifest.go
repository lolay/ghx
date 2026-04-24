// Package manifest reads and writes the .ghx.json file that tracks which
// repositories were synced on the previous run. The format matches the
// Python implementation byte-for-byte so an existing manifest from the
// Python version is interchangeable with this one.
package manifest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/garyrudolph/ghx/internal/config"
	"github.com/garyrudolph/ghx/internal/ghapi"
)

// Repo is one entry in the manifest's repos array.
type Repo struct {
	Name          string `json:"name"`
	DefaultBranch string `json:"default_branch"`
	Archived      bool   `json:"archived"`
	HasWiki       bool   `json:"has_wiki"`
}

// Manifest is the top-level .ghx.json document.
type Manifest struct {
	Org        string `json:"org"`
	ExportedAt string `json:"exported_at"`
	Repos      []Repo `json:"repos"`
}

// Read loads and parses the manifest in dir. It also migrates a legacy
// .gh-export.json file in place if present. A missing file returns
// (nil, "", nil).
//
// The returned message (if non-empty) should be displayed to the user
// (it describes the legacy-file migration).
func Read(dir string) (*Manifest, string, error) {
	_, msg, err := config.MigrateLegacyFile(dir, config.LegacyManifestFilename, config.ManifestFilename)
	if err != nil {
		return nil, "", err
	}

	path := filepath.Join(dir, config.ManifestFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, msg, nil
		}
		return nil, msg, fmt.Errorf("reading %s: %w", path, err)
	}

	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, msg, fmt.Errorf("parsing %s: %w", path, err)
	}
	return &m, msg, nil
}

// Write serializes a manifest capturing all repos in the given list and
// writes it to <dir>/.ghx.json with 2-space indentation and a trailing
// newline (matching the Python output exactly).
func Write(dir, org string, repos []ghapi.RepoInfo) error {
	m := Manifest{
		Org:        org,
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
		Repos:      make([]Repo, 0, len(repos)),
	}
	for _, r := range repos {
		m.Repos = append(m.Repos, Repo{
			Name:          r.Name,
			DefaultBranch: r.DefaultBranch,
			Archived:      r.Archived,
			HasWiki:       r.HasWiki,
		})
	}
	buf, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	buf = append(buf, '\n')
	path := filepath.Join(dir, config.ManifestFilename)
	return os.WriteFile(path, buf, 0o644)
}
