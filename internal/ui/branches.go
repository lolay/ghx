package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/pterm/pterm"

	"github.com/lolay/ghx/internal/branches"
	"github.com/lolay/ghx/internal/ghapi"
)

// Indexes into the branches report's repo columns.
const (
	branchesColumnVisibility = 1
	branchesColumnPushed     = 3
)

// The branches report's repo table classes: a repo scanned, or failed.
const (
	scannedMark = "✓"
	failedMark  = "✗"
)

// newBranchesSpec is the branches report's repo table: each repo's default
// branch, flagged when it isn't expected, and the counts of its branches once
// they are in.
func newBranchesSpec(expected string) *tableSpec {
	return &tableSpec{
		title: "Repos to scan",
		columns: []repoColumn{
			syncColumns[0],
			syncColumns[1],
			{title: "Default Branch", cell: func(r ghapi.RepoInfo) any { return defaultBranchCell(r.DefaultBranch, expected) }},
			syncColumns[columnPushed],
		},
		drops: []int{branchesColumnPushed, branchesColumnVisibility},
		verb:  "Scanning",
		verbs: []string{"fetching", "fetching page 99"},
		statusCells: []string{
			scannedMark + " " + countsText(branches.Counts{Branches: 9999, Merged: 999, Stale: 999, OpenPR: 999}),
			failedMark + " failed",
		},
		classes: []totalClass{
			{symbol: scannedMark, color: pterm.FgGreen, label: "%d scanned", always: true},
			{symbol: failedMark, color: pterm.FgRed, label: "%d failed"},
		},
		extra: func(rows []*tableRow) []string {
			n := 0
			for _, row := range rows {
				if row.state == rowDone && row.class == scannedMark && unexpectedDefault(row.info.DefaultBranch, expected) {
					n++
				}
			}
			if n == 0 {
				return nil
			}
			return []string{text.FgYellow.Sprint("⚠") + fmt.Sprintf(" %d not on %s", n, expected)}
		},
	}
}

func unexpectedDefault(name, expected string) bool { return name != "" && name != expected }

// defaultBranchCell is a default branch name, marked when it isn't expected.
func defaultBranchCell(name, expected string) string {
	if unexpectedDefault(name, expected) {
		return text.FgYellow.Sprint("⚠ " + name)
	}
	return orDash(name)
}

// countsText is a repo's branch counts: "14 · 3 merged · 2 stale · 1 open".
// 14 is every branch but the default one; a count of 0 is left out.
func countsText(c branches.Counts) string {
	parts := []string{fmt.Sprint(c.Branches)}
	for _, p := range []struct {
		n     int
		label string
	}{{c.Merged, "merged"}, {c.Stale, "stale"}, {c.OpenPR, "open"}} {
		if p.n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", p.n, p.label))
		}
	}
	return strings.Join(parts, " · ")
}

// scannedCell is the Status cell of a repo whose branches are in.
func scannedCell(c branches.Counts) string {
	return text.FgGreen.Sprint(scannedMark) + " " + countsText(c)
}

// BranchesProgress is how the branches report shows its repos while it
// fetches their branches: the live repo table on a terminal, nothing
// elsewhere (the report prints the static table when it is done).
type BranchesProgress interface {
	// SetRepos lists the repos the report covers, all queued.
	SetRepos(repos []ghapi.RepoInfo)
	// Start draws the live area.
	Start()
	// RepoStarted marks a repo in flight.
	RepoStarted(name string)
	// RepoPage says which page of branches a repo is on; page 1 is implied
	// by RepoStarted.
	RepoPage(name string, page int)
	// RepoDone records a repo's classified branches.
	RepoDone(r branches.Repo)
	// RepoFailed records a repo whose branches could not be read.
	RepoFailed(name string)
	// Stop erases the live area.
	Stop()
	// FinishTable closes the table, with its totals; see RepoLister.
	FinishTable()
}

// NewBranchesProgress returns the branches report's progress for expected,
// the default branch name repos should use, and whether it draws: the live
// table when Out can show it (see SetupTerminal), else a no-op, and the
// report prints PrintBranchesRepoTable when it is done.
func NewBranchesProgress(expected string) (p BranchesProgress, live bool) {
	if !animate {
		return NoBranchesProgress{}, false
	}
	return &branchesTable{newTableProgress(newBranchesSpec(expected), Out)}, true
}

// branchesTable is the live repo table with the branches report's spec.
type branchesTable struct{ *tableProgress }

func (b *branchesTable) Start() { b.tableProgress.Start(0) }

func (b *branchesTable) RepoStarted(name string) { b.rowStarted(name, "fetching") }

func (b *branchesTable) RepoPage(name string, page int) {
	if page > 1 {
		b.rowVerb(name, fmt.Sprintf("fetching page %d", page))
	}
}

func (b *branchesTable) RepoDone(r branches.Repo) {
	b.rowDone(r.Name, scannedCell(r.Counts), scannedMark)
}

