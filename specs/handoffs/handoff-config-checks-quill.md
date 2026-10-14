# Handoff: config-checks-quill

- **Plan:** `specs/handoffs/plan-config-checks-quill.md` (closes lolay/ghx#1)
- **Branch:** `claude/charming-allen-aaonrw` (assigned to the resume session by the cloud harness; fast-forwarded from `claude/practical-goldberg-xu3jxn` at `254dc42`, where the kickoff ran). Task branch.
- **Mode:** unattended, confirmed 2026-10-14 in session aeb2cfec (dialog: "Unattended (Recommended)"), re-confirmed 2026-10-14 in session 3587b07b by Kickoff prompt (same harness and runner tokens).
- **Done:** wave 1 `[exec]` ghx m1 s1-s4, `45cae77..1b43336` (m1.s1 `90ad6e5`, m1.s2 `53561cf`, m1.s3 `151f469`, m1.s4 `1b43336`). `check_wave.py` passed. Fix-up 1-fix: `c2bfb7a` (m1.s1 Keep a path's case in terminal errors), check passed.
- **Review log:** wave-1 CONCERNS: on a TTY fang title-cases the first word of the error, which is now the config file path, so the path shown is wrong (`/Tmp/.../.Ghx.toml`). Everything else matched the spec; `make ci` and `make vuln` pass, x/text v0.43.0. wave-1-fix CONCERNS: the handler only skips title-casing when the first word has a separator, so `ghx acme .` still shows `.Ghx.toml:`; suggested fix is to also skip it when `errors.Is(err, config.ErrInvalidValue)`.
- **Fix-ups, retries, checkpoints:** wave 1-fix (first CONCERNS, automatic, no gate) for the fang casing finding; wave 1-fix2 (second CONCERNS, gate 1 passed unattended as fix-up 2 of 2) for the separator-less path. The cap is then spent: another CONCERNS stops.
- **Spend:** ~$6.39 logged of ~$4.0 expected (over expected; guard $50). Kickoff ~$2.46, resume parent ~$1.71, wave 1 ~$1.52, 1-fix ~$0.70.
- **Next unit:** wave 1-fix2 `[exec]` ghx m1 s1-s4 (Sonnet high worker, Opus high reviewer), from `45cae77`. Launching after `e3b5910` (which fixed the 1-fix Review log line's order: checkpoint before the date, so `plan_state.py` parses it).
- **Resume:** paste the plan's Kickoff prompt.

## Pending question (verbatim)

None.

## Decisions so far

- Wrong-typed config values and floats for integer keys are errors from `config.Load`; unknown keys are yellow warnings (Gary, 2026-10-14, in planning).

## Environment notes

- `make doctor` fails in this container: `triage` isn't installed (`brew install lolay/tap/triage`). It is a doctor-only tool; `make ci` doesn't need it.

## Deviations from plan

- The wave 1 worker bumped every module in `go.mod` with an update (go-pretty, lipgloss, x/term and others), not only x/text, since `make ci` stayed green; none held back. It also added a CHANGELOG Unreleased entry in m1.s2 (CONTRIBUTING requires one).
- Wave 1 review CONCERNS gets an automatic fix-up `1-fix` (first CONCERNS, no gate in either mode).
- Wave 1-fix review CONCERNS gets an automatic fix-up `1-fix2` (gate 1 passed unattended, fix-up 2 of 2).

- The resume session's harness assigned `claude/charming-allen-aaonrw`, so the run continues there (skill path (b)); the Kickoff prompt's `On branch` line now names it. `claude/practical-goldberg-xu3jxn` stays at `254dc42` and is stale.
