```
--- KICKOFF: plan complete ---

  Status: 4/4 groups done | completed 2026-10-11

  mode: unattended | proposed unattended (harness=claude-code runner=cloud; CLAUDE_CODE_REMOTE=true, assigned branch claude/stoic-planck-qfqg75) | guard 3x min $50 | fixups 2 | confirmed 2026-10-11 session 49ac3a33-e356-58b6-9971-24a023142f66: yes, unattended

  review: every-wave (log-only)

  Next model
    Cursor:      claude-opus-5-5[effort=high]
    Claude Code: /model opus                (/effort high)

  Prompt to paste into the next chat:
    In lolay/ghx, read specs/handoffs/plan-ghx-release-finch.md. The plan is already tagged.
    On branch claude/stoic-planck-qfqg75 (task branch): subagents commit each finished step.
    Run in unattended mode.
    Run the orch-spike-plan-orchestrate skill from the top: walk to
    each tier boundary, dispatch subagents per the skill's
    procedure, and pause only where the recorded mode stops. Do not
    execute plan work inline. Update plan progress after each wave
    returns per the skill's procedure. The mode line above is the
    kickoff answer. If the Status line shows BLOCKED, re-post that
    question and wait; otherwise record the mode, print the kickoff
    summary and begin dispatching.

---
```

**Cost (API-equiv, Claude Code models)**

| wave | expected tokens | expected $ | actual tokens | actual $ |
|---|---|---|---|---|
| 1 [deep] m1 s1 | ~2.7M | ~$1.4 | ~6.5M (output est.) | ~$3.41 (output est.) |
| 2 [exec] m1 s2-s4 | ~3.3M | ~$1.4 | ~3.1M (output est.) | ~$1.74 (output est.) |
| 3 [deep] m2 s1 | ~2.7M | ~$1.4 | ~5.3M (output est.) | ~$3.08 (output est.) |
| 4 [exec] m2 s2 | ~2.0M | ~$0.89 | ~2.6M (output est.) | ~$1.42 (output est.) |
| orchestrator | ~7.3M | ~$6.3 | ~6.8M | ~$4.49 |
| **Total** | ~18M | ~$11 | ~24.3M (output est.) | ~$14.14 (output est.) |

Expected values are estimates, good to about 2-3× per wave. Waves 1 and 3 are one medium step on Opus 5.5 (high); wave 2 is two medium steps and one small folded `[fast]` step on Sonnet 5.5 (high); wave 4 is one medium step on Sonnet 5.5 (high); each wave adds one Opus 5.5 (high) review (~1.0M, ~$0.54). The orchestrator row is the kickoff, four waves of bookkeeping, and one human wait (the kickoff answer, unattended; no new-chat start-up on a runner).

# Plan: ghx release pipeline to the lolay tap and Scoop bucket (finch)

Run C of the orchestrate phase 1 dogfood (`GaryRudolph/public` `specs/handoffs/dogfood-orchestrate-native.md`): Claude Code on the web, unattended, switched to gated mid-run. Third of three plans that bring `lolay/ghx` to the shape of `lolay/triage`. Starts from `main` after plans A (`plan-ghx-baseline-heron`) and B (`plan-ghx-platforms-lynx`) merged. It ends with everything a release needs in the repo and nothing released: no tag, no workflow run, no secret, no commit to the tap or the bucket.

## Context

