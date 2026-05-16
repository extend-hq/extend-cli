#!/usr/bin/env bash
# Print the next v* tag on stdout.
# Env: BUMP=patch|minor|major|explicit; EXPLICIT_VERSION=vX.Y.Z when BUMP=explicit.
set -euo pipefail

: "${BUMP:?BUMP is required (patch|minor|major|explicit)}"

if [ "$BUMP" = "explicit" ]; then
  : "${EXPLICIT_VERSION:?explicit bump requires EXPLICIT_VERSION (e.g. v0.3.0)}"
  case "$EXPLICIT_VERSION" in
    v[0-9]*) next="$EXPLICIT_VERSION" ;;
    *) echo "EXPLICIT_VERSION must look like vX.Y.Z; got '$EXPLICIT_VERSION'" >&2; exit 1 ;;
  esac
else
  latest=$(git tag --list 'v*' --sort=-v:refname | head -1)
  if [ -z "$latest" ]; then
    latest="v0.0.0"
  fi
  echo "Latest tag: $latest" >&2
  v=${latest#v}
  IFS=. read -r major minor patch <<<"$v"
  : "${major:=0}" "${minor:=0}" "${patch:=0}"
  case "$BUMP" in
    major) major=$((major + 1)); minor=0; patch=0 ;;
    minor) minor=$((minor + 1)); patch=0 ;;
    patch) patch=$((patch + 1)) ;;
    *) echo "unknown BUMP value: $BUMP" >&2; exit 1 ;;
  esac
  next="v${major}.${minor}.${patch}"
fi

if git rev-parse "refs/tags/${next}" >/dev/null 2>&1; then
  echo "tag ${next} already exists" >&2
  exit 1
fi

echo "$next"
