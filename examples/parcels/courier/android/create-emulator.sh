#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Creates the emulator device (AVD) the acceptance suite runs the couriers' app on:
# parcels-pixel, a Pixel 8 with Android 15 (API 35), for this machine's processor.
#   usage: ./create-emulator.sh     (needs the Android SDK: ANDROID_HOME)
set -euo pipefail

sdk=${ANDROID_HOME:-${ANDROID_SDK_ROOT:?set ANDROID_HOME to the Android SDK}}
case "$(uname -m)" in
  arm64 | aarch64) abi=arm64-v8a ;;
  *) abi=x86_64 ;;
esac
image="system-images;android-35;google_apis;$abi"

# Google's newest packages need its newest command-line tools: the SDK's own fetch them into a
# folder of their own, and are left as they are. sdkmanager asks to accept licences; yes stops
# when it stops reading.
latest=$(mktemp -d)
trap 'rm -rf "$latest"' EXIT
{ yes || true; } | "$sdk/cmdline-tools/latest/bin/sdkmanager" --sdk_root="$latest" --install "cmdline-tools;latest" > /dev/null
{ yes || true; } | "$latest/cmdline-tools/latest/bin/sdkmanager" --sdk_root="$sdk" --install "$image" emulator platform-tools > /dev/null
# The SDK's own avdmanager: the newest one takes its own folder for the SDK.
tools="$sdk/cmdline-tools/latest/bin"
# Where the emulator looks for its devices: avdmanager may go by XDG_CONFIG_HOME instead.
export ANDROID_AVD_HOME="${ANDROID_AVD_HOME:-$HOME/.android/avd}"
mkdir -p "$ANDROID_AVD_HOME"
# A Pixel the installed command-line tools know: older ones do not know the Pixel 8.
devices=$("$tools/avdmanager" list device -c 2> /dev/null)
device=""
for want in pixel_8 pixel_6; do
  if printf '%s\n' "$devices" | grep -qx "$want"; then
    device=$want
    break
  fi
done
[ -n "$device" ] || device=$(printf '%s\n' "$devices" | grep '^pixel' | tail -1)
echo no | "$tools/avdmanager" create avd --force --name parcels-pixel --package "$image" --device "$device"
echo "parcels-pixel: $image, $device"

# Its boot snapshot: booted once and saved, every emulator axx starts of it boots from it in
# seconds instead of booting cold. axx starts them read-only: they save nothing, so each starts
# as the snapshot left it.
args=(-avd parcels-pixel -port 5580 -no-window -no-audio -no-boot-anim)
if [ "$(uname -s)" = Linux ]; then args+=(-gpu swiftshader_indirect); fi
"$sdk/emulator/emulator" "${args[@]}" > "$ANDROID_AVD_HOME/parcels-pixel-snapshot.log" 2>&1 &
emulator=$!
adb="$sdk/platform-tools/adb"
booted=""
for _ in $(seq 300); do
  kill -0 "$emulator" 2> /dev/null || break
  if [ "$("$adb" -s emulator-5580 shell getprop sys.boot_completed 2> /dev/null | tr -d '\r')" = 1 ]; then
    booted=yes
    break
  fi
  sleep 2
done
if [ -z "$booted" ]; then
  echo "parcels-pixel did not boot for its snapshot; the emulator's log: $ANDROID_AVD_HOME/parcels-pixel-snapshot.log" >&2
  kill "$emulator" 2> /dev/null || true
  exit 1
fi
"$adb" -s emulator-5580 emu kill > /dev/null
wait "$emulator" || true
echo "parcels-pixel: its boot snapshot is saved"
