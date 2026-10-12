#!/usr/bin/env bash
#
# Computes the next version from the repository's existing tags, so releases
# number themselves without anyone editing a file.
#
#   .github/scripts/next-version.sh [patch|minor|major]
#
# Prints the bare version (no leading "v") on stdout; progress goes to stderr so
# it can be captured with $(...) without the log lines leaking into the value.
#
# Only stable vX.Y.Z tags count. Anything else -- prereleases, ad-hoc tags, a
# stray v2 -- is ignored, so a temporary tag can never drag the numbering
# sideways. With no matching tag at all the series starts at 0.1.0: the project
# has not reached 1.0, and a patch bump on top of nothing would be misleading.

set -euo pipefail

bump="${1:-patch}"
case "$bump" in
  patch | minor | major) ;;
  *)
    echo "next-version: bump must be patch, minor or major (got '$bump')" >&2
    exit 2
    ;;
esac

# --sort=-v:refname orders by version, not by date, so v0.10.0 correctly ranks
# above v0.9.0.
latest="$(
  git tag --list 'v[0-9]*' --sort=-v:refname \
    | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' \
    | head -n 1 || true
)"

if [ -z "$latest" ]; then
  echo "next-version: no previous stable tag, starting at 0.1.0" >&2
  echo "0.1.0"
  exit 0
fi

version="${latest#v}"
IFS=. read -r major minor patch <<<"$version"

case "$bump" in
  major)
    major=$((major + 1))
    minor=0
    patch=0
    ;;
  minor)
    minor=$((minor + 1))
    patch=0
    ;;
  patch)
    patch=$((patch + 1))
    ;;
esac

echo "next-version: $latest + $bump -> $major.$minor.$patch" >&2
echo "$major.$minor.$patch"