- **After plans A and B:** module `github.com/lolay/ghx`, binary `ghx` from `cmd/ghx/`, a standard `Makefile` (no `##@ Release` or `##@ Danger` section yet), `Makefile.md` with a CI map, `.go-version`, tests on every package, `.github/workflows/ci.yml` on macOS, Linux and Windows, `CHANGELOG.md` with `## [Unreleased]`. No version number anywhere yet, no `--version`, no tags on the remote.
- **The model, triage** (`lolay/triage`, public; shallow-clone `https://github.com/lolay/triage.git` into a temp directory outside this repo and only read it): `.goreleaser.yaml` (darwin, linux, windows × amd64, arm64; tar.gz, zip on Windows; checksums), `scripts/publish-formula.sh` + `scripts/triage.rb.tmpl` (renders `Formula/triage.rb` from `dist/checksums.txt` and commits it to `lolay/homebrew-tap` with `HOMEBREW_TAP_TOKEN`; `DRY_RUN=1` prints it), `scripts/publish-scoop.sh` + `scripts/triage.json.tmpl` (the same for `bucket/triage.json` in `lolay/scoop-bucket` with `SCOOP_BUCKET_TOKEN`), `.github/workflows/release.yml`, `specs/releasing.md` ("remote pushes happen last", in a fixed order), and the Makefile's `##@ Release` and `##@ Danger` targets behind `CONFIRM_*`.
- **Where ghx follows Gary's standard instead of triage:** triage's version comes from `git describe` and its workflow bumps only the CHANGELOG. Gary's `versioning.md` and the `personal-release` skill say `version.txt` holds the last released version, `scripts/bump_version.py` and the Release workflow template bump it at tag time, dev builds read `<release>+<sha> (<buildCode>)`, and there are no pre-release suffixes, snapshots included. Copy the two templates from the `personal-release` skill (`templates/bump_version.py`, `templates/release.yml`): the loaded `personal` plugin's copy, or `https://raw.githubusercontent.com/GaryRudolph/public/main/plugins/personal/skills/personal-release/templates/`.
- **Tools on the runner:** if `goreleaser` or `actionlint` isn't installed, run them as `go run github.com/goreleaser/goreleaser/v2@<latest v2>` and `go run github.com/rhysd/actionlint/cmd/actionlint@<latest>`, versions looked up now.

## Rules for every step

- Read Gary's standards first: `versioning.md`, `security.md`, `secrets/` (what may live in a repo), `makefile.md`, `git.md`, `go/security.md`; and the `personal-release` skill.
- Commit each finished step on the task branch as `m{N}.s{K} <imperative subject>` (72 characters at most, no period), a blank line, a body if useful, then the session's trailers as the last paragraph (`Assisted-by: Claude Code`, plus the runner's `Claude-Session:` line). Never `Co-authored-by`. Don't push; the orchestrator does after each wave.
- `make ci` passes at the end of every step.
- **No outward actions, and nothing that could release:** no tags, no `gh release`, no workflow dispatch, no secrets created or read, no PRs, no clone-and-push to `lolay/homebrew-tap` or `lolay/scoop-bucket`. The publish scripts are tested only with `DRY_RUN=1`. No token or secret value in any file; workflows read secrets only through `secrets.<NAME>` into a step's `env`.

## m1 - Version and release artifacts

--- WAVE 1 [deep] ---

#### s1 - [deep] Version source of truth and `ghx --version` (done)

