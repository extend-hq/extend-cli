#!/usr/bin/env bash
# Compute the next `v*` git tag for tag-release.yml.
#
# Inputs (env vars):
#   BUMP              required: one of patch|minor|major|explicit
#   EXPLICIT_VERSION  required when BUMP=explicit (e.g. v0.3.0)
#
# Behavior:
#   - For semantic bumps, reads the latest `v*` tag from the local
#     repository (sorted by version) and increments the chosen part.
#     If no `v*` tag exists yet the baseline is v0.0.0.
#   - For explicit, performs a minimal shape check on the input.
#   - Refuses to overwrite an existing tag — the downstream release
#     workflow would otherwise either fail on the duplicate or, worse,
#     publish into an existing GitHub Release.
#
# Output:
#   - The next tag is echoed to stdout, e.g. "v0.4.7".
#   - If $GITHUB_OUTPUT is set (i.e. we're in a GitHub Actions step),
#     also writes `tag=<next>` to it so subsequent steps can reference
#     ${{ steps.<id>.outputs.tag }}.
set -euo pipefail

: "${BUMP:?BUMP is required (patch|minor|major|explicit)}"

if [ "$BUMP" = "explicit" ]; then
  : "${EXPLICIT_VERSION:?explicit bump requires EXPLICIT_VERSION (e.g. v0.3.0)}"
  case "$EXPLICIT_VERSION" in
    v[0-9]*) next="$EXPLICIT_VERSION" ;;
    *)
      echo "::error::EXPLICIT_VERSION must look like vX.Y.Z; got '$EXPLICIT_VERSION'" >&2
      exit 1
      ;;
  esac
else
  latest=$(git tag --list 'v*' --sort=-v:refname | head -1)
  if [ -z "$latest" ]; then
    latest="v0.0.0"
  fi
  echo "Latest tag: $latest" >&2
  v=${latest#v}
  IFS=. read -r major minor patch <<<"$v"
  # Defend against truncated tags like "v0" or "v0.2" by defaulting the
  # missing components to zero rather than tripping on an unset arith
  # operand under `set -u`.
  : "${major:=0}" "${minor:=0}" "${patch:=0}"
  case "$BUMP" in
    major) major=$((major + 1)); minor=0; patch=0 ;;
    minor) minor=$((minor + 1)); patch=0 ;;
    patch) patch=$((patch + 1)) ;;
    *)
      echo "::error::unknown BUMP value: $BUMP" >&2
      exit 1
      ;;
  esac
  next="v${major}.${minor}.${patch}"
fi

if git rev-parse "refs/tags/${next}" >/dev/null 2>&1; then
  echo "::error::tag ${next} already exists; refusing to overwrite" >&2
  exit 1
fi

echo "$next"
if [ -n "${GITHUB_OUTPUT:-}" ]; then
  echo "tag=$next" >>"$GITHUB_OUTPUT"
fi
