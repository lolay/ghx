package ui

import (
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"

	"github.com/lolay/ghx/internal/cloner"
)

// indicatifWidth is the bar's width in cells, as indicatif's default is 40.
const indicatifWidth = 40

// indicatifProgress draws one line, redrawn in place, in the manner of Rust's
// indicatif: a spinner, the title, a bar, the percentage, the elapsed time
// and the repo that started last.
type indicatifProgress struct {
	out io.Writer

	mu   sync.Mutex
	p    *mpb.Progress
	bar  *mpb.Bar
	last string // the repo most recently started
}

func newIndicatifProgress(out io.Writer) *indicatifProgress {
	return &indicatifProgress{out: out}
}

// Start draws the line for total repos.
func (i *indicatifProgress) Start(total int) {
	p := mpb.New(
		mpb.WithOutput(i.out),
		mpb.WithAutoRefresh(),
		mpb.WithRefreshRate(100*time.Millisecond),
	)
	start := time.Now()
	var frozen string // read only by the bar's own goroutine
	bar := p.New(int64(total),
		mpb.BarStyle().Lbound("[").Filler("=").Tip(">").Padding("-").Rbound("]"),
		mpb.BarWidth(indicatifWidth),
		mpb.PrependDecorators(
			decor.OnAbort(decor.OnComplete(decor.Spinner(nil), text.FgGreen.Sprint("✓")), text.FgYellow.Sprint("!")),
			decor.Name(" "+text.Bold.Sprint("Syncing repos")),
		),
		mpb.AppendDecorators(
			decor.Any(func(s decor.Statistics) string {
				if s.Total <= 0 {
					return "0%"
				}
				return fmt.Sprintf("%d%%", s.Current*100/s.Total)
			}),
			// The clock stops with the bar, so the finished line keeps its time.
			decor.Any(func(s decor.Statistics) string {
				if frozen != "" {
					return frozen
				}
				shown := " (" + time.Since(start).Truncate(time.Second).String() + ")"
				if s.Completed || s.Aborted {
					frozen = shown
				}
				return shown
			}),
			decor.Any(func(decor.Statistics) string {
				i.mu.Lock()
				defer i.mu.Unlock()
				if i.last == "" {
					return ""
				}
				return " " + text.FgCyan.Sprint(i.last)
			}),
		),
	)

	i.mu.Lock()
	defer i.mu.Unlock()
	i.p, i.bar, i.last = p, bar, ""
}

// RepoStarted names the repo on the line.
func (i *indicatifProgress) RepoStarted(name, _ string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.last = name
}

// RepoDone advances the bar by one repo.
func (i *indicatifProgress) RepoDone(cloner.RepoResult, time.Duration) {
	i.mu.Lock()
	bar := i.bar
	i.mu.Unlock()
	if bar != nil {
		bar.Increment()
	}
}

// Stop leaves the line as it stands (a cancelled sync isn't full) and waits
// for the last frame to be written.
func (i *indicatifProgress) Stop() {
	i.mu.Lock()
	p, bar := i.p, i.bar
	i.p, i.bar = nil, nil
	i.mu.Unlock()
	if p == nil {
		return
	}
	if !bar.Completed() {
		bar.Abort(false)
	}
	p.Wait()
}
