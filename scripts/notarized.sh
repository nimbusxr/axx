#!/usr/bin/env bash
# Waits for Apple to notarize what a release submitted (ADR 0016): every submission made since
# <since> (an ISO 8601 time), the launcher's axx.app and GoReleaser's macOS binaries.
#
#   scripts/notarized.sh <since> [minutes]
#
# It waits at most [minutes] (8), as a CI job must end within 10. It fails when Apple refused one,
# with Apple's log of it; one Apple has not finished by then is a warning, not a failure: Apple
# notarizes it all the same, and `xcrun notarytool info <id>` says when. The API key is
# APPLE_API_KEY_PATH (its .p8 file), APPLE_API_KEY_ID and APPLE_API_ISSUER_ID.
set -euo pipefail

since=${1:?usage: notarized.sh <since> [minutes]}
minutes=${2:-8}
key=(--key "${APPLE_API_KEY_PATH:?}" --key-id "${APPLE_API_KEY_ID:?}" --issuer "${APPLE_API_ISSUER_ID:?}")

# submissions prints each submission since $since: its id, its status and its name.
submissions() {
  xcrun notarytool history "${key[@]}" --output-format json |
    python3 -c '
import json, sys
since = sys.argv[1]
for s in json.load(sys.stdin).get("history", []):
    if s["createdDate"] >= since:
        print(s["id"], s["status"].replace(" ", "_"), s["name"], sep="\t")
' "$since"
}

deadline=$((SECONDS + minutes * 60))
while true; do
  list=$(submissions)
  if [[ -z $list ]]; then
    echo "no submission to Apple's notary service since $since" >&2
    exit 1
  fi
  refused=$(awk -F'\t' '$2 != "Accepted" && $2 != "In_Progress"' <<<"$list")
  if [[ -n $refused ]]; then
    echo "Apple refused to notarize:" >&2
    echo "$refused" >&2
    while IFS=$'\t' read -r id _ _; do
      xcrun notarytool log "$id" "${key[@]}" >&2 || true
    done <<<"$refused"
    exit 1
  fi
  waiting=$(awk -F'\t' '$2 == "In_Progress"' <<<"$list")
  if [[ -z $waiting ]]; then
    echo "Apple notarized all $(wc -l <<<"$list" | tr -d ' ') of this release's submissions:"
    echo "$list"
    exit 0
  fi
  if ((SECONDS >= deadline)); then
    echo "::warning::Apple is still notarizing after ${minutes} minutes; xcrun notarytool info <id> says when it is done:"
    echo "$waiting"
    exit 0
  fi
  sleep 20
done
