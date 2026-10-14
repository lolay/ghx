package ui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"atomicgo.dev/cursor"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/pterm/pterm"
	"golang.org/x/term"

	"github.com/lolay/ghx/internal/cloner"
	"github.com/lolay/ghx/internal/ghapi"
)

// repaintEvery is how often the live area is redrawn, which is what moves the
// spinners and the running times while no repo finishes.
const repaintEvery = 100 * time.Millisecond

// columnDrops is the order the table gives up columns, by index into
// repoColumns, while it is wider than the terminal.
var columnDrops = []int{columnPushed, columnBranch, columnLanguage}

// spinFrames is the spinner on an in-flight repo's Status cell.
var spinFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// rowState is where a repo stands in the sync.
type rowState int

const (
	rowQueued      rowState = iota // not started
	rowRunning                     // between RepoStarted and RepoDone
	rowDone                        // RepoDone arrived
	rowInterrupted                 // still running when Stop was called
)

// tableRow is one repo of the table and what has happened to it.
type tableRow struct {
	info    ghapi.RepoInfo
	state   rowState
	verb    string        // "cloning" or "pulling", while running
	started time.Time     // when RepoStarted arrived, while running
	action  cloner.Action // the result, once done
	elapsed time.Duration // how long it took, once done
}

// tableProgress draws the "Repos to export" table the way Claude's CLI keeps
// its output: the table grows downward into the terminal's scrollback, and
// only its bottom is live. It is a RepoLister: the run flow hands it the repos
// instead of printing the static table.
//
// The first Start prints the table's top (title, border, header) as ordinary
// output. Rows print above the live area in the order SetRepos listed them,
// each once every row before it has finished, so the scrollback keeps the
// list's order however the workers finish. The live area holds what has not
// printed yet: the in-flight rows with a spinner and a running time, the
// finished rows held back behind one still running, and a footer row spanning
// the table with the progress, "Syncing 34/60 [=====>---] 57% 12s · 4 running
// · 22 queued", counted over every repo the table lists, across both passes.
// The live area is never taller than the terminal. When its rows don't fit,
// the held rows give way first, counted in the footer as "+N done, waiting on
// <the repo holding them>", then the last in-flight rows, as "+N more
// running". FinishTable closes the table: every row not printed yet (held,
// interrupted or queued), then the bottom border and a footer with the totals.
//
// The table's geometry is measured once, when SetRepos gets the repos (see
// tableLayout): every column is as wide as its widest value, so rows printed
// one at a time line up as one table and a terminal resized mid-run keeps it.
// A line is trimmed only when it is wider than the terminal, so none wraps.
//
// The run flow calls CloneRepos once for the active repos and again for the
// archived ones with this one value, so Start and Stop come in pairs. Each
// Stop erases the live area and prints nothing, so the table stays one piece
// across the two passes; the run flow calls FinishTable after the last pass,
// also on an error or a cancel (which leave rows queued or interrupted).
// Closing it from Stop would need to know whether another pass follows, and a
// renderer can't.
//
// It prints a finished row by clearing the area, printing, and starting the
// area again. A pterm AreaPrinter writes to out,
// pterm's switches stay untouched, and the cursor target (the one pterm global
// needed) points at out only between Start and Stop. Every method is safe for
// concurrent use.
type tableProgress struct {
	out  fdWriter
	size func() (width, height int) // read on every repaint, so a resize shows

	mu         sync.Mutex
	rows       []*tableRow
	printed    int // rows[:printed] are in the scrollback; the rest wait
	byName     map[string]*tableRow
	layout     *tableLayout
	listed     bool      // SetRepos was called
	topPrinted bool      // the title and header are out since SetRepos
	closed     bool      // FinishTable has closed the table since SetRepos
	began      time.Time // the first Start since SetRepos
	stopped    time.Time // the last Stop
	area       *pterm.AreaPrinter
	quit       chan struct{} // closed by Stop to end the repaint goroutine
	repainted  chan struct{} // closed when that goroutine has returned
}

// fdWriter gives any io.Writer the Fd method pterm's cursor code asks of its
// writer. Only a Windows console reads the descriptor, and a console is the
// os.File that Fd then reports.
type fdWriter struct{ io.Writer }

// Fd reports the descriptor of the wrapped file, or 0 when it isn't one.
func (w fdWriter) Fd() uintptr {
	if f, ok := w.Writer.(interface{ Fd() uintptr }); ok {
		return f.Fd()
	}
	return 0
}

func newTableProgress(out io.Writer) *tableProgress {
	return &tableProgress{out: fdWriter{out}, size: terminalSize(out)}
}

