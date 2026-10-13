package ui_test

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lolay/ghx/internal/cloner"
	"github.com/lolay/ghx/internal/ghapi"
	"github.com/lolay/ghx/internal/ui"
)

// tableRepos lists n repos named repo-00, repo-01 and so on, every column
// filled in.
func tableRepos(n int) []ghapi.RepoInfo {
	repos := make([]ghapi.RepoInfo, n)
	for i := range repos {
		repos[i] = ghapi.RepoInfo{
			Name:          fmt.Sprintf("repo-%02d", i),
			Language:      "Go",
			SizeKB:        2048,
			DefaultBranch: "main",
			PushedAt:      "2026-10-01T12:00:00Z",
		}
	}
	return repos
}

// newTable returns the table renderer for a terminal width columns wide and
// height lines tall, drawing into the returned buffer, with repos listed.
func newTable(t *testing.T, width, height int, repos []ghapi.RepoInfo) (cloner.Progress, ui.RepoLister, *lockedBuffer) {
	t.Helper()
	var buf lockedBuffer
	p := ui.NewTableProgress(&buf, width, height)
	lister, ok := p.(ui.RepoLister)
	require.True(t, ok, "the table renderer lists repos")
	lister.SetRepos(repos)
	return p, lister, &buf
}

// lastFrame is the live area as drawn last: everything from the last table
// title, with the escape codes removed. A frame in progress is cut short, so
// a test waits for what it expects rather than reading it once.
func lastFrame(buf *lockedBuffer) string {
	out := stripANSI(buf.String())
	return out[max(strings.LastIndex(out, "Repos to export"), 0):]
}

// waitForFrame blocks until the last live frame holds every one of wants.
func waitForFrame(t *testing.T, buf *lockedBuffer, wants ...string) string {
	t.Helper()
	var frame string
	require.Eventually(t, func() bool {
		frame = lastFrame(buf)
		for _, want := range wants {
			if !strings.Contains(frame, want) {
				return false
			}
		}
		return true
	}, 10*time.Second, 5*time.Millisecond, "the live table never showed %q", wants)
	return frame
}

// widest is the width of the widest line of s.
func widest(s string) int { return text.LongestLineLen(stripANSI(s)) }

func TestTableProgress_FinalTableNamesEveryRepoWithItsStatus(t *testing.T) {
	const repos = 12
	results := []cloner.Action{
		cloner.ActionCloned, cloner.ActionPulled, cloner.ActionFailed, cloner.ActionSkippedDirty,
	}
	symbols := map[cloner.Action]string{
		cloner.ActionCloned: "✓", cloner.ActionPulled: "✓", cloner.ActionFailed: "✗", cloner.ActionSkippedDirty: "⚠",
	}
	p, lister, buf := newTable(t, 200, 40, tableRepos(repos))

	within(t, func() {
		p.Start(repos)
		var wg sync.WaitGroup
		for i := range repos {
			wg.Add(1)
			go func() {
				defer wg.Done()
				name := fmt.Sprintf("repo-%02d", i)
				action := cloner.ProgressPull
				if i%2 == 0 {
					action = cloner.ProgressClone
				}
				p.RepoStarted(name, action)
				time.Sleep(time.Duration(i%4) * 30 * time.Millisecond)
				p.RepoDone(cloner.RepoResult{Name: name, Action: results[i%len(results)]}, time.Duration(i+1)*100*time.Millisecond)
			}()
		}
		wg.Wait()
		p.Stop()
	})
	assert.True(t, strings.HasSuffix(stripANSI(buf.String()), "\n"), "Stop leaves the cursor on a fresh line")
	before := len(buf.String())
	lister.PrintTable()

	final := stripANSI(buf.String()[before:])
	assert.Contains(t, final, "Repos to export")
	assert.Contains(t, final, "Total: 12 repos")
	assert.NotContains(t, final, "Syncing repos", "the final table is the table alone")
	lines := strings.Split(final, "\n")
	for i := range repos {
		name := fmt.Sprintf("repo-%02d", i)
		action := results[i%len(results)]
		want := symbols[action] + " " + ui.ActionText(action, time.Duration(i+1)*100*time.Millisecond)
		var row string
		for _, line := range lines {
			if strings.Contains(line, name) {
				row = line
			}
		}
		require.NotEmpty(t, row, "%s is in the final table", name)
		assert.Contains(t, row, want, name)
		assert.Contains(t, row, "Go", "the columns are the static table's")
		assert.Contains(t, row, "2.0", "the size is in MB")
	}
	assert.NotContains(t, final, "queued")

	lister.PrintTable()
	assert.Equal(t, final, stripANSI(buf.String()[before:]), "a second PrintTable adds nothing")
}

