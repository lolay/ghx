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

// finalTable is the last table in out: the one printed as ordinary output
// after the live frames, with the escape codes removed.
func finalTable(out string) string {
	out = ansiCodes.ReplaceAllString(out, "")
	return out[strings.LastIndex(out, "Repos to export"):]
}

func TestRun_TheLiveTableIsPrintedOnceWithEachReposStatus(t *testing.T) {
	w := tableWorld(t)

	out := w.mustRun()

	plain := ansiCodes.ReplaceAllString(out, "")
	assert.Equal(t, 1, strings.Count(plain, "Total: 3 repos"), "the table is printed once, after both passes")
	final := finalTable(out)
	assert.Contains(t, final, "STATUS")
	assert.Equal(t, 3, strings.Count(final, "✓ cloned"), "alpha, beta and the archived repo")
	assert.NotContains(t, final, "queued")
	assert.Less(t, strings.Index(plain, "Cloning 1 archived repo(s)"), strings.LastIndex(plain, "Total: 3 repos"))
	assert.Less(t, strings.LastIndex(plain, "Total: 3 repos"), strings.Index(plain, "Sync Summary"),
		"the sync summary follows the table")
	assert.Equal(t, []string{"ARCHIVED/vault", "alpha", "beta"}, repoDirs(t, w.out))
}

func TestRun_TheLiveTableShowsRowsStillQueuedWhenArchivedReposAreSkipped(t *testing.T) {
	w := tableWorld(t)

	out := w.mustRun("--delete")

	plain := ansiCodes.ReplaceAllString(out, "")
	assert.Equal(t, 1, strings.Count(plain, "Total: 3 repos"))
	final := finalTable(out)
	assert.Equal(t, 2, strings.Count(final, "✓ cloned"))
	assert.Equal(t, 1, strings.Count(final, "queued"), "the archived repo was never started")
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
