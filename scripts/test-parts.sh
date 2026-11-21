#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# The packages of each part of CI's unit tests (the test job), one to a line: the models packs',
# the mobile and desktop packs' (the Android pack's tests run a second time through the axx
# host, 3 minutes in all on Windows), the other packs', internal/cli's, which alone takes as
# long as all the rest, and the rest. A package is in one part by where it is, so a new one
# needs no line here.
#   usage: scripts/test-parts.sh <packs-models|packs-devices|packs|cli|rest|all>
#          scripts/test-parts.sh --check   # every package is in one part, and in one only
set -euo pipefail

parts=(packs-models packs-devices packs cli rest)

part() {
  case "$1" in
    packs-models) go list ./packs/models/... ;;
    packs-devices) go list ./packs/mobile/... ./packs/desktop/... ;;
    packs) go list ./packs/... | grep -v -E '/packs/(models|mobile|desktop)(/|$)' ;;
    cli) go list ./internal/cli/... ;;
    rest) go list ./... | grep -v -e '/packs/' -e '/internal/cli$' -e '/internal/cli/' ;;
    all) go list ./... ;;
    *)
      echo "no part $1: one of ${parts[*]}, or all" >&2
      return 2
      ;;
  esac
}

if [ "${1:-}" != --check ]; then
  part "${1:?usage: scripts/test-parts.sh <${parts[*]} or all>, or --check}"
  exit
fi

all=$(part all | sort)
parted=$(for p in "${parts[@]}"; do part "$p"; done | sort)
if [ "$all" != "$parted" ]; then
  echo "the parts' packages are not every package once (<: in no part, >: in two):" >&2
  diff <(echo "$all") <(echo "$parted") >&2 || true
  exit 1
fi
for p in "${parts[@]}"; do echo "$p: $(part "$p" | wc -l | tr -d ' ') packages"; done
