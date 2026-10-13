package ui_test

import (
	"fmt"
	"regexp"
	"runtime"
	"strconv"
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

// needsANSICursor skips a test that reads what a terminal would show: the
// cursor package moves a Windows console through its API, so a buffer there
// holds no cursor movement to play back.
func needsANSICursor(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the cursor moves through the console API on Windows, not escape codes")
	}
}

// emulate plays out what a terminal does with out, with room for every line
// that ever scrolls by, and returns the lines it ends up showing. It knows the
// few codes the renderer and the pterm area printer use: lines up and down,
// the start of a line, erasing a line, and colours and cursor visibility,
// which it ignores. What the area printer cleared is gone, so the result is
// the ordinary output with the live area as last drawn.
func emulate(out string) []string {
	var lines [][]rune
	row, col := 0, 0
	line := func() *[]rune {
		for len(lines) <= row {
			lines = append(lines, nil)
		}
		return &lines[row]
	}
	rs := []rune(out)
	for i := 0; i < len(rs); i++ {
		switch r := rs[i]; r {
		case '\n':
			row, col = row+1, 0
		case '\r':
			col = 0
		case '\x1b':
			if i+1 >= len(rs) || rs[i+1] != '[' {
				continue
			}
			j := i + 2
			for j < len(rs) && (rs[j] == ';' || rs[j] == '?' || (rs[j] >= '0' && rs[j] <= '9')) {
				j++
			}
			params := string(rs[i+2 : j])
			n, err := strconv.Atoi(params)
			if err != nil {
				n = 1
			}
			final := rs[min(j, len(rs)-1)]
			i = j
			switch final {
			case 'A':
				row = max(row-n, 0)
			case 'B':
				row += n
			case 'G':
				col = max(n-1, 0)
			case 'K':
				l := line()
				if params == "2" {
					*l = nil
				} else if col < len(*l) {
					*l = (*l)[:col]
				}
			}
		default:
			l := line()
			for len(*l) < col {
				*l = append(*l, ' ')
			}
			if col < len(*l) {
				(*l)[col] = r
			} else {
				*l = append(*l, r)
			}
			col++
		}
	}
	shown := make([]string, len(lines))
	for i, l := range lines {
		shown[i] = string(l)
	}
	for len(shown) > 0 && shown[len(shown)-1] == "" {
		shown = shown[:len(shown)-1]
	}
	return shown
}

// screen is what a terminal shows after everything written to buf so far.
func screen(buf *lockedBuffer) string { return strings.Join(emulate(buf.String()), "\n") }

// waitForScreen blocks until the screen shows every one of wants, and returns
// it. A frame is cleared and drawn again by separate writes, so a screen read
// between them is short of the live area; a test waits for what it expects
// rather than reading once.
func waitForScreen(t *testing.T, buf *lockedBuffer, wants ...string) string {
	t.Helper()
	var scr string
	require.Eventually(t, func() bool {
		scr = screen(buf)
		for _, want := range wants {
			if !strings.Contains(scr, want) {
				return false
			}
		}
		return true
	}, 10*time.Second, 5*time.Millisecond, "the screen never showed %q", wants)
	return scr
}

// tableLines are the lines of a screen from the table's top border on.
func tableLines(scr string) []string {
	lines := strings.Split(scr, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "┌") {
			return lines[i:]
		}
	}
	return nil
}

// assertOneWidth asserts that every line of the table is as wide as the first
// and returns that width.
func assertOneWidth(t *testing.T, scr string) int {
	t.Helper()
	lines := tableLines(scr)
	require.NotEmpty(t, lines, "the screen has a table: %q", scr)
	want := text.StringWidthWithoutEscSequences(lines[0])
	for _, line := range lines {
		assert.Equal(t, want, text.StringWidthWithoutEscSequences(line), "line %q", line)
	}
	return want
}

// widest is the width of the widest line of s.
func widest(s string) int { return text.LongestLineLen(stripANSI(s)) }

