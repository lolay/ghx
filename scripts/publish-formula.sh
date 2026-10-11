#!/usr/bin/env bash
# Render Formula/ghx.rb from a goreleaser dist/ and commit it to lolay/homebrew-tap.
#
# Usage: scripts/publish-formula.sh VERSION [dist-dir]
#   VERSION  - semver without the v prefix (e.g. 0.1.0)
#   dist-dir - goreleaser output (default: dist)
#
# Env:
#   HOMEBREW_TAP_TOKEN - PAT with Contents: write on lolay/homebrew-tap (required unless DRY_RUN=1)
#   DRY_RUN=1          - render the formula to stdout only; no git clone or push
set -euo pipefail

VERSION="${1:?VERSION required (no v prefix)}"
DIST="${2:-dist}"
DRY_RUN="${DRY_RUN:-0}"

# VERSION goes into a sed replacement and a git commit message, so refuse
# anything but semver, with build metadata allowed for a snapshot.
if [[ ! "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+(\+[0-9A-Za-z.]+)?$ ]]; then
  echo "VERSION must be semver without a v prefix, got: $VERSION" >&2
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TEMPLATE="${SCRIPT_DIR}/ghx.rb.tmpl"
CHECKSUMS="${DIST}/checksums.txt"

if [[ ! -f "$CHECKSUMS" ]]; then
  echo "checksums file not found: $CHECKSUMS" >&2
  exit 1
fi

sha_for() {
  local archive="$1"
  local hash
  hash="$(awk -v f="$archive" '$2 == f || $2 == "./" f { print $1; exit }' "$CHECKSUMS")"
  if [[ -z "$hash" ]]; then
    echo "sha256 not found for $archive in $CHECKSUMS" >&2
    exit 1
  fi
  printf '%s' "$hash"
}

SHA_DARWIN_ARM64="$(sha_for "ghx_${VERSION}_darwin_arm64.tar.gz")"
SHA_DARWIN_AMD64="$(sha_for "ghx_${VERSION}_darwin_amd64.tar.gz")"
SHA_LINUX_ARM64="$(sha_for "ghx_${VERSION}_linux_arm64.tar.gz")"
SHA_LINUX_AMD64="$(sha_for "ghx_${VERSION}_linux_amd64.tar.gz")"

render_formula() {
  sed \
    -e "s/__VERSION__/${VERSION}/g" \
    -e "s/__SHA_DARWIN_ARM64__/${SHA_DARWIN_ARM64}/g" \
    -e "s/__SHA_DARWIN_AMD64__/${SHA_DARWIN_AMD64}/g" \
    -e "s/__SHA_LINUX_ARM64__/${SHA_LINUX_ARM64}/g" \
    -e "s/__SHA_LINUX_AMD64__/${SHA_LINUX_AMD64}/g" \
    "$TEMPLATE"
}

if [[ "$DRY_RUN" == "1" ]]; then
  render_formula
  exit 0
fi

if [[ -z "${HOMEBREW_TAP_TOKEN:-}" ]]; then
  echo "HOMEBREW_TAP_TOKEN is required (or set DRY_RUN=1)" >&2
  exit 1
fi

# Hand git the token as a header through its environment rather than in the
# clone URL, so it lands in no command line and not in the clone's .git/config.
AUTH="$(printf 'x-access-token:%s' "$HOMEBREW_TAP_TOKEN" | base64 | tr -d '\n')"
# Actions masks the raw secret but not this encoding of it; mask it too, as
# actions/checkout does for its own header.
if [[ -n "${GITHUB_ACTIONS:-}" ]]; then echo "::add-mask::${AUTH}"; fi
export GIT_CONFIG_COUNT=1
export GIT_CONFIG_KEY_0="http.https://github.com/.extraheader"
export GIT_CONFIG_VALUE_0="Authorization: Basic ${AUTH}"

TAP_DIR="$(mktemp -d)"
NEW_FORMULA="$(mktemp)"
trap 'rm -rf "$TAP_DIR" "$NEW_FORMULA"' EXIT

git clone --depth 1 "https://github.com/lolay/homebrew-tap.git" "$TAP_DIR"

FORMULA_PATH="${TAP_DIR}/Formula/ghx.rb"
mkdir -p "$(dirname "$FORMULA_PATH")"

render_formula >"$NEW_FORMULA"

if [[ -f "$FORMULA_PATH" ]] && cmp -s "$NEW_FORMULA" "$FORMULA_PATH"; then
  echo "Formula already at ghx ${VERSION}; nothing to commit"
  exit 0
fi

cp "$NEW_FORMULA" "$FORMULA_PATH"

git -C "$TAP_DIR" config user.name "ghx-release-bot"
git -C "$TAP_DIR" config user.email "ghx-release-bot@lolay.com"
git -C "$TAP_DIR" add Formula/ghx.rb
git -C "$TAP_DIR" commit -m "ghx ${VERSION}"
git -C "$TAP_DIR" push origin HEAD

echo "Published Formula/ghx.rb for ghx ${VERSION}"
