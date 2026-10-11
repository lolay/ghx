```
--- KICKOFF: begin orchestration at [deep] ---

  Status: 0/3 groups done | last review: — | current: ghx m1 s1-s3 [exec] | updated 2026-10-10

  mode: gated | proposed gated (harness=claude-code runner=none; Mac CLI workstation, dogfood run A) | guard 3x min $50 | fixups 2 | confirmed 2026-10-10 session 4e8ee344-6dc5-44cf-8111-98422ff3c14c: gated

  review: every-wave (log-only)

  Next model
    Cursor:      claude-opus-5-5[effort=high]
    Claude Code: /model opus                (/effort high)

  Prompt to paste into the next chat:
    In lolay/ghx, read specs/handoffs/plan-ghx-baseline-heron.md. The plan is already tagged.
    On branch feature/ghx-baseline-heron (task branch): subagents commit each finished step.
    Run in gated mode.
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
| 1 [exec] ghx m1 s1-s3 | ~2.6M | ~$1.1 |
| 2 [deep] ghx m1 s4 | ~6.1M | ~$3.2 |
| 3 [exec] ghx m2 s1-s2 | ~3.0M | ~$1.2 |
| orchestrator | ~7.5M | ~$7.3 |
| **Total** | ~19M | ~$13 |

Expected values are estimates, good to about 2-3× per wave.

# Plan: bring ghx to triage's repo baseline (heron)

Run A of the orchestrate phase 1 dogfood (`GaryRudolph/public` `specs/handoffs/dogfood-orchestrate-native.md`): Mac CLI, gated. First of three plans that bring `lolay/ghx` to the shape of `lolay/triage`; B (`plan-ghx-platforms-lynx`) and C (`plan-ghx-release-finch`) start from `main` after this plan's PR merges.

## Context

- **ghx today** (`main` at `b2b9866`): a Go port of a Python tool that clones and refreshes every repo of a GitHub org. Module `github.com/garyrudolph/ghx` (the repo is `lolay/ghx`, so `go install` of that path fails), binary `ghx-go` from `cmd/ghx-go/` (named so it could sit beside the Python `ghx`; the README says to rename it once the port is accepted). Packages: `internal/{cli,cloner,config,ghapi,manifest,ui}`. No tests, no Makefile, no CI, no CHANGELOG; `gofmt -l` flags `internal/config/resolve.go`. `go vet` passes for darwin, linux and windows.
- **The model, triage** (`lolay/triage`, public; on Gary's Mac at `~/Projects/lolay/triage/triage/`, elsewhere shallow-clone it into a temp directory outside this repo and only read it): `Makefile` + `Makefile.md`, `.go-version`, `.golangci.yml`, `.gitattributes`, `CHANGELOG.md`, `CONTRIBUTING.md`, `SECURITY.md`, `.github/CODEOWNERS`, `triage.yaml` for `make doctor`.
- **Before this plan** Gary committed `/.scratch/` in `.gitignore` and `.claude/settings.json` (the `personal-repo-baseline` attribution) on `main`.

## Rules for every step

- Read Gary's standards first: `makefile.md`, `git.md`, `documentation.md`, `testing.md` and `go/` (`code-style.md`, `testing.md`, `documentation.md`); the `personal-makefile` skill's `reference.md` for the Go recipes.
- Commit each finished step on the task branch as `m{N}.s{K} <imperative subject>` (72 characters at most, no period), a blank line, a body if useful, then `Assisted-by: Claude Code` as the last paragraph. Never `Co-authored-by`. Don't push; the orchestrator does.
- `go build ./...` and `go vet ./...` pass at the end of every step; once the Makefile exists, `make ci` does.
- **No outward actions:** no tags, releases, PRs, GitHub settings, workflow runs, or writes to any other repo. Anything that needs one stops with `needs_info`.
- Paths with `filepath`, never a hard-coded `/`; nothing that works only on macOS (plan B adds Windows and Linux CI, and this plan's code must not get in its way).

## m1 - Identity, build and first tests

--- WAVE 1 [exec] ---

#### s1 - [exec] Rename the module to github.com/lolay/ghx and the binary to ghx

- `go mod edit -module github.com/lolay/ghx`; rewrite every import. `git mv cmd/ghx-go cmd/ghx`; cobra `Use` becomes `ghx <ORG> <PATH>`; the package comment in `cmd/ghx/main.go` drops the side-by-side note.
- README: `ghx-go` becomes `ghx` everywhere, the install path becomes `go install github.com/lolay/ghx/cmd/ghx@latest`, and the side-by-side paragraph goes (one line may say the Python version lives on the `old/python` branch).
- **Accept when:** `grep -rn 'garyrudolph\|ghx-go' --include='*.go' --include='*.md' --include=go.mod .` finds nothing; `go build ./... && go vet ./...` pass; `go run ./cmd/ghx --help` prints `ghx <ORG> <PATH>`.

#### s2 - [fast] Repo hygiene files

- `gofmt -w internal/config/resolve.go` (the only drift).
- `.gitignore`: replace `/ghx-go` and `/ghx` with `/bin/` and `/dist/`; keep `*.test`, `*.out`, `coverage.*`, `/.scratch/` and the Cursor line; add `.DS_Store`.
- `.gitattributes` from triage: `* text=auto`, plus `**/testdata/** text eol=lf` so golden files stay LF on Windows checkouts.
- `.go-version`: the latest stable Go release, looked up now (`curl -s 'https://go.dev/dl/?mode=json' | python3 -c 'import json,sys; print(json.load(sys.stdin)[0]["version"][2:])'`), never from memory. Leave `go.mod`'s `go` line as it is unless the build needs newer.
- **Accept when:** `gofmt -l .` prints nothing; `git check-ignore -q bin/x dist/x .scratch/x` succeeds; `.go-version` holds one `X.Y.Z` line.

#### s3 - [exec] Makefile and Makefile.md to the standard

- A leaf `Makefile` modelled on triage's, in the standard's section order (`##@ Develop`, `##@ GitHub`; no Release or Danger section yet, plan C adds them): `SHELL := bash`, `.DEFAULT_GOAL := help`, the awk `help` with `[a-zA-Z0-9_.-]+`, every target in `.PHONY`, no magic strings in recipes.
- Targets: `help`, `init` (`go mod download`; `INSTALL_PACKAGES=1` also installs golangci-lint), `doctor` (runs `triage` on a new `triage.yaml` that checks `go` against `.go-version`, `git`, `gh`, `golangci-lint`; `MODE=default|release`, release adding `goreleaser`), `build` (`go build -o bin/ghx ./cmd/ghx`; plan C adds version stamping), `lint` (gofmt drift + `go mod tidy -diff` + `go vet` + `golangci-lint run`, hard fail when golangci-lint is missing), `format` (`gofmt -w .` and `go mod tidy`), `test` (`go test -race -cover ./...`), `vuln` (`go tool govulncheck ./...`, with govulncheck added as a `go.mod` tool dependency), `ci` (build, lint, test), `pre-commit` (alias of `ci`), `clean`, and `gh-runs-list`, `gh-runs-watch`, `gh-runs-status` (triage's recipes; three buckets for conclusions).
- `.golangci.yml` from triage (v2 schema), with `local-prefixes: github.com/lolay/ghx`; `GOLANGCI_LINT_VERSION` in the Makefile pinned to the latest v2 release, looked up now. Fix what it reports, or suppress inline with a one-line reason (the `exec.Command("git", …)` sites are gosec G204 by design). If it reports more than about 25 findings, keep `default: standard` plus `misspell` and `errorlint`, and list the linters left out, with a reason, in the file's header comment.
- `Makefile.md`: target overview (mermaid), target tables per section, `make init` behaviour, a "CI map" placeholder line for plan B.
- **Accept when:** bare `make` prints the grouped help; `make ci` and `make vuln` pass on the Mac; `make doctor` runs (a missing tool is a reported result, not a Makefile error); `make clean` leaves `git status` clean; `checkmake Makefile`, if installed, reports nothing new.

--- WAVE 2 [deep] ---

#### s4 - [deep] Test seams and the first unit tests

- Decide the seams the pure packages need, with the smallest change to production code: where `config.Load` finds the home directory (tests set `HOME` and `USERPROFILE` with `t.Setenv`, or the function takes the directory), and how `GITHUB_TOKEN` is isolated. Write the decision as a short `specs/testing.md`: test layout, testify (`require` / `assert`), table-driven style, what needs `git` on `PATH`, and the coverage exemptions with a reason each (`cmd/ghx`; anything plan B covers).
- Tests, colocated, table-driven, with testify: `config` (TOML load and key normalization, legacy `.gh-export.*` migration including the both-exist case, every `Resolve*` precedence order, `ResolveToken`'s flag > env > local > home), `ghapi.FilterRepos` (types, include and exclude, a bad regex), `manifest` (round trip, legacy migration, a missing file, and a golden `.ghx.json` under `testdata/` that pins the Python-compatible format), `ui.ObfuscateToken` and the summary output against a buffer.
- Fix any real bug the tests find, in its own commit with the step ID, and say so in the step's artifact.
- **Accept when:** `make test` passes with `-race`; `config`, `manifest` and `ghapi` (filter) are at or above 80% statement coverage per package, or the gap is listed in `specs/testing.md` with a reason; no test touches the network or the real home directory.

## m2 - Docs

--- WAVE 3 [exec] ---

#### s1 - [exec] README and CHANGELOG

- README restructured on triage's: what ghx is, quick start, install (from source with `make build`, or `go install github.com/lolay/ghx/cmd/ghx@latest`; one line that Homebrew and Scoop come with the first release), authentication, configuration (config paths on each OS: `~/.ghx.toml`, `%USERPROFILE%\.ghx.toml` on Windows), usage, how it works, CLI reference matching `ghx --help` exactly, development (`make help`, `make ci`).
- `CHANGELOG.md` in Keep a Changelog form with `## [Unreleased]`, listing this plan's user-visible changes (binary renamed to `ghx`, module path `github.com/lolay/ghx`).
- **Accept when:** every command in the README runs as written on the Mac (or is a clearly marked placeholder for the release); the CLI reference matches `go run ./cmd/ghx --help`.

#### s2 - [exec] Contributor and agent docs

- `CONTRIBUTING.md` (from triage's: setup, the make loop, `.go-version`, commit style, PR flow), `SECURITY.md` (private reporting via GitHub security advisories; the token handling ghx does), `.github/CODEOWNERS` (`* @GaryRudolph`), and a short `AGENTS.md`: layout, `make ci` as the gate, where tests and specs live, the Go standards, and that `specs/handoffs/` holds plan scratch removed before merge.
- **Accept when:** every relative link in the new docs resolves (`grep -o '](\./[^)]*)'` and check each path); `make ci` passes.

## Review log

## Token log

**Counting header (Claude Code)**

- Line, one per model a chat ran, appended below: `tokens <row> <group-id> (<model>): input ~X / cache read ~R / cache write ~W / output ~Y | ~$C API-equiv`. `<row>` is `wave-N`, `review-wave-N`, or `wave-N-fix` and `review-wave-N-fix` for a fix-up wave; `<group-id>` is the wave's group id with hyphens (`m1-s1-s3`); `<model>` is `message.model` without a date suffix (`claude-haiku-4-5-20251001` is `claude-haiku-4-5`). Round counts to two significant figures with `k` or `M`.
- Usage: `~/.claude/projects/<slug>/$CLAUDE_CODE_SESSION_ID.jsonl` (`<slug>` is the working directory with every character but a letter or digit turned into `-`, matched by prefix when long; without the id, the newest `.jsonl` there; `$CLAUDE_CONFIG_DIR` replaces `~/.claude` when set), plus its subagents' `<session-id>/subagents/**/agent-*.jsonl` in the same directory. Sum `message.usage` over assistant lines once per `message.id`, from the line with `stop_reason`: input `input_tokens`, cache read `cache_read_input_tokens`, cache write `cache_creation.ephemeral_5m_input_tokens` (with `ephemeral_1h_input_tokens` too, `cache write ~40k 5m + ~8k 1h`), output `output_tokens`. A call with no `stop_reason` line keeps its input-side counts, takes output as about 1,000, and its line ends `(output est.) session <id>`.
- Rates by `<model>`, $ per Mtok input / cached / output: `claude-opus-5-5` 4.00 / 0.20 / 20.00; `claude-sonnet-5-5` 2.00 / 0.20 / 10.00; `claude-haiku-4-5` 1.00 / 0.10 / 5.00.
- `$C` = (input × in + cache read × cached + 5m write × in × 1.25 + 1h write × in × 2.00 + output × out) / 1M.
- In another harness, or on a model not listed here, count and price per plan-execution.md "Token accounting" and "Model price table" instead.