func TestTableProgress_ATallListShowsAWindowAndAFooter(t *testing.T) {
	const total, height = 60, 20
	p, _, buf := newTable(t, 120, height, tableRepos(total))

	within(t, func() {
		p.Start(total)
		defer p.Stop()
		// 20 repos finish and 3 are in flight, so 37 are queued.
		for i := range 20 {
			name := fmt.Sprintf("repo-%02d", i)
			p.RepoStarted(name, cloner.ProgressClone)
			p.RepoDone(cloner.RepoResult{Name: name, Action: cloner.ActionCloned}, time.Second)
		}
		for i := 20; i < 23; i++ {
			p.RepoStarted(fmt.Sprintf("repo-%02d", i), cloner.ProgressPull)
		}

		// The window is the in-flight rows and the latest finished ones that
		// fit. Of the 18 lines a 20-line terminal leaves, the table's title,
		// borders and header take 6, the footer and the bar 2, so there is room
		// for 10 rows: 3 running and the last 7 finished.
		frame := waitForFrame(t, buf, "… 13 done, 37 queued")
		assert.Contains(t, frame, "Syncing repos")
		for i := 13; i < 23; i++ {
			assert.Contains(t, frame, fmt.Sprintf("repo-%02d", i))
		}
		for _, hidden := range []string{"repo-00", "repo-12", "repo-23", "repo-59"} {
			assert.NotContains(t, frame, hidden)
		}
		assert.Contains(t, frame, "cloned 1.0s")
		assert.Regexp(t, `pulling \d+\.\ds`, frame)
		assert.LessOrEqual(t, strings.Count(frame, "\n"), height-2, "the frame leaves two lines of the terminal")
	})
}

func TestTableProgress_AShortListShowsEveryRow(t *testing.T) {
	const total, height = 8, 24
	p, _, buf := newTable(t, 120, height, tableRepos(total))

	within(t, func() {
		p.Start(total)
		defer p.Stop()
		p.RepoStarted("repo-00", cloner.ProgressClone)
		frame := waitForFrame(t, buf, "cloning")
		for i := range total {
			assert.Contains(t, frame, fmt.Sprintf("repo-%02d", i))
		}
		assert.NotContains(t, frame, "…", "nothing is left out, so no footer")
		assert.Equal(t, total-1, strings.Count(frame, "queued"))
	})
}

func TestTableProgress_ANarrowTerminalDropsColumnsInOrder(t *testing.T) {
	// go-pretty's light style shows headers in capitals.
	columns := []string{"NAME", "VISIBILITY", "LANGUAGE", "SIZE (MB)", "DEFAULT BRANCH", "ARCHIVED", "LAST PUSHED", "STATUS"}
	tests := []struct {
		name    string
		width   int
		dropped []string
	}{
		{"wide enough keeps every column", 200, nil},
		{"one column short drops last pushed", 0, []string{"LAST PUSHED"}},
		{"then default branch", 0, []string{"LAST PUSHED", "DEFAULT BRANCH"}},
		{"then archived", 0, []string{"LAST PUSHED", "DEFAULT BRANCH", "ARCHIVED"}},
		{"then language", 0, []string{"LAST PUSHED", "DEFAULT BRANCH", "ARCHIVED", "LANGUAGE"}},
	}
	// Each step's width is one less than the table before it needs, found by
	// rendering it at the width of the step before.
	width := 200
	for i := range tests {
		if i > 0 {
			width = renderedWidth(t, width) - 1
			tests[i].width = width
		}
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, run := range []string{"live", "final"} {
				p, lister, buf := newTable(t, tt.width, 40, tableRepos(3))
				var out string
				within(t, func() {
					p.Start(3)
					if run == "live" {
						out = waitForFrame(t, buf, "queued", "Syncing repos")
					}
					p.Stop()
					if run == "final" {
						before := len(buf.String())
						lister.PrintTable()
						out = stripANSI(buf.String()[before:])
					}
				})
				for _, column := range columns {
					if contains(tt.dropped, column) {
						assert.NotContains(t, out, column, "%s table, width %d", run, tt.width)
					} else {
						assert.Contains(t, out, column, "%s table, width %d", run, tt.width)
					}
				}
				assert.LessOrEqual(t, widest(out), tt.width, "no line is wider than the terminal")
			}
		})
	}
}

