package ui

import (
	"io"
	"sync"
	"time"

	"github.com/jedib0t/go-pretty/v6/progress"

	"github.com/lolay/ghx/internal/cloner"
)

// goPrettyProgress draws go-pretty's single overall tracker: the bar ghx has
// always shown.
type goPrettyProgress struct {
	out io.Writer

	mu       sync.Mutex
	tracker  *progress.Tracker
	rendered chan struct{} // closed when the render goroutine has returned
}

func newGoPrettyProgress(out io.Writer) *goPrettyProgress {
	return &goPrettyProgress{out: out}
}

// Start draws the bar for total repos.
func (g *goPrettyProgress) Start(total int) {
	pw := progress.NewWriter()
	pw.SetOutputWriter(g.out)
	pw.SetAutoStop(true)
	pw.SetUpdateFrequency(100 * time.Millisecond)
	pw.SetTrackerLength(25)
	pw.SetNumTrackersExpected(1)
	pw.Style().Visibility.ETA = false
	pw.Style().Visibility.ETAOverall = false

	tracker := &progress.Tracker{
		Message: "Syncing repos",
		Total:   int64(total),
		Units:   progress.UnitsDefault,
	}
	pw.AppendTracker(tracker)

	rendered := make(chan struct{})
	g.mu.Lock()
	g.tracker = tracker
	g.rendered = rendered
	g.mu.Unlock()
	go func() {
		defer close(rendered)
		pw.Render()
	}()
}

// RepoStarted does nothing: this bar counts finished repos only.
func (g *goPrettyProgress) RepoStarted(string, string) {}

// RepoDone advances the bar by one repo.
func (g *goPrettyProgress) RepoDone(cloner.RepoResult, time.Duration) {
	g.mu.Lock()
	tracker := g.tracker
	g.mu.Unlock()
	if tracker != nil {
		tracker.Increment(1)
	}
}

// Stop shows the final frame and waits for the renderer to finish, so the
// cursor is on a fresh line when it returns. A bar that isn't full (the sync
// was cancelled) is marked errored, which also ends go-pretty's render loop.
func (g *goPrettyProgress) Stop() {
	g.mu.Lock()
	tracker, rendered := g.tracker, g.rendered
	g.mu.Unlock()
	if tracker == nil {
		return
	}
	if !tracker.IsDone() {
		tracker.MarkAsErrored()
	}
	<-rendered
}