// terminalSize reads the size of out when it is a terminal, and falls back to
// 80 by 24, the size every terminal is assumed to have.
func terminalSize(out io.Writer) func() (int, int) {
	return func() (int, int) {
		if f, ok := out.(interface{ Fd() uintptr }); ok {
			if w, h, err := term.GetSize(int(f.Fd())); err == nil && w > 0 && h > 0 {
				return w, h
			}
		}
		return 80, 24
	}
}

// SetRepos starts a new table with every repo queued.
func (p *tableProgress) SetRepos(repos []ghapi.RepoInfo) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.rows = make([]*tableRow, len(repos))
	p.byName = make(map[string]*tableRow, len(repos))
	for i, r := range repos {
		row := &tableRow{info: r}
		p.rows[i] = row
		p.byName[r.Name] = row
	}
	p.printed = 0
	p.listed, p.topPrinted, p.closed = true, false, false
	p.began, p.stopped = time.Time{}, time.Time{}
	width, _ := p.size()
	p.layout = newTableLayout(repos, width, 0)
}

// layoutLocked is the table's layout. A renderer used without SetRepos
// measures it at the first Start, from whatever rows it has. The caller holds
// p.mu.
func (p *tableProgress) layoutLocked() *tableLayout {
	if p.layout == nil {
		width, _ := p.size()
		infos := make([]ghapi.RepoInfo, len(p.rows))
		for i, row := range p.rows {
			infos[i] = row.info
		}
		p.layout = newTableLayout(infos, width, unlistedNameWidth)
	}
	return p.layout
}

// rowLocked is the row for name. A repo SetRepos did not list (a renderer
// used without it) gets a row of its own, so the table still names it. The
// caller holds p.mu.
func (p *tableProgress) rowLocked(name string) *tableRow {
	if row, ok := p.byName[name]; ok {
		return row
	}
	if p.byName == nil {
		p.byName = map[string]*tableRow{}
	}
	row := &tableRow{info: ghapi.RepoInfo{Name: name}}
	p.rows = append(p.rows, row)
	p.byName[name] = row
	return row
}

// Start prints the table's top, the first time after SetRepos, and draws the
// live area for the repos of one pass.
func (p *tableProgress) Start(total int) {
	// Hiding the cursor goes through pterm's default cursor, which writes to
	// os.Stdout unless told otherwise.
	cursor.SetTarget(p.out)
	cursor.Hide()
	// Stop erases the area, the same call whether or not a table follows it.
	area := pterm.AreaPrinter{}.WithRemoveWhenDone(true)
	area.SetWriter(p.out)

	quit, repainted := make(chan struct{}), make(chan struct{})
	p.mu.Lock()
	if p.began.IsZero() {
		p.began = time.Now()
	}
	p.printTopLocked()
	p.area = area
	p.quit, p.repainted = quit, repainted
	_, _ = area.Start(p.frameLocked(time.Now()))
	p.mu.Unlock()

	go p.repaint(quit, repainted)
}

// repaint redraws the live area until quit closes, which is what moves the
// spinners and the running times while no repo finishes.
func (p *tableProgress) repaint(quit <-chan struct{}, repainted chan<- struct{}) {
	defer close(repainted)
	ticker := time.NewTicker(repaintEvery)
	defer ticker.Stop()
	for {
		select {
		case <-quit:
			return
		case now := <-ticker.C:
			p.mu.Lock()
			if p.area != nil {
				p.area.Update(p.frameLocked(now))
			}
			p.mu.Unlock()
		}
	}
}

// RepoStarted marks the repo as in flight.
func (p *tableProgress) RepoStarted(name, action string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.area == nil {
		return
	}
	row := p.rowLocked(name)
	row.state, row.verb, row.started = rowRunning, progressVerb(action), time.Now()
}

// RepoDone records the repo's result and prints, above the live area, every
// row it no longer holds back: the run of finished rows from the first one not
// yet printed. A repo that finishes ahead of one listed before it stays in the
// live area until that one is done.
func (p *tableProgress) RepoDone(r cloner.RepoResult, elapsed time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.area == nil {
		return
	}
	row := p.rowLocked(r.Name)
	row.state, row.action, row.elapsed = rowDone, r.Action, elapsed

	from := p.printed
	for p.printed < len(p.rows) && p.rows[p.printed].state == rowDone {
		p.printed++
	}
	now := time.Now()
	if p.printed == from {
		p.area.Update(p.frameLocked(now))
		return
	}
	// Clearing the area leaves the cursor where it began, so the rows take its
	// place and the area starts again beneath them.
	p.area.Clear()
	p.printLocked(p.rowLinesLocked(p.rows[from:p.printed], now)...)
	_, _ = p.area.Start(p.frameLocked(now))
}

