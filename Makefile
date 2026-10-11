# ghx Makefile
#
# The single source of truth for build / lint / test command strings. Humans,
# local agents, cloud agents, and CI all run the same verbs, so a green
# `make ci` locally means the same checks CI runs. See Makefile.md for the
# narrative reference.
#
# Conventions (shared across our Go CLI repos):
#   - `.DEFAULT_GOAL := help`; bare `make` prints the grouped target list.
#   - Self-documenting: a `## comment` after a target shows up in `make help`;
#     a `##@ Section` line renders as a header.
#   - Recipes are tab-indented (a Make requirement) and run under bash
#     (SHELL below), so they behave the same on macOS and Linux.

SHELL := bash

.DEFAULT_GOAL := help

.PHONY: help init doctor build lint format test vuln ci pre-commit clean \
        gh-runs-list gh-runs-watch gh-runs-status bump snapshot release-check

# Binary coordinates.
BINARY  := ghx
CMD_DIR := ./cmd/ghx
BIN_DIR := bin
DIST_DIR := dist

# Build version, per the versioning standard: `ghx --version` prints
# "ghx <RELEASE>+<SHA> (<BUILD_CODE>)".
#   RELEASE    the last released version, from version.txt (only the release
#              step rewrites it; see `make bump`)
#   SHA        short HEAD commit, plus ".dirty" when `git status --porcelain`
#              reports anything (untracked files included)
#   BUILD_CODE commits reachable from HEAD; a shallow clone undercounts, so
#              a checkout that builds a release needs full history
# Outside a git checkout SHA and BUILD_CODE are empty and the binary prints
# "ghx <RELEASE>".
#
# Release builds (goreleaser) take these same three values rather than
# computing their own: `.goreleaser.yaml` stamps `{{ .Env.RELEASE }}`,
# `{{ .Env.SHA }}` and `{{ .Env.BUILD_CODE }}` into BUILDINFO_PKG, and
# goreleaser runs only through Makefile targets whose recipes pass
# `RELEASE="$(RELEASE)" SHA="$(SHA)" BUILD_CODE="$(BUILD_CODE)"` in its
# environment; the Release workflow calls those targets after a full-history
# checkout. goreleaser's own `.Version` is the tag (a `-SNAPSHOT` suffix on
# snapshots, which the standard forbids) and `.ShortCommit` is computed
# separately, so neither is used. One computation means a local build and a
# release build of one commit print the same string.
RELEASE       := $(strip $(shell cat version.txt 2>/dev/null))
GIT_SHA       := $(shell git rev-parse --short HEAD 2>/dev/null)
GIT_DIRTY     := $(shell git status --porcelain 2>/dev/null)
SHA           := $(if $(GIT_SHA),$(GIT_SHA)$(if $(GIT_DIRTY),.dirty))
BUILD_CODE    := $(shell git rev-list --count HEAD 2>/dev/null)
BUILDINFO_PKG := github.com/lolay/ghx/internal/buildinfo
LDFLAGS       := -X $(BUILDINFO_PKG).Version=$(RELEASE) \
                 -X $(BUILDINFO_PKG).Sha=$(SHA) \
                 -X $(BUILDINFO_PKG).BuildCode=$(BUILD_CODE)

# goreleaser; a runner without it installed passes the pinned `go run` form,
# e.g. GORELEASER="go run github.com/goreleaser/goreleaser/v2@v2.18.3".
GORELEASER ?= goreleaser

# LEVEL picks the bump for `make bump` (patch | minor | major).
LEVEL ?=

# MODE selects the triage profile for `make doctor` (default | release).
MODE ?= default

# Pinned golangci-lint version; the CI install step must use the same value.
GOLANGCI_LINT_VERSION ?= v2.14.0
GOLANGCI_LINT_INSTALL_URL := https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh

# Maximum recent runs to fetch for gh-runs-list / gh-runs-watch / gh-runs-status.
GH_LIMIT ?= 50

##@ Develop

help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z0-9_.-]+:.*?##/ {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2} /^##@/ {printf "\n\033[1m%s\033[0m\n", substr($$0,5)}' $(MAKEFILE_LIST)

init: ## Download Go modules (INSTALL_PACKAGES=1 also installs golangci-lint)
	go mod download
	@if [ "$(INSTALL_PACKAGES)" = "1" ] && ! command -v golangci-lint >/dev/null 2>&1; then \
	  echo "Installing golangci-lint $(GOLANGCI_LINT_VERSION)..."; \
	  curl -sSfL $(GOLANGCI_LINT_INSTALL_URL) \
	    | sh -s -- -b "$$(go env GOPATH)/bin" $(GOLANGCI_LINT_VERSION); \
	fi

doctor: ## Check the dev environment with triage (MODE=default|release)
	@if ! command -v triage >/dev/null 2>&1; then \
	  echo "triage not found (brew install lolay/tap/triage)" >&2; exit 1; \
	fi
	@if [ "$(MODE)" = "default" ]; then triage; else triage --profile "$(MODE)"; fi

build: ## Compile bin/ghx, stamped with version.txt, the HEAD sha and the build code
	go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) $(CMD_DIR)

