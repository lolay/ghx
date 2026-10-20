package ui_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lolay/ghx/internal/branches"
	"github.com/lolay/ghx/internal/ghapi"
	"github.com/lolay/ghx/internal/ui"
)

func branchRepos() ([]ghapi.RepoInfo, []branches.Repo) {
	infos := []ghapi.RepoInfo{
		{Name: "api", DefaultBranch: "main", PushedAt: "2026-10-01T12:00:00Z"},
		{Name: "legacy", DefaultBranch: "trunk", Private: true, PushedAt: "2026-01-01T12:00:00Z"},
	}
	repos := []branches.Repo{
		{Name: "api", DefaultBranch: "main", Counts: branches.Counts{Branches: 4, Merged: 1, Stale: 1, OpenPR: 1, Active: 1}},
		{Name: "legacy", DefaultBranch: "trunk", UnexpectedDefault: true},
	}
	return infos, repos
}

func TestBranchesTableProgress_ShowsCountsAndTheUnexpectedDefault(t *testing.T) {
	needsANSICursor(t)
	infos, repos := branchRepos()
	var buf lockedBuffer
	p := ui.NewBranchesTableProgress(&buf, "main", 140, 40)
	p.SetRepos(infos)

	p.Start()
	p.RepoStarted("api")
	p.RepoPage("api", 1)
	p.RepoStarted("legacy")
	p.RepoPage("legacy", 3)
	waitForScreen(t, &buf, "fetching page 3", "Scanning 0/2")
	p.RepoDone(repos[0])
	p.RepoFailed("legacy")
	p.Stop()
	p.FinishTable()

	scr := stripANSI(screen(&buf))
	assert.Contains(t, scr, "Repos to scan")
	assert.Contains(t, scr, "✓ 4 · 1 merged · 1 stale · 1 open")
	assert.Contains(t, scr, "✗ failed")
	assert.Contains(t, scr, "⚠ trunk", "a default branch that isn't main is flagged")
	assert.Contains(t, scr, "2 repos · ✓ 1 scanned · ✗ 1 failed")
	assert.NotContains(t, scr, "not on main", "a failed repo isn't counted")
	assert.NotContains(t, scr, "Scanning", "no live frame is left behind")
	assertOneWidth(t, scr)
}

func TestBranchesTableProgress_TheFooterCountsReposNotOnTheExpectedName(t *testing.T) {
	needsANSICursor(t)
	infos, repos := branchRepos()
	var buf lockedBuffer
	p := ui.NewBranchesTableProgress(&buf, "main", 140, 40)
	p.SetRepos(infos)

	p.Start()
	for _, r := range repos {
		p.RepoStarted(r.Name)
		p.RepoDone(r)
	}
	p.Stop()
	p.FinishTable()

	assert.Contains(t, stripANSI(screen(&buf)), "2 repos · ✓ 2 scanned · ⚠ 1 not on main")
}

func TestPrintBranchesRepoTable(t *testing.T) {
	infos, repos := branchRepos()
	out := captureOut(t)

	ui.PrintBranchesRepoTable(infos, repos, "trunk")

	got := stripANSI(out.String())
	assert.Contains(t, got, "Repos to scan")
	assert.Contains(t, got, "⚠ main", "with trunk expected, main is the odd one")
	assert.NotContains(t, got, "⚠ trunk")
	assert.Contains(t, got, "✓ 0 ")
	assert.Contains(t, got, "private")
}

func TestPrintBranchTable(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	repos := []branches.Repo{{
		Name: "api", DefaultBranch: "main",
		Branches: []branches.Branch{
			{Branch: ghapi.Branch{Name: "main", CommittedAt: now}, Status: branches.Default},
			{Branch: ghapi.Branch{Name: "done", CommittedAt: now.AddDate(0, 0, -10), AuthorLogin: "ada", MergedPR: &ghapi.MergedPR{Number: 42}}, Status: branches.Merged},
			{Branch: ghapi.Branch{Name: "old", CommittedAt: now.AddDate(0, 0, -112), AuthorName: "Bob Builder"}, Status: branches.Stale},
			{Branch: ghapi.Branch{Name: "pr", CommittedAt: now, Protected: true}, Status: branches.OpenPR},
			{Branch: ghapi.Branch{Name: "wip"}, Status: branches.Active},
		},
	}}
	out := captureOut(t)

	ui.PrintBranchTable(repos, now)

	lines := map[string]string{}
	for _, l := range tableLines(stripANSI(out.String())) {
		for _, name := range []string{"main", "done", "old", "pr", "wip"} {
			if strings.Contains(l, "│ "+name+" ") {
				lines[name] = l
			}
		}
	}
	assert.NotContains(t, lines, "main", "the default branch isn't listed")
	require.Contains(t, lines, "done")
	assert.Contains(t, lines["done"], "✓ merged #42")
	assert.Contains(t, lines["done"], "2026-10-10")
	assert.Contains(t, lines["done"], "10d")
	assert.Contains(t, lines["done"], "ada")
	assert.Contains(t, lines["old"], "⚠ stale")
	assert.Contains(t, lines["old"], "112d")
	assert.Contains(t, lines["old"], "Bob Builder", "an author with no account shows the git name")
	assert.Contains(t, lines["pr"], "↑ open-pr")
	assert.Contains(t, lines["pr"], "yes")
	assert.Contains(t, lines["wip"], "– active")
}

func TestPrintBranchTable_NothingButDefaults(t *testing.T) {
	out := captureOut(t)

	ui.PrintBranchTable([]branches.Repo{{Name: "api", Branches: []branches.Branch{{Branch: ghapi.Branch{Name: "main"}, Status: branches.Default}}}}, time.Now())

	assert.Contains(t, out.String(), "No branches besides the default ones.")
	assert.NotContains(t, out.String(), "Branches")
}

func TestPrintBranchTotals(t *testing.T) {
	out := captureOut(t)

	ui.PrintBranchTotals(branches.Totals{
		Repos: 60, Counts: branches.Counts{Branches: 412, Merged: 97, Stale: 58, OpenPR: 12, Active: 245}, UnexpectedDefault: 3,
	}, "main")

	got := stripANSI(out.String())
	for _, want := range []string{"Totals", "Repos", "60", "412", "Merged, not deleted", "97", "Stale", "58", "Default not main", "3"} {
		assert.Contains(t, got, want)
	}
}
