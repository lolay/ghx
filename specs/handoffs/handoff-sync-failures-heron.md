# Handoff: sync-failures-heron

Plan: `specs/handoffs/plan-sync-failures-heron.md` (personal-plan-orchestrate run).

- **Branch:** `claude/ecstatic-archimedes-2dxtun` (assigned by the cloud session; task branch).
- **Mode:** unattended, confirmed 2026-10-13 in session 8e4630de-74dd-5cb4-9a70-cd78dd6ff4a2 (harness=claude-code runner=cloud).
- **Done:** nothing yet. Kickoff answered; wave 1 launching.
- **Next unit:** wave 1 [exec] ghx m1 s1 (git probes and network-command plumbing).
- **Spend:** $0 of ~$10 expected (guard: $50).
- **Resume:** paste the plan's Kickoff prompt into a new session on this branch.

## Pending question (verbatim)

None.

## Review log verdicts

None yet.

## Fix-ups, retries and checkpoints

None yet.

## Decisions so far

None yet.

## Environment notes

- env: The container's `/usr/local/bin/golangci-lint` was 2.5.0, built with go1.25, and refused the go 1.26.2 module (`make lint` failed at baseline). The parent built the pinned v2.14.0 from source (`go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0`; the release download got HTTP 403 through the proxy) and put it at `/usr/local/bin/golangci-lint` (the old binary is `golangci-lint-2.5.0`). A new container needs this again.
- env: The installed Go 1.26.2 lacked the `covdata` tool (`go: no such tool "covdata"` under `make test`). The parent ran `go install cmd/covdata`. A new container needs this again.
- env: `.go-version` pins 1.27.2 while the container has 1.26.2 (`make doctor` flags it). `go.mod`'s minimum is 1.26.2, so `make ci` passes on 1.26.2 with the two fixes above.
- env: `make ci` passes at baseline `d984df8`.
