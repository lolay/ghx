package ui

import (
	"io"

	"github.com/lolay/ghx/internal/cloner"
)

// ResetTerminal undoes SetupTerminal for the next test: the live table draws on
// Out again, as it does before main calls SetupTerminal.
func ResetTerminal() { animate = true }

// StatusMark and ActionText expose the helpers the table renderer and the
// summary share.
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
	p := newTableProgress(syncSpec, out)
	p.size = size
	return p
}

// NewBranchesTableProgress is the branches report's live table drawing to out
// on a terminal width columns wide and height lines tall, whatever out is.
func NewBranchesTableProgress(out io.Writer, expected string, width, height int) BranchesProgress {
	p := newTableProgress(newBranchesSpec(expected), out)
	p.size = func() (int, int) { return width, height }
	return &branchesTable{p}
}
