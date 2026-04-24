package config

import (
	"errors"
	"fmt"
	"os"
)

// Source describes where a resolved option came from, used for display.
type Source string

const (
	SourceFlag      Source = "option"
	SourceEnv       Source = "GITHUB_TOKEN env"
	SourceLocal     Source = "<path>/" + ConfigFilename
	SourceHome      Source = "~/" + ConfigFilename
	SourceDefault   Source = "default"
	SourceArgument  Source = "argument"
)

// ErrNoToken is returned when no GitHub token can be resolved.
var ErrNoToken = errors.New("no GitHub token provided")

// NoTokenMessage is the user-facing explanation for ErrNoToken.
const NoTokenMessage = "No GitHub token provided. Supply one via:\n" +
	"  1. --token / -t flag\n" +
	"  2. GITHUB_TOKEN environment variable\n" +
	"  3. ~/" + ConfigFilename + " config file" +
	` (token = "ghp_...")`

// ResolveString resolves a string option using the precedence:
// CLI flag (if non-nil) > local config > home config > default.
// Returns the value and the source it came from.
func ResolveString(flag *string, home, local Config, key, def string) (string, Source) {
	if flag != nil {
		return *flag, SourceFlag
	}
	if v, ok := local[key]; ok {
		if s, ok := v.(string); ok {
			return s, SourceLocal
		}
	}
	if v, ok := home[key]; ok {
		if s, ok := v.(string); ok {
			return s, SourceHome
		}
	}
	return def, SourceDefault
}

// ResolveBool resolves a boolean option using the same precedence as ResolveString.
// The flag argument is a pointer; nil means "not set on the command line".
func ResolveBool(flag *bool, home, local Config, key string, def bool) (bool, Source) {
	if flag != nil {
		return *flag, SourceFlag
	}
	if v, ok := local[key]; ok {
		if b, ok := v.(bool); ok {
			return b, SourceLocal
		}
	}
	if v, ok := home[key]; ok {
		if b, ok := v.(bool); ok {
			return b, SourceHome
		}
	}
	return def, SourceDefault
}

// ResolveInt resolves an integer option. TOML integers decode to int64.
func ResolveInt(flag *int, home, local Config, key string, def int) (int, Source) {
	if flag != nil {
		return *flag, SourceFlag
	}
	if v, ok := local[key]; ok {
		if n, ok := asInt(v); ok {
			return n, SourceLocal
		}
	}
	if v, ok := home[key]; ok {
		if n, ok := asInt(v); ok {
			return n, SourceHome
		}
	}
	return def, SourceDefault
}

// ResolveOptionalInt resolves an integer option where nil means "no default".
// Returns (value, source, present). When present is false the value is undefined.
func ResolveOptionalInt(flag *int, home, local Config, key string) (int, Source, bool) {
	if flag != nil {
		return *flag, SourceFlag, true
	}
	if v, ok := local[key]; ok {
		if n, ok := asInt(v); ok {
			return n, SourceLocal, true
		}
	}
	if v, ok := home[key]; ok {
		if n, ok := asInt(v); ok {
			return n, SourceHome, true
		}
	}
	return 0, SourceDefault, false
}

// ResolveOptionalString resolves a string option where an unset value is returned
// as present=false (no default).
func ResolveOptionalString(flag *string, home, local Config, key string) (string, Source, bool) {
	if flag != nil {
		return *flag, SourceFlag, true
	}
	if v, ok := local[key]; ok {
		if s, ok := v.(string); ok {
			return s, SourceLocal, true
		}
	}
	if v, ok := home[key]; ok {
		if s, ok := v.(string); ok {
			return s, SourceHome, true
		}
	}
	return "", SourceDefault, false
}

// ResolveToken resolves the GitHub token using the precedence:
// CLI flag > GITHUB_TOKEN env > local config > home config.
// Returns (token, source). If no token is found ErrNoToken is returned
// (the caller can display NoTokenMessage).
func ResolveToken(flag string, home, local Config) (string, Source, error) {
	if flag != "" {
		return flag, SourceFlag, nil
	}
	if env := os.Getenv("GITHUB_TOKEN"); env != "" {
		return env, SourceEnv, nil
	}
	if v, ok := local["token"]; ok {
		if s, ok := v.(string); ok && s != "" {
			return s, SourceLocal, nil
		}
	}
	if v, ok := home["token"]; ok {
		if s, ok := v.(string); ok && s != "" {
			return s, SourceHome, nil
		}
	}
	return "", "", fmt.Errorf("%w", ErrNoToken)
}

func asInt(v any) (int, bool) {
	switch n := v.(type) {
	case int64:
		return int(n), true
	case int:
		return n, true
	case int32:
		return int(n), true
	case float64:
		return int(n), true
	}
	return 0, false
}
