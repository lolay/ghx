```
--- KICKOFF: begin orchestration at [deep] ---

  Status: 0/2 groups done | last review: — | current: ghx m1 s1 [deep] | BLOCKED at gate mode | updated 2026-10-14

  mode: pending | proposed unattended (harness=claude-code runner=cloud; CLAUDE_CODE_REMOTE=true, assigned branch claude/fervent-heisenberg-72nmt2) | guard 3x min $50 | fixups 2

  review: every-wave (log-only)

  Next model
    Cursor:      claude-opus-5-5[effort=high]
    Claude Code: /model opus                (/effort high)

  Prompt to paste into the next chat:
    In lolay/ghx, read specs/handoffs/plan-archive-reconcile-heron.md. The plan is already tagged.
    On branch claude/fervent-heisenberg-72nmt2 (task branch): subagents commit each finished step.
    Run the personal-plan-orchestrate skill from the top: walk to
    each tier boundary, dispatch subagents per the skill's
    procedure, and pause only where the recorded mode stops. Do not
    execute plan work inline. Update plan progress after each wave
    returns per the skill's procedure. The mode line above is the
    kickoff answer. If the Status line shows BLOCKED, re-post that
    question and wait; otherwise record the mode, print the kickoff
    summary and begin dispatching.

---
```

**Cost (API-equiv, Claude Code models)**

| wave | expected tokens | expected $ |
|---|---|---|
| 1 [deep] ghx m1 s1 | ~6.1M | ~$3.2 |
| 2 [exec] ghx m1 s2-s3 | ~2.3M | ~$1.0 |
| orchestrator | ~4.3M | ~$3.8 |
| **Total** | ~13M | ~$8.0 |

Expected values are estimates, good to about 2-3× per wave.

# Reconcile clone location with archived state, verified by origin

## Context

ghx already moves a newly archived repo and then syncs it, in the narrow case. `handleRemovedAndArchived` (`internal/cli/run.go:306`) diffs the previous `.ghx.json` against the current listing, moves a newly archived repo's `<out>/x` into `ARCHIVED/x` with `cloner.MoveRepo`, and the archived pass (`run.go:267`) then fetches and fast-forwards it (`TestRun_ArchivedRepoMovesToArchived`, `internal/cli/run_test.go:67`).

The problem is that the move is driven by the manifest diff, not by what is on disk, and nothing checks identity:

- No previous manifest (deleted, or a pre-existing tree): no move; the archived pass clones a duplicate into `ARCHIVED/x` and `<out>/x` is orphaned.
- A move that fails on run N+1 is never retried: the manifest already records `archived:true`.
- Unarchive: never moved back. A fresh `<out>/x` is cloned and `ARCHIVED/x` goes stale. A later re-archive hits `MoveRepo`, which `RemoveAll`s the existing `ARCHIVED/x` (`internal/cloner/cloner.go:534-545`) silently, local work included.
- `<name>.wiki` is never moved with its repo.
- Nothing confirms `<out>/x` is a clone of `x` before it is moved or deleted.
- Newly archived detection runs over unfiltered `allRepos`, so a repo excluded by `--include`/`--exclude`/`--type` is moved into `ARCHIVED/` and then never synced there.

Decision: replace the "newly archived" half of the manifest diff with a reconciliation of what is on disk, run every sync, in both directions, and gate every move or delete on an origin check. The `.ghx.json` schema does not change (byte compatibility with `internal/manifest/testdata/golden/.ghx.json` stays). Removed-repo handling (`DELETED/`) keeps using the manifest, since a removed repo is absent from the listing, but gets the same origin check.

## Design

For each repo in the filtered `repos` list (not `allRepos`):

