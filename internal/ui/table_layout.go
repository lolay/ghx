package ui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"

	"github.com/lolay/ghx/internal/cloner"
	"github.com/lolay/ghx/internal/ghapi"
)

const (
	// minNameWidth is the least the Name column is cut to on a terminal too
	// narrow for the table once every droppable column is gone.
	minNameWidth = 4
	// unlistedNameWidth is the Name column's least width when the renderer
	// never got the repos (SetRepos): it can't measure the names, so it leaves
	// room for the usual ones.
	unlistedNameWidth = 24
	// longestElapsed is the longest time a Status cell shows, which sets the
	// width of the column: 12 minutes and 59 seconds.
	longestElapsed = 12*time.Minute + 59*time.Second
	// footerBarMax is the widest the progress footer draws its bar.
	footerBarMax = 40
	// footerBarMin is the narrowest bar worth drawing; below it the footer
	// shows the counts alone.
	footerBarMin = 8
)

// columnName is the index of the Name column in repoColumns.
const columnName = 0

// tableLayout is the geometry of the live table, measured once so that no
// frame changes the table's width: the columns it shows, the width of each (the
// widest value over every repo, and the header), and the lines that frame the
// rows. A column too wide for the terminal is dropped, or for Name cut short,
// when the layout is made and never again, so a terminal resized mid-run does
// not move anything.
type tableLayout struct {
	cols   []int // indexes into repoColumns of the columns shown
	widths []int // content width of each column in cols, then of Status
	width  int   // width of every line of the table
	head   []string
	rule   string // the line between the rows and the footer
	bottom string // the closing border
}

// newTableLayout measures repos for a terminal termWidth columns wide.
// nameFloor is the least width of the Name column: 0 for a list of repos
// known up front.
func newTableLayout(repos []ghapi.RepoInfo, termWidth, nameFloor int) *tableLayout {
	natural := make([]int, len(repoColumns))
	for c, col := range repoColumns {
		natural[c] = text.StringWidthWithoutEscSequences(col.title)
		for _, r := range repos {
			natural[c] = max(natural[c], text.StringWidthWithoutEscSequences(fmt.Sprint(col.cell(r))))
		}
	}
	natural[columnName] = max(natural[columnName], nameFloor)
	statusNatural := statusColumnWidth()

	shown := allColumns()
	span := func() int {
		n := 1 + statusNatural + 3
		for _, c := range shown {
			n += natural[c] + 3
		}
		return n
	}
	for _, drop := range columnDrops {
		if span() <= termWidth {
			break
		}
		shown = slices.DeleteFunc(shown, func(c int) bool { return c == drop })
	}
	// With nothing left to drop, Name takes the cut: a long name ends in "…".
	if excess := span() - termWidth; excess > 0 {
		natural[columnName] = min(natural[columnName], max(natural[columnName]-excess, minNameWidth))
	}

	l := &tableLayout{cols: shown}
	for _, c := range shown {
		l.widths = append(l.widths, natural[c])
	}
	l.widths = append(l.widths, statusNatural)

	// A table with no rows is the title, the header and the borders: the
	// pieces the live frames and the printed rows are laid out around.
	lines := strings.Split(l.render(nil, []string{}), "\n")
	l.head, l.bottom = lines[:len(lines)-1], lines[len(lines)-1]
	l.width = text.LongestLineLen(l.bottom)
	box := table.StyleLight.Box
	l.rule = strings.NewReplacer(box.BottomLeft, box.LeftSeparator, box.BottomRight, box.RightSeparator).Replace(l.bottom)
	l.bottom = strings.NewReplacer(box.BottomSeparator, box.MiddleHorizontal).Replace(l.bottom)
	return l
}

// statusColumnWidth is the width of the longest Status cell: the in-flight
// forms with the longest time, "interrupted", "queued", and every action's
// finished form, as in "✓ cloned 12m59s".
func statusColumnWidth() int {
	width := text.StringWidthWithoutEscSequences("Status")
	longest := formatSeconds(longestElapsed)
	cells := []string{
		spinFrames[0] + " cloning " + longest,
		spinFrames[0] + " pulling " + longest,
		"interrupted",
		"queued",
	}
	for _, a := range cloner.AllActions {
		symbol, _ := statusMark(a)
		cells = append(cells, symbol+" "+actionText(a, longestElapsed))
	}
	for _, cell := range cells {
		width = max(width, text.StringWidthWithoutEscSequences(cell))
	}
	return width
}

