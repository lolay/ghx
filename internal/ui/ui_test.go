package ui_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lolay/ghx/internal/cloner"
	"github.com/lolay/ghx/internal/ghapi"
	"github.com/lolay/ghx/internal/ui"
)

// TestMain turns go-pretty's ANSI colors off so output compares as plain
// text whatever terminal or CI runner the tests run under.
func TestMain(m *testing.M) {
	text.DisableColors()
	os.Exit(m.Run())
}

// captureOut points ui.Out at a buffer for the test. Tests that use it must
// not run in parallel: ui.Out is package state.
func captureOut(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := ui.Out
	ui.Out = &buf
	t.Cleanup(func() { ui.Out = prev })
	return &buf
}

func TestObfuscateToken(t *testing.T) {
	tests := []struct {
		name  string
		token string
		want  string
	}{
		{"empty", "", "****"},
		{"eight characters is fully hidden", "abcdefgh", "****"},
		{"nine characters shows four each end", "abcdefghi", "abcd****fghi"},
		{"classic token", "ghp_0123456789abcdefWXYZ", "ghp_****WXYZ"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, ui.ObfuscateToken(tt.token))
		})
	}
}

func TestPrintSettings_AlignsSources(t *testing.T) {
	buf := captureOut(t)

	ui.PrintSettings([]ui.SettingRow{
		{Label: "Organization:", Value: "lolay", Source: "argument"},
		{Label: "Token:", Value: "ghp_****WXYZ", Source: "GITHUB_TOKEN env"},
		{Label: "Type:", Value: "all", Source: "default"},
	})

	want := "\n" +
		"Settings:\n" +
		"  Organization:  lolay         (argument)\n" +
		"  Token:         ghp_****WXYZ  (GITHUB_TOKEN env)\n" +
		"  Type:          all           (default)\n" +
		"\n"
	assert.Equal(t, want, buf.String())
}

func TestPrintSettings_NoRowsPrintsNothing(t *testing.T) {
	buf := captureOut(t)

	ui.PrintSettings(nil)

	assert.Empty(t, buf.String())
}

func TestPrintSummary_CountsActionsAndListsFailedAndFetchedOnly(t *testing.T) {
	buf := captureOut(t)

	ui.PrintSummary(cloner.SyncSummary{Results: []cloner.RepoResult{
		{Name: "a", Action: cloner.ActionCloned},
		{Name: "b", Action: cloner.ActionCloned},
		{Name: "c", Action: cloner.ActionPulled},
		{Name: "g", Action: cloner.ActionUpToDate},
		{Name: "h", Action: cloner.ActionUpToDate},
		{Name: "d", Action: cloner.ActionFailed, Detail: "exit status 128"},
		{Name: "e", Action: cloner.ActionFetchedOnly, Detail: "local changes"},
		{Name: "f", Action: cloner.ActionFailed, Detail: "timeout"},
	}})

	out := buf.String()
	assert.Contains(t, out, "Sync Summary")
	for _, row := range []struct {
		action cloner.Action
		count  string
	}{
		{cloner.ActionCloned, "2"},
		{cloner.ActionPulled, "1"},
		{cloner.ActionUpToDate, "2"},
		{cloner.ActionFailed, "2"},
		{cloner.ActionFetchedOnly, "1"},
	} {
		re := regexp.MustCompile(regexp.QuoteMeta(string(row.action)) + `\s+│\s+` + row.count + `\s+│`)
		assert.Regexp(t, re, out, "row for %q", row.action)
	}
	for _, absent := range []cloner.Action{cloner.ActionDeleted, cloner.ActionMovedArchived, cloner.ActionSkippedTooLarge} {
		assert.NotContains(t, out, string(absent), "zero-count action %q should have no row", absent)
	}
	assert.Contains(t, out, "Failed repos:\n  ✗ d  exit status 128\n  ✗ f  timeout\n")
	assert.Contains(t, out, "Fetched only (working tree not updated):\n  ⚠ e  local changes\n")
}

func TestPrintSummary_ListsUnusableNamesWithTheReason(t *testing.T) {
	buf := captureOut(t)

	ui.PrintSummary(cloner.SyncSummary{Results: []cloner.RepoResult{
		{Name: "CON", Action: cloner.ActionSkippedName, Detail: "a reserved device name on Windows"},
	}})

	assert.Contains(t, buf.String(), "Skipped (unusable name):\n  ⚠ CON  a reserved device name on Windows\n")
}

func TestPrintSummary_OmitsSectionsWithNothingToList(t *testing.T) {
	buf := captureOut(t)

	ui.PrintSummary(cloner.SyncSummary{Results: []cloner.RepoResult{
		{Name: "a", Action: cloner.ActionPulled},
	}})

	out := buf.String()
	assert.Contains(t, out, "Sync Summary")
	assert.NotContains(t, out, "Failed repos:")
	assert.NotContains(t, out, "Fetched only (working tree not updated):")
	assert.NotContains(t, out, "Skipped (unusable name):")
	assert.NotContains(t, out, "Skipped (empty):")
	assert.NotContains(t, out, "Notes:")
}

