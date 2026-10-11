# Handoff: ghx release pipeline (finch)

- **Plan:** `specs/handoffs/plan-ghx-release-finch.md` (complete)
- **Branch:** `claude/stoic-planck-qfqg75` (assigned by the runner). Every working directory is this repo.
- **Mode:** unattended, confirmed 2026-10-11 in session 49ac3a33-e356-58b6-9971-24a023142f66 ("yes, unattended"; harness=claude-code runner=cloud).
- **Done:** all 4 waves, 6 steps. Commits da66e81 (m1.s1), 7d586df b1b49a4 296482d (m1.s2-s4), b4e6da2 (m2.s1), a415872 (m2.s2).
- **Review log:** wave-1 PASS (2fbf197..da66e81); wave-2 PASS (1d6be7b..296482d); wave-3 PASS (2541c0c..b4e6da2); wave-4 PASS (a101696..a415872).
- **Fix-ups, retries:** none. Checkpoints passed unattended: gates 5, 4, 2.
- **Spend:** ~$14.14 API-equiv (output est.) against ~$11 expected; guard limit $50 never approached.
- **Next:** Gary decides on the PR. Cleanup commit before merge: `git rm specs/handoffs/plan-ghx-release-finch.md specs/handoffs/handoff-ghx-release-finch.md`.

## Pending question

Open a PR from `claude/stoic-planck-qfqg75` to `main`? If yes, I first add the cleanup commit removing both `specs/handoffs/` files and put the Completion summary in the PR description.

## Completion summary

**Completed**: 2026-10-11
**What shipped**: ghx has a version source of truth (`version.txt` 0.1.0, `scripts/bump_version.py`, `internal/buildinfo`) and `ghx --version` printing `<release>+<sha> (<buildCode>)`, stamped identically by `make build` and goreleaser. `.goreleaser.yaml` builds six archives plus checksums, with `make snapshot` and `make release-check`. `scripts/publish-formula.sh` and `scripts/publish-scoop.sh` render the Homebrew formula and Scoop manifest from the release checksums, behind `make publish-formula`, `make publish-scoop` and `make release` in a `##@ Danger` section gated by `CONFIRM_*`. `.github/workflows/release.yml` (bump, commit, tag, push with `RELEASE_TOKEN`) and `publish.yml` (goreleaser, then formula, then Scoop, as three re-runnable jobs) are in place, plus a `release-check` job in CI. `specs/releasing.md`, README Install, Makefile.md and CHANGELOG document it. Nothing was tagged, released, dispatched or pushed outside the task branch.
**Deviations from plan**:
- Unattended checkpoints: gate 5 (canary, wave 1), gates 4 and 2 (before wave 3). No retries, no fix-ups.
- m1.s2: goreleaser stamps `{{ .Env.RELEASE }}`, `{{ .Env.SHA }}`, `{{ .Env.BUILD_CODE }}` from the Makefile instead of `{{ .ShortCommit }}`, per m1.s1's decision, so local and release builds print the same string.
- m2.s1: the publish job is three jobs chained by `needs` (release, homebrew, scoop) so a partial failure re-runs only what failed; added `scripts/fetch-release-checksums.sh` so the publishers render only from a complete, non-draft release.
- m2.s1: `release.yml` passes `RELEASE_TOKEN` through `actions/checkout`'s `token:` input, as the personal-release template does, not a step's `env` as the plan's rule reads. No value lands in a file.
- m2.s2: added a one-line pointer to `specs/releasing.md` in CONTRIBUTING.md.
- Every `make ci` ran with runner-only workarounds (golangci-lint v2.14.0 and a covdata-built go1.26.2 in the scratchpad, `GIT_CONFIG_*` unset); CI on GitHub hasn't run this branch yet.
- Spend ~$14.14 against ~$11 expected (output est.; the session is live, so no `cost-state` totals yet).
**Follow-ups**:
- Gary: create the repository secrets `RELEASE_TAG_PAT` (renamed from `RELEASE_TOKEN` after the run, to match triage), `HOMEBREW_TAP_TOKEN`, `SCOOP_BUCKET_TOKEN`; give `RELEASE_TAG_PAT` a ruleset bypass to push to `main`; add `ghx` rows to the tap's and bucket's READMEs.
- Decide the first version: `version.txt` reads 0.1.0 as the last release, so the first workflow release is 0.1.1, 0.2.0 or 1.0.0. Set it to 0.0.0 and bump minor if you want v0.1.0.
- Decide whether `RELEASE_TAG_PAT` via checkout's `token:` is acceptable (triage does the same) or the rule should change.
- `internal/cloner`'s `TestCloneRepos_GivesGitTheTokenOnlyThroughItsEnvironment` fails when the environment sets `GIT_CONFIG_COUNT`; isolate those variables in the test.
- Unverified until a real release: the half-uploaded-release recovery, `fetch-release-checksums.sh` against real `gh`, and `brew trust lolay/tap`.
- Cosmetic: Makefile.md's release.yml row omits `--line`; `ci.yml`'s header comment doesn't mention `release-check`.
- Before merge: remove `specs/handoffs/plan-ghx-release-finch.md` and `handoff-ghx-release-finch.md` (the Completion summary is the only durable part, and it belongs in the PR description).
