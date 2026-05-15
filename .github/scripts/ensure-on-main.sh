#!/usr/bin/env bash
# Refuse to proceed unless GITHUB_REF points at refs/heads/main.
#
# workflow_dispatch's "Use workflow from" picker lets the operator
# pick any branch in the GitHub UI; this script enforces the
# "tags only come from main" invariant. Pair with workflows that
# mutate published state (releases, npm publishes, etc.) so a stray
# feature-branch dispatch can't ship.
set -euo pipefail

: "${GITHUB_REF:?GITHUB_REF is required (e.g. refs/heads/main)}"

if [ "$GITHUB_REF" != "refs/heads/main" ]; then
  echo "::error::tag-release must be dispatched against main; got $GITHUB_REF" >&2
  exit 1
fi
