package ui

// ResetTerminal undoes SetupTerminal for the next test: progress bars draw on
// Out again, as they do before main calls SetupTerminal.
func ResetTerminal() { animate = true }

// StatusMark and ActionText expose the helpers the pterm and table renderers
// share.
var (
	StatusMark = statusMark
	ActionText = actionText
)
