# Plan: `ghb`, a read-only branches report shipped beside `ghx`, plus man pages

Branch: `claude/eloquent-gauss-twqhyb` (assigned by the harness). Run `make ci` before every commit, then push each step.

## Context

ghx gets read-only hygiene reports, and this is the first. It lists every repo's default branch and its branches, each classified as merged, stale, open-pr or active, entirely from the GitHub API. That shows how much cleanup "automatically delete head branches" would have prevented before anyone turns it on.

Decisions from this session:
- **Two binaries, one release.** `ghx <ORG> <PATH>` stays exactly as it is. A new `ghb <ORG>` runs the report. Both ship in the same archives, Homebrew formula and Scoop manifest.
- **No org-name clash.** With no subcommand on either root, an org named `branches` (or anything else) can't collide with a command. That's stated in the spec, and tested with an org named `branches`.
- **No PATH for `ghb`.** It needs no local state. Config resolves flag, then env (`GITHUB_TOKEN`), then `~/.ghx.toml` via `config.Load("")`. The home file is shared, so `ghx` doesn't warn about `ghb`'s keys.
- **Man pages for both binaries in the formula.** fang v1.0.0 already adds a hidden `man` subcommand (mango-cobra is in the module graph), so `ghx man` and `ghb man` print roff with no new dependency.
- **This is additive, not breaking.** The next release is a minor bump.

## m1 - `ghb` binary skeleton, build and packaging [exec]

- `cmd/ghb/main.go` calls `cli.ExecuteBranches()`. In `internal/cli/root.go`, `Execute` and the new `ExecuteBranches` share `execute(ctx, cmd)`. `newBranchesCmd()` (`Use: "ghb <ORG>"`, `cobra.ExactArgs(1)`, `Version: buildinfo.String()`, same version template) starts as a stub in this milestone.
- Makefile: `build` compiles `bin/ghx` and `bin/ghb`. A new `man` target writes `manpages/ghx.1` and `manpages/ghb.1` from `bin/<name> man`. `manpages/` is gitignored and removed by `clean`. Update `Makefile.md`.
- `.goreleaser.yaml`: add a second `builds` entry (`id: ghb`, `main: ./cmd/ghb`, same ldflags and matrix). A `before.hooks` entry runs `make man`. The archive takes both builds and adds `manpages/*.1`; archive names are unchanged.
- `scripts/ghx.rb.tmpl`: `bin.install "ghx", "ghb"`, `man1.install "manpages/ghx.1", "manpages/ghb.1"`, and a `--version` test for each binary. `scripts/ghx.json.tmpl`: `"bin": ["ghx.exe", "ghb.exe"]` (Scoop has no man pages). Check with `make snapshot` and the `DRY_RUN=1` publish scripts.
- Tests: `ghb --version` prints `ghb <release>+<sha>`, and `ghx man` / `ghb man` print a `.TH` header.
- Name clash: fang and cobra add `man`, `completion` and `help` as subcommands, so an org with one of those names would run the subcommand instead of the report. This already affects `ghx` today. Check that `ghb -- man` and `ghx -- completion <dir>` reach the root command as positional args; if they do, document `--` as the escape and test it. If they don't, have the root handle those three names before cobra dispatches them. Update `specs/releasing.md` and the AGENTS.md layout table (`cmd/ghb/`, `internal/branches/`).

## m2 - GraphQL branch listing in `ghapi` [deep]

- **No new dependency.** `ghapi.ListBranches(ctx, token, owner, repo)` sends a raw POST through go-github's `client.NewRequest("POST", "graphql", body)` and `client.Do`. That reuses auth, the test `baseURL` and go-github's `*RateLimitError` and `*AbuseRateLimitError` (with `RetryAfter`). Errors go through `formatAPIError`, so the token stays redacted.
- **One query per repo**, paginated with a `refs(after:)` cursor:
  `repository{ defaultBranchRef{name} refs(refPrefix:"refs/heads/", first:100, after:$c){ pageInfo{hasNextPage endCursor} nodes{ name target{ oid ... on Commit{ committedDate author{name user{login}} } } open: associatedPullRequests(states:OPEN, first:1){totalCount} merged: associatedPullRequests(states:MERGED, first:1, orderBy:{field:UPDATED_AT,direction:DESC}){ nodes{ number mergedAt headRefOid } } } } }`
  The open and merged PRs are separate aliases, so a pile of closed PRs can't push the one that matters out of a `first:5` window.
- **Protected branches** come from one REST `GET /repos/{o}/{r}/branches?protected=true` per repo, paginated. Read access is enough; GraphQL `branchProtectionRule` needs admin.
- **Rate limits.** HTTP 403 and 429 come back as go-github's typed errors. A GraphQL HTTP 200 with `errors[].type == "RATE_LIMITED"` maps to `ErrRateLimited`. Either way the message reads like "GitHub secondary rate limit; retry after 60s" or "resets at 14:05 UTC". There are no retries, and the whole report fails rather than printing a partial one.
- **Fake API.** `ghapitest` serves `POST /graphql` (decodes `variables`, cursor pagination at 100, `SetBranches`), `GET /repos/{o}/{r}/branches` (honours `protected`) and `FailNext` for both rate-limit shapes.
- **Tests.** 250 branches over 3 pages, default branch `trunk`, an empty repo (`defaultBranchRef` null), both rate-limit shapes, and no token in any error text.

## m3 - Classification [deep]

