# Handoff: ghx release pipeline (finch)

- **Plan:** `specs/handoffs/plan-ghx-release-finch.md`
- **Branch:** `claude/stoic-planck-qfqg75` (assigned by the runner; the session started on it). Every working directory is this repo.
- **Mode:** unattended, confirmed 2026-10-11 in session 49ac3a33-e356-58b6-9971-24a023142f66 ("yes, unattended"; harness=claude-code runner=cloud).
- **Done:** wave 1 (m1.s1, da66e81): `version.txt` 0.1.0, `scripts/bump_version.py` from the template, `internal/buildinfo`, `ghx --version`/`-v`, Makefile `build` stamping and `bump`. Reviewed PASS.
- **Waves:** 1 [deep] m1 s1 (done); 2 [exec] m1 s2-s4 (s4 [fast] folded); 3 [deep] m2 s1; 4 [exec] m2 s2.
- **Review log:** wave-1 PASS (2fbf197..da66e81).
- **Fix-ups, retries, checkpoints:** none, except gate 5 (canary), which passed unattended.
- **Spend:** ~$5.37 logged (kickoff ~$1.95, wave 1 plus its review ~$3.41) against ~$11 expected (guard limit $50).
- **Next unit:** wave 2 [exec] m1 s2-s4.
- **Resume:** paste the plan's Kickoff prompt into a new session.

## Decisions carried forward

- goreleaser computes none of the version parts. It stamps `{{ .Env.RELEASE }}`, `{{ .Env.SHA }}` and `{{ .Env.BUILD_CODE }}` into `internal/buildinfo`, and it runs only through Makefile targets that export those values. goreleaser's `.Version` and `.ShortCommit` are not used. This overrides the `{{ .ShortCommit }}` mention in m1.s2, and the Release workflow (m2.s1) needs a `fetch-depth: 0` checkout.

## Gotchas on this runner (not repo defects)

- The installed `golangci-lint` 2.5.0 refuses this go 1.26 module. Workers install the pinned v2.14.0 into the scratchpad.
- The auto-downloaded Go toolchains lack `covdata`, so `go test -cover` fails on packages with no tests. The wave 1 worker built it into a scratchpad copy of go1.26.2 and ran the gate with `GOTOOLCHAIN=local`.
- The runner exports `GIT_CONFIG_COUNT=3`, which breaks `internal/cloner`'s `TestCloneRepos_GivesGitTheTokenOnlyThroughItsEnvironment` on HEAD before this plan too. The gate runs with `GIT_CONFIG_*` unset. Follow-up, out of scope: isolate those variables in the test.

## Pending question

None. Wave 2 is being dispatched.
