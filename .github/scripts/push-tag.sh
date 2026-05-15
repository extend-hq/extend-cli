#!/usr/bin/env bash
# Create an annotated `v*` tag at HEAD and push it to origin.
#
# Inputs (env vars):
#   TAG  required: the tag to create (e.g. v0.4.7)
#
# Identity is fixed to github-actions[bot] so the tag is attributed to
# the bot in GitHub's UI rather than to whichever human happens to
# have signed in to the workflow run. The numeric prefix is the bot
# account's GitHub user id and is what links the noreply email to its
# avatar/account.
#
# If $GITHUB_STEP_SUMMARY is set, a short markdown summary is appended
# so the workflow's UI shows the tagged version and a pointer to the
# downstream release run.
set -euo pipefail

: "${TAG:?TAG is required (e.g. v0.4.7)}"

git config user.name "github-actions[bot]"
git config user.email "41898282+github-actions[bot]@users.noreply.github.com"
git tag -a "$TAG" -m "$TAG"
git push origin "$TAG"
echo "Pushed tag $TAG; release.yml will now build and publish."

if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
  repo="${GITHUB_REPOSITORY:-extend-hq/extend-cli}"
  {
    echo "### Tagged \`$TAG\`"
    echo
    echo "Watch the [release workflow](https://github.com/${repo}/actions/workflows/release.yml) for build and publish progress."
  } >>"$GITHUB_STEP_SUMMARY"
fi
