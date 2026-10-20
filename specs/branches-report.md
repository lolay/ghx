# Branches report (`ghb`)

`ghb` lists every repository of a GitHub organization with its default branch
and every other branch, each classified, from the GitHub API alone. Users: the
"Branches report" section of [`README.md`](../README.md). Terminal layout:
[`terminal-output.md`](./terminal-output.md). Test seams:
[`testing.md`](./testing.md).

## Why

ghx syncs an org's repos and never writes to GitHub. Org settings enforcement
(delete head branches on merge, merge queues) belongs in a dedicated tool such as
github/safe-settings; ghx gets read-only hygiene and audit reports instead, and
this is the first. Branches left behind after their PR merged are what
"automatically delete head branches" prevents, so the report shows how much
cleanup exists before anyone turns that on.

It must not depend on local clones: a repo may never have been synced, or may be
filtered out of the sync. And a local ancestry check misses squash and rebase
merges, which leave the branch's commits out of the default branch's history;
only the pull request knows the branch merged.

## Command

```sh
ghb <ORG> [--type T] [--include RE] [--exclude RE] [--concurrency N]
          [--include-archived] [--stale-days N] [--expected-default NAME]
          [--format text|json] [--token TOKEN]
```

- **A binary of its own.** `ghb` ships beside `ghx` in the same archives,
  Homebrew formula and Scoop manifest, with its own man page (`ghb man`). Its
  root takes one argument and has no subcommands of its own, so an org named
  `branches`, `sync` or anything else is just an org: there is no clash to
  resolve, as there would be with a `ghx branches` subcommand next to
  `ghx <ORG> <PATH>`. An org named like a command fang or cobra adds
  (`completion`, `help`, `man`) is reached with `--`: `ghb -- man`.
- **No PATH.** Nothing is read from or written to disk but `~/.ghx.toml`
  (`%USERPROFILE%\.ghx.toml` on Windows), read the way `ghx` reads it (a legacy
  `.gh-export.toml` there is renamed, as for `ghx`). There is no per-directory
  config.
- **Settings** resolve flag, then `GITHUB_TOKEN` (token only), then
  `~/.ghx.toml`, then the default. `type`, `include`, `exclude`, `concurrency`
  and `token` are the keys `ghx` uses. ghb adds three, in `keyKinds`:

| Key | Flag | Default |
|---|---|---|
| `stale-days` | `--stale-days` | `90`; less than 1 is an error |
| `expected-default` | `--expected-default` | `main` |
| `include-archived` | `--include-archived` | off: archived repos are dropped before any branch is read |

`--format` is flag-only. A `concurrency` below 1 counts as 1.

- **Exit code** 0 when the report is printed, whatever it finds; 1 on any error,
  with no report.

## Flow

1. **Repos.** Print the settings block, validate the token (`GET /user`), list
   the org's repos (`GET /orgs/{org}/repos`, `ghapi.ListOrgRepos`), apply
   `type`, `include` and `exclude` (`ghapi.FilterRepos`), and drop archived
   repos unless asked for.
2. **Branches.** Read every repo's branches (`ghapi.ListBranches`),
   `concurrency` repos at a time (`errgroup` with `SetLimit`), in the live repo
   table on a terminal.
3. **Classifications.** Classify each repo's branches as its data arrives
   (`branches.ClassifyRepo`); this is local work and costs no API call. Then
   print the branch table and the org totals.

`--format json` runs the same three steps and prints nothing but the JSON
document on stdout. A config warning goes to stderr.

## Classification

Every branch but the default one gets one status, by the first rule that
matches (`branches.Classify`):

1. **merged:** it has a merged PR and no open PR, and still points at what
   merged.
2. **stale:** no open PR, and its last commit is more than `stale-days` old
   (exactly `stale-days` is not stale).
3. **open-pr:** it has an open PR.
4. **active:** everything else, including a branch whose PR was closed without
   merging and that has recent commits.

The default branch is never classified (its status is `default`), even when it
is old or was once a PR's head. Protected branches are marked, whatever their
status.

**A branch reused after its PR merged.** "Still points at what merged" means the
branch's tip commit is the merged PR's head (`headRefOid`), or the tip's
committer date is no later than `mergedAt`. New commits after the merge mean the
branch was reused, so it is not merged; it is judged on those commits (stale,
open-pr or active). Only the most recently updated merged PR is read, which is
the one a reuse would follow.

**PRs from forks.** A PR from a fork has its head branch in the fork, so it
never marks a branch of this repo. A branch in this repo that was also pushed to
a fork and merged from there reads as unmerged. Fork branches themselves are
out of scope; a fork that is itself in the org is reported as its own repo.

