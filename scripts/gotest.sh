#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# go test as CI runs it, through gotestsum: a line for each package, the whole output of each test
# that fails, and at the end the tests that took longest (those over $GOTEST_SLOWEST, 5s unless
# set), so a slow job shows where its time goes. The arguments are go test's; the exit status is
# go test's.
set -uo pipefail

json=$(mktemp)
trap 'rm -f "$json"' EXIT
gotestsum --format pkgname-and-test-fails --format-hide-empty-pkg --jsonfile "$json" -- "$@"
status=$?
echo "Tests over ${GOTEST_SLOWEST:-5s}:"
gotestsum tool slowest --jsonfile "$json" --threshold "${GOTEST_SLOWEST:-5s}"
exit "$status"
