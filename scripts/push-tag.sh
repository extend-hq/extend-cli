#!/usr/bin/env bash
# Create an annotated tag and push to origin.
# Env: TAG=vX.Y.Z; TAG_AUTHOR="Name <email>" (optional).
set -euo pipefail

: "${TAG:?TAG is required (e.g. v0.4.7)}"

git_args=()
if [ -n "${TAG_AUTHOR:-}" ]; then
  name="${TAG_AUTHOR% <*}"
  email="${TAG_AUTHOR##*<}"
  email="${email%>}"
  if [ -z "$name" ] || [ -z "$email" ] || [ "$name" = "$TAG_AUTHOR" ]; then
    echo "TAG_AUTHOR must look like 'Name <email>'; got '$TAG_AUTHOR'" >&2
    exit 1
  fi
  git_args+=(-c "user.name=$name" -c "user.email=$email")
fi

git "${git_args[@]}" tag -a "$TAG" -m "$TAG"
git push origin "$TAG"
