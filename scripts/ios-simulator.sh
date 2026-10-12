#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# The simulator the iOS pack's integration tests keep (packs.mobile-ios.keep), set up: a new
# simulator's first boot sets it up, which takes minutes on a hosted runner. It is set up once for
# each macOS, Xcode and iOS runtime, and kept in the registry as ghcr.io/$OWNER/axx-ios-simulator:
# a run pulls it, and the first run that finds none makes it, and publishes it when PUBLISH is
# true. Its keychain is left out: a keychain belongs to the machine it was made on, and a simulator
# that brings another machine's hangs on it. The simulator gets a new one as it boots.
#   usage: OWNER=nimbusxr REPO=nimbusxr/axx [PUBLISH=true ACTOR=... TOKEN=...] scripts/ios-simulator.sh [device type name]
set -euo pipefail

type_name=${1:-iPhone 16}
devices=~/Library/Developer/CoreSimulator/Devices
image=ghcr.io/$OWNER/axx-ios-simulator

# The newest iOS runtime, which the pack's simulators run, and the device type, as axx finds them.
read -r runtime runtime_name runtime_build < <(xcrun simctl list runtimes -j | jq -r '
  [.runtimes[] | select(.platform == "iOS" and .isAvailable)]
  | sort_by(.version | split(".") | map(tonumber)) | last | "\(.identifier) \(.name|gsub(" "; "_")) \(.buildversion)"')
runtime_name=${runtime_name//_/ }
device_type=$(xcrun simctl list devicetypes -j | jq -r --arg n "$type_name" '.devicetypes[] | select(.name == $n) | .identifier')
[[ -n "$device_type" ]] || { echo "no device type $type_name" >&2; exit 1; }
# What axx names the first simulator it keeps (pool.go: "axx <type>, <runtime> (1)").
name="axx $type_name, $runtime_name (1)"
key=$({
  sw_vers -buildVersion
  xcodebuild -version
  echo "$runtime $runtime_build $device_type"
  shasum -a 256 "$0"
} | shasum -a 256 | cut -c1-16)

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
if [[ -n "${TOKEN:-}" ]]; then
  echo "$TOKEN" | oras login ghcr.io -u "$ACTOR" --password-stdin > /dev/null
fi
if (cd "$work" && oras pull "$image:$key" > /dev/null 2>&1); then
  tar -xzf "$work/simulator.tar.gz" -C "$devices"
  # CoreSimulator reads its devices as it starts: started again, it has this one too.
  killall -9 com.apple.CoreSimulator.CoreSimulatorService 2> /dev/null || true
  xcrun simctl list devices | grep -F "$name" >&2
  echo "the simulator, set up, from $image:$key" >&2
  exit 0
fi

echo "$image:$key is not in the registry: setting the simulator up" >&2
started=$(date +%s)
udid=$(xcrun simctl create "$name" "$device_type" "$runtime")
xcrun simctl boot "$udid"
xcrun simctl bootstatus "$udid" -b > /dev/null
xcrun simctl shutdown "$udid"
echo "set up in $(($(date +%s) - started))s" >&2
if [[ "${PUBLISH:-false}" == true ]]; then
  # A copy without its keychain; this machine's simulator keeps its own.
  cp -R "$devices/$udid" "$work/$udid"
  rm -rf "$work/$udid/data/Library/Keychains"
  tar -czf "$work/simulator.tar.gz" -C "$work" "$udid"
  (cd "$work" && oras push "$image:$key" --artifact-type application/vnd.nimbusxr.axx.ios-simulator \
    --annotation "org.opencontainers.image.source=https://github.com/$REPO" \
    --annotation "org.opencontainers.image.description=$name, set up, for axx's iOS tests" \
    simulator.tar.gz:application/gzip > /dev/null)
  echo "published $image:$key ($(du -h "$work/simulator.tar.gz" | cut -f1))" >&2
fi
