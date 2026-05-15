#!/usr/bin/env bash
# Create an annotated `v*` tag at HEAD and push it to origin.
#
# Inputs (env vars):
#   TAG          required: the tag to create (e.g. v0.4.7)
#   TAG_AUTHOR   optional: "Name <email>" used as the tag's author.
#                If unset, git's currently-configured user.name /
#                user.email are used (i.e. whoever runs the script).
#                The identity is applied only to this single tag
#                command via `git -c`; the script never modifies the
#                repo's persistent git config.
#
# Outputs:
#   Prints a confirmation line on success.
set -euo pipefail

: "${TAG:?TAG is required (e.g. v0.4.7)}"

git_args=()
if [ -n "${TAG_AUTHOR:-}" ]; then
  # Split "Name <email>" into name + email so we can hand them to
  # `git -c` independently. The trailing `<email>` chunk is matched
  # greedily so names containing '<' still work.
  name="${TAG_AUTHOR% <*}"
  email="${TAG_AUTHOR##*<}"
  email="${email%>}"
  if [ -z "$name" ] || [ -z "$email" ] || [ "$name" = "$TAG_AUTHOR" ]; then
    echo "TAG_AUTHOR must look like 'Name <email@example.com>'; got '$TAG_AUTHOR'" >&2
    exit 1
  fi
  git_args+=(-c "user.name=$name" -c "user.email=$email")
fi

git "${git_args[@]}" tag -a "$TAG" -m "$TAG"
git push origin "$TAG"
echo "Pushed tag $TAG"
