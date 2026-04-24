// Package config handles loading and merging TOML configuration files
// from the user's home directory and the per-output directory, plus
// migration of legacy .gh-export.* files.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

const (
	// ManifestFilename is the name of the per-output-directory manifest.
	ManifestFilename = ".ghx.json"
	// ConfigFilename is the name of the TOML configuration file.
	ConfigFilename = ".ghx.toml"

	// LegacyManifestFilename is the previous manifest name, migrated on load.
	LegacyManifestFilename = ".gh-export.json"
	// LegacyConfigFilename is the previous config name, migrated on load.
	LegacyConfigFilename = ".gh-export.toml"
)

// Config is the decoded form of a .ghx.toml file. Keys are normalized
// so underscores become hyphens, matching the CLI flag style.
type Config map[string]any

// MigrateLegacyFile renames a legacy file to its new name in place.
// If both exist a warning is printed and nothing happens. If the legacy
// file does not exist this is a no-op.
//
// Returns (migrated, message, err). Callers decide how to surface the
// message (typically as a dim or yellow log line).
func MigrateLegacyFile(dir, legacyName, newName string) (migrated bool, message string, err error) {
	legacyPath := filepath.Join(dir, legacyName)
	newPath := filepath.Join(dir, newName)

	if _, statErr := os.Stat(legacyPath); statErr != nil {
		if os.IsNotExist(statErr) {
			return false, "", nil
		}
		return false, "", statErr
	}

	if _, statErr := os.Stat(newPath); statErr == nil {
		return false, fmt.Sprintf(
			"Found legacy %s but %s already exists; leaving legacy file in place.",
			legacyPath, newPath,
		), nil
	} else if !os.IsNotExist(statErr) {
		return false, "", statErr
	}

	if err := os.Rename(legacyPath, newPath); err != nil {
		return false, "", err
	}
	return true, fmt.Sprintf("Renamed legacy %s -> %s", legacyPath, newPath), nil
}

// Load loads the home-directory config and, if localDir is non-empty, the
// per-directory config. Either may be absent (empty map returned). Legacy
// .gh-export.toml files are migrated in place; any informational messages
// produced by the migration are returned for the caller to display.
func Load(localDir string) (home, local Config, messages []string, err error) {
	home = Config{}
	local = Config{}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("resolving home directory: %w", err)
	}

	if _, msg, err := MigrateLegacyFile(homeDir, LegacyConfigFilename, ConfigFilename); err != nil {
		return nil, nil, nil, err
	} else if msg != "" {
		messages = append(messages, msg)
	}

	homePath := filepath.Join(homeDir, ConfigFilename)
	if err := readTOML(homePath, &home); err != nil {
		return nil, nil, nil, err
	}

	if localDir != "" {
		if _, msg, err := MigrateLegacyFile(localDir, LegacyConfigFilename, ConfigFilename); err != nil {
			return nil, nil, nil, err
		} else if msg != "" {
			messages = append(messages, msg)
		}

		localPath := filepath.Join(localDir, ConfigFilename)
		if err := readTOML(localPath, &local); err != nil {
			return nil, nil, nil, err
		}
	}

	return home, local, messages, nil
}

// readTOML decodes a TOML file into out. A missing file is not an error;
// out is left as an empty map in that case. Keys are normalized so that
// underscores are replaced with hyphens to align with the CLI style.
func readTOML(path string, out *Config) error {
	raw := map[string]any{}
	_, err := toml.DecodeFile(path, &raw)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading %s: %w", path, err)
	}
	normalized := make(Config, len(raw))
	for k, v := range raw {
		normalized[strings.ReplaceAll(k, "_", "-")] = v
	}
	*out = normalized
	return nil
}