// results is the action each repo of the concurrent runs ends with, by index.
var results = []cloner.Action{
	cloner.ActionCloned, cloner.ActionPulled, cloner.ActionFailed, cloner.ActionSkippedDirty,
}

func TestTableProgress_FinishedRowsStreamIntoOneTableInCompletionOrder(t *testing.T) {
	needsANSICursor(t)
	const repos = 12
	p, lister, buf := newTable(t, 200, 40, tableRepos(repos))

	// The renderer orders rows as RepoDone reaches it, so the test records
	// the order under a lock of its own that the call also sits inside.
	var mu sync.Mutex
	var order []string
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
				time.Sleep(time.Duration((i*7)%5) * 25 * time.Millisecond)
				mu.Lock()
				defer mu.Unlock()
				order = append(order, name)
				p.RepoDone(cloner.RepoResult{Name: name, Action: results[i%len(results)]}, time.Duration(i+1)*100*time.Millisecond)
			}()
		}
		wg.Wait()
		p.Stop()
		lister.FinishTable()
	})

	lines := strings.Split(screen(buf), "\n")
	scr := strings.Join(lines, "\n")
	assert.Equal(t, 1, strings.Count(scr, "Repos to export"), "the top is printed once")
	assert.Equal(t, 1, strings.Count(scr, "NAME"), "the header is printed once")
	assert.NotContains(t, scr, "Syncing", "no live frame is left behind")
	assert.NotContains(t, scr, "cloning")
	assertOneWidth(t, scr)

	var rows []string
	for _, line := range lines {
		if name := regexp.MustCompile(`repo-\d\d`).FindString(line); name != "" {
			rows = append(rows, name)
			i, err := strconv.Atoi(name[len("repo-"):])
			require.NoError(t, err)
			action := results[i%len(results)]
			symbol, _ := ui.StatusMark(action)
			assert.Contains(t, line, symbol+" "+ui.ActionText(action, time.Duration(i+1)*100*time.Millisecond), name)
		}
	}
	assert.Equal(t, order, rows, "every repo is in the table once, in the order it finished")

	// The table is closed by the rule, the totals and the bottom border.
	require.GreaterOrEqual(t, len(lines), 3)
	rule, footer, bottom := lines[len(lines)-3], lines[len(lines)-2], lines[len(lines)-1]
	assert.True(t, strings.HasPrefix(rule, "├") && strings.Contains(rule, "┴"), rule)
	assert.Regexp(t, `^│ 12 repos · ✓ 6 · ⚠ 3 skipped · ✗ 3 failed · \d+s +│$`, footer)
	assert.True(t, strings.HasPrefix(bottom, "└") && !strings.Contains(bottom, "┴"), bottom)

	before := len(buf.String())
	lister.FinishTable()
	assert.Equal(t, before, len(buf.String()), "the table is closed once")
}

func TestTableProgress_NoFrameChangesTheTablesWidth(t *testing.T) {
	needsANSICursor(t)
	repos := tableRepos(10)
	repos[7].Name = "a-repository-with-a-name-longer-than-the-rest"
	repos[8].Language = "Objective-C++"
	action, took := longestStatus()
	p, _, buf := newTable(t, 200, 40, repos)

	var widths []int
	capture := func(wants ...string) {
		widths = append(widths, assertOneWidth(t, waitForScreen(t, buf, wants...)))
	}
	within(t, func() {
		p.Start(len(repos))
		defer p.Stop()
		capture("Syncing 0/10", "0 running · 10 queued")

		p.RepoStarted("repo-00", cloner.ProgressClone)
		capture("1 running")
		time.Sleep(120 * time.Millisecond)
		p.RepoStarted("repo-01", cloner.ProgressPull)
		p.RepoStarted(repos[7].Name, cloner.ProgressClone)
		capture("3 running · 7 queued")

		// A finished row with the longest action and the longest time.
		p.RepoDone(cloner.RepoResult{Name: repos[7].Name, Action: action}, took)
		p.RepoDone(cloner.RepoResult{Name: "repo-00", Action: cloner.ActionCloned}, time.Second)
		capture("Syncing 2/10", ui.ActionText(action, took))

		for i := 1; i < 10; i++ {
			if i != 7 {
				p.RepoDone(cloner.RepoResult{Name: fmt.Sprintf("repo-%02d", i), Action: cloner.ActionPulled}, 2*time.Minute)
			}
		}
		capture("Syncing 10/10")
	})

	require.NotEmpty(t, widths)
	for i, width := range widths {
		assert.Equal(t, widths[0], width, "frame %d", i)
	}
}

