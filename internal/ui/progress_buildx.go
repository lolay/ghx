package ui

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"

	"github.com/lolay/ghx/internal/cloner"
)

// overallPriority sorts the overall bar below every repo line: mpb draws the
// lowest priority number first.
const overallPriority = 1 << 30

// buildxProgress draws like `docker buildx build`: one live line per repo in
// flight (a spinner, the name, what it is doing, its own elapsed time), the
// overall bar at the bottom, and a log line for each repo that finishes,
// printed above the bars where it stays.
type buildxProgress struct {
	out io.Writer

	mu      sync.Mutex
	p       *mpb.Progress
	overall *mpb.Bar
	live    map[string]*mpb.Bar // repo name to its line, while in flight
	seq     int                 // the next repo line's priority
}

func newBuildxProgress(out io.Writer) *buildxProgress {
	return &buildxProgress{out: out}
}

// Start draws the overall bar for total repos.
func (b *buildxProgress) Start(total int) {
	// WithAutoRefresh makes mpb redraw on a timer whether or not out is a
	// terminal it recognises, so a test can read the frames from a buffer.
	p := mpb.New(
		mpb.WithOutput(b.out),
		mpb.WithAutoRefresh(),
		mpb.WithRefreshRate(100*time.Millisecond),
	)
	overall := p.New(int64(total),
		mpb.BarStyle(),
		mpb.BarPriority(overallPriority),
		mpb.PrependDecorators(
			decor.Name(text.Bold.Sprint("Syncing repos")+" "),
			decor.CountersNoUnit("%d/%d", decor.WCSyncWidth),
		),
		mpb.AppendDecorators(
			decor.Percentage(decor.WCSyncWidth),
			decor.Name(" "),
			decor.Elapsed(decor.ET_STYLE_GO),
		),
	)

	b.mu.Lock()
	defer b.mu.Unlock()
	b.p, b.overall = p, overall
	b.live = map[string]*mpb.Bar{}
	b.seq = 0
}

// RepoStarted adds the repo's live line.
func (b *buildxProgress) RepoStarted(name, action string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.p == nil {
		return
	}
	start := time.Now()
	verb := map[string]string{cloner.ProgressClone: "cloning", cloner.ProgressPull: "pulling"}[action]
	// Total 0 means unknown: the line stays until RepoDone aborts it. The nop
	// filler draws nothing, so only the decorators show.
	bar := b.p.New(0, mpb.NopStyle(),
		mpb.BarPriority(b.seq),
		mpb.BarRemoveOnComplete(),
		mpb.PrependDecorators(
			decor.Spinner(nil),
			decor.Name(" "+text.FgCyan.Sprint(name), decor.WCSyncWidthR),
			decor.Name(" "+text.Faint.Sprint(verb)),
		),
		mpb.AppendDecorators(
			decor.Any(func(decor.Statistics) string {
				return text.Faint.Sprint(formatSeconds(time.Since(start)))
			}),
		),
	)
	b.seq++
	b.live[name] = bar
}

// RepoDone removes the repo's live line, prints its log entry above the bars
// and advances the overall bar.
func (b *buildxProgress) RepoDone(r cloner.RepoResult, elapsed time.Duration) {
	b.mu.Lock()
	p, overall, bar := b.p, b.overall, b.live[r.Name]
	delete(b.live, r.Name)
	b.mu.Unlock()
	if p == nil {
		return
	}
	// A write after Stop fails harmlessly; there is no terminal left to fix.
	_, _ = fmt.Fprintln(p, logEntry(r, elapsed))
	if bar != nil {
		bar.Abort(true)
	}
	overall.Increment()
}

// Stop removes any line still in flight, leaves the overall bar as it stands
// (a cancelled sync isn't full) and waits for the last frame to be written.
func (b *buildxProgress) Stop() {
	b.mu.Lock()
	p, overall, live := b.p, b.overall, b.live
	b.p, b.overall, b.live = nil, nil, nil
	b.mu.Unlock()
	if p == nil {
		return
	}
	for _, bar := range live {
		bar.Abort(true)
	}
	if !overall.Completed() {
		overall.Abort(false)
	}
	p.Wait()
}

// logEntry is the line printed when a repo finishes: a mark, the name, what
// happened and, when git ran, how long it took.
func logEntry(r cloner.RepoResult, elapsed time.Duration) string {
	action := string(r.Action)
	switch {
	case r.Action == cloner.ActionFailed:
		return fmt.Sprintf("%s %s %s %s", text.FgRed.Sprint("✗"), r.Name, action, formatSeconds(elapsed))
	case strings.HasPrefix(action, "skipped"):
		return fmt.Sprintf("%s %s %s", text.FgYellow.Sprint("-"), r.Name, action)
	default:
		return fmt.Sprintf("%s %s %s %s", text.FgGreen.Sprint("✓"), r.Name, action, formatSeconds(elapsed))
	}
}