- `version.txt` per the `personal-release` skill's set-up section (`0.1.0`, since nothing has shipped), `scripts/bump_version.py` copied from the template unchanged, and an `internal/buildinfo` package with `Version`, `Sha` and `BuildCode` set by `-ldflags -X` (defaults for an unstamped `go build`/`go run`, such as `dev`).
- `ghx --version` prints `ghx <release>+<sha> (<buildCode>)`, with `.dirty` on the metadata for a dirty tree, per the standard's "Reading a build version" (not triage's format). Wire it through cobra's `Version` with a template.
- Makefile: `build` stamps `RELEASE` from `version.txt`, `SHA` from `git rev-parse --short HEAD` (plus `.dirty`), `BUILD_CODE` from `git rev-list --count HEAD`; a `bump` target (`LEVEL=patch|minor|major`, `##@ Release`) that runs `scripts/bump_version.py`. Decide and write down (a comment in the Makefile and in the step's artifact) how goreleaser will get the same three values in m1.s2, so a local build and a release build of one commit print the same string.
- Tests for the version string (stamped, unstamped, dirty), and a `make build && bin/ghx --version` check in the step's artifact.
- **Accept when:** `bin/ghx --version` prints `ghx 0.1.0+<sha> (<count>)` on a clean tree; `python3 scripts/bump_version.py patch --dry-run` reports `0.1.1` and changes nothing; `make ci` passes.

--- WAVE 2 [exec] ---

#### s2 - [exec] goreleaser configuration and a local snapshot (done)

- `.goreleaser.yaml` (v2 schema) from triage's: `main: ./cmd/ghx`, binary `ghx`, `CGO_ENABLED=0`, `-trimpath`, ldflags into `github.com/lolay/ghx/internal/buildinfo` as m1.s1 decided (`BUILD_CODE` from the environment, `{{ .ShortCommit }}`), darwin, linux and windows × amd64 and arm64, archives `ghx_<version>_<os>_<arch>` (tar.gz, zip on Windows) carrying `README.md`, `LICENSE` and `CHANGELOG.md`, `checksums.txt`, `release.github` `lolay/ghx`. Snapshot naming without a pre-release suffix (`<release>+<sha>`), per the standard.
- Makefile `##@ Release`: `snapshot` (`goreleaser release --snapshot --clean`) and `release-check` (`goreleaser check`), with `GORELEASER ?= goreleaser` so the runner can pass the `go run` form.
- **Accept when:** `goreleaser check` passes; `make snapshot` builds all six archives and `checksums.txt` under `dist/`; the darwin or linux binary for this machine prints the same `--version` string as `make build` for this commit, apart from `.dirty`; `dist/` stays ignored.

#### s3 - [exec] Homebrew formula and Scoop manifest publishers (done)

- `scripts/publish-formula.sh` + `scripts/ghx.rb.tmpl` and `scripts/publish-scoop.sh` + `scripts/ghx.json.tmpl`, ported from triage's: `class Ghx`, the description from the GitHub repo ("Clones and refreshes all repos for a GitHub organization"), homepage `https://github.com/lolay/ghx`, license `Apache-2.0`, `bin.install "ghx"` (no man pages), a `test do` that matches the version in `ghx --version`, Scoop `"bin": "ghx.exe"` with `checkver` and `autoupdate`. Same flow as triage: render from `dist/checksums.txt`, `DRY_RUN=1` prints and exits, otherwise clone the target repo with its token, commit `ghx <version>` as a bot identity, push.
- **Accept when:** after `make snapshot`, `DRY_RUN=1 scripts/publish-formula.sh <snapshot version> dist` and `DRY_RUN=1 scripts/publish-scoop.sh <snapshot version> dist` print a formula and a manifest whose every sha256 matches `dist/checksums.txt`; `ruby -c` on the rendered formula and `python3 -m json.tool` on the manifest pass (skip either tool if missing and say so); `shellcheck` on both scripts is clean if installed; neither script ran without `DRY_RUN=1`.

#### s4 - [fast] Danger targets for the publish steps (done)

- Makefile `##@ Danger`, copied from triage's shape with the standard's `confirm` macro: `publish-formula` (`CONFIRM_PUBLISH_FORMULA=1`, `VERSION=x.y.z`), `publish-scoop` (`CONFIRM_PUBLISH_SCOOP=1`), `release` (`CONFIRM_RELEASE=1`, `goreleaser release --clean` for the current tag). No `tag` target: tags come from the Release workflow.
- **Accept when:** each Danger target refuses without its `CONFIRM_*` variable (run each bare and see the refusal; never set the variable); `make help` lists them under Danger.

## m2 - Release workflows and docs

--- WAVE 3 [deep] ---

#### s1 - [deep] Release and publish workflows (done)

- `.github/workflows/release.yml` from the `personal-release` template (dispatch with `level`, from `main` or a `release/vX[.Y]` line branch, bump with `bump_version.py`, commit `release vX.Y.Z`, tag, push with `RELEASE_TOKEN`) and `.github/workflows/publish.yml` on `push` of `v[0-9]+.[0-9]+.[0-9]+` tags: checkout with `fetch-depth: 0`, `setup-go` from `.go-version`, `BUILD_CODE` computed, `goreleaser release --clean` with `GITHUB_TOKEN`, then `scripts/publish-formula.sh`, then `scripts/publish-scoop.sh`, in that order so the formula and manifest only point at assets that exist (triage's `specs/releasing.md` "remote pushes happen LAST"). A `workflow_dispatch` on `publish.yml` re-runs a failed publish for an existing tag, as triage's does.
- Decide, and record in the step's artifact with a reason each: floating `vX.Y` / `vX` tags (triage has them for its GitHub Action consumers; a CLI likely doesn't need them); least-privilege `permissions` per job; `concurrency`; pinning third-party actions to a major or a SHA (`go/security.md`); what happens when the tap push succeeds and the bucket push fails.
- Add a `release-check` job (`goreleaser check`) to `ci.yml`.
- Secrets the workflows name, and nothing else: `RELEASE_TOKEN` (contents write on `lolay/ghx`, so its tag push starts `publish.yml`), `HOMEBREW_TAP_TOKEN` (contents write on `lolay/homebrew-tap`), `SCOOP_BUCKET_TOKEN` (contents write on `lolay/scoop-bucket`). Gary creates them; the step only lists them.
- **Accept when:** `actionlint` reports nothing on all workflows; no workflow references a secret outside that list or echoes one; the publish job's order is goreleaser, formula, Scoop; `make ci` passes.

--- WAVE 4 [exec] ---

#### s2 - [exec] Release docs (done)

- `specs/releasing.md` modelled on triage's, for this repo's flow: the core rule (remote pushes last, in order), versioning (`version.txt`, lazy bump, `<release>+<sha> (<buildCode>)`), pre-flight, cutting a release (Actions, Release, Run workflow, level), re-running a failed publish, the emergency local path with the Danger targets, the three secrets with the scope each needs, and a first-release checklist: create the secrets, add a `ghx` row to the tap's and the bucket's README tables, confirm `brew install lolay/tap/ghx` and `scoop install lolay/ghx` afterwards.
- README "Install": `brew trust lolay/tap`, `brew install lolay/tap/ghx`; `scoop bucket add lolay https://github.com/lolay/scoop-bucket`, `scoop install lolay/ghx`; GitHub Releases; from source. Marked as available from the first release.
- `Makefile.md`: the Release and Danger sections and the `publish.yml` / `release.yml` rows in the CI map. `CHANGELOG.md` Unreleased: `--version`, the release pipeline.
- **Accept when:** every target, script, workflow and secret named in the docs exists under that name; `make ci` passes.

## Review log

review wave-1 (ghx m1 s1) 2fbf197..da66e81: PASS - acceptance re-run (ghx 0.1.0+da66e81 (14), bump dry-run 0.1.1 with a clean tree, make ci green); goreleaser takes RELEASE/SHA/BUILD_CODE from the Makefile via .Env.*, not .Version/.ShortCommit (m1.s2 follows); make ci needed runner workarounds (golangci-lint v2.14.0 and a covdata-built go1.26.2 in the scratchpad, GIT_CONFIG_* unset for a pre-existing cloner test); passed unattended: gate 5 - 2026-10-11
review wave-2 (ghx m1 s2-s4) 1d6be7b..296482d: PASS - reviewer re-ran everything on a clean tree: release-check ok (goreleaser v2.18.3 via go run), snapshot built 6 archives ghx_0.1.0+296482d_<os>_<arch> plus checksums, dist binary and make build both print ghx 0.1.0+296482d (18); DRY_RUN=1 formula and manifest hashes match checksums.txt, ruby -c and json.tool pass, shellcheck clean (scratchpad venv); Danger targets refuse without CONFIRM_* and exit 2; publish scripts validate VERSION and pass the token via GIT_CONFIG_* extraheader, not the URL; make ci green with wave 1's runner workarounds - 2026-10-11
review wave-3 (ghx m2 s1) 2541c0c..b4e6da2: PASS - actionlint v1.7.12 clean on 3 workflows (shellcheck rule on), shellcheck clean on scripts/*.sh; secrets named: RELEASE_TOKEN (release.yml checkout token:, as the personal-release template does), HOMEBREW_TAP_TOKEN and SCOOP_BUCKET_TOKEN (step env), none echoed; publish.yml order enforced by needs: release (make release, goreleaser) then homebrew then scoop, each re-runnable; scripts/fetch-release-checksums.sh renders from the published release (stub-tested only, gh has no API access here); release-check job in ci.yml; no floating tags, per-job permissions from {}, first-party actions at a major, goreleaser pinned v2.18.3 via go run; make ci green with the runner workarounds; for m2.s2: a hotfix from an old line moves the tap and bucket back, partial-release recovery, RELEASE_TOKEN ruleset bypass; ci.yml header comment doesn't mention release-check (cosmetic); passed unattended: gate 4, gate 2 - 2026-10-11
review wave-4 (ghx m2 s2) a101696..a415872: PASS - specs/releasing.md (push order, versioning, pre-flight, cutting, re-running, hotfixes, half-uploaded recovery, emergency Danger path, three secrets with scopes, first-release checklist), README Install, Makefile.md workflow table, CHANGELOG, CONTRIBUTING pointer; every target, script, workflow, secret and CONFIRM_* named in the docs checked against the code; make ci green with the runner workarounds; not blocking: Makefile.md release.yml row omits --line, half-uploaded recovery and `brew trust lolay/tap` unverified, first workflow release is 0.1.1/0.2.0/1.0.0 since version.txt reads 0.1.0 - 2026-10-11

## Token log

**Counting header (Claude Code)**

- Line, one per model a chat ran, appended below: `tokens <row> <group-id> (<model>): input ~X / cache read ~R / cache write ~W / output ~Y | ~$C API-equiv`. `<row>` is `wave-N`, `review-wave-N`, or `wave-N-fix` and `review-wave-N-fix` for a fix-up wave; `<group-id>` is the wave's group id with hyphens (`m1-s2-s4`); `<model>` is `message.model` without a date suffix (`claude-haiku-4-5-20251001` is `claude-haiku-4-5`). Round counts to two significant figures with `k` or `M`.
- Usage: `~/.claude/projects/<slug>/$CLAUDE_CODE_SESSION_ID.jsonl` (`<slug>` is the working directory with every character but a letter or digit turned into `-`, matched by prefix when long; without the id, the newest `.jsonl` there), plus its subagents' `<session-id>/subagents/**/agent-*.jsonl` in the same directory. Sum `message.usage` over assistant lines once per `message.id`, from the line with `stop_reason`: input `input_tokens`, cache read `cache_read_input_tokens`, cache write `cache_creation.ephemeral_5m_input_tokens` (with `ephemeral_1h_input_tokens` too, `cache write ~40k 5m + ~8k 1h`), output `output_tokens`. A call with no `stop_reason` line keeps its input-side counts, takes output as about 1,000, and its line ends `(output est.) session <id>`.
- Rates by `<model>`, $ per Mtok input / cached / output: `claude-opus-5-5` 4.00 / 0.20 / 20.00; `claude-sonnet-5-5` 2.00 / 0.20 / 10.00; `claude-haiku-4-5` 1.00 / 0.10 / 5.00.
- `$C` = (input × in + cache read × cached + 5m write × in × 1.25 + 1h write × in × 2.00 + output × out) / 1M.
- In another harness, or on a model not listed here, count and price per plan-execution.md "Token accounting" and "Model price table" instead.

tokens orchestrator-kickoff plan-ghx-release-finch (claude-opus-5-5): input ~36 / cache read ~2.5M / cache write ~0 5m + ~130k 1h / output ~20k | ~$1.95 API-equiv
tokens review-wave-1 ghx-m1-s1 (claude-opus-5-5): input ~20 / cache read ~300k / cache write ~42k / output ~11k | ~$0.48 API-equiv (output est.) session 49ac3a33-e356-58b6-9971-24a023142f66
tokens wave-1 ghx-m1-s1 (claude-opus-5-5): input ~88 / cache read ~5.9M / cache write ~170k / output ~44k | ~$2.93 API-equiv (output est.) session 49ac3a33-e356-58b6-9971-24a023142f66
tokens orchestrator-wave-1 ghx-m1-s1 (claude-opus-5-5): input ~32 / cache read ~1.8M / cache write ~0 5m + ~66k 1h / output ~13k | ~$1.14 API-equiv
tokens review-wave-2 ghx-m1-s2-s4 (claude-opus-5-5): input ~36 / cache read ~630k / cache write ~51k / output ~18k | ~$0.74 API-equiv (output est.) session 49ac3a33-e356-58b6-9971-24a023142f66
tokens wave-2 ghx-m1-s2-s4 (claude-sonnet-5-5): input ~50 / cache read ~2.3M / cache write ~120k / output ~25k | ~$1.00 API-equiv (output est.) session 49ac3a33-e356-58b6-9971-24a023142f66
tokens orchestrator-wave-2 ghx-m1-s2-s4 (claude-opus-5-5): input ~16 / cache read ~960k / cache write ~0 5m + ~28k 1h / output ~9.9k | ~$0.62 API-equiv
tokens review-wave-3 ghx-m2-s1 (claude-opus-5-5): input ~24 / cache read ~460k / cache write ~55k / output ~12k | ~$0.61 API-equiv (output est.) session 49ac3a33-e356-58b6-9971-24a023142f66
tokens wave-3 ghx-m2-s1 (claude-opus-5-5): input ~66 / cache read ~4.6M / cache write ~180k / output ~33k | ~$2.47 API-equiv (output est.) session 49ac3a33-e356-58b6-9971-24a023142f66
tokens orchestrator-wave-3 ghx-m2-s1 (claude-opus-5-5): input ~9 / cache read ~820k / cache write ~0 5m + ~27k 1h / output ~10k | ~$0.58 API-equiv
tokens review-wave-4 ghx-m2-s2 (claude-opus-5-5): input ~24 / cache read ~340k / cache write ~35k / output ~12k | ~$0.48 API-equiv (output est.) session 49ac3a33-e356-58b6-9971-24a023142f66
tokens wave-4 ghx-m2-s2 (claude-sonnet-5-5): input ~40 / cache read ~2.1M / cache write ~130k / output ~20k | ~$0.94 API-equiv (output est.) session 49ac3a33-e356-58b6-9971-24a023142f66
tokens orchestrator-wave-4 ghx-m2-s2 (claude-opus-5-5): input ~6 / cache read ~360k / cache write ~0 5m + ~14k 1h / output ~990 | ~$0.20 API-equiv

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
- Gary: create `RELEASE_TOKEN`, `HOMEBREW_TAP_TOKEN`, `SCOOP_BUCKET_TOKEN`; give `RELEASE_TOKEN` a ruleset bypass to push to `main`; add `ghx` rows to the tap's and bucket's READMEs.
- Decide the first version: `version.txt` reads 0.1.0 as the last release, so the first workflow release is 0.1.1, 0.2.0 or 1.0.0. Set it to 0.0.0 and bump minor if you want v0.1.0.
- Decide whether `RELEASE_TOKEN` via checkout's `token:` is acceptable or the rule should change.
- `internal/cloner`'s `TestCloneRepos_GivesGitTheTokenOnlyThroughItsEnvironment` fails when the environment sets `GIT_CONFIG_COUNT`; isolate those variables in the test.
- Unverified until a real release: the half-uploaded-release recovery, `fetch-release-checksums.sh` against real `gh`, and `brew trust lolay/tap`.
- Cosmetic: Makefile.md's release.yml row omits `--line`; `ci.yml`'s header comment doesn't mention `release-check`.
- Before merge: remove `specs/handoffs/plan-ghx-release-finch.md` and `handoff-ghx-release-finch.md` (the Completion summary is the only durable part, and it belongs in the PR description).
