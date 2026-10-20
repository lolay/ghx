package cli_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lolay/ghx/internal/cli"
	"github.com/lolay/ghx/internal/ghapi/ghapitest"
	"github.com/lolay/ghx/internal/ui"
)

// reportNow is the time ghb's tests measure ages from.
var reportNow = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

// ago is the RFC 3339 time days before reportNow.
func ago(days int) string { return reportNow.AddDate(0, 0, -days).Format(time.RFC3339) }

// branchesWorld is a fake GitHub for ghb, with an isolated home directory and
// the token in the environment, and the report's clock pinned. ghb needs no
// directory of its own.
type branchesWorld struct {
	t    *testing.T
	api  *ghapitest.Server
	home string
	org  string
	// repos is what the fake lists for org, in the order added.
	repos []ghapitest.Repo
}

func newBranchesWorld(t *testing.T, org string) *branchesWorld {
	t.Helper()
	home := t.TempDir()
	for _, key := range homeEnvKeys {
		t.Setenv(key, home)
	}
	t.Setenv("GITHUB_TOKEN", testToken)
	cli.SetNow(t, reportNow)
	// A rate-limit wait takes no time unless a test says otherwise.
	cli.SetPacerSleep(t, func(ctx context.Context, _ time.Duration) error { return ctx.Err() })

	api := ghapitest.NewServer(t, testToken, "octocat")
	api.RedirectGitHub(t)
	return &branchesWorld{t: t, api: api, home: home, org: org}
}

// addRepo lists a repo with its branches. defaultBranch is the default both
// REST and GraphQL report.
func (w *branchesWorld) addRepo(name, defaultBranch string, branches []ghapitest.Branch, opts ...func(*ghapitest.Repo)) {
	r := ghapitest.Repo{Name: name, DefaultBranch: defaultBranch, PushedAt: "2026-10-01T12:00:00Z"}
	for _, o := range opts {
		o(&r)
	}
	w.repos = append(w.repos, r)
	w.api.SetRepos(w.org, w.repos)
	w.api.SetBranches(w.org, name, defaultBranch, branches)
}

// run runs ghb with args and the org last, and returns what it printed to
// ui.Out and to stderr.
func (w *branchesWorld) run(args ...string) (stdout, stderr string, err error) {
	w.t.Helper()
	out, errOut := &lockedBuffer{}, &lockedBuffer{}
	orig := ui.Out
	ui.Out = out
	w.t.Cleanup(func() { ui.Out = orig })

	cmd := cli.NewBranchesCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs(append(args, w.org))
	err = cmd.ExecuteContext(w.t.Context())
	return out.String(), errOut.String(), err
}

// classified is a repo with one branch of every classification, plus main.
func classified() []ghapitest.Branch {
	return []ghapitest.Branch{
		{Name: "active-work", OID: "a1", CommittedDate: ago(2), AuthorName: "Ada", AuthorLogin: "ada"},
		{Name: "main", OID: "m1", CommittedDate: ago(400), Protected: true},
		{Name: "merged-squash", OID: "s1", CommittedDate: ago(20), AuthorName: "Bob",
			Merged: &ghapitest.MergedPR{Number: 11, MergedAt: ago(19), HeadRefOID: "s0"}},
		{Name: "open-pr", OID: "o1", CommittedDate: ago(300), OpenPRs: 1},
		{Name: "reused", OID: "r2", CommittedDate: ago(3),
			Merged: &ghapitest.MergedPR{Number: 12, MergedAt: ago(30), HeadRefOID: "r1"}},
		{Name: "stale-work", OID: "t1", CommittedDate: ago(120), AuthorName: "Cy Nologin"},
	}
}

// branchLine is the line of the branch table for repo and branch.
func branchLine(t *testing.T, out, repo, branch string) string {
	t.Helper()
	for line := range strings.SplitSeq(plain(out), "\n") {
		if strings.HasPrefix(line, "│ "+repo+" ") && strings.Contains(line, "│ "+branch+" ") {
			return line
		}
	}
	t.Fatalf("no branch line for %s/%s in:\n%s", repo, branch, plain(out))
	return ""
}