// Stop erases the live area and gives the terminal back, with the cursor at
// the start of the line the area began on. A repo still running (a cancelled
// sync) is marked interrupted.
func (p *tableProgress) Stop() {
	p.mu.Lock()
	quit, repainted := p.quit, p.repainted
	p.quit = nil
	p.mu.Unlock()
	if quit == nil {
		return
	}
	// The repaint goroutine takes p.mu, so wait for it without holding it.
	close(quit)
	<-repainted

	p.mu.Lock()
	defer p.mu.Unlock()
	_ = p.area.Stop()
	cursor.Show()
	cursor.SetTarget(os.Stdout)
	p.stopped = time.Now()
	for _, row := range p.rows {
		if row.state == rowRunning {
			row.state = rowInterrupted
		}
	}
	p.area = nil
}

// FinishTable closes the table, once per SetRepos: the top if no pass ever
// started, every row not printed yet in list order (finished but held back,
// interrupted, or queued), then the bottom border with the totals. It does
// nothing when SetRepos has not been called, a live area is still on screen,
// or the table is closed.
func (p *tableProgress) FinishTable() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.listed || p.closed || p.area != nil {
		return
	}
	p.closed = true
	p.printTopLocked()
	l := p.layoutLocked()
	lines := p.rowLinesLocked(p.rows[p.printed:], time.Now())
	p.printed = len(p.rows)
	// With no rows at all the header's separator is the line above the footer,
	// and a rule would be a second one.
	if len(p.rows) > 0 {
		lines = append(lines, l.rule)
	}
	lines = append(lines, l.footerLines(p.totalsLocked())...)
	lines = append(lines, l.bottom)
	p.printLocked(lines...)
}

// printTopLocked prints the table's title and header, once per SetRepos,
// after a blank line that sets the table off from what came before. The
// caller holds p.mu.
func (p *tableProgress) printTopLocked() {
	if p.topPrinted {
		return
	}
	p.topPrinted = true
	_, _ = fmt.Fprintln(p.out)
	p.printLocked(p.layoutLocked().head...)
}

// printLocked writes lines to out as ordinary output, each cut to the
// terminal's width so none wraps. The caller holds p.mu.
func (p *tableProgress) printLocked(lines ...string) {
	width, _ := p.size()
	var b strings.Builder
	for _, line := range lines {
		b.WriteString(trimLine(line, width))
		b.WriteByte('\n')
	}
	_, _ = fmt.Fprint(p.out, b.String())
}

// rowLinesLocked is the table line of each row, as it stands at now. The
// caller holds p.mu.
func (p *tableProgress) rowLinesLocked(rows []*tableRow, now time.Time) []string {
	repos, status := make([]ghapi.RepoInfo, len(rows)), make([]string, len(rows))
	for i, row := range rows {
		repos[i], status[i] = row.info, row.status(now)
	}
	return p.layoutLocked().rows(repos, status)
}

// status is the repo's Status cell.
func (r *tableRow) status(now time.Time) string {
	switch r.state {
	case rowRunning:
		frame := spinFrames[int(now.UnixMilli()/100)%len(spinFrames)]
		return fmt.Sprintf("%s %s %s", frame, r.verb, formatSeconds(now.Sub(r.started)))
	case rowDone:
		symbol, color := statusMark(r.action)
		return color.Sprint(symbol) + " " + actionText(r.action, r.elapsed)
	case rowInterrupted:
		return text.Faint.Sprint("interrupted")
	default:
		return text.Faint.Sprint("queued")
	}
}

// countsLocked is how many repos are done, running and queued. The caller
// holds p.mu.
func (p *tableProgress) countsLocked() (done, running, queued int) {
	for _, row := range p.rows {
		switch row.state {
		case rowDone:
			done++
		case rowRunning:
			running++
		case rowQueued:
			queued++
		}
	}
	return done, running, queued
}

// progressLocked is the text of the live footer, counted over every repo the
// table lists, across both passes; more is how many in-flight rows the frame
// left out, and held how many finished rows it left out. The caller holds p.mu.
func (p *tableProgress) progressLocked(now time.Time, more, held int) string {
	done, running, queued := p.countsLocked()
	var elapsed time.Duration
	if !p.began.IsZero() {
		elapsed = now.Sub(p.began)
	}
	var suffix string
	bar := p.layoutLocked().barWidth(len(p.rows))
	if more > 0 {
		suffix += fmt.Sprintf(" · +%d more running", more)
	}
	if held > 0 {
		// Rows are held only behind an unfinished one, so rows[p.printed] is
		// the repo they wait on.
		suffix += fmt.Sprintf(" · +%d done, waiting on %s", held, p.rows[p.printed].info.Name)
	}
	if suffix != "" {
		// The rare footer that says the frame left rows out gives the words
		// their room out of the bar's.
		if bar -= text.StringWidthWithoutEscSequences(suffix); bar < footerBarMin {
			bar = 0
		}
	}
	return progressText(done, len(p.rows), running, queued, bar, elapsed) + suffix
}

