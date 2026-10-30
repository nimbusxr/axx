#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# The simulator the iOS pack's integration tests keep (packs.mobile-ios.keep), made and booted
# ahead of them, once the job has built the app and the tests: a new simulator's first boot takes
# a minute or more on a hosted runner, and slows whatever runs beside it. The tests find it booted
# under the name axx gives its first kept simulator, and use it. (A simulator set up on another
# machine and copied here boots slower still: its first boot on this machine does that work
# again, and then some.)
#   usage: scripts/ios-simulator.sh [device type name]
set -euo pipefail

type_name=${1:-iPhone 16}

# The device type, and the newest iOS runtime that runs it, which the pack's simulators run, as
# axx finds them (Xcode's newest may not run an older iPhone).
device_type=$(xcrun simctl list devicetypes -j | jq -r --arg n "$type_name" '.devicetypes[] | select(.name == $n) | .identifier')
[[ -n "$device_type" ]] || { echo "no device type $type_name" >&2; exit 1; }
read -r runtime runtime_name < <(xcrun simctl list runtimes -j | jq -r --arg t "$device_type" '
  [.runtimes[] | select(.platform == "iOS" and .isAvailable)
    | select((.supportedDeviceTypes // []) as $s | ($s | length) == 0 or any($s[]; .identifier == $t))]
  | sort_by(.version | split(".") | map(tonumber)) | last | "\(.identifier) \(.name|gsub(" "; "_"))"')
[[ -n "$runtime" && "$runtime" != null ]] || { echo "no iOS runtime runs $type_name" >&2; exit 1; }
runtime_name=${runtime_name//_/ }
# What axx names the first simulator it keeps (pool.go: "axx <type>, <runtime> (1)").
name="axx $type_name, $runtime_name (1)"

started=$(date +%s)
udid=$(xcrun simctl create "$name" "$device_type" "$runtime")
xcrun simctl boot "$udid"

# A new simulator's first minutes go to work no test here needs: analysing the photo library
# (mediaanalysisd), Screen Time, Fitness's intelligence and the App Store's engagement. On three
# cores it ran alongside the first tests. Off, the tests took 268 seconds rather than 367. And its
# push service (apsd) keeps a connection to Apple's servers that the tests' apps, which notify
# locally, never use: it took 125 s of processor time in one job on GitHub's runner. And chronod
# has a dozen widgets' extensions draw their timelines, on a runner with no GPU. Without apsd and
# chronod, the tests took 195 s rather than 221 on a VM of the runner's size. And its asset
# subscriptions (assetsubscriptiond) have it download 1.6 GB of Siri's, speech's and languages'
# assets in its first minutes, and decrypt and unpack them (STExtractionService), and its
# accessibility (axassetsd) another 96 MB: the tests' apps take their models from axx's stubs and
# need none of them. Nor the Health app's first start (healthappd), the shortcuts Siri runs in the
# background (siriactionsd), the lock screen's posters, each drawn by an extension of its own
# (PosterBoard), or Spotlight's knowledge of the apps (spotlightknowledged). Without those, a new
# simulator's processes took 217 s of processor time to the end of the courier's Foundation
# Models test rather than 313, on a Mac, and downloaded 8 MB rather than 1.6 GB. They are turned
# off as the simulator starts to boot, so that its boot does none of that work either: launchd
# keeps them off from then on, through the rest of the boot. That runs alongside the wait for the
# boot, each service at once with the others: one after another, an iOS 27 simulator's took two
# and a half minutes past its boot (simctl spawn takes seconds each, there).
disabled=$(mktemp)
(
  for service in com.apple.mediaanalysisd com.apple.mediaanalysisd.analysis com.apple.mediaanalysisd.computeservice \
    com.apple.mediaanalysisd.photos com.apple.ScreenTimeAgent com.apple.fitnessintelligenced com.apple.amsengagementd \
    com.apple.apsd com.apple.chronod com.apple.assetsubscriptiond com.apple.accessibility.axassetsd com.apple.healthappd \
    com.apple.siriactionsd com.apple.PosterBoard com.apple.spotlightknowledged com.apple.spotlightknowledged.updater; do
    (
      xcrun simctl spawn "$udid" launchctl disable "user/501/$service"
      echo "$(($(date +%s) - started))" >> "$disabled"
      xcrun simctl spawn "$udid" launchctl bootout "user/501/$service" 2> /dev/null || true
    ) &
  done
  wait
  # When the first was off too: a boot that runs on past it does its work regardless.
  echo "services off in $(($(date +%s) - started))s (the first disabled at $(sort -n "$disabled" | head -1)s)"
  rm -f "$disabled"
) &
off=$!
xcrun simctl bootstatus "$udid" -b > /dev/null
echo "$name ($udid) booted in $(($(date +%s) - started))s"
wait "$off"
