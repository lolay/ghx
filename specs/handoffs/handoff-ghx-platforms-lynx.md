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
- Wave 2 `[deep]` m1.s4-m1.s5: PASS, `f9d4148..83231a0` (`580d54a`,
  `83231a0`). Windows touchpoints audited (table in `specs/testing.md`),
  `CheckRepoName`, `FindGit` with a git 2.31 minimum, Windows/Unix platform
  files; the token goes to git only through `GIT_CONFIG_*` env, old origins
  are cleaned, `internal/redact` masks failure details. Gate 2 passed
  unattended. The m2 milestone handoff is `handoff-m2-ci.md` (gate 4).

## Spend

- Token log: ~$9.0 so far against ~$18 expected (API-equiv; guard ~$54).

## Next

- Wave 3 `[exec]` ghx m2 s1-s2 (CI workflow; CI map docs), gate 4 logged
  unattended, launching now. Push after it, before wave 4 reads the CI run.

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
  set to `NUL`, the `git.bat` fake, open-file rename retries) are only
  type-checked until the m2 CI run.
- On a 401, git can still fall back to the user's credential helper or a
  terminal prompt; `GIT_TERMINAL_PROMPT=0` needs a decision.
- Minor: an existing lower-case `git_config_count` on Windows isn't detected,
  so ghx would overwrite that user's entry 0.

## Gotchas

- Single working directory (`/Users/gary/Projects/lolay/ghx`), so each wave
  has one group and the canary is all of wave 1.
- m2.s3 reads this branch's CI run with `gh`; the orchestrator must push after
  wave 3 before wave 4 launches (the per-wave push does this).
- The repo's `.claude/settings.json` attribution is already
  `Assisted-by: Claude Code`.
