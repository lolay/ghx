#!/usr/bin/env bash
# Render bucket/ghx.json from a goreleaser dist/ and commit it to lolay/scoop-bucket.
#
# Usage: scripts/publish-scoop.sh VERSION [dist-dir]
#   VERSION  - semver without the v prefix (e.g. 0.1.0)
#   dist-dir - goreleaser output (default: dist)
#
# Env:
#   SCOOP_BUCKET_TOKEN - PAT with Contents: write on lolay/scoop-bucket (required unless DRY_RUN=1)
#   DRY_RUN=1          - render the manifest to stdout only; no git clone or push
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
TEMPLATE="${SCRIPT_DIR}/ghx.json.tmpl"
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

SHA_WINDOWS_AMD64="$(sha_for "ghx_${VERSION}_windows_amd64.zip")"
SHA_WINDOWS_ARM64="$(sha_for "ghx_${VERSION}_windows_arm64.zip")"

render_manifest() {
  sed \
    -e "s/__VERSION__/${VERSION}/g" \
    -e "s/__SHA_WINDOWS_AMD64__/${SHA_WINDOWS_AMD64}/g" \
    -e "s/__SHA_WINDOWS_ARM64__/${SHA_WINDOWS_ARM64}/g" \
    "$TEMPLATE"
}

if [[ "$DRY_RUN" == "1" ]]; then
  render_manifest
  exit 0
fi

if [[ -z "${SCOOP_BUCKET_TOKEN:-}" ]]; then
  echo "SCOOP_BUCKET_TOKEN is required (or set DRY_RUN=1)" >&2
  exit 1
fi

# Hand git the token as a header through its environment rather than in the
# clone URL, so it lands in no command line and not in the clone's .git/config.
AUTH="$(printf 'x-access-token:%s' "$SCOOP_BUCKET_TOKEN" | base64 | tr -d '\n')"
# Actions masks the raw secret but not this encoding of it; mask it too, as
# actions/checkout does for its own header.
if [[ -n "${GITHUB_ACTIONS:-}" ]]; then echo "::add-mask::${AUTH}"; fi
export GIT_CONFIG_COUNT=1
export GIT_CONFIG_KEY_0="http.https://github.com/.extraheader"
export GIT_CONFIG_VALUE_0="Authorization: Basic ${AUTH}"

BUCKET_DIR="$(mktemp -d)"
NEW_MANIFEST="$(mktemp)"
trap 'rm -rf "$BUCKET_DIR" "$NEW_MANIFEST"' EXIT

git clone --depth 1 "https://github.com/lolay/scoop-bucket.git" "$BUCKET_DIR"

MANIFEST_PATH="${BUCKET_DIR}/bucket/ghx.json"
mkdir -p "$(dirname "$MANIFEST_PATH")"

render_manifest >"$NEW_MANIFEST"

if [[ -f "$MANIFEST_PATH" ]] && cmp -s "$NEW_MANIFEST" "$MANIFEST_PATH"; then
  echo "Manifest already at ghx ${VERSION}; nothing to commit"
  exit 0
fi

cp "$NEW_MANIFEST" "$MANIFEST_PATH"

git -C "$BUCKET_DIR" config user.name "ghx-release-bot"
git -C "$BUCKET_DIR" config user.email "ghx-release-bot@lolay.com"
git -C "$BUCKET_DIR" add bucket/ghx.json
git -C "$BUCKET_DIR" commit -m "ghx ${VERSION}"
git -C "$BUCKET_DIR" push origin HEAD

echo "Published bucket/ghx.json for ghx ${VERSION}"