// render is the table as go-pretty draws it with every column at its fixed
// width: the title, the header, the rows and a closing border.
func (l *tableLayout) render(repos []ghapi.RepoInfo, status []string) string {
	return repoTable(l.cols, repos, status, l.widths).Render()
}

// rows is the line for each repo, with its Status cell. A table's frame is
// five lines above the rows and one below.
func (l *tableLayout) rows(repos []ghapi.RepoInfo, status []string) []string {
	if len(repos) == 0 {
		return nil
	}
	lines := strings.Split(l.render(repos, status), "\n")
	return lines[len(l.head) : len(lines)-1]
}

// innerWidth is how many columns of text a footer line holds between the
// table's side borders.
func (l *tableLayout) innerWidth() int {
	box := table.StyleLight.Box
	return l.width - text.StringWidthWithoutEscSequences(box.Left+box.PaddingLeft+box.PaddingRight+box.Right)
}

// footer is one line spanning the table, holding s (cut with "…" when it is
// too long).
func (l *tableLayout) footer(s string) string {
	box := table.StyleLight.Box
	inner := l.innerWidth()
	s = truncate(s, inner)
	pad := strings.Repeat(" ", max(inner-text.StringWidthWithoutEscSequences(s), 0))
	return box.Left + box.PaddingLeft + s + pad + box.PaddingRight + box.Right
}

// footerLines is the footer for parts joined with " · ": one line when they
// fit, else as many as they need, so a count at the end (the failures) is
// never the one cut off.
func (l *tableLayout) footerLines(parts []string) []string {
	const sep = " · "
	inner := l.innerWidth()
	var lines []string
	var cur string
	for _, part := range parts {
		switch {
		case cur == "":
			cur = part
		case text.StringWidthWithoutEscSequences(cur+sep+part) <= inner:
			cur += sep + part
		default:
			lines = append(lines, l.footer(cur))
			cur = part
		}
	}
	if cur != "" {
		lines = append(lines, l.footer(cur))
	}
	return lines
}

// barWidth is the width of the progress bar for a footer that counts total
// repos: what is left of the line after the other words at their longest,
// capped, or 0 when too little is left to draw a bar.
func (l *tableLayout) barWidth(total int) int {
	longest := fmt.Sprintf("Syncing %d/%d [] 100%% %s · %d running · %d queued",
		total, total, formatElapsed(longestElapsed), total, total)
	inner := l.width - 4
	w := min(inner-text.StringWidthWithoutEscSequences(longest), footerBarMax)
	if w < footerBarMin {
		return 0
	}
	return w
}

// truncate cuts s to width columns, ending in "…" when it had to.
func truncate(s string, width int) string {
	if text.StringWidthWithoutEscSequences(s) <= width {
		return s
	}
	if width <= 0 {
		return ""
	}
	return text.Trim(s, width-1) + "…"
}

// progressText is the footer's progress: "Syncing 34/60 [=========>------]
// 57% 12s · 4 running · 22 queued". barWidth 0 leaves the bar out.
func progressText(done, total, running, queued, barWidth int, elapsed time.Duration) string {
	pct := 0
	if total > 0 {
		pct = done * 100 / total
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Syncing %d/%d ", done, total)
	if barWidth > 0 {
		b.WriteString("[" + progressBar(done, total, barWidth) + "] ")
	}
	fmt.Fprintf(&b, "%d%% %s · %d running · %d queued", pct, formatElapsed(elapsed), running, queued)
	return b.String()
}

// progressBar is width plain characters: "=" for the share done, ">" at its
// head and "-" for the rest.
func progressBar(done, total, width int) string {
	filled := 0
	if total > 0 {
		filled = min(done, total) * width / total
	}
	if filled >= width {
		return strings.Repeat("=", width)
	}
	return strings.Repeat("=", filled) + ">" + strings.Repeat("-", width-filled-1)
}

// formatElapsed shows how long a sync has run in whole seconds: "12s", "1m12s".
func formatElapsed(d time.Duration) string {
	return d.Round(time.Second).String()
}
