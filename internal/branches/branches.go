// Package branches classifies the branches of an organization's repositories
// and totals them, for ghb's report. It does no I/O: the branches come from
// ghapi.ListBranches.
package branches

import (
	"time"

	"github.com/lolay/ghx/internal/ghapi"
)

// Status is a branch's classification.
type Status string

const (
	// Default is the repository's default branch, which is never classified.
	Default Status = "default"
	// Merged is a branch whose pull request merged and that has no open one:
	// left behind after the merge. It catches squash and rebase merges, which
	// an ancestry check misses.
	Merged Status = "merged"
	// Stale is a branch with no open pull request whose last commit is older
	// than the threshold.
	Stale Status = "stale"
	// OpenPR is a branch with an open pull request.
	OpenPR Status = "open-pr"
	// Active is every other branch.
	Active Status = "active"
)

// Classify returns b's status, by the first rule that matches: merged, stale,
// open-pr, active. A merged PR counts only while the branch still points at
// what merged: its tip is the PR's head, or no newer than the merge. A branch
// with commits after the merge was reused and is judged on those commits.
func Classify(b ghapi.Branch, now time.Time, staleAfter time.Duration) Status {
	hasOpenPR := b.OpenPRs > 0
	switch {
	case !hasOpenPR && mergedAsIs(b):
		return Merged
	case !hasOpenPR && !b.CommittedAt.IsZero() && now.Sub(b.CommittedAt) > staleAfter:
		return Stale
	case hasOpenPR:
		return OpenPR
	default:
		return Active
	}
}

func mergedAsIs(b ghapi.Branch) bool {
	m := b.MergedPR
	if m == nil {
		return false
	}
	if m.HeadOID != "" && m.HeadOID == b.CommitOID {
		return true
	}
	return !b.CommittedAt.After(m.MergedAt)
}

// Options are the report's settings.
type Options struct {
	// Now is the time ages are measured from.
	Now time.Time
	// StaleAfter is how old a last commit must be for the branch to be stale.
	StaleAfter time.Duration
	// ExpectedDefault is the default branch name a repo is expected to use.
	ExpectedDefault string
}

// Branch is a classified branch.
type Branch struct {
	ghapi.Branch
	Status Status
}

// Repo is one repository's classified branches.
type Repo struct {
	Name          string
	DefaultBranch string
	// UnexpectedDefault is true when the repo has a default branch and it
	// isn't Options.ExpectedDefault.
	UnexpectedDefault bool
	// Branches holds every branch, the default one included, in name order.
	Branches []Branch
	Counts   Counts
}

// Counts totals branches by status. Default branches are not counted in
// Branches.
type Counts struct {
	Branches int `json:"branches"`
	Merged   int `json:"merged"`
	Stale    int `json:"stale"`
	OpenPR   int `json:"openPr"`
	Active   int `json:"active"`
}

// Totals are the org-wide counts.
type Totals struct {
	Repos int `json:"repos"`
	Counts
	// UnexpectedDefault counts repos whose default branch isn't the expected
	// name.
	UnexpectedDefault int `json:"unexpectedDefault"`
}

// ClassifyRepo classifies every branch of one repository.
func ClassifyRepo(name string, rb ghapi.RepoBranches, opts Options) Repo {
	r := Repo{
		Name:              name,
		DefaultBranch:     rb.DefaultBranch,
		UnexpectedDefault: rb.DefaultBranch != "" && rb.DefaultBranch != opts.ExpectedDefault,
		Branches:          make([]Branch, 0, len(rb.Branches)),
	}
	for _, b := range rb.Branches {
		status := Default
		if b.Name != rb.DefaultBranch {
			status = Classify(b, opts.Now, opts.StaleAfter)
			r.Counts.add(status)
		}
		r.Branches = append(r.Branches, Branch{Branch: b, Status: status})
	}
	return r
}

func (c *Counts) add(s Status) {
	c.Branches++
	switch s {
	case Merged:
		c.Merged++
	case Stale:
		c.Stale++
	case OpenPR:
		c.OpenPR++
	case Active:
		c.Active++
	}
}

// Total sums repos into org totals.
func Total(repos []Repo) Totals {
	t := Totals{Repos: len(repos)}
	for _, r := range repos {
		t.Branches += r.Counts.Branches
		t.Merged += r.Counts.Merged
		t.Stale += r.Counts.Stale
		t.OpenPR += r.Counts.OpenPR
		t.Active += r.Counts.Active
		if r.UnexpectedDefault {
			t.UnexpectedDefault++
		}
	}
	return t
}
