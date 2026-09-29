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
tools="$sdk/cmdline-tools/latest/bin"

# sdkmanager asks to accept licences; yes stops when it stops reading.
{ yes || true; } | "$tools/sdkmanager" --install "$image" emulator platform-tools > /dev/null
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