func (b *branchesTable) RepoFailed(name string) {
	b.rowDone(name, text.FgRed.Sprint(failedMark)+" failed", failedMark)
}

// NoBranchesProgress is a BranchesProgress that draws nothing, for a report
// that shows no progress (JSON).
type NoBranchesProgress struct{}

func (NoBranchesProgress) SetRepos([]ghapi.RepoInfo) {}
func (NoBranchesProgress) Start()                    {}
func (NoBranchesProgress) RepoStarted(string)        {}
func (NoBranchesProgress) RepoPage(string, int)      {}
func (NoBranchesProgress) RepoDone(branches.Repo)    {}
func (NoBranchesProgress) RepoFailed(string)         {}
func (NoBranchesProgress) Stop()                     {}
func (NoBranchesProgress) FinishTable()              {}

// PrintBranchesRepoTable prints the branches report's repo table once, with
// each repo's counts: the form for a pipe, a file or a console without ANSI,
// where the live table can't draw. repos holds the classified repos in the
// order of infos.
func PrintBranchesRepoTable(infos []ghapi.RepoInfo, repos []branches.Repo, expected string) {
	spec := newBranchesSpec(expected)
	status := make([]string, len(repos))
	for i, r := range repos {
		status[i] = scannedCell(r.Counts)
	}
	tw := repoTable(spec, allColumns(spec), infos, status, nil)
	tw.SetOutputMirror(Out)
	Println()
	tw.Render()
}

// branchMark is the symbol and colour of a branch status in the branch table.
// Every symbol is one terminal column wide.
func branchMark(s branches.Status) (string, text.Color) {
	switch s {
	case branches.Merged:
		return "✓", text.FgGreen
	case branches.Stale:
		return "⚠", text.FgYellow
	case branches.OpenPR:
		return "↑", text.FgCyan
	default:
		return "–", text.FgHiBlack
	}
}

// PrintBranchTable prints every branch but the default ones, repo by repo in
// the order of repos and by name within a repo: its status (with the merged
// PR's number), the date and age of its last commit, the author and whether
// it is protected. now is what ages are measured from.
func PrintBranchTable(repos []branches.Repo, now time.Time) {
	tw := table.NewWriter()
	tw.SetTitle("Branches")
	tw.AppendHeader(table.Row{"Repo", "Branch", "Status", "Last Commit", "Age", "Author", "Protected"})
	rows := 0
	for _, r := range repos {
		for _, b := range r.Branches {
			if b.Status == branches.Default {
				continue
			}
			rows++
			tw.AppendRow(table.Row{
				text.FgCyan.Sprint(r.Name), b.Name, branchStatusCell(b),
				commitDate(b.CommittedAt), age(b.CommittedAt, now), orDash(author(b.Branch)), protectedCell(b.Protected),
			})
		}
	}
	tw.SetColumnConfigs([]table.ColumnConfig{{Number: 5, Align: text.AlignRight}})
	tw.SetStyle(table.StyleLight)
	tw.SetOutputMirror(Out)
	Println()
	if rows == 0 {
		Println(text.Faint.Sprint("No branches besides the default ones."))
		return
	}
	tw.Render()
}

func branchStatusCell(b branches.Branch) string {
	symbol, color := branchMark(b.Status)
	cell := color.Sprint(symbol) + " " + string(b.Status)
	if b.Status == branches.Merged && b.MergedPR != nil {
		cell += fmt.Sprintf(" #%d", b.MergedPR.Number)
	}
	return cell
}

// author is the GitHub login of a branch's last commit, or the git name when
// the commit maps to no account.
func author(b ghapi.Branch) string {
	if b.AuthorLogin != "" {
		return b.AuthorLogin
	}
	return b.AuthorName
}

func commitDate(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.UTC().Format(time.DateOnly)
}

// age is how long ago t was, in whole days: "112d".
func age(t, now time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return fmt.Sprintf("%dd", int(now.Sub(t).Hours()/24))
}

func protectedCell(protected bool) string {
	if protected {
		return "yes"
	}
	return ""
}

// PrintBranchTotals prints the org totals that end the report.
func PrintBranchTotals(t branches.Totals, expected string) {
	tw := table.NewWriter()
	tw.SetTitle("Totals")
	for _, row := range []struct {
		label string
		n     int
	}{
		{"Repos", t.Repos},
		{"Branches (not default)", t.Branches},
		{"Merged, not deleted", t.Merged},
		{"Stale", t.Stale},
		{"Open PR", t.OpenPR},
		{"Active", t.Active},
		{"Default not " + expected, t.UnexpectedDefault},
	} {
		tw.AppendRow(table.Row{row.label, row.n})
	}
	tw.SetColumnConfigs([]table.ColumnConfig{{Number: 2, Align: text.AlignRight}})
	tw.SetStyle(table.StyleLight)
	tw.SetOutputMirror(Out)
	Println()
	tw.Render()
}