- `want` = `<out>/<archived-dir>/x` if the repo is archived, else `<out>/x`; `other` = the opposite location.
- `other` has no `.git` directory: nothing to do.
- `other`'s origin doesn't name `x` (below): leave it, report `skipped (origin mismatch)` with the URL found.
- `want` already exists (anything at that path): don't overwrite. Report a conflict naming both paths; the sync pass then syncs `want` as usual. Never call `MoveRepo` onto an existing destination from this path.
- Archived and `--delete`: delete `other` (today's semantics, now origin-gated).
- Otherwise move `other` to `want`'s parent with `cloner.MoveRepo`; then move `other.wiki` likewise when it exists and `want.wiki` doesn't.
- Dry run reports each planned action with detail `dry-run` and touches nothing.

Origin check: the directory name matches by construction, so it proves nothing. Read the raw `remote.origin.url` with `git config --get remote.origin.url` (not `git remote get-url`, which expands `insteadOf` and so breaks under the test harness's URL rewrite). Take the last non-empty path segment, splitting on `/`, `\` and `:`, trim a trailing `.git`, and compare case-insensitively with `repo.Name`. That is protocol-agnostic: `https://github.com/org/x.git`, `https://user:tok@github.com/org/x`, `git@github.com:org/x.git`, `ssh://git@github.com/org/x/` and a local path all reduce to `x`. Host and owner are ignored on purpose, so an org rename, a GitHub Enterprise host or an SSH host alias doesn't cause a false mismatch. A missing or unreadable origin counts as a mismatch. A mismatch only ever leaves things alone and reports, so the check fails safe.

Where it runs: in `run.go` where `handleRemovedAndArchived` runs today (before the dry-run return, so dry runs report it), but unconditionally, not only when a previous manifest exists. `MoveRepo` itself keeps its current replace-destination semantics; callers check first. `checkSubdir` (`run.go:394`) stays as is.

Reuse: `cloner.CheckRepoName` (`internal/cloner/names.go:18`), `hasGitDir` (`cloner.go:448`), `cloner.MoveRepo` (`cloner.go:534`), `cloner.DeleteRepo` (`cloner.go:552`), the `gitRunner` in `internal/cloner/git.go`, the `world` harness in `internal/cli/harness_test.go`, and `gittest` helpers in `internal/gittest/gittest.go`.

## m1 - Reconcile archived clones

--- WAVE 1 [deep] ---

#### s1 - [deep] Reconcile clone location with archived state

Implement the Design above in one commit that passes `make ci`:

- Origin helper in `internal/cloner` (for example `origin.go`): a pure function that turns an origin URL into its repo name (`(string, bool)`), and a way to read a directory's raw origin URL through the existing `gitRunner`. Table tests in a black-box `_test.go` covering HTTPS, HTTPS with user info, scp-style SSH, `ssh://`, trailing slash, `.git` suffix, case, a POSIX local path, a Windows path, and empty input.
- The reconcile step, where the move/delete orchestration lives today (prefer `internal/cli/reconcile.go` beside `handleRemovedAndArchived`, keeping `cloner` free of layout policy; expose from `cloner` only what that needs). New actions in `internal/cloner/action.go` as needed (moved back from archived, skipped for origin mismatch, conflict), added to `AllActions` and rendered by the summary in `internal/ui`. Remove `ActionSkippedArchived` only if nothing references it.
- In `run.go`: drop the `newlyArchived` branch of `handleRemovedAndArchived`, keep the `removed` → `DELETED/` branch and add the origin check before its move or delete, and call the reconcile step on every run over the filtered `repos`. Update the section header text to cover unarchived repos.
- Keep `TestRun_ArchivedRepoMovesToArchived`, `..._OnAFirstSyncIsClonedIntoArchived`, `TestRun_CustomDirectoriesReplaceDeletedAndArchived`, `TestRun_DeleteRemovesRemovedAndArchivedRepos`, `TestRun_DryRunChangesNothingOnDisk`, `TestRun_RepoRemovedUpstreamMovesToDeleted`, `internal/cli/safety_test.go` and the table tests passing; adjust an expectation only where the Design changes the behavior, and say which in the commit body.
- Add `world.unarchiveRepo` to `internal/cli/harness_test.go` and two cli tests that prove the core: an archived repo with no previous manifest is moved, not duplicated; an unarchived repo moves back from `ARCHIVED/` and is then pulled.

Acceptance: `make ci` passes; the two new cli tests and the helper table tests pass; no test touches the network or the real home directory; the manifest golden file is untouched.

--- WAVE 2 [exec] ---

#### s2 - [exec] Cover the reconcile edge cases in cli tests

In `internal/cli/run_test.go`, using the `world` harness, add one test per case (table-driven where the setup shares a shape):

- a move that failed on one run is retried by the next (make the move fail, for example with a read-only parent or a file squatting the destination parent, then clear it)
- archive → unarchive → archive: exactly one clone exists after each run and a local commit made in it survives all three
- both `<out>/x` and `ARCHIVED/x` present: a conflict is reported and neither is deleted
- `<out>/x` whose origin names another repo: left alone and reported
- `--clone-wiki`: `x.wiki` moves with `x` in both directions
- a repo excluded by `--exclude` that becomes archived is not moved
- dry run reports the move and changes nothing on disk
- a removed repo whose clone's origin names another repo is not moved to `DELETED/`

Acceptance: `make ci` passes; each case above has a test that fails if the behavior is reverted.

#### s3 - [fast] Document reconciled placement

- `README.md` around lines 277-283: replace "moved there on the next run once they become archived" with the reconciled behavior: each run puts every clone where its archived state says, in both directions, moves a wiki with its repo, checks the clone's origin names the repo first, and reports a conflict instead of overwriting when both locations exist.
- Add `specs/repo-layout.md`: where each repo lives (`<out>/x`, `<out>/<archived-dir>/x`, `<out>/<deleted-dir>/x`, wikis beside them), the reconcile rules from the Design, and the origin check.
- `CHANGELOG.md`: one entry under Unreleased.

Acceptance: `make ci` passes; README, spec and changelog agree with the code.

## Verification

`make ci` at each step. The doctor reports go 1.26.2 against `.go-version` 1.27.2; `go.mod` requires 1.26.2 and `make ci` doesn't run the doctor, so the build is valid on 1.26.2. Then `go test ./internal/cli -run 'Archiv|Unarchiv|Reconcile' -v` to read the action strings.

## Review log

## Token log

**Counting header (Claude Code)**

- Line, one per model a chat ran, appended below: `tokens <row> <group-id> (<model>): input ~X / cache read ~R / cache write ~W / output ~Y | ~$C API-equiv`. `<row>` is `wave-N`, `review-wave-N`, or `wave-N-fix` and `review-wave-N-fix` for a fix-up wave; `<group-id>` is the wave's group id with hyphens (`ghx-m1-s1`); `<model>` is `message.model` without a date suffix. Round counts to two significant figures with `k` or `M`.
- Usage: `~/.claude/projects/<slug>/$CLAUDE_CODE_SESSION_ID.jsonl` (`<slug>` is the working directory with every character but a letter or digit turned into `-`, matched by prefix when long; without the id, the newest `.jsonl` there), plus its subagents' `<session-id>/subagents/**/agent-*.jsonl` in the same directory. Sum `message.usage` over assistant lines once per `message.id`, from the line with `stop_reason`: input `input_tokens`, cache read `cache_read_input_tokens`, cache write `cache_creation.ephemeral_5m_input_tokens` (with `ephemeral_1h_input_tokens` too, `cache write ~40k 5m + ~8k 1h`), output `output_tokens`. A call with no `stop_reason` line keeps its input-side counts, takes output as about 1,000, and its line ends `(output est.) session <id>`.
- Rates by `<model>`, $ per Mtok input / cached / output: `claude-opus-5-5` 4.00 / 0.20 / 20.00; `claude-sonnet-5-5` 2.00 / 0.20 / 10.00.
- `$C` = (input × in + cache read × cached + 5m write × in × 1.25 + 1h write × in × 2.00 + output × out) / 1M.
- In another harness, or on a model not listed here, count and price per plan-execution.md "Token accounting" and "Model price table" instead.

tokens orchestrator-kickoff plan-archive-reconcile-heron (claude-opus-5-5): input ~56 / cache read ~3.4M / cache write ~94k 5m + ~170k 1h / output ~27k | ~$3.01 API-equiv (output est.) session db99f13c-13a7-5420-b626-79e0efa04ff4
