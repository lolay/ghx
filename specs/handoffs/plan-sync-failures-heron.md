```
--- KICKOFF: begin orchestration at [deep] ---

  Status: 0/3 groups done | last review: — | current: ghx m1 s1 [exec] | BLOCKED at gate mode | updated 2026-10-13

  mode: pending | proposed unattended (harness=claude-code runner=cloud; CLAUDE_CODE_REMOTE=true, assigned branch claude/ecstatic-archimedes-2dxtun) | guard 3x min $50 | fixups 2

  review: every-wave (log-only)

  Next model
    Cursor:      claude-opus-5-5[effort=high]
    Claude Code: /model opus                (/effort high)

  Prompt to paste into the next chat:
    In lolay/ghx, read specs/handoffs/plan-sync-failures-heron.md. The plan is already tagged.
    On branch claude/ecstatic-archimedes-2dxtun (task branch): subagents commit each finished step.
    Run the personal-plan-orchestrate skill from the top: walk to
    each tier boundary, dispatch subagents per the skill's procedure,
    and pause only where the recorded mode stops. Do not execute plan
    work inline. Update plan progress after each wave returns per the
    skill's procedure. The mode line above is the kickoff answer. If the
    Status line shows BLOCKED, re-post that question and wait; otherwise
    record the mode, print the kickoff summary and begin dispatching.

---
```

**Cost (API-equiv, Claude Code models)**

| wave | expected tokens | expected $ |
|---|---|---|
| 1 [exec] ghx m1 s1 | ~2.0M | ~$0.89 |
| 2 [deep] ghx m1 s2 | ~6.1M | ~$3.2 |
| 3 [exec] ghx m1 s3-s5 | ~2.6M | ~$1.1 |
| orchestrator | ~5.7M | ~$5.0 |
| **Total** | ~16M | ~$10 |

Expected values are estimates, good to about 2-3× per wave.

# Fetch-first sync: fewer failures, no stale backups

## Context

The user's run against the Agerpoint org failed or skipped seven repos:

| Repo | What happened | Root cause in ghx |
|---|---|---|
| product, citrus_inventory, ap-agent-api, LichtFeld-Studio | `fatal: Remote branch main not found in upstream origin` on clone | `processRepo` (`internal/cloner/cloner.go`) passes `--branch <api default_branch>`. GitHub reports `main` for a repo whose HEAD names a branch that doesn't exist: an empty repo, or one whose pushes never created `main` (imports, mirrors). |
| apc-potree | `main` was force-pushed upstream (`+ 7095a0ac...3cfe8706 main -> origin/main (forced update)`), then `fatal: Not possible to fast-forward, aborting.` | `pull --ff-only` can't follow a rewritten remote even when the local branch holds no work of its own |
| tokenburn | Plain update, still `Not possible to fast-forward` | Local commits on `main` not on GitHub: real divergence that needs a human |
| streamlit-dabs-test | `skipped (local changes)`, not even fetched | Any `git status --porcelain` line skips the repo, untracked files included |

The failure details also print git's whole stderr (fetch progress, `hint:` blocks), burying the one useful line.

Design decisions (agreed with the user):

