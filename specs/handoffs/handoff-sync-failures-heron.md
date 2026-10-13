# Handoff: sync-failures-heron

Plan: `specs/handoffs/plan-sync-failures-heron.md` (personal-plan-orchestrate run).

- **Branch:** `claude/ecstatic-archimedes-2dxtun` (assigned by the cloud session; task branch).
- **Mode:** unattended, confirmed 2026-10-13 in session 8e4630de-74dd-5cb4-9a70-cd78dd6ff4a2 (harness=claude-code runner=cloud).
- **Done:** wave 1 [exec] ghx m1 s1, 32a86d9..38bcf57, review PASS; wave 2 [deep] ghx m1 s2, aafde10..f4532de, review PASS; wave 3 [exec] ghx m1 s3-s5, c057053..0a7d343, review CONCERNS (three README defects); fix-up 3-fix, c057053..0ed8ddd, review PASS. Plan complete.
- **Next unit:** none. The plan is complete; waiting on Gary for the PR decision.
- **Spend:** ~$12.48 of ~$10 expected, over expected (guard: $50); the kickoff parent was ~$4.52 of that.
- **Resume:** paste the plan's Kickoff prompt into a new session on this branch.

## Pending question (verbatim)

Open a PR for `claude/ecstatic-archimedes-2dxtun` into `main`? Before merge, a cleanup commit removes `specs/handoffs/plan-sync-failures-heron.md` and this handoff (nothing here needs promoting to `specs/`; `specs/testing.md` already carries the durable test notes).

## Review log verdicts

- wave-1 (ghx m1 s1) 32a86d9..38bcf57: PASS.
- wave-2 (ghx m1 s2) aafde10..f4532de: PASS.
- wave-3-fix (ghx m1 s3-s5) c057053..0ed8ddd: PASS.
- wave-3 (ghx m1 s3-s5) c057053..0a7d343: CONCERNS, three README defects (60-minute limit omitted, stale "Safe with local work" bullet, wrong git-lfs claim).

## Fix-ups, retries and checkpoints

- Gate 2 (before wave 2 [deep], after wave 1 [exec]) passed as an unattended checkpoint.
- 3-fix: automatic fix-up for the wave-3 review CONCERNS (first concern in the streak, no gate).

## Deviations from plan

- Gate 2 before wave 2 was logged, not asked (unattended mode).
- Fix-up 3-fix for wave 3's README findings, run automatically.

## Follow-ups flagged (not in this plan)

- A clone kept after its checkout failed (LFS) reads as `fetched only / local changes` on every later run, since every file shows deleted in the index.
- A ctx cancelled between the fetch and `remoteBranches` can report `skipped (empty)`; nothing is deleted on that path.
- Retry matches transient markers only, with no explicit `Repository not found` / auth exclusion; today's git output for those carries no marker.
- From planning: Git LFS content isn't fetched, submodules aren't initialized, and a repo renamed on GitHub is re-cloned under its new name.

## Decisions so far

- wave-1 (ghx m1 s1), review PASS: `operationInProgress(ctx, dir)` takes a ctx (the spec wrote `(dir)`); `upstreamTrack` returns the full ref (`refs/remotes/origin/x`), as `%(upstream)` prints it; a production seam `withTimeout` (package variable wrapping `context.WithTimeout`) lets `RecordGit(...).Timeouts()` show each command's timeout; fetch doesn't retry once ctx is done.
- wave-1: `gitResult.Message()` now returns git's last `fatal:`/`error:` line, so `TestCloneRepos_MasksTheTokenInFailureDetails`' real-clone case asserts `Contains "fatal:"` plus no token rather than the quoted URL.
- wave-2 (ghx m1 s2), review PASS: following a force-push uses `reset --keep refs/remotes/origin/<b>`, not `reset --hard` (it refuses to overwrite an untracked file the new tip adds; on refusal the backup ref is deleted and the result is fetched only). No path runs `reset --hard`. Docs (s5) should describe it as a reset that never overwrites files.
- wave-2: `status` passes `--untracked-files=no` inside `git.go`, so the wiki path already follows the untracked-files rule; `syncExisting(ctx, git, dest, repo, opts) RepoResult` is the entry point s3 reuses, with helpers `updateTarget`, `followForcePush`, `updateOther`, `tipsBeforeFetch`; `cloneNew` holds the clone path.
- wave-2: the backup ref is created with `update-ref <ref> <sha> ""`, so it never overwrites an earlier backup; after a force-push over one unpushed commit the detail counts 2 local commits (the local one plus the dropped upstream one).
- wave-3 (ghx m1 s3-s5), review CONCERNS (code PASS): the wiki target branch is read from `origin/HEAD` before `syncExisting` (fetch never moves it); every `failed wiki` result is named `<repo>.wiki`; an existing wiki that is now empty upstream is dropped quietly; wiki not-found is case-insensitive "not found" or "error: 404"; the counts table also prefixes `failed wiki` with ✗.
- wave-1: the worker already added an `## [Unreleased]` CHANGELOG entry and a `specs/testing.md` seams row for s1; later steps extend those entries rather than duplicating them.

## Environment notes

- env: The container's `/usr/local/bin/golangci-lint` was 2.5.0, built with go1.25, and refused the go 1.26.2 module (`make lint` failed at baseline). The parent built the pinned v2.14.0 from source (`go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0`; the release download got HTTP 403 through the proxy) and put it at `/usr/local/bin/golangci-lint` (the old binary is `golangci-lint-2.5.0`). A new container needs this again.
- env: The installed Go 1.26.2 lacked the `covdata` tool (`go: no such tool "covdata"` under `make test`). The parent ran `go install cmd/covdata`. A new container needs this again.
- env: `.go-version` pins 1.27.2 while the container has 1.26.2 (`make doctor` flags it). `go.mod`'s minimum is 1.26.2, so `make ci` passes on 1.26.2 with the two fixes above.
- env: `make ci` passes at baseline `d984df8`.

## Completion summary

See the plan's `## Completion summary` (same content).
