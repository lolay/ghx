# Handoff: archive-reconcile-heron

Scratch for the orchestrate run of
[`plan-archive-reconcile-heron.md`](plan-archive-reconcile-heron.md). Remove it
(or promote what's durable to `specs/`) before the branch merges.

- **Branch:** `claude/fervent-heisenberg-72nmt2` in `lolay/ghx` (assigned by the
  cloud session; cut from `main` at `9785f6f`).
- **Mode:** unattended, confirmed 2026-10-14 in session db99f13c (dialog: "Unattended (Recommended)", "This session (Recommended)").
- **Done:** wave 1 [deep] ghx m1 s1, `b884660..976fa01`, review CONCERNS (one doc line); 1-fix `421f8c1` PASS. 1/2 waves.
- **Spend:** ~$9.8 of ~$8.0 expected (guard: $50). Over expected: wave 1 [deep] ran ~$4.2 against ~$3.2, 1-fix ~$0.6 unplanned, the parent ~$5.1 so far.
- **Next unit:** wave 2 [exec] ghx m1 s2-s4. `origin/main` (`051e844`) is merged.
- **Resume:** paste the plan's Kickoff prompt into a new session.

## Pending question

None. Wave 1 is next.

## Decisions so far

- Identity check compares only the last path segment of the raw
  `remote.origin.url` (with `.git` trimmed, case-insensitive) against the repo
  name, so SSH vs HTTPS, host aliases, GHE hosts and org renames don't cause
  false mismatches. Gary raised SSH vs HTTPS fragility at planning; this
  resolves it.
- `MoveRepo` keeps its replace-destination semantics; the reconcile path checks
  the destination first and reports a conflict instead.
- The `.ghx.json` schema doesn't change.
- Wave 1 (review flagged the doc gap, otherwise passed the spec): the origin is read with `git --git-dir=<dir>/.git config --local --get remote.origin.url` (no repo discovery, no global config). A removed directory without `.git` is now reported `skipped (origin mismatch)` instead of being moved to `DELETED/`. The conflict check comes before the `--delete` branch, so with both copies present under `--delete` nothing is deleted. A wiki gets the same checks as its repo and follows a move but not a `--delete`. New actions: `moved (unarchived)`, `skipped (origin mismatch)`, `conflict`; `cloner.HasGitDir` is now exported; `handleRemovedAndArchived` is now `handleRemoved`; tests live in `internal/cli/reconcile_test.go`.
- Gary, 2026-10-14: the Visibility column reads `public`, `private` or `archived` (archived wins), and the separate Archived column is dropped (wave 2 s2).

## Environment notes

- env: the base Go is 1.24.7 at /usr/local/go; with GOTOOLCHAIN=auto it downloads go1.26.2 (the go.mod minimum), which is valid for `make ci` (the doctor's 1.27.2 pin is a warning, and `make ci` doesn't run the doctor). That downloaded toolchain has no `covdata` tool, so `go test -cover` fails on packages without tests: copy the toolchain to a scratch GOROOT, build `src/cmd/covdata` into its `pkg/tool/linux_amd64`, and run `GOTOOLCHAIN=local PATH=<scratch>/bin:<scratch>/goroot/bin:$PATH make ci`.
- env: /usr/local/bin/golangci-lint is v2.5.0 built with go1.25 and refuses this module. Build the Makefile's pinned v2.14.0 with `GOBIN=<scratch>/bin go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0` and put it first on PATH.
- env: the git identity is the runner's (`noreply@anthropic.com`); accept it.

## Deviations from plan

- Re-plan of wave 2 before it ran, on Gary's requests: s2 (Visibility shows archived, Archived column dropped) added; the edge-case tests trimmed to what wave 1 didn't cover (s3); the docs step (s4) adjusted for docs wave 1 already wrote and for the Visibility change. Wave 2 keeps its planned ~$1.0 expected, which now understates it.
- `origin/main` gained `051e844` (#11, live table order) after the branch was cut. It conflicts with wave 1 in `internal/cli/run.go`. The parent merged it as bookkeeping after 1-fix and before wave 2's snapshot, with a merge commit (no rebase; the branch is pushed). The only conflict was the `sort` import, which the branch no longer uses; `make ci` passes on the merge. Not a worker step, because `check_wave.py` would count main's commit in the worker's range.
- Follow-ups wave 1 found, not in scope: `--delete` still leaves `<out>/x.wiki` behind; a move into `DELETED/` still silently replaces an existing `DELETED/x`. Review nit, not blocking: under `--delete` the conflict detail says "syncing ARCHIVED/x", but archived repos aren't synced then.