// totalsLocked is the text of the footer that closes the table, one class per
// Status mark: "60 repos · ✓ 3 cloned · ↓ 12 pulled · – 40 up to date · ⇣ 2
// fetched only · ⚠ 1 skipped · ∅ 1 empty · ✗ 1 failed · 1m12s". Only the
// cloned count is always shown; the others appear when they are not zero, then
// come the repos that never finished. The parts are returned apart, for a
// footer too long for one line to wrap between them. The caller holds p.mu.
func (p *tableProgress) totalsLocked() []string {
	bySymbol := map[string]int{}
	var interrupted, queued int
	for _, row := range p.rows {
		switch row.state {
		case rowDone:
			symbol, _ := statusMark(row.action)
			bySymbol[symbol]++
		case rowInterrupted:
			interrupted++
		case rowQueued, rowRunning:
			queued++
		}
	}
	noun := "repos"
	if len(p.rows) == 1 {
		noun = "repo"
	}
	parts := []string{fmt.Sprintf("%d %s", len(p.rows), noun)}
	// One class per symbol, with an action that stands for it.
	for _, c := range []struct {
		action cloner.Action
		label  string
		always bool
	}{
		{cloner.ActionCloned, "%d cloned", true},
		{cloner.ActionPulled, "%d pulled", false},
		{cloner.ActionUpToDate, "%d up to date", false},
		{cloner.ActionFetchedOnly, "%d fetched only", false},
		{cloner.ActionSkippedName, "%d skipped", false},
		{cloner.ActionSkippedEmpty, "%d empty", false},
		{cloner.ActionFailed, "%d failed", false},
	} {
		symbol, color := statusMark(c.action)
		if n := bySymbol[symbol]; n > 0 || c.always {
			parts = append(parts, color.Sprint(symbol)+" "+fmt.Sprintf(c.label, n))
		}
	}
	if interrupted > 0 {
		parts = append(parts, fmt.Sprintf("%d interrupted", interrupted))
	}
	if queued > 0 {
		parts = append(parts, fmt.Sprintf("%d queued", queued))
	}
	var elapsed time.Duration
	if !p.began.IsZero() && !p.stopped.IsZero() {
		elapsed = p.stopped.Sub(p.began)
	}
	parts = append(parts, formatElapsed(elapsed))
	return parts
}

// frameLocked is the live area: the rows not printed yet that are in flight or
// finished, in list order, then a rule, the progress footer and the closing
// border, each line ending in a newline. It is never taller than the terminal
// less two lines, nor wider than the terminal. When the rows don't fit, the
// finished ones are left out first, then the last in-flight ones, and the
// footer counts both. The caller holds p.mu.
func (p *tableProgress) frameLocked(now time.Time) string {
	width, height := p.size()
	l := p.layoutLocked()

	var live, running []*tableRow
	for _, row := range p.rows[p.printed:] {
		switch row.state {
		case rowRunning:
			running = append(running, row)
			live = append(live, row)
		case rowDone:
			live = append(live, row)
		}
	}

	// Besides the rows: the rule, the footer and the closing border. With no
	// room even for those (a terminal only a few lines tall) the footer stands
	// alone.
	room := height - 2 - 3
	var lines []string
	if room >= 0 {
		shown, held := live, 0
		if len(live) > room {
			// Every in-flight row is moving, and a held row only waits, so
			// the held ones give way first.
			shown, held = running[:min(len(running), room)], len(live)-len(running)
		}
		more := len(live) - len(shown) - held
		lines = p.rowLinesLocked(shown, now)
		// The rule closes the columns of the rows above it. With no row above
		// (the header's separator is the line before), a rule would be a second
		// separator in a row.
		if p.printed > 0 || len(shown) > 0 {
			lines = append(lines, l.rule)
		}
		lines = append(lines, l.footer(p.progressLocked(now, more, held)), l.bottom)
	} else {
		lines = []string{l.footer(p.progressLocked(now, 0, 0))}
	}

	var b strings.Builder
	for _, line := range lines {
		b.WriteString(trimLine(line, width))
		b.WriteByte('\n')
	}
	return b.String()
}

// trimLine cuts line to width columns, keeping its escape sequences.
func trimLine(line string, width int) string {
	if text.StringWidthWithoutEscSequences(line) <= width {
		return line
	}
	return text.Trim(line, width)
}