func TestPrintSummary_ListsEmptyReposWithTheirGlyph(t *testing.T) {
	buf := captureOut(t)

	ui.PrintSummary(cloner.SyncSummary{Results: []cloner.RepoResult{
		{Name: "new-repo", Action: cloner.ActionSkippedEmpty, Detail: "empty repository"},
	}})

	out := buf.String()
	assert.Contains(t, out, "Skipped (empty):\n  ∅ new-repo  empty repository, skipped\n")
	assert.Regexp(t, `∅ skipped \(empty\)\s+│\s+1\s+│`, out, "the counts row carries the glyph and the words")
}

func TestPrintSummary_PutsAGlyphOnEachCountsRowThatNeedsAttention(t *testing.T) {
	buf := captureOut(t)

	ui.PrintSummary(cloner.SyncSummary{Results: []cloner.RepoResult{
		{Name: "a", Action: cloner.ActionFailed, Detail: "boom"},
		{Name: "b", Action: cloner.ActionFetchedOnly, Detail: "local changes"},
		{Name: "c.wiki", Action: cloner.ActionFailedWiki, Detail: "could not resolve host"},
		{Name: "d", Action: cloner.ActionPulled},
	}})

	out := buf.String()
	assert.Regexp(t, `✗ failed\s+│\s+1\s+│`, out)
	assert.Regexp(t, `⚠ fetched only\s+│\s+1\s+│`, out)
	assert.Regexp(t, `✗ failed wiki\s+│\s+1\s+│`, out)
	assert.Regexp(t, `│ pulled\s+│\s+1\s+│`, out, "an action that needs nothing has no glyph")
	assert.Contains(t, out, "Failed repos:\n  ✗ a  boom\n  ✗ c.wiki  could not resolve host\n",
		"a failed wiki is listed with the failed repos")
}

func TestPrintSummary_NotesNameCloneAndPullDetails(t *testing.T) {
	buf := captureOut(t)

	ui.PrintSummary(cloner.SyncSummary{Results: []cloner.RepoResult{
		{Name: "a", Action: cloner.ActionCloned, Detail: "remote HEAD names main, which doesn't exist; checked out dev"},
		{Name: "b", Action: cloner.ActionPulled, Detail: "origin/main was force-pushed; old tip kept at refs/ghx/forced/main/20260101T000000Z"},
		{Name: "c", Action: cloner.ActionPulled, Detail: "switched from master to main"},
		{Name: "quiet", Action: cloner.ActionPulled},
	}})

	out := buf.String()
	assert.Contains(t, out, "Notes:\n"+
		"  · a  remote HEAD names main, which doesn't exist; checked out dev\n"+
		"  · b  origin/main was force-pushed; old tip kept at refs/ghx/forced/main/20260101T000000Z\n"+
		"  · c  switched from master to main\n")
	assert.NotContains(t, out, "quiet  ", "a pull with no detail is not a note")
}

func TestSetupTerminal_DrawsProgressOnlyOnATerminal(t *testing.T) {
	buf := captureOut(t)
	t.Cleanup(ui.ResetTerminal)
	assert.Same(t, buf, ui.ProgressOut(), "before setup, progress draws on Out")

	file, err := os.Create(filepath.Join(t.TempDir(), "log.txt"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = file.Close() })
	ui.SetupTerminal(file)
	assert.Equal(t, io.Discard, ui.ProgressOut(), "a file gets no progress bar")

	// The null device is a character device, as a console is, on every OS.
	null, err := os.Open(os.DevNull)
	require.NoError(t, err)
	t.Cleanup(func() { _ = null.Close() })
	ui.SetupTerminal(null)
	if text.ANSICodesSupported {
		assert.Same(t, buf, ui.ProgressOut(), "a terminal that takes ANSI codes gets the bar")
	} else {
		assert.Equal(t, io.Discard, ui.ProgressOut(), "a console without ANSI support gets no bar")
	}
}

func TestPrintRepoTable_FormatsEachColumn(t *testing.T) {
	buf := captureOut(t)

	ui.PrintRepoTable([]ghapi.RepoInfo{
		{Name: "ghx", Language: "Go", SizeKB: 1536, DefaultBranch: "main", PushedAt: "2026-10-01T12:00:00Z"},
		{Name: "vault", Private: true, Archived: true, DefaultBranch: "master"},
	})

	out := buf.String()
	assert.Contains(t, out, "Repos to export")
	assert.Regexp(t, `ghx\s+│\s+public\s+│\s+Go\s+│\s+1\.5\s+│\s+main\s+│\s+no\s+│\s+2026-10-01T12:00:00Z\s+│`, out)
	assert.Regexp(t, `vault\s+│\s+private\s+│\s+-\s+│\s+0\.0\s+│\s+master\s+│\s+yes\s+│\s+-\s+│`, out)
	assert.Contains(t, out, "\nTotal: 2 repos\n")
}
