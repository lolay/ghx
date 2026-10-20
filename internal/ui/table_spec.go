package ui

import (
	"time"

	"github.com/pterm/pterm"

	"github.com/lolay/ghx/internal/cloner"
)

// tableSpec is what makes a live repo table the sync's or the branches
// report's: the renderer (tableProgress) and its layout (tableLayout) are the
// same for both.
type tableSpec struct {
	title   string
	columns []repoColumn
	// drops is the order the table gives up columns, by index into columns,
	// while it is wider than the terminal.
	drops []int
	// verb starts the live footer: "Syncing 34/60 ...".
	verb string
	// statusCells are the longest forms a finished Status cell takes; the
	// column is as wide as the widest of them and of the in-flight forms.
	statusCells []string
	// verbs are what an in-flight row can be doing, as RowStarted is told.
	verbs []string
	// classes are the closing footer's counts, one per row class, in order.
	classes []totalClass
	// extra, when set, adds parts to the closing footer after the classes.
	extra func(rows []*tableRow) []string
}

// totalClass is one count of the closing footer: the finished rows whose
// class is symbol, shown as the coloured symbol and label (which holds one
// %d). A class that isn't always shown appears only when its count isn't 0.
type totalClass struct {
	symbol string
	color  pterm.Color
	label  string
	always bool
}

// syncSpec is the sync's "Repos to export" table.
var syncSpec = newSyncSpec()

func newSyncSpec() *tableSpec {
	spec := &tableSpec{
		title:   "Repos to export",
		columns: syncColumns,
		drops:   []int{columnPushed, columnBranch, columnLanguage},
		verb:    "Syncing",
		verbs:   []string{"cloning", "pulling"},
	}
	for _, a := range cloner.AllActions {
		symbol, _ := statusMark(a)
		spec.statusCells = append(spec.statusCells, symbol+" "+actionText(a, longestElapsed))
	}
	// One class per symbol, with an action that stands for it.
	for _, c := range []struct {
		action cloner.Action
		label  string
		always bool
	}{
		{cloner.ActionCloned, "%d cloned", true},
		{cloner.ActionPulled, "%d pulled", false},
		{cloner.ActionUpToDate, "%d up to date", false},
		{cloner.ActionFetchedOnly, "%d fetched only", false},
		{cloner.ActionSkippedName, "%d skipped", false},
		{cloner.ActionSkippedEmpty, "%d empty", false},
		{cloner.ActionFailed, "%d failed", false},
	} {
		symbol, color := statusMark(c.action)
		spec.classes = append(spec.classes, totalClass{symbol: symbol, color: color, label: c.label, always: c.always})
	}
	return spec
}

// syncResult is a sync row's finished Status cell and class.
func syncResult(action cloner.Action, elapsed time.Duration) (cell, class string) {
	symbol, color := statusMark(action)
	return color.Sprint(symbol) + " " + actionText(action, elapsed), symbol
}
