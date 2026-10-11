# Handoff: ghx platforms (lynx)

Session handoff for the `personal-plan-orchestrate` run of
[`plan-ghx-platforms-lynx.md`](./plan-ghx-platforms-lynx.md). Scratch: removed
or promoted before the branch merges.

## Branch

- `feature/ghx-platforms-lynx` (task branch), cut from `main` at `b5d3119`
  with `--no-track` and pushed.

## Mode

- **unattended**, confirmed 2026-10-10 in session
  `98cba6b5-3f71-4517-9cdd-ab4709007e5a` (proposed gated; harness=claude-code,
  runner=none). An unattended record counts only in the session that
  confirmed it, or in a new session started by pasting the Kickoff prompt
  with the same harness and runner.
- Guard: 3x the expected total, min $50 (limit ~$54). Fix-up cap: 2 per group.
- The Cost table's orchestrator row was recomputed for unattended (one wait):
  expected total ~$18.

## Waves

| Wave | Tier | Steps | Gates before it (unattended: checkpoints) |
|---|---|---|---|
| 1 | `[exec]` | m1.s1-m1.s3 | 5 (canary) |
| 2 | `[deep]` | m1.s4-m1.s5 | 2 |
| 3 | `[exec]` | m2.s1-m2.s2 (m2.s2 `[fast]`, folded) | 4 (new milestone; write `specs/handoffs/handoff-m2-ci.md` first) |
| 4 | `[deep]` | m2.s3 | 2 |

## Done

- Kickoff: wave markers, Kickoff block, Cost table and counting header
  written; mode recorded. No wave has run.

## Spend

- Token log: $0 so far against ~$18 expected (API-equiv).

## Next

- Wave 1 `[exec]` ghx m1 s1-s3 (the canary of the orchestrating session).

## Pending question

- None.

## How to resume

Paste the plan's Kickoff prompt into a new chat on this machine (Opus, effort
high).

## Gotchas

- Single working directory (`/Users/gary/Projects/lolay/ghx`), so each wave
  has one group and the canary is all of wave 1.
- m2.s3 reads this branch's CI run with `gh`; the orchestrator must push after
  wave 3 before wave 4 launches (the per-wave push does this).
- The repo's `.claude/settings.json` attribution is already
  `Assisted-by: Claude Code`.
