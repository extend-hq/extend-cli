#!/usr/bin/env bash
# Exit non-zero unless HEAD is on main.
set -euo pipefail

current=$(git rev-parse --abbrev-ref HEAD)
if [ "$current" != "main" ]; then
  echo "must be on main, got '$current'" >&2
  exit 1
fi