// longestStatus is the finished repo whose Status cell is the widest: the
// action with the longest text, after the longest time.
func longestStatus() (cloner.Action, time.Duration) {
	const took = 779 * time.Second // 12m59s
	longest := cloner.AllActions[0]
	for _, a := range cloner.AllActions {
		if len(ui.ActionText(a, took)) > len(ui.ActionText(longest, took)) {
			longest = a
		}
	}
	return longest, took
}

func TestTableProgress_AResizedTerminalKeepsTheWidthsChosenAtTheStart(t *testing.T) {
	needsANSICursor(t)
	var buf lockedBuffer
	var mu sync.Mutex
	width := 200
	p := ui.NewTableProgressSized(&buf, func() (int, int) {
		mu.Lock()
		defer mu.Unlock()
		return width, 40
	})
	lister := p.(ui.RepoLister)
	lister.SetRepos(tableRepos(3))
	resize := func(w int) {
		mu.Lock()
		defer mu.Unlock()
		width = w
	}

	within(t, func() {
		p.Start(3)
		defer p.Stop()
		start := assertOneWidth(t, waitForScreen(t, &buf, "Syncing"))

		resize(300)
		p.RepoStarted("repo-00", cloner.ProgressClone)
		wide := assertOneWidth(t, waitForScreen(t, &buf, "1 running"))
		assert.Equal(t, start, wide, "a wider terminal does not widen the table")

		// A narrower one cuts the live lines rather than let them wrap.
		resize(start - 10)
		require.Eventually(t, func() bool {
			return widest(lastLines(&buf, 4)) <= start-10
		}, 10*time.Second, 5*time.Millisecond)
	})
}

// lastLines are the last n lines of the screen.
func lastLines(buf *lockedBuffer, n int) string {
	lines := emulate(buf.String())
	return strings.Join(lines[max(len(lines)-n, 0):], "\n")
}

func TestTableProgress_TheLiveAreaHoldsOnlyWhatIsInFlight(t *testing.T) {
	needsANSICursor(t)
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

		scr := waitForScreen(t, buf, "Syncing 20/60", "3 running · 37 queued")
		assertOneWidth(t, scr)
		lines := strings.Split(scr, "\n")
		// The 20 finished rows stand above the live area, one after the other.
		var first int
		for i, line := range lines {
			if strings.Contains(line, "repo-00") {
				first = i
			}
		}
		for i := range 20 {
			assert.Contains(t, lines[first+i], fmt.Sprintf("repo-%02d", i))
		}
		// The live area is the 3 in-flight rows, the rule that closes their
		// columns, the footer and the closing border: nothing finished again,
		// nothing queued.
		live := lines[first+20:]
		require.Len(t, live, 6, strings.Join(live, "\n"))
		for i, name := range []string{"repo-20", "repo-21", "repo-22"} {
			assert.Contains(t, live[i], name)
			assert.Regexp(t, `pulling \d+\.\ds`, live[i])
		}
		assert.True(t, strings.HasPrefix(live[3], "├") && strings.Contains(live[3], "┴"), live[3])
		assert.Contains(t, live[4], "Syncing 20/60")
		assert.True(t, strings.HasPrefix(live[5], "└"))
		assert.NotContains(t, scr, "repo-23", "a queued repo has no row yet")
		assert.NotContains(t, scr, "done,", "the window footer is gone")
		assert.LessOrEqual(t, len(live), height-2, "the area leaves two lines of the terminal")
	})
}

