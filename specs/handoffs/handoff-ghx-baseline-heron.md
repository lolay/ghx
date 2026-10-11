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
  Claude Code path: phase 1 dogfood.
- Re-confirmed 2026-10-10 by Kickoff prompt in session
  `bf82fd5c-3117-418c-934b-c0d47391532a` (same harness and runner tokens);
  this session orchestrates.

## Waves

| wave | tier | steps | state |
|---|---|---|---|
| 1 | exec | m1.s1-s3 (s2 is `[fast]`, folded) | done, PASS, `3e99c74..92f4ee3` |
| 2 | deep | m1.s4 | next, BLOCKED at gates 5 and 2 |
| 3 | exec | m2.s1-s2 | pending |

## Done

- Wave 1 (`ghx m1 s1-s3`), run `wf_d549e87d-674`, the canary (the wave's
  only group), commits `e6feff5` m1.s1, `08684bc` m1.s2, `92f4ee3` m1.s3.
  `check_wave.py`: ok. Review: PASS.
- Notes from the worker and review: golangci-lint found 29 issues with
  triage's set, so `.golangci.yml` keeps standard + misspell + errorlint
  and lists the exclusions in its header; `.go-version` is 1.27.2 (go.dev),
  golangci-lint pinned v2.14.0; `make format` also runs `golangci-lint fmt`
  when installed; `make doctor` exits 1 with a hint when `triage` itself is
  missing; `git check-ignore -q a b c` is rejected by git (each path
  checked singly, all ignored); README still says "Requires Go 1.22+"
  (wave 3's README rewrite should fix it); govulncheck reports 2 uncalled
  vulns in old deps (e.g. golang.org/x/text 0.22.0); checkmake not
  installed.

## Spend

~$2.02 of ~$13 expected (API-equiv). Guard: 3x min $50, so stops past $50.
Projected after wave 2: ~$5.2.

## Next

Wave 2 `[deep]` ghx m1 s4 (test seams and first unit tests), Opus high
worker and reviewer, ~$3.2.

## Pending question (verbatim)

> Wave 1 passed: canary `ghx m1 s1-s3` in `lolay/ghx` `3e99c74..92f4ee3`
> (m1.s1-s3), check ok, review PASS. Next unit: wave 2 `[deep]` ghx m1 s4,
> ~$3.2. That crosses gate 5 (canary done) and gate 2 (`[exec]` →
> `[deep]`). Approve wave 2?

## Resume

Paste the plan's Kickoff prompt into a new chat (Opus, effort high). If
Status shows BLOCKED, the session re-posts that question.

## Deviations from plan

- The plan's Context says `main` at `b2b9866`; `main` is at `7c394ba`
  (three later commits: `.gitignore`, `.claude/settings.json`, this plan).
- m1.s1's acceptance grep matches the plan itself in `specs/handoffs/`;
  the dispatch excluded that folder.
- m1.s3: `make format` also runs `golangci-lint fmt` (not in the spec).
