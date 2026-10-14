```
--- KICKOFF: begin orchestration at [deep] ---

  Status: 1/1 groups done | last review: wave-1 CONCERNS | current: wave 1-fix ghx m1 s1-s4 [exec] | updated 2026-10-14 | over expected: ~$5.20 of ~$4.0

  mode: unattended | proposed unattended (harness=claude-code runner=cloud; CLAUDE_CODE_REMOTE=true, assigned branch) | guard 3x min $50 | fixups 2 | confirmed 2026-10-14 session 3587b07b-d4af-58e8-ab63-47e6c35d27e1 by Kickoff prompt: Run in unattended mode.

  review: every-wave (log-only)

  Next model
    Cursor:      claude-opus-5-5[effort=high]
    Claude Code: /model opus                (/effort high)

  Prompt to paste into the next chat:
    In lolay/ghx, read specs/handoffs/plan-config-checks-quill.md. The plan is already tagged.
    On branch claude/charming-allen-aaonrw (task branch): subagents commit each finished step.
    Run in unattended mode.
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
| 1 [exec] ghx m1 s1-s4 | ~2.9M | ~$1.2 |
| 1-fix [exec] ghx m1 s1-s4 | — | — |
| orchestrator | ~3.1M | ~$2.8 |
| **Total** | ~6.0M | ~$4.0 |

Expected values are estimates, good to about 2-3× per wave.

# Plan: close issue #1 (config value checks, x/text bump, README nits)

## Context

[lolay/ghx#1](https://github.com/lolay/ghx/issues/1) lists follow-ups from the baseline work. Two config behaviours are pinned by tests but were never decided:

- A wrong-typed value (`concurrency = "8"`) is skipped silently and the next source wins, so a typo in `./acme/.ghx.toml` quietly hands control to `~/.ghx.toml` and the settings table reports that source as if it were intended.
- A float for an integer key is truncated (`asInt` in `internal/config/resolve.go`), so `concurrency = 4.9` becomes 4.

Decisions (answered by Gary, 2026-10-14):

- Wrong type → **error**.
- Float for an integer key → **error**, `8.0` included.
- Unknown key (`concurency = 8`) → **yellow warning**, keep running.
- The rest of #1 rides the same branch: bump `golang.org/x/text` to v0.41.0+ (GO-2026-6629; v0.39.0 already covers GO-2026-5970), review the other indirect modules, and fix two README nits.

Branch: `claude/charming-allen-aaonrw` (assigned task branch; kicked off on `claude/practical-goldberg-xu3jxn`). The working directory is the ghx repo root.

Gate: `make ci` (build, lint, test) must pass before each step's commit. Follow `AGENTS.md` (Go standards, tests, git rules) and `CONTRIBUTING.md`.

## m1 - Config validation and issue #1 follow-ups

--- WAVE 1 [exec] ---
#### s1 - [exec] Check config values in config.Load (done)

The check goes in `Load`, not the resolvers, because `Load` is the only place that knows which file a value came from, and the 15 `Resolve*` call sites in `internal/cli/run.go` stay untouched.

- New `internal/config/keys.go`: an unexported table of known keys to kinds (string, bool, int), taken from the README option table and the keys `run.go` resolves: `token`, `protocol`, `type`, `include`, `exclude`, `clone-wiki`, `concurrency`, `dry-run`, `git-author`, `git-email`, `delete`, `archived-dir`, `deleted-dir`, `max-size`. `ssh` is deliberately absent (a flag-only shortcut for `protocol`). Export `IsKey(name string) bool` for the drift test in s2.
- An unexported check over a decoded, normalized `Config` and its file path, called from `Load` for the home file and the local file:
  - wrong kind → an error like `<path>: concurrency must be an integer, got string "8"`. Collect every bad key in the file (sorted, for stable output) and combine them with `errors.Join`; each wraps a new exported sentinel `ErrInvalidValue` so callers and tests can use `errors.Is`.
  - TOML integers decode to `int64`; a `float64` is the wrong kind for an int key, `8.0` included.
  - unknown key → a warning like `<path>: unknown key "concurency" ignored`.
- `Load`'s `messages []string` becomes `[]Notice`, with `Notice{Text string; IsWarning bool}` (doc comments on both). Migration notes stay plain; unknown-key notes are warnings. `run.go` prints warnings with `text.FgYellow` and the rest faint, as today.
- `asInt` in `resolve.go`: drop the `float64` case. The resolvers keep their type assertions; `Load` now guarantees they never see a wrong type from a file.

Tests (`internal/config`, black-box, table-driven, testify, `t.TempDir` plus `t.Setenv` for `HOME` and `USERPROFILE`, as the existing `TestLoad_*` do):

- New `TestLoad_RejectsWrongTypes`: string for int, float for int (`4.9` and `8.0`), int for bool, bool for string, in the home file and in the local file. The error satisfies `errors.Is(err, config.ErrInvalidValue)` and names the file and key. Two bad keys in one file are reported together.
- New `TestLoad_WarnsOnUnknownKeys`: `concurency = 8` and `ssh = true` each return a warning notice and no error, and known values still load.
- Existing `TestLoad_*` that read the messages adjust to `Notice`.
- `resolve_test.go`: replace the `"TOML float truncates"` row with a float-falls-through row at resolver level. Keep the other wrong-type fall-through rows, with one comment that `Load` rejects these before resolution and the rows pin the resolver's defensive branch.
- `internal/cli/run_test.go`: one case where the output directory's `.ghx.toml` has `concurrency = "8"` and the run fails with that error before authenticating (the fake GitHub API sees no request). Use the existing harness in `internal/cli/harness_test.go`.

Acceptance: `make ci` passes; the new tests fail on the old code and pass on the new.

#### s2 - [exec] Update the README and add a flag drift guard (done)

- README Configuration section: replace "A config value of the wrong type (for example `concurrency = "8"`) is ignored and the next source is used. A TOML float given for an integer key is truncated." with the new behaviour: a wrong-typed value, or a float for an integer key, stops the run with an error naming the file and key; an unknown key gets a warning and is ignored.
- README: reword "Every command-line option can be set as a config key, hyphenated like the flag." so it no longer claims `--ssh` has a key (every option except `--ssh`, which `protocol` covers).
- README: if `dry-run` is missing from the TOML example or the option table, add it.
- `internal/cli/root_test.go`: a test that walks the root command's flags and asserts each one except `ssh`, `help` and `version` satisfies `config.IsKey(name)`, so the README's promise can't drift.
- Check `specs/terminal-output.md` for anything describing config load messages; update it if so.

Acceptance: `make ci` passes; the README matches the s1 behaviour.

#### s3 - [fast] Tag the README help code fence (done)

- The fenced block holding the `ghx --help` output (README, around line 347) gets the language tag `text`. Change nothing else.

Acceptance: `make ci` passes.

#### s4 - [exec] Bump golang.org/x/text and review indirect modules (done)

- `go get golang.org/x/text@latest` (must be v0.41.0 or later), then `go mod tidy`.
- `go list -m -u all` to review the other indirect modules; bump the ones with updates where it's safe (via their direct parents where possible), and list any held back, with why, in the commit body.
- `make vuln` must report neither GO-2026-6629 nor GO-2026-5970.

Acceptance: `make ci` and `make vuln` pass.

## Verification

- `make ci` passes after every step; it's the gate.
- `make vuln` is clean for x/text after s4.
- Manual check with a built binary and a temp `HOME`:
  - `~/.ghx.toml` with `concurrency = "8"`: `ghx --dry-run acme <tmp>` exits non-zero with `~/.ghx.toml: concurrency must be an integer, got string "8"` (the real path) before "Authenticating...".
  - `concurrency = 4.9`: the same, naming the float.
  - `concurency = 8`: a yellow unknown-key warning, and the run proceeds to auth.
- When done: ask Gary before opening a PR (it should say `Closes #1`), and remove the plan and handoff from `specs/handoffs/` as the last commit before merge.

