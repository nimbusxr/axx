#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# The simulator the iOS pack's integration tests keep (packs.mobile-ios.keep), made and booted
# ahead of them: a new simulator's first boot takes minutes on a hosted runner, and started once the
# job has restored its caches, it boots while the job builds the app and the tests. The
# tests find it booted under the name axx gives its first kept simulator, and use it. (A simulator
# set up on another machine and copied here boots slower still: its first boot on this machine
# does that work again, and then some.)
#   usage: scripts/ios-simulator.sh [device type name]
set -euo pipefail

type_name=${1:-iPhone 16}

# The newest iOS runtime, which the pack's simulators run, and the device type, as axx finds them.
read -r runtime runtime_name < <(xcrun simctl list runtimes -j | jq -r '
  [.runtimes[] | select(.platform == "iOS" and .isAvailable)]
  | sort_by(.version | split(".") | map(tonumber)) | last | "\(.identifier) \(.name|gsub(" "; "_"))"')
runtime_name=${runtime_name//_/ }
device_type=$(xcrun simctl list devicetypes -j | jq -r --arg n "$type_name" '.devicetypes[] | select(.name == $n) | .identifier')
[[ -n "$device_type" ]] || { echo "no device type $type_name" >&2; exit 1; }
# What axx names the first simulator it keeps (pool.go: "axx <type>, <runtime> (1)").
name="axx $type_name, $runtime_name (1)"

started=$(date +%s)
udid=$(xcrun simctl create "$name" "$device_type" "$runtime")
xcrun simctl boot "$udid"
xcrun simctl bootstatus "$udid" -b > /dev/null
echo "$name ($udid) booted in $(($(date +%s) - started))s"

# A new simulator's first minutes go to work no test here needs: analysing the photo library
# (mediaanalysisd), Screen Time, Fitness's intelligence and the App Store's engagement. On three
# cores it ran alongside the first tests. Off, the tests took 268 seconds rather than 367. And its
# push service (apsd) keeps a connection to Apple's servers that the tests' apps, which notify
# locally, never use: it took 125 s of processor time in one job on GitHub's runner. And chronod
# has a dozen widgets' extensions draw their timelines, on a runner with no GPU. Without apsd and
# chronod, the tests took 195 s rather than 221 on a VM of the runner's size.
for service in com.apple.mediaanalysisd com.apple.mediaanalysisd.analysis com.apple.mediaanalysisd.computeservice \
  com.apple.mediaanalysisd.photos com.apple.ScreenTimeAgent com.apple.fitnessintelligenced com.apple.amsengagementd \
  com.apple.apsd com.apple.chronod; do
  xcrun simctl spawn "$udid" launchctl disable "user/501/$service"
  xcrun simctl spawn "$udid" launchctl bootout "user/501/$service" 2> /dev/null || true
done
