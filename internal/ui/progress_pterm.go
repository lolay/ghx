package ui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"atomicgo.dev/cursor"
	"github.com/pterm/pterm"

	"github.com/lolay/ghx/internal/cloner"
)

// ptermRepaint is how often the live area is redrawn, which is what moves the
// spinner and the elapsed time while no repo finishes.
const ptermRepaint = 100 * time.Millisecond

// ptermShown is how many in-flight repos the spinner line names before it
// says "+N more", so the line stays short of the terminal's width.
const ptermShown = 3

// ptermProgress draws pterm's default look: a progress bar for the overall
// count under a spinner naming the repos in flight, and one SUCCESS, ERROR or
// WARNING line above them for each repo that finishes.
//
// pterm's MultiPrinter can't print a line above its area, so the bar and the
// spinner each write into a buffer of their own, and a pterm AreaPrinter
// draws the last frame of each, which lets a log line go above it: clear the
// area, print, start the area again.
//
// pterm keeps state of its own. Nothing here touches its colour, output or
// raw-output switches, and the one global it needs (where the progress bar
// hides and shows the cursor) points at out only between Start and Stop.
type ptermProgress struct {
	out fdWriter // what pterm writes to: its cursor code wants a file descriptor

	mu        sync.Mutex
	area      *pterm.AreaPrinter
	bar       *pterm.ProgressbarPrinter
	barFrame  *frameBuffer
	spinner   *pterm.SpinnerPrinter
	spinFrame *frameBuffer
	flight    []inFlight
	quit      chan struct{} // closed by Stop to end the repaint goroutine
	repainted chan struct{} // closed when that goroutine has returned
}

// inFlight is a repo being worked on.
type inFlight struct{ name, verb string }

func newPtermProgress(out io.Writer) *ptermProgress {
	return &ptermProgress{out: fdWriter{out}}
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

// frameBuffer keeps the last line a pterm live printer wrote. Those printers
// redraw by writing "\r" and the whole line again, from goroutines of their
// own, so only what follows the last "\r" is the current frame.
type frameBuffer struct {
	mu  sync.Mutex
	cur string
}

func (f *frameBuffer) Write(p []byte) (int, error) {
	s := string(p)
	f.mu.Lock()
	defer f.mu.Unlock()
	if i := strings.LastIndex(s, "\r"); i >= 0 {
		f.cur = s[i+1:]
	} else {
		f.cur += s
	}
	return len(p), nil
}

// line is the current frame without the erase-line code or line end the
// printers add.
func (f *frameBuffer) line() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.TrimRight(strings.TrimPrefix(f.cur, "\x1b[K"), "\r\n")
}

// Start draws the bar for total repos and the spinner under it.
func (p *ptermProgress) Start(total int) {
	barFrame, spinFrame := &frameBuffer{}, &frameBuffer{}
	// The progress bar hides the cursor through pterm's default cursor, which
	// writes to os.Stdout unless told otherwise.
	cursor.SetTarget(p.out)

	bar, _ := pterm.DefaultProgressbar.
		WithTotal(total).
		WithTitle("Syncing repos").
		WithWriter(barFrame).
		Start()
	spinner, _ := pterm.DefaultSpinner.
		WithWriter(spinFrame).
		Start(idleText)
	area := &pterm.AreaPrinter{}
	area.SetWriter(p.out)

	quit, repainted := make(chan struct{}), make(chan struct{})
	p.mu.Lock()
	p.area, p.bar, p.barFrame, p.spinner, p.spinFrame = area, bar, barFrame, spinner, spinFrame
	p.flight, p.quit, p.repainted = nil, quit, repainted
	_, _ = area.Start(p.contentLocked())
	p.mu.Unlock()

	go p.repaint(quit, repainted)
}

// idleText is the spinner's text while no repo is in flight.
const idleText = "waiting for a worker"

// repaint redraws the live area until quit closes.
func (p *ptermProgress) repaint(quit <-chan struct{}, repainted chan<- struct{}) {
	defer close(repainted)
	ticker := time.NewTicker(ptermRepaint)
	defer ticker.Stop()
	for {
		select {
		case <-quit:
			return
		case <-ticker.C:
			p.mu.Lock()
			if p.area != nil {
				p.area.Update(p.contentLocked())
			}
			p.mu.Unlock()
		}
	}
}

// contentLocked is the live area: the spinner above the bar. The caller holds p.mu.
func (p *ptermProgress) contentLocked() string {
	return p.spinFrame.line() + "\n" + p.barFrame.line() + "\n"
}

// RepoStarted adds the repo to the spinner's line.
func (p *ptermProgress) RepoStarted(name, action string) {
	verb := map[string]string{cloner.ProgressClone: "cloning", cloner.ProgressPull: "pulling"}[action]
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.area == nil {
		return
	}
	p.flight = append(p.flight, inFlight{name: name, verb: verb})
	p.spinner.UpdateText(p.flightTextLocked())
}

// RepoDone prints the repo's entry above the live area, takes it off the
// spinner's line and advances the bar.
func (p *ptermProgress) RepoDone(r cloner.RepoResult, elapsed time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.area == nil {
		return
	}
	for i, f := range p.flight {
		if f.name == r.Name {
			p.flight = append(p.flight[:i], p.flight[i+1:]...)
			break
		}
	}
	p.bar.Add(1)
	p.spinner.UpdateText(p.flightTextLocked())

	// Clearing the area leaves the cursor where it began, so the entry takes
	// its place and the area starts again beneath it.
	p.area.Clear()
	printEntry(p.out, r, elapsed)
	_, _ = p.area.Start(p.contentLocked())
}

// printEntry writes the line for a finished repo in pterm's own look.
func printEntry(w io.Writer, r cloner.RepoResult, elapsed time.Duration) {
	action := string(r.Action)
	switch {
	case r.Action == cloner.ActionFailed:
		pterm.Error.WithWriter(w).Println(fmt.Sprintf("%s %s %s", r.Name, action, formatSeconds(elapsed)))
	case strings.HasPrefix(action, "skipped"):
		pterm.Warning.WithWriter(w).Println(fmt.Sprintf("%s %s", r.Name, action))
	default:
		pterm.Success.WithWriter(w).Println(fmt.Sprintf("%s %s %s", r.Name, action, formatSeconds(elapsed)))
	}
}

// flightTextLocked is the spinner's line. The caller holds p.mu.
func (p *ptermProgress) flightTextLocked() string {
	if len(p.flight) == 0 {
		return idleText
	}
	var parts []string
	for _, f := range p.flight[:min(len(p.flight), ptermShown)] {
		parts = append(parts, f.verb+" "+f.name)
	}
	if more := len(p.flight) - ptermShown; more > 0 {
		parts = append(parts, fmt.Sprintf("+%d more", more))
	}
	return fmt.Sprintf("%d in flight: %s", len(p.flight), strings.Join(parts, ", "))
}

// Stop ends the spinner, leaves the bar as it stands (a cancelled sync isn't
// full) as the last line, and gives the terminal back.
func (p *ptermProgress) Stop() {
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
	_ = p.spinner.Stop()
	_, _ = p.bar.Stop()
	p.area.Update(p.barFrame.line() + "\n")
	_ = p.area.Stop()
	cursor.SetTarget(os.Stdout)
	p.area, p.bar, p.spinner, p.flight = nil, nil, nil, nil
}
