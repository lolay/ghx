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
	seq     int           // the order repos finished in, once done
}

// tableProgress redraws the "Repos to export" table in place, with a Status
// column that follows each repo from queued to cloning or pulling to its
// result, and the progress in a footer row of the table itself. It is a
// RepoLister: the run flow hands it the repos instead of printing the static
// table.
//
// The table's geometry is measured once, when SetRepos gets the repos (see
// tableLayout): every column is as wide as its widest value, so no frame
// changes the table's width, and a terminal resized mid-run keeps it.
//
// While the table is taller than the terminal it draws a window: the header,
// every in-flight row, the most recently finished rows that fit, and a footer
// counting the rows left out. While it is wider, the layout dropped columns,
// last pushed first. No line is ever longer than the terminal, so none wraps
// and the area can be erased by counting lines.
//
// The run flow calls CloneRepos once for the active repos and again for the
// archived ones with this one value, so Start and Stop come in pairs. Each
// Stop erases the live area. The full table, with every row and its status, is
// printed once, as ordinary output, by PrintTable, which the run flow calls
// after the last pass (also on an error or a cancel, which leave rows
// queued). Printing it from Stop would need to know whether another pass
// follows, and a renderer can't.
//
// It draws the same way the pterm renderer does: a pterm AreaPrinter writing
// to out, pterm's switches untouched, and the cursor target (the one pterm
// global needed) pointing at out only between Start and Stop. Every method is
// safe for concurrent use.
type tableProgress struct {
	out  fdWriter
	size func() (width, height int) // read on every repaint, so a resize shows

	mu        sync.Mutex
	rows      []*tableRow
	byName    map[string]*tableRow
	layout    *tableLayout
	listed    bool      // SetRepos was called
	printed   bool      // PrintTable has printed since SetRepos
	finished  int       // how many repos have finished, for tableRow.seq
	began     time.Time // the first Start since SetRepos
	area      *pterm.AreaPrinter
	quit      chan struct{} // closed by Stop to end the repaint goroutine
	repainted chan struct{} // closed when that goroutine has returned
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
	p.listed, p.printed, p.finished, p.began = true, false, 0, time.Time{}
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

// Start draws the table for the repos of one pass.
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
	p.layoutLocked()
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

// RepoDone records the repo's result.
func (p *tableProgress) RepoDone(r cloner.RepoResult, elapsed time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.area == nil {
		return
	}
	row := p.rowLocked(r.Name)
	p.finished++
	row.state, row.action, row.elapsed, row.seq = rowDone, r.Action, elapsed, p.finished
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
	for _, row := range p.rows {
		if row.state == rowRunning {
			row.state = rowInterrupted
		}
	}
	p.area = nil
}

// PrintTable prints the full table as ordinary output, once per SetRepos.
func (p *tableProgress) PrintTable() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.listed || p.printed || p.area != nil {
		return
	}
	p.printed = true
	width, _ := p.size()
	l := p.layoutLocked()
	repos, status := p.cellsLocked(allIndexes(len(p.rows)), time.Now())
	lines := slices.Concat(l.head, l.rows(repos, status), []string{l.bottom})
	for i, line := range lines {
		lines[i] = trimLine(line, width)
	}
	_, _ = fmt.Fprintf(p.out, "\n%s\n\nTotal: %s repos\n", strings.Join(lines, "\n"), text.Bold.Sprint(len(p.rows)))
}

// cellsLocked is the repos at the given indexes and their Status cells. The
// caller holds p.mu.
func (p *tableProgress) cellsLocked(idx []int, now time.Time) ([]ghapi.RepoInfo, []string) {
	repos, status := make([]ghapi.RepoInfo, len(idx)), make([]string, len(idx))
	for i, n := range idx {
		repos[i], status[i] = p.rows[n].info, p.rows[n].status(now)
	}
	return repos, status
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

// progressLocked is the text of the progress footer, counted over every repo
// the table lists, across both passes. The caller holds p.mu.
func (p *tableProgress) progressLocked(now time.Time) string {
	done, running, queued := p.countsLocked()
	var elapsed time.Duration
	if !p.began.IsZero() {
		elapsed = now.Sub(p.began)
	}
	return progressText(done, len(p.rows), running, queued, p.layoutLocked().barWidth(len(p.rows)), elapsed)
}

// frameLocked is the live area: the table, or a window of it, with the
// progress in its footer, each line ending in a newline. It is never taller
// than the terminal less two lines, nor wider than the terminal. The caller
// holds p.mu.
func (p *tableProgress) frameLocked(now time.Time) string {
	width, height := p.size()
	budget := height - 2
	l := p.layoutLocked()
	progress := l.footer(p.progressLocked(now))
	// Besides the rows: the title and header, the rule, the footer and the
	// closing border.
	fixed := len(l.head) + 3

	var lines []string
	if len(p.rows)+fixed <= budget {
		repos, status := p.cellsLocked(allIndexes(len(p.rows)), now)
		lines = slices.Concat(l.head, l.rows(repos, status), []string{l.rule, progress, l.bottom})
	} else {
		// Room for the rows is what is left after the table's own lines and
		// the window's footer. With none (a terminal only a few lines tall)
		// the table is left out and the footers stand alone.
		room := budget - fixed - 1
		shown, windowFooter := p.windowLocked(room)
		if room >= 0 {
			repos, status := p.cellsLocked(shown, now)
			lines = slices.Concat(l.head, l.rows(repos, status), []string{l.rule})
		}
		if windowFooter != "" {
			lines = append(lines, l.footer(text.Faint.Sprint(windowFooter)))
		}
		lines = append(lines, progress)
		if room >= 0 {
			lines = append(lines, l.bottom)
		}
	}

	var b strings.Builder
	for _, line := range lines {
		b.WriteString(trimLine(line, width))
		b.WriteByte('\n')
	}
	return b.String()
}

// windowLocked picks at most room rows to show: every in-flight row first, then
// the most recently finished, in table order. The footer counts the rows left
// out. The caller holds p.mu.
func (p *tableProgress) windowLocked(room int) (shown []int, footer string) {
	room = max(room, 0)
	var running, finished []int
	for i, row := range p.rows {
		switch row.state {
		case rowRunning:
			running = append(running, i)
		case rowDone:
			finished = append(finished, i)
		}
	}
	// Most recently finished first.
	slices.SortStableFunc(finished, func(a, b int) int { return p.rows[b].seq - p.rows[a].seq })

	shown = append(shown, running[:min(len(running), room)]...)
	shown = append(shown, finished[:min(len(finished), max(room-len(shown), 0))]...)
	slices.Sort(shown)

	var done, queued, flying int
	for i, row := range p.rows {
		if _, ok := slices.BinarySearch(shown, i); ok {
			continue
		}
		switch row.state {
		case rowRunning:
			flying++
		case rowQueued:
			queued++
		default:
			done++
		}
	}
	var parts []string
	for _, c := range []struct {
		n     int
		label string
	}{{done, "done"}, {queued, "queued"}, {flying, "running"}} {
		if c.n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", c.n, c.label))
		}
	}
	if len(parts) > 0 {
		footer = "… " + strings.Join(parts, ", ")
	}
	return shown, footer
}

// allIndexes is 0 up to n-1.
func allIndexes(n int) []int {
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	return idx
}

// trimLine cuts line to width columns, keeping its escape sequences.
func trimLine(line string, width int) string {
	if text.StringWidthWithoutEscSequences(line) <= width {
		return line
	}
	return text.Trim(line, width)
}
