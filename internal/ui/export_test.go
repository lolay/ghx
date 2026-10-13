package ui

import (
	"io"

	"github.com/lolay/ghx/internal/cloner"
)

// ResetTerminal undoes SetupTerminal for the next test: progress bars draw on
// Out again, as they do before main calls SetupTerminal.
func ResetTerminal() { animate = true }

// StatusMark and ActionText expose the helpers the pterm and table renderers
// share.
var (
	StatusMark = statusMark
	ActionText = actionText
)

// NewTableProgress is the table renderer drawing to out on a terminal width
// columns wide and height lines tall, whatever out is.
func NewTableProgress(out io.Writer, width, height int) cloner.Progress {
	return NewTableProgressSized(out, func() (int, int) { return width, height })
}

// NewTableProgressSized is the table renderer drawing to out on a terminal
// whose size, width then height, size reads on every repaint: a test resizes
// the terminal by changing what it returns.
func NewTableProgressSized(out io.Writer, size func() (width, height int)) cloner.Progress {
	p := newTableProgress(out)
	p.size = size
	return p
}
