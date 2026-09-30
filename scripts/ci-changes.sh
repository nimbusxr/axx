#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Which CI jobs a pull request needs, from the files it changes, one per line on stdin. Prints
# name=value lines for $GITHUB_OUTPUT: go, integration, web, mobile, examples, acceptance and workflows
# (true or false), and codeql, the matrix of the languages CodeQL analyzes.
#   usage: git diff --name-only BASE...HEAD | scripts/ci-changes.sh
#          scripts/ci-changes.sh --all   # every job: pushes to main, the merge queue
# A change to CI itself runs every job too.
set -euo pipefail

go=false integration=false web=false mobile=false examples=false acceptance=false workflows=false
lang_go=false lang_java=false lang_js=false lang_actions=false
all=false
if [[ "${1:-}" == --all ]]; then
  all=true
fi

while ! $all && IFS= read -r f; do
  [[ -n "$f" ]] || continue
  case "$f" in
    .github/workflows/ci.yml | .github/workflows/codeql.yml | .github/actions/* | scripts/ci-changes.sh)
      all=true
      continue
      ;;
  esac
  case "$f" in
    .github/*) workflows=true lang_actions=true ;;
  esac
  # The main Go module: its code, what its tests read, and its build. The example apps are
  # modules of their own.
  case "$f" in
    examples/*/app/*) ;;
    cmd/* | core/* | internal/* | packs/* | plugins/* | testdata/* | build/* | scripts/* | \
      examples/parcels/acceptance/* | go.mod | go.sum | mise.toml | .golangci.yml | .goreleaser.yaml | *.go)
      go=true
      case "$f" in
        packs/web/*) web=true ;;
        packs/mobile/*) mobile=true ;;
        core/* | internal/* | go.mod | go.sum | mise.toml) web=true mobile=true integration=true ;;
        # Read by the unit tests, or only built: nothing the integration tests use.
        plugins/* | examples/* | scripts/* | build/* | .golangci.yml | .goreleaser.yaml) ;;
        *) integration=true ;;
      esac
      ;;
  esac
  case "$f" in
    examples/*/app/* | mise.toml) examples=true ;;
  esac
  # The parcels example's Android scenarios run on an emulator, against the whole example.
  case "$f" in
    examples/parcels/* | extensions/wiremock-openapi/*) mobile=true ;;
  esac
  # The examples' acceptance suites: axx, the examples, and the WireMock extension the parcels
  # example builds its mocks from.
  case "$f" in
    cmd/* | core/* | internal/* | packs/* | go.mod | go.sum | mise.toml | examples/* | extensions/wiremock-openapi/*)
      acceptance=true ;;
  esac
  case "$f" in
    *.go | go.mod | go.sum | */go.mod | */go.sum) lang_go=true ;;
    *.java | *.kt | *.kts | *.gradle | *gradle.properties | *pom.xml) lang_java=true ;;
    *.js | *.mjs | *.cjs | *.ts | *.tsx | *.astro | *package.json | *package-lock.json) lang_js=true ;;
  esac
done

if $all; then
  go=true integration=true web=true mobile=true examples=true acceptance=true workflows=true
  lang_go=true lang_java=true lang_js=true lang_actions=true
fi

langs=""
add() { langs="${langs:+$langs,}{\"language\":\"$1\",\"build-mode\":\"$2\"}"; }
if $lang_go; then add go autobuild; fi
if $lang_js; then add javascript-typescript none; fi
if $lang_java; then add java-kotlin none; fi
if $lang_actions; then add actions none; fi

echo "go=$go"
echo "integration=$integration"
echo "web=$web"
echo "mobile=$mobile"
echo "examples=$examples"
echo "acceptance=$acceptance"
echo "workflows=$workflows"
echo "codeql={\"include\":[$langs]}"
