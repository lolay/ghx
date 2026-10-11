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
  runner=none), and re-confirmed by Kickoff prompt paste in session
  `43a767ca-8b50-41c3-9808-49159f0ef9f6` (same harness and runner). An unattended record counts only in the session that
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
  written; mode recorded.
- Wave 1 `[exec]` m1.s1-m1.s3 (canary): PASS, `2ed9d2b..9f0db0c`
  (`fb00a5e`, `8dfa741`, `9f0db0c`). Hermetic tests for cloner (95.9%),
  ghapi (100%, new unexported `baseURL` seam) and the cli run flow (91.6%);
  helpers `internal/gittest` and `internal/ghapi/ghapitest`. cli reaches the
  fake API by swapping `http.DefaultTransport` (`ghapitest.RedirectGitHub`),
  documented in `specs/testing.md`. Gate 5 passed unattended.

## Spend

- Token log: ~$2.5 so far against ~$18 expected (API-equiv; guard ~$54).

## Next

- Wave 2 `[deep]` ghx m1 s4-s5 (Windows OS touchpoints; token out of clone
  URLs), gate 2 logged unattended, launching now.

## Pending question

- None.

## How to resume

Paste the plan's Kickoff prompt into a new chat on this machine (Opus, effort
high).

## Follow-ups found (out of scope, not fixed)

- `cloner.resolveWikiURL` uses `strings.Replace(CloneURL, ".git", ".wiki.git", 1)`,
  which hits the first `.git` anywhere: a repo like `acme.github.io` gets a
  malformed wiki URL. Needs its own step or issue.
- `manifest.Write` is given the repo list before `--include` / `--exclude`
  filtering, so excluded repos appear in `.ghx.json`. Existing behaviour.
- Windows-only paths (read-only `.git/objects` removal, `GIT_CONFIG_GLOBAL`
  set to `NUL`) are only type-checked until the m2 CI run.

## Gotchas

- Single working directory (`/Users/gary/Projects/lolay/ghx`), so each wave
  has one group and the canary is all of wave 1.
- m2.s3 reads this branch's CI run with `gh`; the orchestrator must push after
  wave 3 before wave 4 launches (the per-wave push does this).
- The repo's `.claude/settings.json` attribution is already
  `Assisted-by: Claude Code`.
