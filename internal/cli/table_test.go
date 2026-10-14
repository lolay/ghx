package cli_test

import (
	"cmp"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lolay/ghx/internal/ghapi/ghapitest"
	"github.com/lolay/ghx/internal/ui"
)

// ansiCodes matches the colour and cursor escape sequences the live renderers
// write.
var ansiCodes = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

// terminal makes the rest of the test a run on a terminal that takes ANSI
// codes: ui.NewProgress then gives the live table, with no variable to say so.
// The null device is a character device, as a console is, on every OS, and a
// Windows console may take no ANSI codes under `go test`, so the test says it
// does. The package's non-terminal default (TestMain) comes back afterwards.
func terminal(t *testing.T) {
	t.Helper()
	null, err := os.Open(os.DevNull)
	require.NoError(t, err)
	supported := text.ANSICodesSupported
	text.ANSICodesSupported = true
	ui.SetupTerminal(null)
	t.Cleanup(func() {
		text.ANSICodesSupported = supported
		ui.SetupTerminal(stdoutFile)
		_ = null.Close()
	})
}

// tableWorld is a world with an active and an archived repo, on a terminal.
func tableWorld(t *testing.T) *world {
	t.Helper()
	terminal(t)
	w := newWorld(t)
	w.addRepo("alpha")
	w.addRepo("beta")
	w.addRepo("vault", func(r *ghapitest.Repo) { r.Archived = true })
	return w
}

// plain is out with the escape codes removed.
func plain(out string) string { return ansiCodes.ReplaceAllString(out, "") }

// tableRow matches a row of the repo table, capturing its name.
var tableRow = regexp.MustCompile(`^│ (\S+) +│`)

// rowNames are the repos with a table row in out whose line contains status,
// each once, in the order the table printed them. The output is the raw
// stream, every live frame included, so a row in flight, or finished and held
// back behind a slower one, appears more than once. Its last appearance is
// where it printed: no frame draws a row again once it has.
func rowNames(out, status string) []string {
	last := map[string]int{}
	var names []string
	for i, line := range strings.Split(out, "\n") {
		m := tableRow.FindStringSubmatch(line)
		if m == nil || m[1] == "Repos" || m[1] == "NAME" || !strings.Contains(line, status) {
			continue
		}
		if _, ok := last[m[1]]; !ok {
			names = append(names, m[1])
		}
		last[m[1]] = i
	}
	slices.SortFunc(names, func(a, b string) int { return cmp.Compare(last[a], last[b]) })
	return names
}

func TestRun_TheLiveTableIsOnePieceWithEachReposStatus(t *testing.T) {
	w := tableWorld(t)

	out := plain(w.mustRun())

	// The top and the totals come once, around both passes.
	assert.Equal(t, 1, strings.Count(out, "Repos to export"))
	assert.Equal(t, 1, strings.Count(out, "NAME"))
	assert.Contains(t, out, "STATUS")
	assert.ElementsMatch(t, []string{"alpha", "beta", "vault"}, rowNames(out, "✓ cloned"), "alpha, beta and the archived repo")
	assert.Regexp(t, `│ 3 repos · ✓ 3 cloned · \d+s +│`, out)
	assert.NotContains(t, out, "│ queued", "no row is left queued")
	assert.NotContains(t, out, "Total:", "the footer carries the count")
	assert.NotContains(t, out, "Cloning 1 archived repo(s)", "a message between the passes would split the table")
	assert.Less(t, strings.Index(out, "Repos to export"), strings.Index(out, "✓ cloned"))
	assert.Less(t, strings.LastIndex(out, "✓ cloned"), strings.Index(out, "3 repos · "))
	assert.NotContains(t, out, "Sync Summary", "the footer has the counts")
	assert.Equal(t, []string{"ARCHIVED/vault", "alpha", "beta"}, repoDirs(t, w.out))
}