New pure package `internal/branches`. `Classify(b, now, staleAfter)` checks the rules in order and the first match wins. The default branch is never classified.
1. **merged:** no open PR, and a merged PR whose `headRefOid` equals the tip oid, or whose `mergedAt` is at or after the tip's `committedDate`. New commits after the merge mean the branch was reused, so it falls through to the next rule.
2. **stale:** no open PR, and the tip is older than `staleAfter`.
3. **open-pr:** has an open PR.
4. **active:** everything else.

PRs from forks have their head ref in the fork, so they never mark a branch here. A `Report` model holds per-repo rows and the org totals: repos, branches, merged-not-deleted, stale, and repos whose default isn't the expected name. Table tests cover each rule, reuse after a merge, an oid match with an older date, exactly-at-threshold, and the default branch.

## m4 - Make the live repo table reusable [deep]

`ghb` should look and feel like `ghx`: the same settings table, the same "Authenticating / Fetching repos / Found N repos" lines, and the same live repo grid (fixed widths, finished rows scrolling up in list order, an in-place footer with a progress bar, a totals footer when it closes). Today `tableProgress` and `tableLayout` in `internal/ui` are hard-wired to the sync, through its columns, `cloner.RepoResult`/`Action` and its totals.

- Pull out a table spec: the title, a column list (header, a value function over `ghapi.RepoInfo`, a drop priority for narrow terminals), the status-column width, a footer verb ("Syncing" / "Scanning"), and a totals function. `tableProgress` takes a spec and records a generic row result (mark, colour, status text, a count key). `cloner.Progress` becomes a thin adapter that maps `RepoResult` onto it, so `NewProgress()` and the run flow don't change.
- `ghx` output must not change by a byte. The existing `progress_table_test.go`, `table_test.go` and `ui_test.go` must pass unchanged, and they are the proof.
- Add `ui.NewBranchesProgress()` and `ui.PrintBranchesRepoTable` (the static table for pipes) with the `ghb` spec below.

## m5 - `ghb` command and output [exec]

- **Flags:** `--token`, `--type`, `--include`, `--exclude`, `--concurrency`, `--include-archived`, `--stale-days`, `--expected-default`, `--format text|json`.
- **New `keyKinds`:** `stale-days` (int, default 90), `expected-default` (string, default `main`), `include-archived` (bool).
- **Reuse:** `ResolveToken`, `Resolve*`, `ValidateToken`, `ListOrgRepos` and `FilterRepos`. Archived repos are dropped before any GraphQL call.
- **Concurrency:** repos are fetched with `errgroup` `SetLimit(concurrency)` (`x/sync` moves from indirect to direct). The first error cancels the rest. A package-level `now` lets tests pin the clock.
- **Flow**, in the same three phases as `ghx`:
  1. **Repos.** Print the settings table (`ui.PrintSettings`: token, type, include and exclude, stale days, expected default, include archived, concurrency), authenticate, then "Fetching repos for acme... Found 60 repos" with `ListOrgRepos` and `FilterRepos`, dropping archived repos unless asked.
  2. **Branches.** The live repo grid, one row per repo, with `concurrency` workers each running the paginated GraphQL query and the protection call. Columns: Name, Visibility, Default Branch (yellow with `⚠` when it isn't the expected name), Branches, Status. While a repo is in flight its status reads `fetching branches · page 2`. When it finishes, the status shows the counts, e.g. `✓ 14 · 3 merged · 2 stale · 1 open-pr`. A rate-limit or API error marks that row `✗`, cancels the rest (they close as `interrupted`) and fails the run.
  3. **Classifications.** Classification is local work over the data from phase 2, so it costs no API calls. It runs as each repo's data arrives, which is how the row can show its counts. When the grid closes, its totals footer reads `60 repos · 412 branches · ✓ 97 merged-not-deleted · ⧗ 58 stale · ⚠ 3 not on main · 41s`. Then a second grid in the same style lists the branches: Repo, Branch, Status (marks: merged `✓` green, stale `⧗` yellow, open-pr `↑` cyan, active `–` grey), Last Commit, Author, Protected (plain `yes`; every mark must be one column wide). Rows are sorted by repo and then branch, default branches excluded.
- **Pipes and non-ANSI consoles** get the static form, as with `ghx`: a note per phase, the repo table printed once, then the branch grid and the totals.
- **JSON output:** `--format json` writes one document to stdout, with no settings table and no grids.
- **Exit codes:** 0 whatever the report finds, 1 on an error.
- **CLI tests:** each classification end to end, archived repos skipped and shown with `--include-archived`, the non-`main` flag, the totals, the JSON shape, a rate-limit error that exits 1 and prints no report, and an org named `branches`.

## m6 - Specs and docs [exec]

- **`specs/branches-report.md`:** the command and its config keys, why it's a separate binary (no clash), the classification rules with the reuse and fork cases, API cost (one GraphQL request per 100 branches plus one REST call per repo), rate-limit behaviour, token scopes (read access to contents, metadata and pull requests), and limitations: rulesets only appear as protected if REST `protected` reflects them, `committedDate` is supplied by the committer, and there are no retries.
- **Other docs:** a report-layout section in `specs/terminal-output.md`; `ghb`, the CLI reference and man pages in `README.md`; an Unreleased entry in `CHANGELOG.md`.

## Verification

- `make ci` after each milestone.
- `make snapshot`, then check that every archive holds `ghx`, `ghb` and `manpages/*.1`.
- Run both `DRY_RUN=1` publish scripts and read the rendered formula and manifest.
- `bin/ghb --help`, `bin/ghb man | head`, `bin/ghx acme ./x --dry-run` (unchanged).
- With a real token: `bin/ghb lolay` and `bin/ghb lolay --format json`.
