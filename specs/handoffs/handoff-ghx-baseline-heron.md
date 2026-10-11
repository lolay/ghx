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
  `bf82fd5c-3117-418c-934b-c0d47391532a` (same harness and runner tokens),
  which ran waves 1 and 2.
- Re-confirmed 2026-10-10 by Kickoff prompt in session
  `6517b573-c846-47e6-ac6e-39aa5313307e` (same tokens); this session
  orchestrated wave 3 and final completion.

## Waves

| wave | tier | steps | state |
|---|---|---|---|
| 1 | exec | m1.s1-s3 (s2 is `[fast]`, folded) | done, PASS, `3e99c74..92f4ee3` |
| 2 | deep | m1.s4 | done, PASS, `cd6de0a..2797c2d` (gates 5 and 2 approved: "yes, continue wave 2") |
| 3 | exec | m2.s1-s2 | done, PASS, `8ef9623..ec72869` (gates 4 and 5 approved: "yes, continue wave 3") |

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

- Wave 2 (`ghx m1 s4`), run `wf_1b70355a-563`, commits `8a26cd8` (tests
  and `specs/testing.md`) and `2797c2d` (manifest doc comment).
  `check_wave.py`: ok. Review: PASS. Coverage: config 95.3%, manifest
  90.9%, ui 98.6%, ghapi 42.7% (FilterRepos 100%; gap listed in
  `specs/testing.md`). No behaviour bug; pinned for a later decision:
  `ResolveInt` truncates TOML floats and wrong-typed values fall through.
  Review nit: the manifest comment omits the `ensure_ascii`/HTML-escape
  differences from Python.
- Milestone handoff for m2 written: `specs/handoffs/handoff-m2-docs.md`.
- Wave 3 (`ghx m2 s1-s2`), run `wf_56f9ca73-223`, the canary (the wave's
  only group), commits `8a50cdc` m2.s1 (README, CHANGELOG) and `ec72869`
  m2.s2 (CONTRIBUTING, SECURITY, CODEOWNERS, AGENTS). `check_wave.py`: ok.
  Review: PASS (CLI reference identical to `--help`, 29 relative links
  resolve, `make ci` passes). The worker found that HTTPS clones embed the
  token in the origin URL; SECURITY.md documents it, code fix is a
  follow-up.
- Plan complete 2026-10-10; Completion summary at the bottom of the plan.

## Spend

~$8.13 of ~$13 expected (API-equiv; subagent lines are output estimates).
Per wave: 1 ~$1.30, 2 ~$3.15, 3 ~$1.35, orchestrator ~$2.33.

## Next

The plan is complete. Pending: Gary's answer on the PR (below). Before
merge, the last commit removes `specs/handoffs/` (this handoff, the plan,
`handoff-m2-docs.md`) or promotes what's durable; the Completion summary's
follow-ups (token in HTTPS origin URL, `ResolveInt`, old deps) would go to
an issue or `specs/` first.

## Pending question (verbatim)

> Plan complete: 3/3 groups, every review PASS, no fix-ups. Open a PR from
> `feature/ghx-baseline-heron` to `main`? Before merge, a cleanup commit
> removes `specs/handoffs/` (plan, session handoff, m2 handoff); tell me
> whether to promote anything (the Completion summary's follow-ups) to
> `specs/` or an issue first.

## Resume

Nothing to dispatch. Answer the pending question; the PR and the cleanup
commit need no orchestration.

## Deviations from plan

- The plan's Context says `main` at `b2b9866`; `main` is at `7c394ba`
  (three later commits: `.gitignore`, `.claude/settings.json`, this plan).
- m1.s1's acceptance grep matches the plan itself in `specs/handoffs/`;
  the dispatch excluded that folder.
- m1.s3: `make format` also runs `golangci-lint fmt` (not in the spec).
- m2.s1: `go install github.com/lolay/ghx/cmd/ghx@latest` fails until the
  rename merges to `main`; the README says so.
- Session `bf82fd5c` ended before writing its `orchestrator-wave-2` token
  line; session `6517b573` tallied it from that transcript.
