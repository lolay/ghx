# Handoff: archive-reconcile-heron

Scratch for the orchestrate run of
[`plan-archive-reconcile-heron.md`](plan-archive-reconcile-heron.md). Remove it
(or promote what's durable to `specs/`) before the branch merges.

- **Branch:** `claude/fervent-heisenberg-72nmt2` in `lolay/ghx` (assigned by the
  cloud session; cut from `main` at `9785f6f`).
- **Mode:** unattended, confirmed 2026-10-14 in session db99f13c (dialog: "Unattended (Recommended)", "This session (Recommended)").
- **Done:** nothing yet. 0/2 waves.
- **Spend:** ~$3.0 (kickoff) of ~$8.0 expected (guard: $50).
- **Next unit:** wave 1 [deep] ghx m1 s1, reconcile clone location with archived
  state.
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

## Environment notes

- env: the session doctor reports go 1.26.2 against `.go-version` 1.27.2.
  `go.mod` requires 1.26.2 and `make ci` doesn't run the doctor, so build, lint
  and test on 1.26.2 are valid. Don't try to install another Go.
- env: the git identity is the runner's (`noreply@anthropic.com`); accept it.

## Deviations from plan

None yet.
