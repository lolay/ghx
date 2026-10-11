package cli

// NewRootCmd gives the black-box tests in package cli_test the cobra command
// that Execute builds, so they can drive it in-process with their own args
// and context.
var NewRootCmd = newRootCmd
