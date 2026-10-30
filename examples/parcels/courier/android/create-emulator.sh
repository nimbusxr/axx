#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Creates the emulator device (AVD) the acceptance suite runs the couriers' app on:
# parcels-pixel, a Pixel 8 with Android 15 (API 35), or Android 17 (API 37) on an arm64
# processor, for this machine's processor.
#   usage: ./create-emulator.sh     (needs the Android SDK: ANDROID_HOME)
set -euo pipefail

sdk=${ANDROID_HOME:-${ANDROID_SDK_ROOT:?set ANDROID_HOME to the Android SDK}}
case "$(uname -m)" in
  arm64 | aarch64) abi=arm64-v8a ;;
  *) abi=x86_64 ;;
esac
# Android 15's kernel (6.6) tells apps that a processor with SME but no SVE (Apple's M4 and
# later) has SVE2, which it has only in SME's streaming mode: libraries that go by it then stop
# on an illegal instruction (LiteRT-LM's, which runs the couriers' app's delivery notes model).
# Android 17's (6.12) tells them what the processor has.
platform=android-35
if [ "$abi" = arm64-v8a ]; then platform=android-37.0; fi
image="system-images;$platform;google_apis;$abi"

# say tells what the script does, and when: a CI job's log shows how long each part took.
say() { echo "$(date +%T) $*"; }

# limited runs a command for at most $1 seconds, where the system can stop it (timeout; killed
# if it does not stop when asked): an SDK tool that hangs fails the script, instead of a CI job's
# wait for it.
limited() {
  local seconds=$1
  shift
  if command -v timeout > /dev/null; then timeout -k 5 "$seconds" "$@"; else "$@"; fi
}

# fetch installs SDK packages until the files they leave are there: Google's servers drop a
# download now and then, and sdkmanager may still exit 0, leaving the SDK without the package.
# sdkmanager asks to accept licences; yes stops when it stops reading.
#   fetch <sdkmanager> <SDK folder> <a file each package leaves>... -- <package>...
fetch() {
  local manager=$1 root=$2 files=() missing attempt
  shift 2
  while [ "$1" != -- ]; do
    files+=("$1")
    shift
  done
  shift
  for attempt in 1 2 3; do
    say "installing $* (attempt $attempt)"
    { yes || true; } | limited 900 "$manager" --sdk_root="$root" --install "$@" > /dev/null || true
    missing=""
    for f in "${files[@]}"; do [ -e "$f" ] || missing="$missing $f"; done
    [ -z "$missing" ] && return 0
    echo "sdkmanager did not install it all: no$missing" >&2
    sleep 15
  done
  echo "cannot install $* from Google's servers" >&2
  return 1
}

# Google's newest packages need its newest command-line tools: the SDK's own fetch them into a
# folder of their own, and are left as they are.
latest=$(mktemp -d)
trap 'rm -rf "$latest"' EXIT
fetch "$sdk/cmdline-tools/latest/bin/sdkmanager" "$latest" "$latest/cmdline-tools/latest/bin/sdkmanager" -- "cmdline-tools;latest"
fetch "$latest/cmdline-tools/latest/bin/sdkmanager" "$sdk" \
  "$sdk/system-images/$platform/google_apis/$abi/system.img" "$sdk/emulator/emulator" "$sdk/platform-tools/adb" \
  -- "$image" emulator platform-tools
# The SDK's own avdmanager: the newest one takes its own folder for the SDK.
tools="$sdk/cmdline-tools/latest/bin"
# Where the emulator looks for its devices: avdmanager may go by XDG_CONFIG_HOME instead.
export ANDROID_AVD_HOME="${ANDROID_AVD_HOME:-$HOME/.android/avd}"
mkdir -p "$ANDROID_AVD_HOME"
# A Pixel the installed command-line tools know: older ones do not know the Pixel 8.
say "making parcels-pixel"
devices=$(limited 300 "$tools/avdmanager" list device -c 2> /dev/null)
device=""
for want in pixel_8 pixel_6; do
  if printf '%s\n' "$devices" | grep -qx "$want"; then
    device=$want
    break
  fi
done
[ -n "$device" ] || device=$(printf '%s\n' "$devices" | grep '^pixel' | tail -1)
echo no | limited 300 "$tools/avdmanager" create avd --force --name parcels-pixel --package "$image" --device "$device"
say "parcels-pixel: $image, $device"
# Its back camera renders the AVD's environment (environment.ini), which axx sets to what an
# app's camera sees in a scenario: an emulator started from the snapshot keeps the camera the
# snapshot was saved with.
config="$ANDROID_AVD_HOME/parcels-pixel.avd/config.ini"
grep -v '^hw.camera.back=' "$config" > "$config.new"
echo 'hw.camera.back=environment' >> "$config.new"
mv "$config.new" "$config"
printf 'version = 1\nscene.mode = mesh3d:Toren1BD.obj\n' > "$ANDROID_AVD_HOME/parcels-pixel.avd/environment.ini"

# Its boot snapshot: booted once and saved, every emulator axx starts of it boots from it in
# seconds instead of booting cold. axx starts them read-only: they save nothing, so each starts
# as the snapshot left it.
say "parcels-pixel: booting it for its snapshot"
args=(-avd parcels-pixel -port 5580 -no-window -no-audio -no-boot-anim)
if [ "$(uname -s)" = Linux ]; then args+=(-gpu swiftshader_indirect); fi
"$sdk/emulator/emulator" "${args[@]}" > "$ANDROID_AVD_HOME/parcels-pixel-snapshot.log" 2>&1 &
emulator=$!
adb="$sdk/platform-tools/adb"
booted=""
# Ten minutes at most (a boot takes two to five), each question to adb ten seconds.
deadline=$((SECONDS + 600))
while [ "$SECONDS" -lt "$deadline" ]; do
  kill -0 "$emulator" 2> /dev/null || break
  if [ "$(limited 10 "$adb" -s emulator-5580 shell getprop sys.boot_completed 2> /dev/null | tr -d '\r')" = 1 ]; then
    booted=yes
    break
  fi
  sleep 2
done
if [ -z "$booted" ]; then
  say "parcels-pixel did not boot for its snapshot in ten minutes; the end of the emulator's log ($ANDROID_AVD_HOME/parcels-pixel-snapshot.log):" >&2
  tail -n 40 "$ANDROID_AVD_HOME/parcels-pixel-snapshot.log" >&2 || true
  kill "$emulator" 2> /dev/null || true
  exit 1
fi
# It saves the snapshot as it stops: five minutes at most.
say "parcels-pixel: booted; saving its snapshot"
limited 30 "$adb" -s emulator-5580 emu kill > /dev/null || true
deadline=$((SECONDS + 300))
while kill -0 "$emulator" 2> /dev/null && [ "$SECONDS" -lt "$deadline" ]; do sleep 2; done
if kill -0 "$emulator" 2> /dev/null; then
  say "parcels-pixel did not save its boot snapshot in five minutes; the end of the emulator's log ($ANDROID_AVD_HOME/parcels-pixel-snapshot.log):" >&2
  tail -n 40 "$ANDROID_AVD_HOME/parcels-pixel-snapshot.log" >&2 || true
  kill -9 "$emulator" 2> /dev/null || true
  exit 1
fi
wait "$emulator" || true
say "parcels-pixel: its boot snapshot is saved"
