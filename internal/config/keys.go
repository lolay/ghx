package config

import (
	"errors"
	"fmt"
	"sort"
)

// ErrInvalidValue is wrapped by the error Load returns when a config file
// holds a value of the wrong type for its key.
var ErrInvalidValue = errors.New("invalid config value")

// Notice is a message Load wants the caller to show.
type Notice struct {
	// Text is the message, ready to print.
	Text string
	// IsWarning is true for something the user should fix, such as an unknown
	// key, and false for an informational note such as a legacy migration.
	IsWarning bool
}

// valueError carries the message exactly as shown and unwraps to
// ErrInvalidValue, so the text has no sentinel suffix and errors.Is still works.
type valueError struct{ msg string }

func (e valueError) Error() string { return e.msg }

func (e valueError) Unwrap() error { return ErrInvalidValue }

// kind is the TOML type a key's value must have.
type kind int

const (
	kindString kind = iota
	kindBool
	kindInt
)

// keyKinds lists every key a config file may set. `ssh` is absent on purpose:
// it is a flag-only shortcut for `protocol`.
var keyKinds = map[string]kind{
	"token":        kindString,
	"protocol":     kindString,
	"type":         kindString,
	"include":      kindString,
	"exclude":      kindString,
	"clone-wiki":   kindBool,
	"concurrency":  kindInt,
	"dry-run":      kindBool,
	"git-author":   kindString,
	"git-email":    kindString,
	"delete":       kindBool,
	"archived-dir": kindString,
	"deleted-dir":  kindString,
	"max-size":     kindInt,
	// ghb's keys; ghb reads only the home config.
	"include-archived": kindBool,
	"stale-days":       kindInt,
	"expected-default": kindString,
}

// IsKey reports whether name is a key a config file may set.
func IsKey(name string) bool {
	_, ok := keyKinds[name]
	return ok
}

// check validates a decoded, normalized config read from path. Every key of
// the wrong type is reported in one joined error that wraps ErrInvalidValue;
// unknown keys come back as warnings, because a typo should not stop a run
// that would otherwise work.
func check(path string, cfg Config) ([]Notice, error) {
	// Sorted so the output is the same on every run.
	names := make([]string, 0, len(cfg))
	for name := range cfg {
		names = append(names, name)
	}
	sort.Strings(names)

	var notices []Notice
	var errs []error
	for _, name := range names {
		want, known := keyKinds[name]
		if !known {
			notices = append(notices, Notice{
				Text:      fmt.Sprintf("%s: unknown key %q ignored", path, name),
				IsWarning: true,
			})
			continue
		}
		if !matches(want, cfg[name]) {
			errs = append(errs, valueError{msg: fmt.Sprintf("%s: %s must be %s, got %s",
				path, name, want, describe(cfg[name]))})
		}
	}
	return notices, errors.Join(errs...)
}

// matches reports whether v, as the TOML decoder returns it, has the kind
// want. Integers decode to int64, so a float64 is never an integer, 8.0
// included.
func matches(want kind, v any) bool {
	switch want {
	case kindString:
		_, ok := v.(string)
		return ok
	case kindBool:
		_, ok := v.(bool)
		return ok
	case kindInt:
		_, ok := v.(int64)
		return ok
	}
	return false
}

// String names the kind the way an error message says it.
func (k kind) String() string {
	switch k {
	case kindBool:
		return "a boolean"
	case kindInt:
		return "an integer"
	default:
		return "a string"
	}
}

// describe names the type of a decoded value, and quotes it when it is a
// string, for an error message.
func describe(v any) string {
	switch t := v.(type) {
	case string:
		return fmt.Sprintf("string %q", t)
	case bool:
		return fmt.Sprintf("boolean %t", t)
	case int64:
		return fmt.Sprintf("integer %d", t)
	case float64:
		return fmt.Sprintf("float %v", t)
	default:
		return fmt.Sprintf("%T", v)
	}
}
