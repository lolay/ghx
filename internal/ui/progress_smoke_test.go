package ui_test

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lolay/ghx/internal/cloner"
	"github.com/lolay/ghx/internal/ui"
)

// lockedBuffer is a bytes.Buffer safe to write from a renderer's goroutine
// while the test reads it.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// ansiCodes matches the colour and cursor escape sequences a renderer writes.
var ansiCodes = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

// stripANSI removes the escape sequences from s, so a test reads the text a
// terminal would show.
func stripANSI(s string) string { return ansiCodes.ReplaceAllString(s, "") }

// renderers lists the GHX_PROGRESS names the smoke tests cover, and whether
// each also prints a line for a repo that finishes (a one-line bar names only
// the repo that started last).
var renderers = []struct {
	name     string
	logsDone bool
}{
	{"buildx", true},
	{"indicatif", false},
	{"pterm", true},
	{"table", true},
}

// useRenderer makes ui.NewProgress return the named renderer, drawing into
// the returned buffer.
func useRenderer(t *testing.T, name string) (cloner.Progress, *lockedBuffer) {
	t.Helper()
	var buf lockedBuffer
	prev := ui.Out
	ui.Out = &buf
	t.Cleanup(func() { ui.Out = prev })
	t.Cleanup(ui.ResetTerminal)
	ui.ResetTerminal()
	t.Setenv("GHX_PROGRESS", name)
	return ui.NewProgress(), &buf
}

// waitFor blocks until the renderer has drawn name. A one-line renderer shows
// only the repo that started last, so the test starts repos one at a time and
// waits for each to be drawn before the next.
func waitFor(t *testing.T, buf *lockedBuffer, name string) {
	t.Helper()
	require.Eventually(t, func() bool { return strings.Contains(buf.String(), name) },
		10*time.Second, 5*time.Millisecond, "%s was never drawn", name)
}

// within fails the test if fn doesn't return in time: a renderer that
// deadlocks must not hang the suite.
func within(t *testing.T, fn func()) {
	t.Helper()
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		fn()
	}()
	select {
	case <-finished:
	case <-time.After(20 * time.Second):
		require.FailNow(t, "the renderer deadlocked")
	}
}

// TestRenderers_SurviveConcurrentCallsAndNameEveryRepo drives each renderer
// as CloneRepos does: Start, then RepoStarted and RepoDone from several
// goroutines at once, then Stop. It must return, and the output must name
// every repo.
func TestRenderers_SurviveConcurrentCallsAndNameEveryRepo(t *testing.T) {
	results := []cloner.Action{
		cloner.ActionCloned, cloner.ActionPulled, cloner.ActionFailed, cloner.ActionSkippedDirty,
	}
	for _, r := range renderers {
		t.Run(r.name, func(t *testing.T) {
			p, buf := useRenderer(t, r.name)
			const repos = 12

			within(t, func() {
				p.Start(repos)
				var starting sync.Mutex
				var wg sync.WaitGroup
				for i := range repos {
					wg.Add(1)
					go func() {
						defer wg.Done()
						repo := fmt.Sprintf("repo-%02d", i)
						action := cloner.ProgressPull
						if i%2 == 0 {
							action = cloner.ProgressClone
						}
						starting.Lock()
						p.RepoStarted(repo, action)
						waitFor(t, buf, repo)
						starting.Unlock()
						time.Sleep(time.Duration(i%4) * 60 * time.Millisecond)
						p.RepoDone(cloner.RepoResult{Name: repo, Action: results[i%len(results)]}, time.Duration(i)*time.Millisecond)
					}()
				}
				wg.Wait()
				p.Stop()
			})

			out := buf.String()
			for i := range repos {
				assert.Contains(t, out, fmt.Sprintf("repo-%02d", i))
			}
		})
	}
}

// TestRenderers_ToleratePartialRuns covers what a cancelled sync and an
// unusable repo name do to a renderer: a RepoDone with no RepoStarted, a
// RepoStarted with no RepoDone, a Stop before the count is reached, a Stop
// with nothing started, and a second sync on the same value.
func TestRenderers_ToleratePartialRuns(t *testing.T) {
	for _, r := range renderers {
		t.Run(r.name, func(t *testing.T) {
			p, buf := useRenderer(t, r.name)

			within(t, func() {
				p.Start(5)
				p.RepoDone(cloner.RepoResult{Name: "bad-name", Action: cloner.ActionSkippedName}, 0)
				p.RepoStarted("quick", cloner.ProgressClone)
				waitFor(t, buf, "quick")
				p.RepoDone(cloner.RepoResult{Name: "quick", Action: cloner.ActionCloned}, time.Millisecond)
				p.RepoStarted("stuck", cloner.ProgressPull)
				waitFor(t, buf, "stuck")
				p.Stop()

				p.Start(1)
				p.Stop()
			})

			if r.logsDone {
				assert.Contains(t, buf.String(), "bad-name", "a repo that finished without starting is still logged")
			}
		})
	}
}
