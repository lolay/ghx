# Handoff: CLI UX spike (cli-ux-sparkling)

- **Branch:** `claude/magical-ride-7h4rhi` in `lolay/ghx` (assigned by the cloud
  session; cut from `main` at d984df8).
- **Plan:** `specs/handoffs/plan-cli-ux-sparkling.md`.
- **Mode:** unattended (confirmed 2026-10-13; runner=cloud).
- **Done:** nothing yet. Kickoff written.
- **Spend:** ~$0 of ~$4.6 expected.
- **Next unit:** wave 1 [exec], group `ghx s1-s5`, Sonnet high worker + Opus high reviewer.
- **Resume:** paste the plan's Kickoff prompt into a new session on this branch.

## Pending question

None. Kickoff answered "unattended"; wave 1 launching.

## Decisions so far

- fang module path is `github.com/charmbracelet/fang` (v1.0.0); `charm.land/fang`
  serves a go.mod declaring the github path, so don't use the alias.
- Kong was considered and dropped: it changes code shape, not the experience.

## Environment notes

- env: The container has Go 1.26.2; `.go-version` wants 1.27.2. Run every Go and
  make command with `GOTOOLCHAIN=go1.27.2` exported (the toolchain downloads
  through the proxy and works).
- env: `/usr/local/bin/golangci-lint` is v2.5.0 built with go1.25 and fails on
  this module ("Go language version (go1.25) used to build golangci-lint is lower
  than the targeted Go version"). The Makefile pins v2.14.0. Don't install it
  globally. Instead put a shim in the gitignored `.scratch/bin/golangci-lint`
  (`#!/bin/sh` then `exec go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 "$@"`,
  chmod +x) and run the gate as
  `PATH="$PWD/.scratch/bin:$PATH" GOTOOLCHAIN=go1.27.2 make ci`.

## Deviations from plan

None yet.
