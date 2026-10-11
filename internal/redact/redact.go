// Package redact takes secrets out of text ghx shows the user, such as git's
// stderr in a failed repo's detail or an API error. ghx no longer puts the
// token anywhere git or GitHub would echo it, so this is the second line of
// defence: a user's trace settings or an old clone's remote URL can still
// bring it back.
package redact

import "strings"

// Mask replaces each secret in redacted text.
const Mask = "***"

// String returns s with every non-empty secret replaced by Mask.
func String(s string, secrets ...string) string {
	for _, secret := range secrets {
		if secret != "" {
			s = strings.ReplaceAll(s, secret, Mask)
		}
	}
	return s
}

// Error returns err with every secret masked in its message. The result still
// wraps err for errors.Is and errors.As; an err that holds no secret comes back
// unchanged.
func Error(err error, secrets ...string) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if clean := String(msg, secrets...); clean != msg {
		return &redactedError{msg: clean, err: err}
	}
	return err
}

type redactedError struct {
	msg string
	err error
}

func (e *redactedError) Error() string { return e.msg }
func (e *redactedError) Unwrap() error { return e.err }