func TestBranches_ClassifiesEveryBranch(t *testing.T) {
	w := newBranchesWorld(t, testOrg)
	w.addRepo("api", "main", classified())

	out, _, err := w.run()
	require.NoError(t, err, out)

	tests := []struct {
		branch string
		wants  []string
	}{
		{"merged-squash", []string{"✓ merged #11", "2026-09-30", "20d", "Bob"}},
		{"stale-work", []string{"⚠ stale", "120d", "Cy Nologin"}},
		{"open-pr", []string{"↑ open-pr", "300d"}},
		{"active-work", []string{"– active", "2d", "ada"}},
		{"reused", []string{"– active", "3d"}},
	}
	for _, tt := range tests {
		t.Run(tt.branch, func(t *testing.T) {
			line := branchLine(t, out, "api", tt.branch)
			for _, want := range tt.wants {
				assert.Contains(t, line, want)
			}
		})
	}
	assert.NotRegexp(t, `│ api +│ main `, plain(out), "the default branch has no line")
}

func TestBranches_PrintsTheReportInOrder(t *testing.T) {
	w := newBranchesWorld(t, testOrg)
	w.addRepo("api", "main", classified())
	w.addRepo("legacy", "trunk", []ghapitest.Branch{{Name: "trunk", CommittedDate: ago(1)}, {Name: "old", CommittedDate: ago(200)}})

	out, _, err := w.run()
	require.NoError(t, err, out)
	got := plain(out)

	order := []string{"Settings:", "Stale after:", "90 days", "Authenticated as octocat", "Found 2 repos",
		"Repos to scan", "Branches", "Totals"}
	last := -1
	for _, want := range order {
		i := strings.Index(got, want)
		require.GreaterOrEqual(t, i, 0, "missing %q in:\n%s", want, got)
		assert.Greater(t, i, last, "%q is out of order", want)
		last = i
	}
	assert.Contains(t, got, "⚠ trunk", "a default branch that isn't main is flagged")
	assert.Contains(t, got, "✓ 5 · 1 merged · 1 stale · 1 open", "api's counts")
	assert.Contains(t, got, "✓ 1 · 1 stale", "legacy's counts")
	for _, want := range [][2]string{
		{"Repos", "2"}, {"Branches (not default)", "6"}, {"Merged, not deleted", "1"},
		{"Stale", "2"}, {"Open PR", "1"}, {"Active", "2"}, {"Default not main", "1"},
	} {
		assert.Regexp(t, fmt.Sprintf(`│ %s +│ +%s │`, regexpQuote(want[0]), want[1]), got)
	}
}

func regexpQuote(s string) string { return strings.NewReplacer("(", `\(`, ")", `\)`).Replace(s) }

func TestBranches_ArchivedRepos(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		config    string
		wantVault bool
	}{
		{"skipped by default", nil, "", false},
		{"included by flag", []string{"--include-archived"}, "", true},
		{"included by the home config", nil, "include-archived = true\n", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newBranchesWorld(t, testOrg)
			w.addRepo("api", "main", []ghapitest.Branch{{Name: "main"}, {Name: "topic", CommittedDate: ago(1)}})
			w.addRepo("vault", "main", []ghapitest.Branch{{Name: "main"}, {Name: "frozen", CommittedDate: ago(1)}},
				func(r *ghapitest.Repo) { r.Archived = true })
			if tt.config != "" {
				require.NoError(t, os.WriteFile(filepath.Join(w.home, ".ghx.toml"), []byte(tt.config), 0o600))
			}

			out, _, err := w.run(tt.args...)
			require.NoError(t, err, out)

			assert.Equal(t, tt.wantVault, strings.Contains(plain(out), "frozen"))
			asked := false
			for _, r := range w.api.Requests() {
				if r.Path == "/repos/acme/vault/branches" {
					asked = true
				}
			}
			assert.Equal(t, tt.wantVault, asked, "a skipped archived repo costs no API call")
		})
	}
}

func TestBranches_Settings(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		config string
		want   []string
	}{
		{"stale days by flag", []string{"--stale-days", "300"}, "", []string{"300 days", "(option)"}},
		{"stale days by config", nil, "stale-days = 400\n", []string{"400 days"}},
		{"expected default by flag", []string{"--expected-default", "trunk"}, "", []string{"⚠ main", "Default not trunk"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newBranchesWorld(t, testOrg)
			w.addRepo("api", "main", classified())
			if tt.config != "" {
				require.NoError(t, os.WriteFile(filepath.Join(w.home, ".ghx.toml"), []byte(tt.config), 0o600))
			}

			out, _, err := w.run(tt.args...)
			require.NoError(t, err, out)
			for _, want := range tt.want {
				assert.Contains(t, plain(out), want)
			}
		})
	}

	t.Run("a longer threshold makes stale active", func(t *testing.T) {
		w := newBranchesWorld(t, testOrg)
		w.addRepo("api", "main", classified())

		out, _, err := w.run("--stale-days", "365")
		require.NoError(t, err, out)
		assert.Contains(t, branchLine(t, out, "api", "stale-work"), "– active")
	})
}

