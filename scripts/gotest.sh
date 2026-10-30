#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# go test as CI runs it, through gotestsum: a line for each package, the whole output of each test
# that fails, and at the end the tests that took longest (those over $GOTEST_SLOWEST, 5s unless
# set), so a slow job shows where its time goes. GOTEST_FORMAT=standard-verbose shows every test's
# output, the packs' log among it, passing tests' too. The arguments are go test's; the exit status
# is go test's.
#
# Or the first argument is a test binary go test -c built into its package's folder (a job whose
# tests another job built: CI's Mac jobs), and the rest its flags (-test.run, -test.timeout): it
# runs in that folder, as go test runs it, through $GOTEST_TEST2JSON (go tool test2json unless set).
set -uo pipefail

json=$(mktemp)
trap 'rm -f "$json"' EXIT
format=(--format "${GOTEST_FORMAT:-pkgname-and-test-fails}" --format-hide-empty-pkg --jsonfile "$json")
if [[ "${1:-}" == *.test ]]; then
  dir=$(dirname "$1")
  pkg=$(sed -n 's/^module //p' go.mod)/$dir
  if [[ -n "${GOTEST_TEST2JSON:-}" ]]; then test2json=("$GOTEST_TEST2JSON"); else test2json=(go tool test2json); fi
  (cd "$dir" && gotestsum "${format[@]}" --raw-command -- "${test2json[@]}" -t -p "$pkg" "./$(basename "$1")" -test.v=test2json "${@:2}")
else
  gotestsum "${format[@]}" -- "$@"
fi
status=$?
echo "Tests over ${GOTEST_SLOWEST:-5s}:"
gotestsum tool slowest --jsonfile "$json" --threshold "${GOTEST_SLOWEST:-5s}"
exit "$status"
