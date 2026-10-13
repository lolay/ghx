# Handoff: sync-failures-heron

Plan: `specs/handoffs/plan-sync-failures-heron.md` (personal-plan-orchestrate run).

- **Branch:** `claude/ecstatic-archimedes-2dxtun` (assigned by the cloud session; task branch).
- **Mode:** unattended, confirmed 2026-10-13 in session 8e4630de-74dd-5cb4-9a70-cd78dd6ff4a2 (harness=claude-code runner=cloud).
- **Done:** wave 1 [exec] ghx m1 s1, 32a86d9..38bcf57, review PASS.
- **Next unit:** wave 2 [deep] ghx m1 s2 (fetch-first decision table, clone fallback, empty skip); gate 2 passes as an unattended checkpoint.
- **Spend:** ~$5.65 of ~$10 expected (guard: $50); the kickoff parent was ~$4.52 of that.
- **Resume:** paste the plan's Kickoff prompt into a new session on this branch.

## Pending question (verbatim)

None.

## Review log verdicts

- wave-1 (ghx m1 s1) 32a86d9..38bcf57: PASS.

## Fix-ups, retries and checkpoints

None yet.

## Decisions so far

- wave-1 (ghx m1 s1), review PASS: `operationInProgress(ctx, dir)` takes a ctx (the spec wrote `(dir)`); `upstreamTrack` returns the full ref (`refs/remotes/origin/x`), as `%(upstream)` prints it; a production seam `withTimeout` (package variable wrapping `context.WithTimeout`) lets `RecordGit(...).Timeouts()` show each command's timeout; fetch doesn't retry once ctx is done.
- wave-1: `gitResult.Message()` now returns git's last `fatal:`/`error:` line, so `TestCloneRepos_MasksTheTokenInFailureDetails`' real-clone case asserts `Contains "fatal:"` plus no token rather than the quoted URL.
- wave-1: the worker already added an `## [Unreleased]` CHANGELOG entry and a `specs/testing.md` seams row for s1; later steps extend those entries rather than duplicating them.

## Environment notes

- env: The container's `/usr/local/bin/golangci-lint` was 2.5.0, built with go1.25, and refused the go 1.26.2 module (`make lint` failed at baseline). The parent built the pinned v2.14.0 from source (`go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0`; the release download got HTTP 403 through the proxy) and put it at `/usr/local/bin/golangci-lint` (the old binary is `golangci-lint-2.5.0`). A new container needs this again.
- env: The installed Go 1.26.2 lacked the `covdata` tool (`go: no such tool "covdata"` under `make test`). The parent ran `go install cmd/covdata`. A new container needs this again.
- env: `.go-version` pins 1.27.2 while the container has 1.26.2 (`make doctor` flags it). `go.mod`'s minimum is 1.26.2, so `make ci` passes on 1.26.2 with the two fixes above.
- env: `make ci` passes at baseline `d984df8`.
