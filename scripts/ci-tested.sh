#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Whether a passing CI run of the pull request the merge queue tests (HEAD, its group) tested the
# very same tree: main has not moved since that run, so the group needs no run of its own. Each
# pull request's run keeps the tree it tested (its tested-tree artifact). Exits 0 if one did.
# The queue's branch names the pull request and the commit of main it goes onto
# (gh-readonly-queue/main/pr-<n>-<main's sha>); the pull request's head comes from the API.
#   usage: REPO=owner/name GH_TOKEN=... scripts/ci-tested.sh gh-readonly-queue/main/pr-<n>-<sha>
set -euo pipefail

[[ "${1:-}" =~ /pr-([0-9]+)-[0-9a-f]{40}$ ]] || exit 1
pr=${BASH_REMATCH[1]}
head=$(gh api "repos/$REPO/pulls/$pr" --jq .head.sha)
tree=$(git rev-parse 'HEAD^{tree}')
for run in $(gh api "repos/$REPO/actions/workflows/ci.yml/runs?event=pull_request&head_sha=$head&status=success" --jq '.workflow_runs[].id'); do
  dir=$(mktemp -d)
  if gh run download "$run" -R "$REPO" -n tested-tree -D "$dir" > /dev/null 2>&1 && [[ "$(cat "$dir/tested-tree")" == "$tree" ]]; then
    echo "run $run of pull request #$pr passed on this very tree: the merge queue runs nothing" >&2
    exit 0
  fi
done
echo "no passing run of pull request #$pr ($head) tested this tree: the merge queue runs its jobs" >&2
exit 1