// renderedWidth is the width of the widest line the table draws on a terminal
// width columns wide.
func renderedWidth(t *testing.T, width int) int {
	t.Helper()
	p, lister, buf := newTable(t, width, 40, tableRepos(3))
	p.Start(3)
	p.Stop()
	before := len(buf.String())
	lister.PrintTable()
	return widest(buf.String()[before:])
}

func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

func TestTableProgress_AVeryNarrowTerminalNeverWrapsALine(t *testing.T) {
	const width = 30
	p, lister, buf := newTable(t, width, 40, tableRepos(3))

	within(t, func() {
		p.Start(3)
		frame := waitForFrame(t, buf, "Syncing")
		assert.LessOrEqual(t, widest(frame), width)
		p.Stop()
		before := len(buf.String())
		lister.PrintTable()
		assert.LessOrEqual(t, widest(buf.String()[before:]), width)
	})
}

func TestTableProgress_ATerminalTooShortForTheTableShowsTheFooterAndTheBar(t *testing.T) {
	p, _, buf := newTable(t, 100, 6, tableRepos(10))

	within(t, func() {
		p.Start(10)
		defer p.Stop()
		frame := waitForFrame(t, buf, "… 10 queued", "Syncing repos")
		assert.NotContains(t, frame, "repo-00")
	})
}

func TestTableProgress_TwoPassesKeepEveryRow(t *testing.T) {
	p, lister, buf := newTable(t, 200, 40, tableRepos(4))

	within(t, func() {
		for _, pass := range [][]string{{"repo-00", "repo-01"}, {"repo-02", "repo-03"}} {
			p.Start(len(pass))
			for _, name := range pass {
				p.RepoStarted(name, cloner.ProgressClone)
				p.RepoDone(cloner.RepoResult{Name: name, Action: cloner.ActionCloned}, time.Second)
			}
			p.Stop()
		}
	})
	before := len(buf.String())
	lister.PrintTable()

	final := stripANSI(buf.String()[before:])
	assert.Equal(t, 4, strings.Count(final, "✓ cloned 1.0s"))
	assert.Contains(t, final, "Total: 4 repos")
	assert.Equal(t, 1, strings.Count(final, "Repos to export"), "the table is printed once")
}

func TestTableProgress_ACancelledRunLeavesRowsQueuedOrInterrupted(t *testing.T) {
	p, lister, buf := newTable(t, 200, 40, tableRepos(3))

	within(t, func() {
		p.Start(3)
		p.RepoStarted("repo-00", cloner.ProgressClone)
		p.RepoDone(cloner.RepoResult{Name: "repo-00", Action: cloner.ActionCloned}, time.Second)
		p.RepoStarted("repo-01", cloner.ProgressPull)
		waitForFrame(t, buf, "pulling")
		p.Stop()
	})
	before := len(buf.String())
	lister.PrintTable()

	final := stripANSI(buf.String()[before:])
	assert.Contains(t, final, "✓ cloned 1.0s")
	assert.Contains(t, final, "interrupted")
	assert.Contains(t, final, "queued")
	assert.NotContains(t, final, "pulling")
}

func TestTableProgress_PrintTableBeforeAnyRunStillListsTheRepos(t *testing.T) {
	_, lister, buf := newTable(t, 200, 40, tableRepos(2))

	lister.PrintTable()

	out := stripANSI(buf.String())
	assert.Equal(t, 2, strings.Count(out, "queued"))
	assert.Contains(t, out, "Total: 2 repos")
}

func TestTableProgress_PrintTableWaitsForTheLiveAreaAndForSetRepos(t *testing.T) {
	var buf lockedBuffer
	p := ui.NewTableProgress(&buf, 200, 40)
	lister := p.(ui.RepoLister)

	lister.PrintTable()
	assert.Empty(t, buf.String(), "with no repos listed there is no table")

	lister.SetRepos(tableRepos(1))
	within(t, func() {
		p.Start(1)
		before := len(buf.String())
		lister.PrintTable()
		assert.Equal(t, before, len(buf.String()), "while the live area is up nothing is printed")
		p.Stop()
	})
	lister.PrintTable()
	assert.Contains(t, stripANSI(buf.String()), "Total: 1 repos")
}

func TestTableProgress_SurvivesAnEmptyRunAndStopWithoutStart(t *testing.T) {
	p, lister, buf := newTable(t, 100, 24, nil)

	within(t, func() {
		p.Stop()
		p.Start(0)
		p.Stop()
	})
	lister.PrintTable()

	assert.Contains(t, stripANSI(buf.String()), "Total: 0 repos")
}
