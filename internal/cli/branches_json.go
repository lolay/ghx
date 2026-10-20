package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/lolay/ghx/internal/branches"
)

// jsonReport is ghb --format json: one document, keys in lowerCamel.
type jsonReport struct {
	Org             string          `json:"org"`
	GeneratedAt     time.Time       `json:"generatedAt"`
	StaleDays       int             `json:"staleDays"`
	ExpectedDefault string          `json:"expectedDefault"`
	Repos           []jsonRepo      `json:"repos"`
	Totals          branches.Totals `json:"totals"`
}

type jsonRepo struct {
	Name              string          `json:"name"`
	DefaultBranch     string          `json:"defaultBranch"`
	UnexpectedDefault bool            `json:"unexpectedDefault"`
	Counts            branches.Counts `json:"counts"`
	Branches          []jsonBranch    `json:"branches"`
}

type jsonBranch struct {
	Name      string          `json:"name"`
	Status    branches.Status `json:"status"`
	Protected bool            `json:"protected"`
	CommitOID string          `json:"commitOid"`
	// CommittedAt is null when the branch's tip is not a commit.
	CommittedAt *time.Time    `json:"committedAt"`
	AuthorName  string        `json:"authorName"`
	AuthorLogin string        `json:"authorLogin"`
	OpenPRs     int           `json:"openPrs"`
	MergedPR    *jsonMergedPR `json:"mergedPr"`
}

type jsonMergedPR struct {
	Number   int       `json:"number"`
	MergedAt time.Time `json:"mergedAt"`
	HeadOID  string    `json:"headOid"`
}

// writeJSON writes the report as indented JSON. Every branch is listed, the
// default one with status "default".
func writeJSON(w io.Writer, s branchesSettings, opts branches.Options, repos []branches.Repo) error {
	report := jsonReport{
		Org:             s.org,
		GeneratedAt:     opts.Now.UTC(),
		StaleDays:       s.staleDays,
		ExpectedDefault: s.expectedDefault,
		Repos:           make([]jsonRepo, 0, len(repos)),
		Totals:          branches.Total(repos),
	}
	for _, r := range repos {
		jr := jsonRepo{
			Name: r.Name, DefaultBranch: r.DefaultBranch, UnexpectedDefault: r.UnexpectedDefault,
			Counts: r.Counts, Branches: make([]jsonBranch, 0, len(r.Branches)),
		}
		for _, b := range r.Branches {
			jb := jsonBranch{
				Name: b.Name, Status: b.Status, Protected: b.Protected, CommitOID: b.CommitOID,
				AuthorName: b.AuthorName, AuthorLogin: b.AuthorLogin, OpenPRs: b.OpenPRs,
			}
			if !b.CommittedAt.IsZero() {
				at := b.CommittedAt.UTC()
				jb.CommittedAt = &at
			}
			if m := b.MergedPR; m != nil {
				jb.MergedPR = &jsonMergedPR{Number: m.Number, MergedAt: m.MergedAt.UTC(), HeadOID: m.HeadOID}
			}
			jr.Branches = append(jr.Branches, jb)
		}
		report.Repos = append(report.Repos, jr)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		return fmt.Errorf("writing the JSON report: %w", err)
	}
	return nil
}