## Review log

review wave-1 (ghx m1 s1-s4) 45cae77..1b43336: CONCERNS - make ci and make vuln pass (x/text v0.43.0, no vulns); s1-s4 match the spec. Finding: on a TTY, fang's default error handler title-cases the first word of the error, which is now the file path, so the user sees `/Tmp/.../H/.Ghx.toml: concurrency must be an integer, got string "8".` instead of the real path (fang@v1.0.0 theme.go:186-206, help.go:112-121; internal/cli/root.go:41 has no custom error handler); this breaks "names the file" and the manual check. Suggested fix: a fang error handler in internal/cli/root.go that does not change the message case - 2026-10-14

## Token log

**Counting header (Claude Code)**

- Line, one per model a chat ran, appended below: `tokens <row> <group-id> (<model>): input ~X / cache read ~R / cache write ~W / output ~Y | ~$C API-equiv`. `<row>` is `wave-N`, `review-wave-N`, or `wave-N-fix` and `review-wave-N-fix` for a fix-up wave; `<group-id>` is the wave's group id with hyphens (`ghx-m1-s1-s4`); `<model>` is `message.model` without a date suffix. Round counts to two significant figures with `k` or `M`.
- Usage: `~/.claude/projects/<slug>/$CLAUDE_CODE_SESSION_ID.jsonl` (`<slug>` is the working directory with every character but a letter or digit turned into `-`), plus its subagents' `<session-id>/subagents/**/agent-*.jsonl` in the same directory. Sum `message.usage` over assistant lines once per `message.id`, from the line with `stop_reason`: input `input_tokens`, cache read `cache_read_input_tokens`, cache write `cache_creation.ephemeral_5m_input_tokens` (with `ephemeral_1h_input_tokens` too, `cache write ~40k 5m + ~8k 1h`), output `output_tokens`. A call with no `stop_reason` line keeps its input-side counts, takes output as about 1,000, and its line ends `(output est.) session <id>`.
- Rates by `<model>`, $ per Mtok input / cached / output: `claude-opus-5-5` 4.00 / 0.20 / 20.00; `claude-sonnet-5-5` 2.00 / 0.20 / 10.00; `claude-haiku-4-5` 1.00 / 0.10 / 5.00.
- `$C` = (input × in + cache read × cached + 5m write × in × 1.25 + 1h write × in × 2.00 + output × out) / 1M.
- In another harness, or on a model not listed here, count and price per plan-execution.md "Token accounting" and "Model price table" instead.
tokens orchestrator-kickoff plan-config-checks-quill (claude-opus-5-5): input ~66 / cache read ~4.3M / cache write ~0 5m + ~140k 1h / output ~23k | ~$2.46 API-equiv
tokens orchestrator-resume plan-config-checks-quill (claude-opus-5-5): input ~28 / cache read ~1.6M / cache write ~0 5m + ~87k 1h / output ~10k | ~$1.23 API-equiv
tokens wave-1 ghx-m1-s1-s4 (claude-sonnet-5-5): input ~56 / cache read ~2.7M / cache write ~120k / output ~28k | ~$1.12 API-equiv (output est.) session 3587b07b-d4af-58e8-ab63-47e6c35d27e1
tokens review-wave-1 ghx-m1-s1-s4 (claude-opus-5-5): input ~16 / cache read ~170k / cache write ~32k / output ~10k | ~$0.40 API-equiv (output est.) session 3587b07b-d4af-58e8-ab63-47e6c35d27e1
