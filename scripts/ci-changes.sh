#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Which CI jobs a pull request needs, from the files it changes, one per line on stdin. Prints
# name=value lines for $GITHUB_OUTPUT: go, integration, web, mobile, desktop, models, examples, acceptance and workflows
# (true or false), and codeql, the matrix of the languages CodeQL analyzes.
#   usage: git diff --name-only BASE...HEAD | scripts/ci-changes.sh
#          scripts/ci-changes.sh --all    # every job: pushes to main
#          scripts/ci-changes.sh --none   # no job: the merge queue, on a tree a run passed
# A change to CI itself runs every job too.
set -euo pipefail

go=false integration=false web=false mobile=false desktop=false models=false examples=false acceptance=false workflows=false
lang_go=false lang_java=false lang_js=false lang_actions=false
all=false none=false
case "${1:-}" in
  --all) all=true ;;
  --none) none=true ;;
esac

while ! $all && ! $none && IFS= read -r f; do
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
        packs/web/* | packs/models/web/*) web=true ;;
        packs/mobile/* | packs/models/ios/* | packs/models/android/*) mobile=true ;;
        packs/models/internal/apple/*) mobile=true desktop=true ;;
        packs/models/macos/*) desktop=true ;;
        packs/models/core/*) integration=true models=true web=true mobile=true ;;
        packs/files/*) integration=true web=true mobile=true models=true ;;
        packs/desktop/*) desktop=true integration=true ;;
        packs/app/*) desktop=true integration=true web=true mobile=true ;;
        core/* | internal/* | go.mod | go.sum | mise.toml) web=true mobile=true desktop=true integration=true ;;
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
  # The depot desk's feature runs against every build of the desk.
  case "$f" in
    examples/parcels/depot-desk/*) desktop=true ;;
  esac
  # Core ML stubs and the Apple platform adapters (iOS's, including their real courier app, and
  # the Mac's).
  case "$f" in
    packs/models/* | packs/mobile/ios/* | packs/mobile/core/* | packs/mobile/internal/* | packs/app/core/* | \
      packs/desktop/core/* | packs/desktop/macos/* | \
      examples/parcels/courier/ios/* | examples/parcels/acceptance/models/delivery-note-ios/* | \
      examples/parcels/acceptance/features/courier-app/models/delivery-note.axxmodel | \
      examples/parcels/acceptance/models/dictated-note-ios/* | \
      examples/parcels/acceptance/features/courier-app/models/dictated-note-ios.axxmodel | \
      examples/parcels/acceptance/features/courier-app/recordings/* | \
      core/* | internal/* | go.mod | go.sum | mise.toml | .mise.toml) models=true ;;
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
  go=true integration=true web=true mobile=true desktop=true models=true examples=true acceptance=true workflows=true
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
echo "desktop=$desktop"
echo "models=$models"
echo "examples=$examples"
echo "acceptance=$acceptance"
echo "workflows=$workflows"
echo "codeql={\"include\":[$langs]}"