func TestTableProgress_TheRuleSitsDirectlyAboveTheFooter(t *testing.T) {
	needsANSICursor(t)
	p, _, buf := newTable(t, 120, 20, tableRepos(10))

	within(t, func() {
		p.Start(10)
		defer p.Stop()

		// Nothing has run, so the header's separator is the line above the
		// footer: one separator, not two in a row.
		scr := waitForScreen(t, buf, "Syncing 0/10")
		lines := strings.Split(scr, "\n")
		require.GreaterOrEqual(t, len(lines), 3)
		assert.True(t, strings.HasPrefix(lines[len(lines)-3], "├") && strings.Contains(lines[len(lines)-3], "┼"), scr)
		assert.NotContains(t, strings.Join(lines, "\n"), "┴", "no rule yet")

		// With a row in flight the rule closes it, right above the footer.
		p.RepoStarted("repo-00", cloner.ProgressClone)
		scr = waitForScreen(t, buf, "1 running")
		lines = strings.Split(scr, "\n")
		require.GreaterOrEqual(t, len(lines), 4)
		assert.Contains(t, lines[len(lines)-4], "repo-00")
		assert.True(t, strings.HasPrefix(lines[len(lines)-3], "├") && strings.Contains(lines[len(lines)-3], "┴"), scr)
		assert.Contains(t, lines[len(lines)-2], "Syncing 0/10")
		assert.True(t, strings.HasPrefix(lines[len(lines)-1], "└"))
	})
}

func TestTableProgress_TooManyInFlightShowTheFirstOnesAndCountTheRest(t *testing.T) {
	needsANSICursor(t)
	const total, height = 60, 10
	p, _, buf := newTable(t, 120, height, tableRepos(total))

	within(t, func() {
		p.Start(total)
		defer p.Stop()
		for i := range 8 {
			p.RepoStarted(fmt.Sprintf("repo-%02d", i), cloner.ProgressClone)
		}

		// Of the 8 lines a 10-line terminal leaves, the rule, the footer and
		// the closing border take 3, so 5 rows fit, with the rule under them.

		scr := waitForScreen(t, buf, "8 running · 52 queued · +3 more running")
		all := strings.Split(scr, "\n")
		live := strings.Join(all[len(all)-8:], "\n")
		assert.Contains(t, all[len(all)-3], "┴", "the rule is directly above the footer")
		for i := range 5 {
			assert.Contains(t, live, fmt.Sprintf("repo-%02d", i))
		}
		for i := 5; i < 8; i++ {
			assert.NotContains(t, live, fmt.Sprintf("repo-%02d", i))
		}
		assertOneWidth(t, scr)
	})
}

func TestTableProgress_ATerminalTooShortForTheLiveAreaShowsTheFooterAlone(t *testing.T) {
	needsANSICursor(t)
	p, _, buf := newTable(t, 100, 4, tableRepos(10))

	within(t, func() {
		p.Start(10)
		defer p.Stop()
		p.RepoStarted("repo-00", cloner.ProgressClone)
		scr := waitForScreen(t, buf, "1 running · 9 queued")
		lines := strings.Split(scr, "\n")
		assert.Contains(t, lines[len(lines)-1], "Syncing 0/10", "the footer is the last line")
		assert.NotContains(t, scr, "cloning", "no row fits")
	})
}

