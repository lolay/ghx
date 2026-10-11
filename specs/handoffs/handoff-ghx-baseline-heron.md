# Handoff: ghx baseline (heron)

Session handoff for the `personal-plan-orchestrate` run of
[`plan-ghx-baseline-heron.md`](plan-ghx-baseline-heron.md). Removed, or
promoted to durable docs, before the PR merges.

## Branch

- `feature/ghx-baseline-heron` (task branch), cut from `main` at `7c394ba`
  on 2026-10-10 and pushed to `origin`.

## Mode

- Gated, confirmed 2026-10-10 in session
  `4e8ee344-6dc5-44cf-8111-98422ff3c14c` (answer: "gated";
  `harness=claude-code runner=none`: Mac CLI workstation, dogfood run A).
  Orchestrate in a new chat (default). Claude Code path: phase 1 dogfood.
- Re-confirmed 2026-10-10 by Kickoff prompt in session
  `bf82fd5c-3117-418c-934b-c0d47391532a` (same harness and runner tokens);
  this session orchestrates.

## Waves

| wave | tier | steps | state |
|---|---|---|---|
| 1 | exec | m1.s1-s3 (s2 is `[fast]`, folded) | pending |
| 2 | deep | m1.s4 | pending |
| 3 | exec | m2.s1-s2 | pending |

Gates the plan crosses: 5 (canary, wave 1), 2 (before wave 2), 4 (before
wave 3, new milestone m2).

## Done

Nothing yet. Kickoff wrote the Kickoff block, wave markers, Cost table,
Review log and Token log into the plan.

## Spend

~$0 of ~$13 expected (API-equiv). Guard: 3x min $50, so stops past $50.

## Next

Wave 1 `[exec]` ghx m1 s1-s3, launching in session `bf82fd5c`. Its first
launch is the canary (group ghx m1 s1-s3) and stops at gate 5.

## Resume

Paste the plan's Kickoff prompt into a new chat (Opus, effort high). If
Status shows BLOCKED, the session re-posts that question.

## Deviations from plan

- The plan's Context says `main` at `b2b9866`; `main` is at `7c394ba`
  (three later commits: `.gitignore`, `.claude/settings.json`, this plan).
