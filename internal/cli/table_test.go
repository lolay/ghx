package cli_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lolay/ghx/internal/ghapi/ghapitest"
	"github.com/lolay/ghx/internal/ui"
)

// ansiCodes matches the colour and cursor escape sequences the live renderers
// write.
var ansiCodes = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

// tableWorld is a world with an active and an archived repo, and the live
// table renderer selected.
func tableWorld(t *testing.T) *world {
	t.Helper()
	t.Setenv("GHX_PROGRESS", "table")
	w := newWorld(t)
	w.addRepo("alpha")
	w.addRepo("beta")
	w.addRepo("vault", func(r *ghapitest.Repo) { r.Archived = true })
	return w
}

// plain is out with the escape codes removed.
func plain(out string) string { return ansiCodes.ReplaceAllString(out, "") }

func TestRun_TheLiveTableIsOnePieceWithEachReposStatus(t *testing.T) {
	w := tableWorld(t)

	out := plain(w.mustRun())

	// A finished row is printed once, above the live area, and the live area
	// holds no finished row, so each shows once; the top and the totals come
	// once, around both passes.
	assert.Equal(t, 1, strings.Count(out, "Repos to export"))
	assert.Equal(t, 1, strings.Count(out, "NAME"))
	assert.Contains(t, out, "STATUS")
	assert.Equal(t, 3, strings.Count(out, "✓ cloned"), "alpha, beta and the archived repo")
	assert.Regexp(t, `│ 3 repos · ✓ 3 cloned · \d+s +│`, out)
	assert.NotContains(t, out, "│ queued", "no row is left queued")
	assert.NotContains(t, out, "Total:", "the footer carries the count")
	assert.NotContains(t, out, "Cloning 1 archived repo(s)", "a message between the passes would split the table")
	assert.Less(t, strings.Index(out, "Repos to export"), strings.Index(out, "✓ cloned"))
	assert.Less(t, strings.LastIndex(out, "✓ cloned"), strings.Index(out, "3 repos · "))
	assert.NotContains(t, out, "Sync Summary", "the footer has the counts")
	assert.Equal(t, []string{"ARCHIVED/vault", "alpha", "beta"}, repoDirs(t, w.out))
}

func TestRun_TheLiveTableShowsRowsStillQueuedWhenArchivedReposAreSkipped(t *testing.T) {
	w := tableWorld(t)

	out := plain(w.mustRun("--delete"))

	assert.Equal(t, 1, strings.Count(out, "Repos to export"))
	assert.Equal(t, 2, strings.Count(out, "✓ cloned"))
	assert.Equal(t, 1, strings.Count(out, "│ queued"), "the archived repo was never started")
	assert.Regexp(t, `│ 3 repos · ✓ 2 cloned · 1 queued · \d+s +│`, out)
	// The note on skipping them waits for the table's close.
	require.Contains(t, out, "Skipping 1 archived repo(s) (--delete)")
	assert.Less(t, strings.Index(out, "3 repos · "), strings.Index(out, "Skipping 1 archived repo(s) (--delete)"))
}

func TestRun_OtherRenderersAnnounceTheArchivedPass(t *testing.T) {
	for _, renderer := range []string{"gopretty", "buildx", "indicatif", "pterm"} {
		t.Run(renderer, func(t *testing.T) {
			w := tableWorld(t)
			t.Setenv("GHX_PROGRESS", renderer)

			out := plain(w.mustRun())

			assert.Contains(t, out, "Cloning 1 archived repo(s) into ARCHIVED/...")
			assert.Equal(t, 1, strings.Count(out, "Repos to export"), "the static table")
			assert.Contains(t, out, "Total: 3 repos")
		})
	}
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

func TestRun_OtherRenderersPrintTheSummaryAsBefore(t *testing.T) {
	for _, renderer := range []string{"gopretty", "buildx", "indicatif", "pterm"} {
		t.Run(renderer, func(t *testing.T) {
			w := failingWorld(t)
			t.Setenv("GHX_PROGRESS", renderer)

			out := plain(w.mustRun())

			assert.Contains(t, out, "Sync Summary")
			assert.Regexp(t, `cloned\s+│\s+3\s+│`, out)
			assert.Contains(t, out, "Failed repos:\n  ✗ broken  ")
		})
	}
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
	w := tableWorld(t)
	file, err := os.Create(filepath.Join(t.TempDir(), "log.txt"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = file.Close() })
	// The null device is a character device, as a console is, on every OS;
	// setting it up again after the test puts the terminal back as it began.
	null, err := os.Open(os.DevNull)
	require.NoError(t, err)
	t.Cleanup(func() { _ = null.Close() })
	t.Cleanup(func() { ui.SetupTerminal(null) })
	ui.SetupTerminal(file)

	out := w.mustRun()

	assert.Equal(t, 1, strings.Count(out, "Repos to export"))
	assert.Equal(t, 1, strings.Count(out, "Total: 3 repos"))
	assert.NotContains(t, out, "STATUS")
	assert.NotContains(t, out, "Syncing repos")
	assert.NotContains(t, out, "\x1b[", "a log file gets no escape codes")
	assert.Contains(t, out, "Sync Summary")
}