func TestTableProgress_ANarrowTerminalDropsColumnsInOrder(t *testing.T) {
	needsANSICursor(t)
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
						out = waitForScreen(t, buf, "queued", "Syncing")
					}
					p.Stop()
					if run == "final" {
						lister.FinishTable()
						out = screen(buf)
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

// renderedWidth is the width of the table on a terminal width columns wide.
func renderedWidth(t *testing.T, width int) int {
	t.Helper()
	p, lister, buf := newTable(t, width, 40, tableRepos(3))
	p.Start(3)
	p.Stop()
	lister.FinishTable()
	return widest(screen(buf))
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
	needsANSICursor(t)
	const width = 30
	p, lister, buf := newTable(t, width, 40, tableRepos(3))

	within(t, func() {
		p.Start(3)
		assert.LessOrEqual(t, widest(waitForScreen(t, buf, "Syncing")), width)
		p.Stop()
		lister.FinishTable()
		assert.LessOrEqual(t, widest(screen(buf)), width)
	})
}

func TestTableProgress_ALongNameIsCutToFitANarrowTerminal(t *testing.T) {
	needsANSICursor(t)
	const width = 72
	repos := tableRepos(2)
	repos[1].Name = "a-repository-with-a-name-far-too-long-for-this-terminal-to-hold-in-full"
	p, lister, buf := newTable(t, width, 40, repos)

	within(t, func() {
		p.Start(2)
		p.RepoStarted(repos[1].Name, cloner.ProgressClone)
		p.RepoDone(cloner.RepoResult{Name: repos[1].Name, Action: cloner.ActionCloned}, time.Second)
		p.Stop()
		lister.FinishTable()
	})

	scr := screen(buf)
	assert.NotContains(t, scr, repos[1].Name)
	assert.Regexp(t, `│ a-repository[a-z-]*… +│`, scr)
	assert.Equal(t, width, assertOneWidth(t, scr), "the table takes the terminal")
}

func TestTableProgress_TheFooterCountsEveryRepoAcrossBothPasses(t *testing.T) {
	needsANSICursor(t)
	p, _, buf := newTable(t, 120, 40, tableRepos(4))
	// The footer reads "Syncing 2/4 [====>---] 50% 3s · 0 running · 2 queued".
	footerLine := func(scr string) string {
		lines := strings.Split(scr, "\n")
		for i := len(lines) - 1; i >= 0; i-- {
			if strings.Contains(lines[i], "Syncing") {
				return lines[i]
			}
		}
		return ""
	}
	counts := regexp.MustCompile(`Syncing (\d+)/4 \[[=>-]+\] (\d+)% \d+s · (\d+) running · (\d+) queued`)

	within(t, func() {
		p.Start(2)
		scr := waitForScreen(t, buf, "Syncing 0/4", "0 running · 4 queued")
		assert.Regexp(t, `\[>-+\] 0% `, footerLine(scr))

		p.RepoStarted("repo-00", cloner.ProgressClone)
		p.RepoStarted("repo-01", cloner.ProgressClone)
		waitForScreen(t, buf, "2 running · 2 queued")
		p.RepoDone(cloner.RepoResult{Name: "repo-00", Action: cloner.ActionCloned}, time.Second)
		p.RepoDone(cloner.RepoResult{Name: "repo-01", Action: cloner.ActionCloned}, time.Second)
		scr = waitForScreen(t, buf, "Syncing 2/4")
		assert.Regexp(t, `\[=+>-+\] 50% `, footerLine(scr))
		assert.Contains(t, footerLine(scr), "0 running · 2 queued")
		p.Stop()

		// The second pass starts from the first one's count, not from zero.
		p.Start(2)
		scr = waitForScreen(t, buf, "Syncing 2/4")
		assert.Contains(t, footerLine(scr), "0 running · 2 queued")
		assert.Equal(t, 1, strings.Count(scr, "NAME"), "the header is not printed again")
		p.RepoStarted("repo-02", cloner.ProgressClone)
		waitForScreen(t, buf, "1 running · 1 queued")
		p.RepoDone(cloner.RepoResult{Name: "repo-02", Action: cloner.ActionCloned}, time.Second)
		p.RepoStarted("repo-03", cloner.ProgressClone)
		p.RepoDone(cloner.RepoResult{Name: "repo-03", Action: cloner.ActionFailed}, time.Second)
		scr = waitForScreen(t, buf, "Syncing 4/4")
		line := footerLine(scr)
		assert.Regexp(t, `\[=+\] 100% `, line)
		m := counts.FindStringSubmatch(line)
		require.NotNil(t, m, line)
		assert.Equal(t, []string{"4", "100", "0", "0"}, m[1:])
		p.Stop()
	})
}

func TestTableProgress_TwoPassesMakeOneTable(t *testing.T) {
	needsANSICursor(t)
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
		lister.FinishTable()
	})

	scr := screen(buf)
	assert.Equal(t, 4, strings.Count(scr, "✓ cloned 1.0s"))
	assert.Equal(t, 1, strings.Count(scr, "Repos to export"), "the table is opened once")
	assert.Equal(t, 1, strings.Count(scr, "NAME"))
	assert.Equal(t, 1, strings.Count(scr, "└"), "and closed once")
	assert.Regexp(t, `│ 4 repos · ✓ 4 · \d+s +│`, scr)
	assertOneWidth(t, scr)
}

