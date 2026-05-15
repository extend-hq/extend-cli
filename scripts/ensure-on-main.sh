#!/usr/bin/env bash
# Exit non-zero unless the working tree is checked out on `main`.
#
# Pair with workflows (or local commands) that mutate published state
# so a stray feature-branch dispatch can't ship. Uses `git` directly
# so it works the same way locally and in CI runners — no reliance on
# GITHUB_REF or any other CI-only variable.
set -euo pipefail

current=$(git rev-parse --abbrev-ref HEAD)
if [ "$current" != "main" ]; then
  echo "refusing to run: must be on main, got '$current'" >&2
  exit 1
fi