func TestRun_TheTableShowsArchivedUnderVisibilityAndHasNoArchivedColumn(t *testing.T) {
	tests := []struct {
		name  string
		build func(t *testing.T) *world
		clean func(string) string
	}{
		{"live", func(t *testing.T) *world {
			w := tableWorld(t)
			w.addRepo("secret", func(r *ghapitest.Repo) { r.Archived = true; r.Private = true })
			return w
		}, plain},
		{"static", func(t *testing.T) *world {
			w := newWorld(t)
			w.addRepo("alpha")
			w.addRepo("vault", func(r *ghapitest.Repo) { r.Archived = true })
			w.addRepo("secret", func(r *ghapitest.Repo) { r.Archived = true; r.Private = true })
			return w
		}, func(s string) string { return s }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := tt.clean(tt.build(t).mustRun())

			var header string
			for _, line := range strings.Split(out, "\n") {
				if strings.Contains(line, "NAME") {
					header = line
				}
			}
			require.NotEmpty(t, header)
			assert.Contains(t, header, "VISIBILITY")
			assert.NotContains(t, header, "ARCHIVED", "no Archived column")
			assert.Regexp(t, `│ vault +│ archived +│`, out, "an archived public repo reads archived")
			assert.Regexp(t, `│ secret +│ archived +│`, out, "an archived private repo reads archived, not private")
			assert.Regexp(t, `│ alpha +│ public +│`, out)
		})
	}
}

func TestRun_TheLiveTableListsActiveReposByNameThenArchivedOnes(t *testing.T) {
	terminal(t)
	w := newWorld(t)
	// The API lists repos by creation date, which is the order they are added.
	w.addRepo("zulu")
	w.addRepo("aardvark", func(r *ghapitest.Repo) { r.Archived = true })
	w.addRepo("Mike")
	w.addRepo("alpha")

	out := plain(w.mustRun())

	assert.Equal(t, []string{"alpha", "Mike", "zulu", "aardvark"}, rowNames(out, "✓ cloned"),
		"the passes' order: active repos by name, ignoring case, then archived ones")
}

func TestRun_TheLiveTableShowsRowsStillQueuedWhenArchivedReposAreSkipped(t *testing.T) {
	w := tableWorld(t)

	out := plain(w.mustRun("--delete"))

	assert.Equal(t, 1, strings.Count(out, "Repos to export"))
	assert.ElementsMatch(t, []string{"alpha", "beta"}, rowNames(out, "✓ cloned"))
	assert.Equal(t, 1, strings.Count(out, "│ queued"), "the archived repo was never started")
	assert.Regexp(t, `│ 3 repos · ✓ 2 cloned · 1 queued · \d+s +│`, out)
	// The note on skipping them waits for the table's close.
	require.Contains(t, out, "Skipping 1 archived repo(s) (--delete)")
	assert.Less(t, strings.Index(out, "3 repos · "), strings.Index(out, "Skipping 1 archived repo(s) (--delete)"))
}

func TestRun_ANonTerminalAnnouncesTheArchivedPass(t *testing.T) {
	w := newWorld(t)
	w.addRepo("alpha")
	w.addRepo("beta")
	w.addRepo("vault", func(r *ghapitest.Repo) { r.Archived = true })

	out := plain(w.mustRun())

	assert.Contains(t, out, "Cloning 1 archived repo(s) into ARCHIVED/...", "the live table prints this nowhere")
	assert.Equal(t, 1, strings.Count(out, "Repos to export"), "the static table")
	assert.Contains(t, out, "Total: 3 repos")
}

func TestRun_ANonTerminalPrintsTheDeleteNoteBeforeTheSummary(t *testing.T) {
	w := newWorld(t)
	w.addRepo("alpha")
	w.addRepo("vault", func(r *ghapitest.Repo) { r.Archived = true })

	out := plain(w.mustRun("--delete"))

	require.Contains(t, out, "Skipping 1 archived repo(s) (--delete)")
	assert.Less(t, strings.Index(out, "Skipping 1 archived repo(s) (--delete)"), strings.Index(out, "Sync Summary"))
}

// failingWorld is tableWorld with a repo whose clone URL points nowhere, so
// its clone fails.
func failingWorld(t *testing.T) *world {
	t.Helper()
	w := tableWorld(t)
	w.addRepo("broken", func(r *ghapitest.Repo) {
		r.CloneURL = filepath.Join(w.upstream, "no-such-repo.git")
		r.SSHURL = r.CloneURL
	})
	return w
}

