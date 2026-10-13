package ui

import (
	"fmt"
	"io"
	"os"
	"slices"
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

// columnDrops is the order the table gives up columns, by index into
// repoColumns, while it is wider than the terminal.
var columnDrops = []int{columnPushed, columnBranch, columnArchived, columnLanguage}

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
// output. Each repo that finishes prints its row, in the order repos finish,
// above the live area, which holds only what is still moving: the in-flight
// rows with a spinner and a running time, and a footer row spanning the table
// with the progress, "Syncing 34/60 [=====>---] 57% 12s · 4 running · 22
// queued", counted over every repo the table lists, across both passes. The
// live area is never taller than the terminal; with more repos in flight than
// fit it shows the first ones and "+N more running" in the footer. FinishTable
// closes the table: any row never finished, then the bottom border and a
// footer with the totals.
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
// It prints a finished row the way the pterm renderer prints a log line: clear
// the area, print, start the area again. A pterm AreaPrinter writes to out,
// pterm's switches stay untouched, and the cursor target (the one pterm global
// needed) points at out only between Start and Stop. Every method is safe for
// concurrent use.
type tableProgress struct {
	out  fdWriter
	size func() (width, height int) // read on every repaint, so a resize shows

	mu         sync.Mutex
	rows       []*tableRow
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
	ticker := time.NewTicker(ptermRepaint)
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

// RepoDone records the repo's result and prints its row above the live area.
func (p *tableProgress) RepoDone(r cloner.RepoResult, elapsed time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.area == nil {
		return
	}
	row := p.rowLocked(r.Name)
	row.state, row.action, row.elapsed = rowDone, r.Action, elapsed

	// Clearing the area leaves the cursor where it began, so the row takes its
	// place and the area starts again beneath it.
	now := time.Now()
	p.area.Clear()
	p.printLocked(p.rowLinesLocked([]*tableRow{row}, now)...)
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
// started, a row for each repo that never finished (queued, or interrupted),
// then the bottom border with the totals. It does nothing when SetRepos has
// not been called, a live area is still on screen, or the table is closed.
func (p *tableProgress) FinishTable() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.listed || p.closed || p.area != nil {
		return
	}
	p.closed = true
	p.printTopLocked()
	var unfinished []*tableRow
	for _, row := range p.rows {
		if row.state != rowDone {
			unfinished = append(unfinished, row)
		}
	}
	l := p.layoutLocked()
	lines := p.rowLinesLocked(unfinished, time.Now())
	// With no rows at all the header's separator is the line above the footer,
	// and a rule would be a second one.
	if len(p.rows) > 0 {
		lines = append(lines, l.rule)
	}
	lines = append(lines, l.footer(p.totalsLocked()), l.bottom)
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
// left out. The caller holds p.mu.
func (p *tableProgress) progressLocked(now time.Time, more int) string {
	done, running, queued := p.countsLocked()
	var elapsed time.Duration
	if !p.began.IsZero() {
		elapsed = now.Sub(p.began)
	}
	var suffix string
	bar := p.layoutLocked().barWidth(len(p.rows))
	if more > 0 {
		// The rare footer that says more is in flight than the frame shows
		// gives the words their room out of the bar's.
		suffix = fmt.Sprintf(" · +%d more running", more)
		if bar -= text.StringWidthWithoutEscSequences(suffix); bar < footerBarMin {
			bar = 0
		}
	}
	return progressText(done, len(p.rows), running, queued, bar, elapsed) + suffix
}

// totalsLocked is the text of the footer that closes the table: "60 repos ·
// ✓ 52 · ⚠ 5 not updated · ∅ 1 empty · ✗ 2 failed · 1m12s", with a count of
// the repos that never finished when there are any. "Not updated" is every
// repo marked ⚠: fetched only, or skipped. The caller holds p.mu.
func (p *tableProgress) totalsLocked() string {
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
		{cloner.ActionCloned, "%d", true},
		{cloner.ActionFetchedOnly, "%d not updated", false},
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
	return strings.Join(parts, " · ")
}

// frameLocked is the live area: the in-flight rows, a rule, the progress
// footer and the closing border, each line ending in a newline. It is never taller than the terminal
// less two lines, nor wider than the terminal. With too many repos in flight
// for that, it shows the first ones and counts the rest in the footer. The
// caller holds p.mu.
func (p *tableProgress) frameLocked(now time.Time) string {
	width, height := p.size()
	l := p.layoutLocked()

	var running []*tableRow
	for _, row := range p.rows {
		if row.state == rowRunning {
			running = append(running, row)
		}
	}
	slices.SortStableFunc(running, func(a, b *tableRow) int { return a.started.Compare(b.started) })

	// Besides the rows: the rule, the footer and the closing border. With no
	// room even for those (a terminal only a few lines tall) the footer stands
	// alone.
	room := height - 2 - 3
	var lines []string
	if room >= 0 {
		shown := running[:min(len(running), room)]
		lines = p.rowLinesLocked(shown, now)
		// The rule closes the columns of the rows above it. With no row above
		// (the header's separator is the line before), a rule would be a second
		// separator in a row.
		if done, _, _ := p.countsLocked(); done > 0 || len(shown) > 0 {
			lines = append(lines, l.rule)
		}
		lines = append(lines, l.footer(p.progressLocked(now, len(running)-len(shown))), l.bottom)
	} else {
		lines = []string{l.footer(p.progressLocked(now, 0))}
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