func TestTableProgress_ACancelledRunLeavesRowsQueuedOrInterrupted(t *testing.T) {
	needsANSICursor(t)
	p, lister, buf := newTable(t, 200, 40, tableRepos(3))

	within(t, func() {
		p.Start(3)
		p.RepoStarted("repo-00", cloner.ProgressClone)
		p.RepoDone(cloner.RepoResult{Name: "repo-00", Action: cloner.ActionCloned}, time.Second)
		p.RepoStarted("repo-01", cloner.ProgressPull)
		waitForScreen(t, buf, "pulling")
		p.Stop()
		lister.FinishTable()
	})

	scr := screen(buf)
	assert.NotContains(t, scr, "pulling")
	assert.NotContains(t, scr, "Syncing")
	lines := strings.Split(scr, "\n")
	require.GreaterOrEqual(t, len(lines), 7)
	rows := lines[len(lines)-6 : len(lines)-3]
	assert.Contains(t, rows[0], "repo-00")
	assert.Contains(t, rows[0], "✓ cloned 1.0s")
	assert.Contains(t, rows[1], "repo-01")
	assert.Contains(t, rows[1], "interrupted")
	assert.Contains(t, rows[2], "repo-02")
	assert.Contains(t, rows[2], "queued")
	assert.Regexp(t, `│ 3 repos · ✓ 1 · 1 interrupted · 1 queued · \d+s +│`, lines[len(lines)-2])
	assertOneWidth(t, scr)
}

func TestTableProgress_FinishTableBeforeAnyRunStillListsTheRepos(t *testing.T) {
	needsANSICursor(t)
	_, lister, buf := newTable(t, 200, 40, tableRepos(2))

	lister.FinishTable()

	scr := screen(buf)
	assert.Equal(t, 2, strings.Count(scr, "│ queued"))
	assert.Regexp(t, `│ 2 repos · ✓ 0 · 2 queued · 0s +│`, scr)
	assertOneWidth(t, scr)
}

func TestTableProgress_FinishTableWaitsForTheLiveAreaAndForSetRepos(t *testing.T) {
	var buf lockedBuffer
	p := ui.NewTableProgress(&buf, 200, 40)
	lister := p.(ui.RepoLister)

	lister.FinishTable()
	assert.Empty(t, buf.String(), "with no repos listed there is no table")

	lister.SetRepos(tableRepos(1))
	within(t, func() {
		p.Start(1)
		before := len(buf.String())
		lister.FinishTable()
		assert.Equal(t, before, len(buf.String()), "while the live area is up nothing is printed")
		p.Stop()
	})
	lister.FinishTable()
	assert.Contains(t, stripANSI(buf.String()), "1 repo ·")
}

func TestTableProgress_SurvivesAnEmptyRunAndStopWithoutStart(t *testing.T) {
	p, lister, buf := newTable(t, 100, 24, nil)

	within(t, func() {
		p.Stop()
		p.Start(0)
		p.Stop()
	})
	lister.FinishTable()

	assert.Contains(t, stripANSI(buf.String()), "0 repos · ✓ 0 ·")
}