func TestBranches_Filters(t *testing.T) {
	w := newBranchesWorld(t, testOrg)
	w.addRepo("api", "main", []ghapitest.Branch{{Name: "main"}, {Name: "a-topic", CommittedDate: ago(1)}})
	w.addRepo("web", "main", []ghapitest.Branch{{Name: "main"}, {Name: "w-topic", CommittedDate: ago(1)}}, func(r *ghapitest.Repo) { r.Private = true })

	out, _, err := w.run("--type", "private")
	require.NoError(t, err, out)
	assert.Contains(t, plain(out), "w-topic")
	assert.NotContains(t, plain(out), "a-topic")

	out, _, err = w.run("--exclude", "^web$")
	require.NoError(t, err, out)
	assert.Contains(t, plain(out), "a-topic")
	assert.NotContains(t, plain(out), "w-topic")
}

func TestBranches_PagesPastAHundredBranches(t *testing.T) {
	w := newBranchesWorld(t, testOrg)
	many := []ghapitest.Branch{{Name: "main"}}
	for i := range 230 {
		many = append(many, ghapitest.Branch{Name: fmt.Sprintf("topic-%03d", i), CommittedDate: ago(200)})
	}
	w.addRepo("big", "main", many)

	out, _, err := w.run()
	require.NoError(t, err, out)
	assert.Contains(t, plain(out), "✓ 230 · 230 stale")
	branchLine(t, out, "big", "topic-229")
}