lint: ## Static checks: gofmt and go.mod drift, go vet, golangci-lint (required; run make init)
	@set -o pipefail; \
	drift="$$(gofmt -l .)"; \
	if [ -n "$$drift" ]; then \
	  echo "gofmt drift detected; run 'make format':"; echo "$$drift"; exit 1; \
	fi
	go mod tidy -diff
	go vet ./...
	@if ! command -v golangci-lint >/dev/null 2>&1; then \
	  echo "golangci-lint not found; run 'make init INSTALL_PACKAGES=1' to install" >&2; exit 1; \
	fi
	golangci-lint run

format: ## Auto-fix formatting (gofmt, golangci-lint fmt when installed) and tidy go.mod
	gofmt -w .
	@if command -v golangci-lint >/dev/null 2>&1; then golangci-lint fmt; fi
	go mod tidy

test: ## Run all tests (race detector on; prints per-package coverage)
	go test -race -cover ./...

vuln: ## Scan dependencies for known vulnerabilities (govulncheck)
	go tool govulncheck ./...

ci: build lint test ## Full pre-push gate: build, lint, test (what CI runs)

pre-commit: ci ## Local gate before committing or pushing (alias of ci)

clean: ## Remove build artifacts (bin/, dist/)
	rm -rf $(BIN_DIR) $(DIST_DIR)

##@ GitHub

gh-runs-list: ## List this repo's in-flight Actions runs (status != completed)
	@out=$$(gh run list --limit $(GH_LIMIT) \
	  --json status,workflowName,headBranch,event,url \
	  --jq '.[] | select(.status != "completed") | "  \(.status)\t\(.workflowName)\t\(.headBranch)\t\(.event)\t\(.url)"' 2>&1) \
	  || { printf '  \033[33m⚠\033[0m gh run list failed (auth? run `gh auth login`)\n'; exit 0; }; \
	if [ -z "$$out" ]; then printf '  \033[2mno active runs\033[0m\n'; \
	else printf '%s\n' "$$out" | column -t -s "$$(printf '\t')"; fi

gh-runs-watch: ## Watch this repo's in-flight Actions runs until each completes
	@ids=$$(gh run list --limit $(GH_LIMIT) --json status,databaseId \
	  --jq '.[] | select(.status != "completed") | .databaseId' 2>/dev/null); \
	if [ -z "$$ids" ]; then printf '  \033[2mno active runs\033[0m\n'; exit 0; fi; \
	for id in $$ids; do \
	  gh run watch "$$id" --compact || printf '  \033[33m⚠\033[0m watch failed for run %s\n' "$$id"; \
	done

gh-runs-status: ## Show pass/skip/fail of the last completed run per workflow
	@out=$$(gh run list --limit $(GH_LIMIT) \
	  --json conclusion,workflowName,headBranch,url,status,updatedAt \
	  --jq '[.[] | select(.status == "completed")] | group_by(.workflowName) | map(sort_by(.updatedAt) | last) | sort_by(.updatedAt) | .[] | (now - (.updatedAt | fromdateiso8601)) as $$age | "\(.conclusion)\t\(.workflowName)\t\(.headBranch)\t\(.url)\t\($$age | floor)"' \
	  2>&1) \
	  || { printf '  \033[33m⚠\033[0m gh run list failed (auth? run `gh auth login`)\n'; exit 0; }; \
	if [ -z "$$out" ]; then printf '  \033[2mno completed runs\033[0m\n'; exit 0; fi; \
	esc=$$(printf '\033'); \
	printf '%s\n' "$$out" | while IFS=$$'\t' read -r conclusion name branch url age_secs; do \
	  if [ "$$conclusion" = "success" ]; then mark="ok"; \
	  elif [ "$$conclusion" = "skipped" ] || [ "$$conclusion" = "neutral" ]; then mark="skip"; \
	  else mark="fail"; fi; \
	  if [ "$$age_secs" -lt 60 ]; then age="$${age_secs}s"; \
	  elif [ "$$age_secs" -lt 3600 ]; then age="$$((age_secs / 60))m"; \
	  elif [ "$$age_secs" -lt 86400 ]; then age="$$((age_secs / 3600))h"; \
	  else age="$$((age_secs / 86400))d"; fi; \
	  printf '%s\t%s\t%s\t%s\t%s\n' "$$mark" "$$name" "$$branch" "$$age" "$$url"; \
	done | column -t -s "$$(printf '\t')" \
	| sed -e "s/^ok  /$${esc}[32m✓$${esc}[0m   /" \
	      -e "s/^skip/$${esc}[2m-$${esc}[0m   /" \
	      -e "s/^fail/$${esc}[31m✗$${esc}[0m   /" \
	      -e 's/^/  /'

##@ Release

bump: ## Bump version.txt and move CHANGELOG's Unreleased entries (LEVEL=patch|minor|major; DRY_RUN=1 previews)
	@case "$(LEVEL)" in \
	  patch|minor|major) ;; \
	  *) echo "LEVEL must be patch, minor or major (make bump LEVEL=patch)" >&2; exit 1 ;; \
	esac
	python3 scripts/bump_version.py $(LEVEL)$(if $(filter 1,$(DRY_RUN)), --dry-run)

snapshot: ## Build every release archive and checksums.txt into dist/ without publishing (goreleaser snapshot)
	RELEASE="$(RELEASE)" SHA="$(SHA)" BUILD_CODE="$(BUILD_CODE)" $(GORELEASER) release --snapshot --clean

release-check: ## Validate .goreleaser.yaml (goreleaser check)
	$(GORELEASER) check
