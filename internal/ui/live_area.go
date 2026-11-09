package ui

import (
	"fmt"
	"io"
	"strings"
)

// Escape sequences the live area writes. Synchronized output (DEC private
// mode 2026) asks the terminal to hold its screen until the frame is whole;
// a terminal that doesn't know the mode ignores it, as it does any unknown
// private mode.
const (
	syncBegin   = "\x1b[?2026h"
	syncEnd     = "\x1b[?2026l"
	clearEOL    = "\x1b[K"
	clearBelow  = "\x1b[J"
	startOfLine = "\x1b[G"
	hideCursor  = "\x1b[?25l"
	showCursor  = "\x1b[?25h"
)

// liveArea is the redrawn bottom of the terminal, under the ordinary output.
//
// It draws each frame over the last in place, in one write: back to the top
// of the area, each line erased and written again, then whatever the old
// frame had below erased. The area is never blank between frames, as it is
// with an erase-then-write area (pterm's), which the eye sees 10 times a
// second as flicker, worse the taller the area.
//
// The cursor rests at the start of the line below the area. It moves with
// relative codes only, so the area must stay shorter than the terminal: a
// line scrolled off the top can't be reached again.
type liveArea struct {
	out    io.Writer
	height int // lines the area takes on screen
}

// draw prints above, ordinary output that stays in the scrollback, then
// frame in place of the area's last frame. Both are whole lines, each ending
// in a newline.
func (a *liveArea) draw(above, frame string) {
	var b strings.Builder
	b.WriteString(syncBegin)
	a.toTop(&b)
	writeOver(&b, above)
	writeOver(&b, frame)
	b.WriteString(clearBelow)
	b.WriteString(syncEnd)
	a.height = strings.Count(frame, "\n")
	_, _ = io.WriteString(a.out, b.String())
}

// erase removes the area, leaving the cursor at the start of the line it
// began on.
func (a *liveArea) erase() {
	var b strings.Builder
	a.toTop(&b)
	b.WriteString(clearBelow)
	a.height = 0
	_, _ = io.WriteString(a.out, b.String())
}

// toTop moves the cursor to the start of the area's first line. The column
// goes back with an escape code rather than a carriage return, so output
// with the codes stripped (a log of a run) has no stray control character.
func (a *liveArea) toTop(b *strings.Builder) {
	b.WriteString(startOfLine)
	if a.height > 0 {
		fmt.Fprintf(b, "\x1b[%dA", a.height)
	}
}

// writeOver writes text's lines over whatever the screen shows there. Each
// line is erased before it is written, not after: a line as wide as the
// terminal leaves the cursor on its last column, where an erase would take
// the last character with it.
func writeOver(b *strings.Builder, text string) {
	for line := range strings.Lines(text) {
		b.WriteString(clearEOL)
		b.WriteString(line)
	}
}
