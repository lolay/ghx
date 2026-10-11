#!/usr/bin/env bash
# Download checksums.txt for a tag's GitHub release, and fail unless the
# release is published and holds every archive the file lists.
#
# Usage: scripts/fetch-release-checksums.sh TAG [dir]
#   TAG - the release tag, vX.Y.Z
#   dir - where checksums.txt is written (default: dist)
#
# Env:
#   GH_TOKEN - a token that can read this repository's releases (the Publish
#              workflow passes github.token with contents: read)
#   GH_REPO  - owner/repo, when the working directory isn't a clone of it
#
# The Publish workflow runs it twice. Before goreleaser, success means the
# release is already complete, so a re-run skips goreleaser rather than
# uploading the assets again. Before the formula and the manifest, it supplies
# the checksums they render from, taken from the published release itself, so
# neither ever names an asset that isn't there.
set -euo pipefail

TAG="${1:?TAG required (vX.Y.Z)}"
DIR="${2:-dist}"

if [[ ! "$TAG" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "TAG must be vMAJOR.MINOR.PATCH, got: $TAG" >&2
  exit 1
fi

# A draft's download URLs don't resolve for anyone without write access, so
# a draft counts as not released.
if ! is_draft="$(gh release view "$TAG" --json isDraft --jq .isDraft)"; then
  echo "no GitHub release for $TAG" >&2
  exit 1
fi
if [[ "$is_draft" != "false" ]]; then
  echo "the GitHub release for $TAG is a draft" >&2
  exit 1
fi

mkdir -p "$DIR"
if ! gh release download "$TAG" --pattern checksums.txt --dir "$DIR" --clobber; then
  echo "the GitHub release for $TAG has no checksums.txt" >&2
  exit 1
fi

assets="$(gh release view "$TAG" --json assets --jq '.assets[].name')"
missing=0
while read -r _ name; do
  name="${name#./}"
  if ! grep -qxF -- "$name" <<<"$assets"; then
    echo "the GitHub release for $TAG lacks $name" >&2
    missing=1
  fi
done <"${DIR}/checksums.txt"
if [[ "$missing" != 0 ]]; then
  exit 1
fi

echo "the GitHub release for $TAG holds every asset in checksums.txt"