**A branch with no commit date** (its ref points at something other than a
commit) is never stale.

## Output

- **Repo table**, the live table `ghx` draws (see
  [`terminal-output.md`](./terminal-output.md)): Name, Visibility, Default
  Branch (yellow `⚠ trunk` when it isn't `expected-default`), Last Pushed and
  Status. In flight a row reads `⠋ fetching 1.2s`, then `fetching page 2` from
  the second page. A finished row reads `✓ 14 · 3 merged · 2 stale · 1 open`:
  14 branches besides the default, and each non-zero count. A failed one reads
  `✗ failed`, and repos stopped by that failure close as `interrupted`. The
  closing footer: `60 repos · ✓ 59 scanned · ⚠ 3 not on main · 41s`.
- **Branch table**, titled Branches: Repo, Branch, Status (`✓ merged #123`,
  `⚠ stale`, `↑ open-pr`, `– active`), Last Commit (UTC date), Age (whole
  days), Author (the GitHub login, else the git author name) and Protected
  (`yes`). Repos in list order, branches by name, default branches left out.
- **Totals**, the report's last table: repos, branches (not default), merged
  but not deleted, stale, open PR, active, and repos whose default isn't the
  expected name.

Piped or redirected output gets the same three tables, the repo table printed
once when every repo is in, with a note before the reading starts.

**JSON** (`--format json`) is one indented document, keys in lowerCamel:
`org`, `generatedAt`, `staleDays`, `expectedDefault`, `repos` (each with
`name`, `defaultBranch`, `unexpectedDefault`, `counts` and every branch,
default included: `name`, `status`, `protected`, `commitOid`, `committedAt`
(null without a commit), `authorName`, `authorLogin`, `openPrs`, `mergedPr`
(`number`, `mergedAt`, `headOid`, or null)), and `totals` (`repos`, `branches`,
`merged`, `stale`, `openPr`, `active`, `unexpectedDefault`).

## API usage

Read-only throughout: GET requests and GraphQL queries, never a mutation. A
token needs read access only: a classic token's `repo` scope (`public_repo` for
public repos), or a fine-grained token with **Metadata**, **Contents** and
**Pull requests** read.

| Call | When | Cost |
|---|---|---|
| `GET /user` | Once | 1 REST request |
| `GET /orgs/{org}/repos` | Once, 100 a page | 1 REST request per 100 repos |
| `POST /graphql`, the branches query | Per repo, 100 branches a page | 1 GraphQL request per 100 branches |
| `GET /repos/{owner}/{repo}/branches?protected=true` | Per repo | Usually 1 REST request |

REST would need one request per branch to read its last commit's date; GraphQL
reads 100 branches, their tip commits and their PRs in one. The query asks for
open and merged PRs as two connections (`states: OPEN` counted, `states: MERGED`
first by `UPDATED_AT`), so a pile of closed PRs can't push the one that matters
out of a short window. Protection comes from REST because GraphQL's
`branchProtectionRule` needs admin access.

**Raw POST, no GraphQL library.** The query goes through go-github's
`NewRequest` and `Do`: one fixed query, decoded into plain structs. That keeps
the client's auth, its test base URL, its typed rate-limit errors and
`formatAPIError`'s token redaction. `shurcooL/githubv4` would add a dependency
and a second client to wire into the test seam, for one query.

## Rate limits

A rate limit fails the whole report, with an error that says which limit and
when to retry; nothing is retried and no partial report is printed:

- **Secondary limit** (403 with `Retry-After`): `Failed to list branches of
  acme/web: GitHub secondary rate limit hit; retry after 1m0s`.
- **Primary limit** over REST (403 or 429 with `X-RateLimit-Remaining: 0`) or
  GraphQL (HTTP 200 with a `RATE_LIMITED` error): `... GitHub rate limit hit; it
  resets at 14:05 UTC`, from `X-RateLimit-Reset`.

Both are `*ghapi.RateLimitError` and match `ghapi.ErrRateLimited` with
`errors.Is`. The first failure cancels the repos still being read. The repo
listing that `ghx` shares maps the same errors, so a sync hitting a limit says
so the same way.

## Limitations

- **Rulesets.** A branch is marked protected when REST's `protected` filter
  includes it, which covers branch protection rules; a branch covered only by a
  repository ruleset may not be marked.
- **Dates are the committer's.** `committedDate` comes from the commit, so a
  rebased or clock-skewed commit can make a branch look newer or older than its
  push. The merged rule prefers the head-sha match for that reason.
- **No retries.** A large org can hit the secondary limit at a high
  `concurrency`; lower it and run again.
- **Fork PRs** don't count (see above).
- **No timing yet against a large real org.**
