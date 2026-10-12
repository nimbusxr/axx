#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Whether a passing CI run of the pull request the merge queue tests (HEAD, its group) tested the
# very same tree: main has not moved since that run, so the group needs no run of its own. Each
# pull request's run keeps the tree it tested (its tested-tree artifact). Exits 0 if one did.
#   usage: REPO=owner/name GH_TOKEN=... scripts/ci-tested.sh gh-readonly-queue/main/pr-<n>-<head sha>
set -euo pipefail

[[ "${1:-}" =~ /pr-[0-9]+-([0-9a-f]{40})$ ]] || exit 1
head=${BASH_REMATCH[1]}
tree=$(git rev-parse 'HEAD^{tree}')
for run in $(gh api "repos/$REPO/actions/workflows/ci.yml/runs?event=pull_request&head_sha=$head&status=success" --jq '.workflow_runs[].id'); do
  dir=$(mktemp -d)
  if gh run download "$run" -R "$REPO" -n tested-tree -D "$dir" > /dev/null 2>&1 && [[ "$(cat "$dir/tested-tree")" == "$tree" ]]; then
    echo "run $run of the pull request passed on this very tree: the merge queue runs nothing" >&2
    exit 0
  fi
done
echo "no passing run of the pull request tested this tree: the merge queue runs its jobs" >&2
exit 1