func TestRun_TheTableRendererPrintsNoSummaryCountsButKeepsTheLists(t *testing.T) {
	w := failingWorld(t)

	out := plain(w.mustRun())

	assert.NotContains(t, out, "Sync Summary", "the table's footer has the counts")
	assert.NotContains(t, out, "COUNT")
	assert.Regexp(t, `│ 4 repos · ✓ 3 cloned · ✗ 1 failed · \d+s +│`, out)
	require.Contains(t, out, "Failed repos:\n  ✗ broken  ")
	assert.Less(t, strings.Index(out, "4 repos · "), strings.Index(out, "Failed repos:"),
		"the list follows the table")
}

func TestRun_TheTableRendererKeepsTheFetchedOnlyList(t *testing.T) {
	w := tableWorld(t)
	w.mustRun()
	require.NoError(t, os.WriteFile(filepath.Join(w.out, "alpha", "README.md"), []byte("mine\n"), 0o644))

	out := plain(w.mustRun())

	assert.NotContains(t, out, "Sync Summary")
	assert.Contains(t, out, "Fetched only (working tree not updated):\n  ⇣ alpha  ")
	assert.Regexp(t, `│ 3 repos · ✓ 0 cloned · – 2 up to date · ⇣ 1 fetched only · \d+s +│`, out)
}

func TestRun_TheTableRendererPrintsNoListsWhenEverythingWorked(t *testing.T) {
	w := tableWorld(t)

	out := plain(w.mustRun())

	assert.NotContains(t, out, "Sync Summary")
	assert.NotContains(t, out, "Failed repos:")
	assert.NotContains(t, out, "Skipped (")
}

func TestRun_ANonTerminalPrintsTheFullSummary(t *testing.T) {
	w := failingWorld(t)
	// failingWorld is on a terminal; a pipe is what the test is about.
	ui.SetupTerminal(stdoutFile)

	out := plain(w.mustRun())

	assert.NotContains(t, out, "STATUS")
	assert.Contains(t, out, "Sync Summary")
	assert.Regexp(t, `cloned\s+│\s+3\s+│`, out)
	assert.Contains(t, out, "Failed repos:\n  ✗ broken  ")
}

func TestRun_ADryRunPrintsTheStaticTable(t *testing.T) {
	w := tableWorld(t)

	out := w.mustRun("--dry-run")

	assert.Equal(t, 1, strings.Count(out, "Repos to export"))
	assert.Equal(t, 1, strings.Count(out, "Total: 3 repos"))
	assert.NotContains(t, out, "STATUS")
	assert.NotContains(t, out, "queued")
	assert.NotContains(t, out, "Syncing repos")
	assert.NotContains(t, out, "\x1b[", "a dry run draws nothing live")
	assert.Contains(t, out, "Dry run complete")
}

func TestRun_ANonTerminalPrintsTheStaticTable(t *testing.T) {
	w := newWorld(t)
	w.addRepo("alpha")
	w.addRepo("beta")
	w.addRepo("vault", func(r *ghapitest.Repo) { r.Archived = true })

	out := w.mustRun()

	assert.Equal(t, 1, strings.Count(out, "Repos to export"))
	assert.Equal(t, 1, strings.Count(out, "Total: 3 repos"))
	assert.NotContains(t, out, "STATUS")
	assert.NotContains(t, out, "Syncing")
	assert.NotContains(t, out, "\x1b[", "a log file gets no escape codes")
	assert.Contains(t, out, "Sync Summary")
}

func TestRun_ATerminalDrawsTheLiveTableWithoutAnyVariable(t *testing.T) {
	w := tableWorld(t)

	out := plain(w.mustRun())

	assert.Contains(t, out, "STATUS")
	assert.Len(t, rowNames(out, "✓ cloned"), 3)
	assert.NotContains(t, out, "Total: ", "no static table")
	assert.NotContains(t, out, "Sync Summary")
}
