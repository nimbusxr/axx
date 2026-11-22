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
#
# In the nightly run (AXX_NIGHTLY=true, ci.yml's), the tests are also measured and checked:
#   - they write a profile of the statements they ran, over every package of axx, into
#     $AXX_COVERAGE ($RUNNER_TEMP/coverage unless set), which the run's coverage job adds up
#     (internal/tools/covermerge); a test binary must have been built for it (scripts/mac-tests.sh).
#     They run without the race detector then (-race is left out): with it, counting statements
#     made the model packs' tests take over ten minutes, and every pull request runs them with it;
#   - the packs build their C, C++ and Objective-C under a memory checker, which reports into
#     $AXX_MEMCHECK ($RUNNER_TEMP/memcheck unless set; packs/internal/memcheck).
set -uo pipefail

cover=() covertest=()
if [[ "${AXX_NIGHTLY:-}" == true ]]; then
  temp=${RUNNER_TEMP:-${TMPDIR:-/tmp}}
  temp=${temp//\\//} # a Windows runner's, for bash
  export AXX_COVERAGE=${AXX_COVERAGE:-$temp/coverage} AXX_MEMCHECK=${AXX_MEMCHECK:-$temp/memcheck}
  mkdir -p "$AXX_COVERAGE" "$AXX_MEMCHECK"
  profile="$AXX_COVERAGE/$(date +%s)-$$-$RANDOM.out"
  cover=("-coverpkg=$(sed -n 's/^module //p' go.mod)/..." "-coverprofile=$profile")
  covertest=("-test.coverprofile=$profile")
  args=()
  for arg in "$@"; do
    [[ "$arg" == -race ]] || args+=("$arg")
  done
  set -- ${args[@]+"${args[@]}"}
fi

json=$(mktemp)
trap 'rm -f "$json"' EXIT
format=(--format "${GOTEST_FORMAT:-pkgname-and-test-fails}" --format-hide-empty-pkg --jsonfile "$json")
if [[ "${1:-}" == *.test ]]; then
  dir=$(dirname "$1")
  pkg=$(sed -n 's/^module //p' go.mod)/$dir
  if [[ -n "${GOTEST_TEST2JSON:-}" ]]; then test2json=("$GOTEST_TEST2JSON"); else test2json=(go tool test2json); fi
  (cd "$dir" && gotestsum "${format[@]}" --raw-command -- "${test2json[@]}" -t -p "$pkg" "./$(basename "$1")" -test.v=test2json ${covertest[@]+"${covertest[@]}"} "${@:2}")
else
  gotestsum "${format[@]}" -- ${cover[@]+"${cover[@]}"} "$@"
fi
status=$?
echo "Tests over ${GOTEST_SLOWEST:-5s}:"
gotestsum tool slowest --jsonfile "$json" --threshold "${GOTEST_SLOWEST:-5s}"
exit "$status"
