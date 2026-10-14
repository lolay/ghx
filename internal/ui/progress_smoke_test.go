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

// useRenderer makes ui.NewProgress draw into the returned buffer, as it does
// on a terminal, and returns what it gives.
func useRenderer(t *testing.T) (cloner.Progress, *lockedBuffer) {
	t.Helper()
	var buf lockedBuffer
	prev := ui.Out
	ui.Out = &buf
	t.Cleanup(func() { ui.Out = prev })
	t.Cleanup(ui.ResetTerminal)
	ui.ResetTerminal()
	return ui.NewProgress(), &buf
}

// waitFor blocks until the renderer has drawn name, so a test can act on a
// repo that is on screen as in flight.
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

// TestRenderer_SurvivesConcurrentCallsAndNamesEveryRepo drives the renderer
// as CloneRepos does: Start, then RepoStarted and RepoDone from several
// goroutines at once, then Stop. It must return, and the output must name
// every repo.
func TestRenderer_SurvivesConcurrentCallsAndNamesEveryRepo(t *testing.T) {
	results := []cloner.Action{
		cloner.ActionCloned, cloner.ActionPulled, cloner.ActionFailed, cloner.ActionFetchedOnly,
	}
	p, buf := useRenderer(t)
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
}

// TestRenderer_ToleratesPartialRuns covers what a cancelled sync and an
// unusable repo name do to the renderer: a RepoDone with no RepoStarted, a
// RepoStarted with no RepoDone, a Stop before the count is reached, a Stop
// with nothing started, and a second sync on the same value.
func TestRenderer_ToleratesPartialRuns(t *testing.T) {
	p, buf := useRenderer(t)

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

	assert.Contains(t, buf.String(), "bad-name", "a repo that finished without starting is still logged")
}