- **Clone without `--branch`.** Plain `git clone` follows the remote's HEAD. When HEAD names a missing branch but other branches exist, git clones everything and checks out nothing; ghx then picks `main`, else `master`, else the remote branch with the newest commit. That's the user's fallback, used only where there is no real default to follow.
- **Empty repos are skipped, not failed**: new outcome `skipped (empty)`, shown with the `∅` glyph. An empty clone is removed so the output tree holds only real repos; the next run tries again.
- **Always fetch existing clones first.** `fetch` never touches the working tree, so every remote branch is backed up each run, dirty or not. The checkout moves only when that's safe; otherwise the new outcome `fetched only` carries a one-line reason. `failed` then means the data didn't come down (auth, network, repo gone).
- **Follow force-pushes when local holds no work.** Record `origin/<branch>` before the fetch. If the remote was rewritten and HEAD is an ancestor of that old tip (nothing unpushed) and the tree is clean, keep the old tip under `refs/ghx/forced/<branch>/<UTC timestamp>` (a backup tool shouldn't lose history GitHub dropped), then `reset --hard` to the new tip. With local work on top: `fetched only`.
- **Untracked files don't block.** Dirty means tracked changes (`--untracked-files=no`). A fast-forward that would overwrite an untracked file is refused by git, safely, and reported as `fetched only`.
- **No autostash**: a conflicting stash pop would leave a backup tool editing the user's work.
- **Other failure modes handled in the same change**: 5-minute timeout killing big clones; transient network errors (one retry); clone succeeded but checkout failed (LFS); destination exists but isn't a clone; merge/rebase/cherry-pick in progress; wiki errors hidden behind `skipped wiki (not found)`.
- **Not handled, flagged to the user as follow-ups**: ghx never runs `git lfs fetch` (only LFS pointers are backed up); submodules aren't initialized; a repo renamed on GitHub moves to `DELETED/` and is re-cloned under its new name.

Expected result on a rerun of the user's sync: the four clone failures clone (or show `∅ skipped (empty)` if truly empty); apc-potree resets with its old tip kept under `refs/ghx/forced/`; tokenburn and streamlit-dabs-test are listed under `fetched only` with reasons, their remote refs current.

## Standards for every step

- `AGENTS.md` and `CONTRIBUTING.md` at the repo root: `make ci` is the gate and must pass after every step; tests are black-box `_test.go` beside the code, table-driven, testify; no network, no real home directory (`specs/testing.md` has the seams: `gittest.Isolate`, `gittest.NewBareRepo`, `gittest.AddCommit`, `gittest.Git`, `cloner.RecordGit` in `internal/cloner/export_test.go`).
- Go standards: `/root/.claude/plugins/synced/4149bb76-d9dd-4519-8b56-59de6bf4ffb6_53f5ad27-2152-4efe-a9c5-d0a87ed95169/personal/skills/personal-standards/standards/go/code-style.md` and `.../go/testing.md` (same folder).
- Paths with `filepath`; nothing macOS-only (Windows and Linux are supported). Comments explain why. Functions under about 50 lines.
- git 2.31 is the minimum ghx supports (`minGitMajor`/`minGitMinor` in `internal/cloner/git.go`); use no git feature newer than that.

## m1 - Fetch-first sync

--- WAVE 1 [exec] ---
#### s1 - [exec] Git probes and network-command plumbing

Files: `internal/cloner/git.go`, `internal/cloner/git_test.go` (and `export_test.go` if a test needs a new seam).

- Add local (no-credential) probe methods on `gitRunner`:
  - `currentBranch(ctx, dir) (name string, detached bool, err error)` via `symbolic-ref --short -q HEAD` (exit 1 with no output means detached).
  - `revParse(ctx, dir, ref) (sha string, ok bool)` via `rev-parse -q --verify <ref>^{commit}`.
  - `isAncestor(ctx, dir, a, b) bool` via `merge-base --is-ancestor`.
  - `upstreamTrack(ctx, dir, branch) (upstream string, gone bool)` via `for-each-ref --format=%(upstream)%00%(upstream:track) refs/heads/<branch>`; `gone` when the track field is `[gone]`.
  - `remoteBranches(ctx, dir) []string` via `for-each-ref --sort=-committerdate --format=%(refname:lstrip=3) refs/remotes/origin`, excluding `HEAD`, newest commit first.
  - `operationInProgress(dir) string` that resolves the git dir (`rev-parse --git-dir`) and returns `merge`, `rebase`, `cherry-pick` or `revert` when `MERGE_HEAD`, `rebase-merge/` or `rebase-apply/`, `CHERRY_PICK_HEAD`, `REVERT_HEAD` exists there, else `""`.
- Add a package function `pickBranch(defaultBranch string, branches []string) string`: `defaultBranch` if present in `branches`, else `main`, else `master`, else `branches[0]` (newest), else `""`.
- Network commands (clone, fetch, pull): run them with `-c http.lowSpeedLimit=1000 -c http.lowSpeedTime=60` prepended and a 60-minute timeout (`networkTimeout`); local commands keep `gitTimeout` (5 minutes). Do it inside `gitRunner.fetch` so every caller gets it.
- Retry once on transient failures: in `gitRunner.fetch`, if the command fails and its stderr matches a fixed, documented list (`RPC failed`, `early EOF`, `the remote end hung up unexpectedly`, `Could not resolve host`, `Connection timed out`, `Connection reset`, `HTTP 429`, `The requested URL returned error: 5`, `unexpected disconnect`), wait 2 seconds (a package variable so tests set it to 0) and run it once more. A clone retry first removes a destination the failed attempt left behind only if it didn't exist before the attempt. Never retry `Repository not found` or an auth failure.
- `gitResult.Message()`: when stderr has any line starting `fatal:` or `error:`, return the last such line (still redacted); otherwise keep today's behavior. This drops fetch progress and `hint:` blocks from details.
- Tests (table-driven, `gittest` bare repos, no network): each probe against real local repos (detached HEAD, a `[gone]` upstream, unborn HEAD, rebase in progress), `pickBranch` cases, the network args and timeout through `RecordGit`, retry on `RPC failed` (first call fails, second succeeds) and no retry on `Repository not found`, `Message()` trimming.

Acceptance: `make ci` passes; no behavior change yet in `processRepo` beyond the network args, retry and message trimming.

--- WAVE 2 [deep] ---
#### s2 - [deep] Fetch-first decision table, clone fallback and empty skip

Files: `internal/cloner/cloner.go`, `internal/cloner/action.go`, `internal/cloner/cloner_test.go`, plus the minimum in `internal/ui/ui.go` and `internal/ui/ui_test.go` to keep `make ci` green (s4 does the presentation).

`action.go`:
- Add `ActionFetchedOnly Action = "fetched only"` (after `ActionPulled`), `ActionSkippedEmpty Action = "skipped (empty)"`, `ActionFailedWiki Action = "failed wiki"`, all in `AllActions`.
- Remove `ActionSkippedDirty`; nothing produces it any more. Update the Python-parity comment: the Go port already diverged with `ActionSkippedName`.
- In `ui.go`, swap the `ActionSkippedDirty` case for `ActionFetchedOnly` printing `name: detail` under the existing yellow heading renamed "Fetched only (working tree not updated):". Nothing more in ui; s4 owns the presentation.

Clone path in `processRepo`:
- If the destination exists and has no `.git` directory: `ActionFailed`, detail `<dest> exists and isn't a git clone; move it aside`, before running git. Never touch it.
- Drop `"--branch", repo.DefaultBranch` from the clone args.
- After the clone command: if it failed and the destination now has a `.git` directory, and stderr contains `Clone succeeded, but checkout failed`, report `ActionFetchedOnly` with the trimmed error (objects are down; typically LFS). Other failures stay `ActionFailed`.
- After a successful clone: if `remoteBranches` is empty, remove the clone (`DeleteRepo`) and report `ActionSkippedEmpty`, detail `empty repository`. If HEAD is unborn (`revParse HEAD` not ok) and branches exist, `b := pickBranch(repo.DefaultBranch, branches)`, run `switch --track -c <b> origin/<b>`, then `ActionCloned` with detail `remote HEAD names <repo.DefaultBranch>, which doesn't exist; checked out <b>`.

Existing clone: replace the `hasGitDir` branch of `processRepo` with `syncExisting(ctx, git, dest, repo, opts) RepoResult`, keeping `processRepo` short. Decision table, in order:
1. `setIdentity`, then `cleanOrigin` (failure: `ActionFailed`). Both run dirty or not; they only touch config.
2. Record pre-fetch tips: `old := revParse(refs/remotes/origin/<b>)` for the target `b` (step 4 settles `b`; record the old tip of `repo.DefaultBranch` and, if HEAD is on a branch, of that branch's upstream).
3. `git.fetch(ctx, dest, repo.CloneURL, "fetch", "--prune", "--quiet", "origin")`. Failure: `ActionFailed` with `Message()`.
4. `branches := remoteBranches`. Empty: `ActionSkippedEmpty`. Target `b := pickBranch(repo.DefaultBranch, branches)`.
5. Point `refs/remotes/origin/HEAD` at `refs/remotes/origin/<b>` with `symbolic-ref` (local only).
6. `operationInProgress` non-empty: `ActionFetchedOnly`, detail `<op> in progress`.
7. `currentBranch` detached: `ActionFetchedOnly`, detail `detached HEAD`.
8. Dirty is `status --porcelain --untracked-files=no` non-empty. A failed status: `ActionFailed`, as today.
9. HEAD on `b`:
   - dirty: `ActionFetchedOnly`, `local changes`.
   - HEAD unborn: `switch --track -C <b> origin/<b>` (an empty clone that gained commits), then `ActionPulled`.
   - `isAncestor(HEAD, origin/<b>)`: `merge --ff-only refs/remotes/origin/<b>`. Success is `ActionPulled`. Failure (an untracked file would be overwritten, a stale `index.lock`) is `ActionFetchedOnly` with the trimmed message.
   - else, when `old` was recorded, `old` is not an ancestor of the new `origin/<b>` (a force-push), and `isAncestor(HEAD, old)` (nothing unpushed): `update-ref refs/ghx/forced/<b>/<UTC yyyymmddThhmmssZ> <HEAD sha>`, then `reset --hard refs/remotes/origin/<b>`. `ActionPulled`, detail `origin/<b> was force-pushed; old tip kept at <ref>`.
   - else: `ActionFetchedOnly`, detail `<n> local commit(s) not on origin/<b>` (`rev-list --count origin/<b>..HEAD`).
10. HEAD on another branch `cur`:
    - Renamed default (GitHub's documented post-rename fix, done only when nothing can be lost): no local branch `b` (`revParse refs/heads/<b>` not ok), `cur`'s upstream is `[gone]` or unset, not dirty, and HEAD unborn or `isAncestor(HEAD, origin/<b>)`. Run `switch --track -c <b> origin/<b>`, then `branch -d <cur>` (only if HEAD was born; `-d` refuses unmerged work, and a refusal is ignored). `ActionPulled`, detail `switched from <cur> to <b>`.
    - Otherwise: if local `b` exists, fast-forward it without checking it out (`fetch . refs/remotes/origin/<b>:refs/heads/<b>`, which git refuses unless it's a fast-forward; ignore a refusal). `ActionFetchedOnly`, detail `on branch <cur>`.

Tests in `cloner_test.go`, table-driven where shapes repeat, `gittest` bare repos, no network:
- Empty bare repo: `skipped (empty)`, no directory left. After a commit lands: `cloned`.
- Bare repo whose HEAD names missing `main` with branches `master` and `dev`: `master` checked out, detail set. With only `dev` and `feature`: the one with the newer commit.
- Destination exists without `.git`: `failed`, contents untouched.
- Force-push upstream, clean local: `pulled`, HEAD at the new tip, backup ref at the old tip. Same with an unpushed local commit: `fetched only`, commit intact.
- Diverged (the tokenburn case): `fetched only`, `origin/main` advanced, local untouched.
- Tracked change: `fetched only` / `local changes`, `origin/main` advanced. Untracked file only: `pulled`. Untracked file the incoming commit adds: `fetched only`, file intact.
- Feature branch checked out: `fetched only` / `on branch x`, local `main` fast-forwarded.
- Detached HEAD: `fetched only`. Rebase stopped on a conflict: `fetched only` / `rebase in progress`.
- Rename `master` to `main` upstream (rename in the bare repo and set its HEAD; pass `DefaultBranch: "main"`): `pulled`, HEAD on `main`, `master` deleted. With an unpushed commit on `master`: `fetched only`, `master` kept.
- Fetch failure (bad remote URL): `failed`, detail is a single `fatal:` line.
- Clone whose checkout fails (a smudge filter configured to fail via `GIT_CONFIG_*` env in the test, after `gittest.Isolate`): `fetched only`, objects present.
- Update `TestCloneRepos_SkipsADirtyWorkingTree` and `TestCloneRepos_ReportsAFailedPull` to the new semantics.

Acceptance: `make ci` passes; every case above is covered by a test; no path runs `reset --hard`, `switch` or `branch -d` unless the guards above hold.

--- WAVE 3 [exec] ---
#### s3 - [exec] Wikis through the same sync and honest wiki errors

Files: `internal/cloner/cloner.go` (`processWiki`), `internal/cloner/cloner_test.go`.

- Existing wiki: call `syncExisting` with the target branch taken from `refs/remotes/origin/HEAD` after the fetch (wikis have no API default branch; pass a `RepoInfo` copy whose `DefaultBranch` is `""` so `pickBranch` falls to `main`/`master`/newest). Return its result named `<repo>.wiki` only when it is not `ActionPulled` (a quiet success stays quiet, as today), so `fetched only` and failures surface.
- Wiki clone: `Repository not found` (or a 404) in stderr stays `ActionSkippedWiki`. Any other failure is `ActionFailedWiki` with the trimmed message. An empty wiki clone is removed and reports nothing.
- Update `TestCloneRepos_Wikis` and add cases: a wiki clone failing with another error gives `failed wiki`; an existing wiki that diverged gives `fetched only`.

Acceptance: `make ci` passes.

#### s4 - [exec] Summary glyphs and sections

Files: `internal/ui/ui.go`, `internal/ui/ui_test.go`.

- One map in `ui.go`, beside the colors, from action to glyph: `✗` failed and failed wiki (red), `⚠` fetched only (yellow), `∅` skipped (empty) (faint), `·` notes.
- `PrintSummary` sections, each line `  <glyph> <name>  <detail>`:
  - "Failed repos:" (`ActionFailed`, `ActionFailedWiki`), red.
  - "Fetched only (working tree not updated):", yellow.
  - "Skipped (empty):", faint, detail `empty repository, skipped`.
  - "Skipped (unusable name):", as today plus a glyph (`⚠`).
  - "Notes:" for `ActionCloned`/`ActionPulled` results that carry a detail (branch fallback, force-push reset, rename switch), so none is silent.
- In the counts table, prefix the `skipped (empty)`, `fetched only` and `failed` rows' labels with their glyph.
- The words always accompany the glyph, so output stays readable when the font lacks it or output is piped.
- Tests: the `∅` line and section for an empty result; the glyph on each other section; the Notes section; no section printed when it has no rows.

Acceptance: `make ci` passes.

#### s5 - [exec] README and CHANGELOG

Files: `README.md` ("How it works"), `CHANGELOG.md` (`## [Unreleased]`).

- README "How it works", replacing the existing-repos bullet: existing repos are fetched every run (every remote branch backed up); the working tree fast-forwards when it is on the default branch with no tracked changes; otherwise the summary lists the repo as fetched only, with the reason. Describe: untracked files don't block; force-pushed branches are followed when local has no unpushed work, with the old tip kept under `refs/ghx/forced/<branch>/<timestamp>`; a renamed default branch is switched to when nothing would be lost; a repo whose HEAD names a missing branch checks out `main`, else `master`, else the newest branch; empty repos are skipped (`∅`); big clones aren't capped at 5 minutes (stalled transfers abort after 60 s below 1 KB/s); transient network errors retry once. Add a short "Not backed up" note: Git LFS content, and submodules.
- CHANGELOG `[Unreleased]`: `### Changed` (every clone is fetched; `fetched only` replaces `skipped (local changes)`; summary glyphs; failure details show git's last `fatal:`/`error:` line), `### Fixed` (repos whose HEAD names a missing branch, empty repos, force-pushed default branches, renamed default branches, feature branches and detached HEADs, big clones killed at 5 minutes, wiki errors reported as not found). Match the file's existing style.

Acceptance: `make ci` passes; docs match the behavior shipped in s1-s4.

## Verification

- `make ci` after every step.
- Manual, by the user after the branch lands: rerun `ghx Agerpoint <dir>` on the same output directory. Expect no `failed` rows for the seven repos above, apc-potree reset with a backup ref, tokenburn and streamlit-dabs-test under `fetched only` with reasons.

## Git

One commit per step on `claude/ecstatic-archimedes-2dxtun`, subject `m1.sN <imperative subject>`. No PR unless asked.

## Review log

## Token log

**Counting header (Claude Code)**

- Line, one per model a chat ran, appended below: `tokens <row> <group-id> (<model>): input ~X / cache read ~R / cache write ~W / output ~Y | ~$C API-equiv`. `<row>` is `wave-N`, `review-wave-N`, or `wave-N-fix` and `review-wave-N-fix` for a fix-up wave; `<group-id>` is the wave's group id with hyphens (`ghx-m1-s1`); `<model>` is `message.model` without a date suffix. Round counts to two significant figures with `k` or `M`.
- Usage: `~/.claude/projects/<slug>/$CLAUDE_CODE_SESSION_ID.jsonl` (`<slug>` is the working directory with every character but a letter or digit turned into `-`, matched by prefix when long; without the id, the newest `.jsonl` there), plus its subagents' `<session-id>/subagents/**/agent-*.jsonl` in the same directory. Sum `message.usage` over assistant lines once per `message.id`, from the line with `stop_reason`: input `input_tokens`, cache read `cache_read_input_tokens`, cache write `cache_creation.ephemeral_5m_input_tokens` (with `ephemeral_1h_input_tokens` too, `cache write ~40k 5m + ~8k 1h`), output `output_tokens`. A call with no `stop_reason` line keeps its input-side counts, takes output as about 1,000, and its line ends `(output est.) session <id>`.
- Rates by `<model>`, $ per Mtok input / cached / output: `claude-opus-5-5` 4.00 / 0.20 / 20.00; `claude-sonnet-5-5` 2.00 / 0.20 / 10.00.
- `$C` = (input × in + cache read × cached + 5m write × in × 1.25 + 1h write × in × 2.00 + output × out) / 1M.
- In another harness, or on a model not listed here, count and price per plan-execution.md "Token accounting" and "Model price table" instead.
