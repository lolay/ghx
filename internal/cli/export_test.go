package cli

import (
	"testing"
	"time"
)

// NewRootCmd gives the black-box tests in package cli_test the cobra command
// that Execute builds, so they can drive it in-process with their own args
// and context.
var NewRootCmd = newRootCmd

// Run runs cmd through fang as Execute does, with the caller's context, and
// returns the exit code.
var Run = execute

// PlainWriter is the ANSI-stripping writer Run uses under NO_COLOR.
var PlainWriter = plainWriter

// ErrorStyles picks the styles fang's error handler renders err with.
var ErrorStyles = errorStyles

// NewBranchesCmd gives the tests the ghb command ExecuteBranches builds.
var NewBranchesCmd = newBranchesCmd

// SetNow pins the time ghb measures branch ages from, for one test.
func SetNow(t *testing.T, at time.Time) {
	t.Helper()
	orig := now
	now = func() time.Time { return at }
	t.Cleanup(func() { now = orig })
}