func TestBranches_JSON(t *testing.T) {
	w := newBranchesWorld(t, testOrg)
	w.addRepo("api", "main", classified())
	w.addRepo("legacy", "trunk", []ghapitest.Branch{{Name: "trunk", CommittedDate: ago(1)}})

	out, errOut, err := w.run("--format", "json")
	require.NoError(t, err, errOut)

	var report struct {
		Org             string    `json:"org"`
		GeneratedAt     time.Time `json:"generatedAt"`
		StaleDays       int       `json:"staleDays"`
		ExpectedDefault string    `json:"expectedDefault"`
		Repos           []struct {
			Name              string `json:"name"`
			DefaultBranch     string `json:"defaultBranch"`
			UnexpectedDefault bool   `json:"unexpectedDefault"`
			Branches          []struct {
				Name      string `json:"name"`
				Status    string `json:"status"`
				Protected bool   `json:"protected"`
				MergedPR  *struct {
					Number int `json:"number"`
				} `json:"mergedPr"`
			} `json:"branches"`
		} `json:"repos"`
		Totals map[string]int `json:"totals"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &report), "stdout is the JSON alone:\n%s", out)

	assert.Equal(t, testOrg, report.Org)
	assert.True(t, reportNow.Equal(report.GeneratedAt))
	assert.Equal(t, 90, report.StaleDays)
	assert.Equal(t, "main", report.ExpectedDefault)
	require.Len(t, report.Repos, 2)
	assert.Equal(t, "trunk", report.Repos[1].DefaultBranch)
	assert.True(t, report.Repos[1].UnexpectedDefault)

	status := map[string]string{}
	for _, b := range report.Repos[0].Branches {
		status[b.Name] = b.Status
		if b.Name == "merged-squash" {
			require.NotNil(t, b.MergedPR)
			assert.Equal(t, 11, b.MergedPR.Number)
		}
		if b.Name == "main" {
			assert.True(t, b.Protected)
		}
	}
	assert.Equal(t, map[string]string{
		"main": "default", "merged-squash": "merged", "stale-work": "stale",
		"open-pr": "open-pr", "active-work": "active", "reused": "active",
	}, status)
	assert.Equal(t, map[string]int{
		"repos": 2, "branches": 5, "merged": 1, "stale": 1, "openPr": 1, "active": 2, "unexpectedDefault": 1,
	}, report.Totals)
	assert.NotContains(t, out, "Settings:")
}

func TestBranches_WaitsOutARateLimitAndFinishes(t *testing.T) {
	tests := []struct {
		name    string
		failure ghapitest.Failure
		want    string
	}{
		{"secondary", ghapitest.SecondaryLimit, "GitHub secondary rate limit hit; waiting 1m0s, until "},
		{"GraphQL", ghapitest.RateLimited, "GitHub rate limit hit; waiting "},
	}
	for _, tt := range tests {
		t.Run(tt.name+" text", func(t *testing.T) {
			w := newBranchesWorld(t, testOrg)
			w.addRepo("api", "main", classified())
			w.addRepo("web", "main", classified())
			w.api.Fail(testOrg, "web", tt.failure, 2)

			out, _, err := w.run()
			require.NoError(t, err, out)
			got := plain(out)
			assert.Contains(t, got, tt.want)
			assert.Contains(t, got, "Ctrl-C to stop.")
			assert.Contains(t, got, "Totals", "the report finishes")
			branchLine(t, out, "web", "stale-work")
		})

		t.Run(tt.name+" json", func(t *testing.T) {
			w := newBranchesWorld(t, testOrg)
			w.addRepo("api", "main", classified())
			w.addRepo("web", "main", classified())
			w.api.Fail(testOrg, "web", tt.failure, 1)

			out, errOut, err := w.run("--format", "json")
			require.NoError(t, err, errOut)
			assert.Contains(t, errOut, tt.want, "the wait is said on stderr")
			var report struct {
				Repos []struct{ Name string } `json:"repos"`
			}
			require.NoError(t, json.Unmarshal([]byte(out), &report), "stdout is the JSON alone:\n%s", out)
			assert.Len(t, report.Repos, 2)
		})
	}
}

func TestBranches_OnATerminalARateLimitCountsDownInTheFooter(t *testing.T) {
	terminal(t)
	w := newBranchesWorld(t, testOrg)
	w.addRepo("api", "main", classified())
	w.api.Fail(testOrg, "api", ghapitest.SecondaryLimit, 1)
	// Long enough for the live area to repaint during the wait.
	cli.SetPacerSleep(t, func(ctx context.Context, _ time.Duration) error {
		time.Sleep(300 * time.Millisecond)
		return ctx.Err()
	})

	out, _, err := w.run()
	require.NoError(t, err, out)
	got := plain(out)

	assert.Regexp(t, `⚠ GitHub secondary rate limit · resuming in [0-9m]+s · Ctrl-C to stop`, got)
	assert.NotContains(t, got, "hit; waiting", "the terminal gets the footer, not a note")
	assert.Contains(t, got, "1 repo · ✓ 1 scanned")
	assert.Contains(t, got, "Totals")
}

func TestBranches_AFailureFailsTheWholeReport(t *testing.T) {
	for _, format := range []string{"text", "json"} {
		t.Run(format, func(t *testing.T) {
			w := newBranchesWorld(t, testOrg)
			w.addRepo("api", "main", classified())
			// Listed by REST, unknown to GraphQL: the query fails.
			w.repos = append(w.repos, ghapitest.Repo{Name: "gone", DefaultBranch: "main"})
			w.api.SetRepos(testOrg, w.repos)

			out, _, err := w.run("--format", format)
			require.Error(t, err)
			assert.Equal(t, "Failed to list branches of acme/gone: Could not resolve to a Repository with the name 'acme/gone'.", err.Error())
			assert.NotContains(t, plain(out), "Totals", "no partial report")
			if format == "json" {
				assert.Empty(t, out)
			}
		})
	}
}

func TestBranches_ExitCodes(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantCode int
		wantErr  string
	}{
		{"a report that finds plenty is still success", []string{testOrg}, 0, ""},
		{"no org", nil, 1, "Accepts 1 arg(s), received 0"},
		{"a path is not taken", []string{testOrg, "./acme"}, 1, "Accepts 1 arg(s), received 2"},
		{"an unknown format", []string{"--format", "yaml", testOrg}, 1, `Unknown format "yaml": use text or json`},
		{"a stale threshold under a day", []string{"--stale-days", "0", testOrg}, 1, "Stale days must be at least 1, got 0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newBranchesWorld(t, testOrg)
			w.addRepo("api", "main", classified())

			var out, errOut lockedBuffer
			orig := ui.Out
			ui.Out = &out
			t.Cleanup(func() { ui.Out = orig })
			cmd := cli.NewBranchesCmd()
			cmd.SetOut(&out)
			cmd.SetErr(&errOut)
			cmd.SetArgs(tt.args)

			assert.Equal(t, tt.wantCode, cli.Run(t.Context(), cmd), "output:\n%s%s", out.String(), errOut.String())
			assert.Contains(t, errOut.String(), tt.wantErr)
		})
	}
}

// ghb takes one argument and has no subcommand of its own, so an org named
// branches (or sync) is just an org. One named like fang's and cobra's
// subcommands (completion, help, man) is reached with --.
func TestBranches_OrgsNamedLikeCommands(t *testing.T) {
	for _, tt := range []struct {
		org  string
		args []string
	}{
		{"branches", nil},
		{"sync", nil},
		{"man", []string{"--"}},
		{"completion", []string{"--"}},
	} {
		t.Run(tt.org, func(t *testing.T) {
			w := newBranchesWorld(t, tt.org)
			w.addRepo("api", "main", []ghapitest.Branch{{Name: "main"}, {Name: "topic", CommittedDate: ago(1)}})

			// Through fang, which adds the man command.
			var out, errOut lockedBuffer
			orig := ui.Out
			ui.Out = &out
			t.Cleanup(func() { ui.Out = orig })
			cmd := cli.NewBranchesCmd()
			cmd.SetOut(&out)
			cmd.SetErr(&errOut)
			cmd.SetArgs(append(tt.args, tt.org))

			require.Equal(t, 0, cli.Run(t.Context(), cmd), "output:\n%s%s", out.String(), errOut.String())
			assert.Regexp(t, `Organization: +`+tt.org+` +\(argument\)`, plain(out.String()))
			branchLine(t, out.String(), "api", "topic")
		})
	}
}

func TestBranches_NeedsNoLocalState(t *testing.T) {
	w := newBranchesWorld(t, testOrg)
	w.addRepo("api", "main", classified())
	cwd := t.TempDir()
	t.Chdir(cwd)
	before := snapshot(t, w.home)

	out, _, err := w.run()
	require.NoError(t, err, out)

	assert.Equal(t, before, snapshot(t, w.home), "the home directory is untouched")
	entries, err := os.ReadDir(cwd)
	require.NoError(t, err)
	assert.Empty(t, entries, "nothing is written to the working directory")
	for _, r := range w.api.Requests() {
		assert.Contains(t, []string{"GET", "POST"}, r.Method)
		if r.Method == "POST" {
			assert.Equal(t, "/graphql", r.Path, "the only POST is a GraphQL query, which reads")
		}
	}
}

func TestBranches_OnATerminalDrawsTheLiveTable(t *testing.T) {
	terminal(t)
	w := newBranchesWorld(t, testOrg)
	w.addRepo("api", "main", classified())
	w.addRepo("legacy", "trunk", []ghapitest.Branch{{Name: "trunk"}})

	out, _, err := w.run()
	require.NoError(t, err, out)
	got := plain(out)

	assert.Equal(t, 1, strings.Count(got, "Repos to scan"), "the table's top prints once")
	assert.Contains(t, got, "2 repos · ✓ 2 scanned · ⚠ 1 not on main")
	assert.NotContains(t, got, "Reading the branches of", "the live table replaces the note")
	assert.Less(t, strings.Index(got, "Repos to scan"), strings.Index(got, "Totals"))
}

func TestBranchesCmd_EveryFlagButFormatIsAConfigKey(t *testing.T) {
	assertFlagsAreConfigKeys(t, cli.NewBranchesCmd(), "format")
}

func TestBranches_OnATerminalAFailureClosesTheTable(t *testing.T) {
	terminal(t)
	w := newBranchesWorld(t, testOrg)
	w.addRepo("api", "main", classified())
	w.repos = append(w.repos, ghapitest.Repo{Name: "gone", DefaultBranch: "main"})
	w.api.SetRepos(testOrg, w.repos)

	out, _, err := w.run("--concurrency", "1")
	require.Error(t, err)
	got := plain(out)

	assert.Contains(t, got, "✗ failed")
	assert.Contains(t, got, "2 repos · ✓ 1 scanned · ✗ 1 failed", "the table closes with its totals")
	assert.NotContains(t, got, "Totals", "no report after a failure")
}
