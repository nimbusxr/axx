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

# On Linux with an x86_64 processor, which is CI's runners, the emulator is the build named
# here, and not the one sdkmanager installs that day. Build 16970787 (37.3.10), which
# sdkmanager has installed since November 2026, drops adb's connection to a busy device there:
# the write of the device's adb daemon to the host fails ("host-14: connection terminated:
# write failed" in the device's log), and with the connection go the shells adb runs on the
# device and the ports it forwards, the UiAutomator2 server's among them. On CI it did in 2 of
# 34 jobs with a device made as before, and in 6 of 34 with its snapshot saved at rest; build
# 16740729 (37.2.13) in none of 34. On a Mac the newest is left: 14 runs of a scenario on a
# loaded emulator there lost no connection. A newer build is named here once CI's jobs show it
# keeps the connection (an android job warns of each loss).
emulator_build=16740729
emulator_version=37.2.13
emulator_sha256=cbc7c3cfb83220b8a0f9c513fdab557712e74d339ec19ddffa43f23350ae9bdd
# Google's repository of SDK packages, or a copy of it (ANDROID_REPOSITORY_URL).
repository=${ANDROID_REPOSITORY_URL:-https://dl.google.com/android/repository}

# named_emulator puts the named build of the emulator in the SDK, in place of the one sdkmanager
# installed: its archive from Google's repository, checked against its SHA-256. The archive has
# no package.xml, which sdkmanager writes and avdmanager asks for ("emulator" package must be
# installed): the installed one's is kept, with the named build's version in it.
named_emulator() {
  if grep -qx "Pkg.BuildId=$emulator_build" "$sdk/emulator/source.properties" 2> /dev/null; then return 0; fi
  local zip="$latest/emulator.zip"
  say "installing the emulator's build $emulator_build"
  limited 900 curl -fsSL --retry 3 --retry-all-errors -o "$zip" "$repository/emulator-linux_x64-$emulator_build.zip" || {
    echo "cannot download the emulator's build $emulator_build from $repository" >&2
    return 1
  }
  echo "$emulator_sha256  $zip" | sha256sum -c --quiet || {
    echo "the emulator's archive from $repository is not the build $emulator_build that this script names" >&2
    return 1
  }
  local major minor micro
  IFS=. read -r major minor micro <<< "$emulator_version"
  sed -E -e "s|<major>[0-9]+</major>|<major>$major</major>|" -e "s|<minor>[0-9]+</minor>|<minor>$minor</minor>|" \
    -e "s|<micro>[0-9]+</micro>|<micro>$micro</micro>|" "$sdk/emulator/package.xml" > "$latest/package.xml" || return 1
  rm -rf "$sdk/emulator"
  unzip -q "$zip" -d "$sdk" || return 1
  mv "$latest/package.xml" "$sdk/emulator/package.xml"
  grep -qx "Pkg.BuildId=$emulator_build" "$sdk/emulator/source.properties"
}

# Google's newest packages need its newest command-line tools: the SDK's own fetch them into a
# folder of their own, and are left as they are.
latest=$(mktemp -d)
trap 'rm -rf "$latest"' EXIT
fetch "$sdk/cmdline-tools/latest/bin/sdkmanager" "$latest" "$latest/cmdline-tools/latest/bin/sdkmanager" -- "cmdline-tools;latest"
fetch "$latest/cmdline-tools/latest/bin/sdkmanager" "$sdk" \
  "$sdk/system-images/$platform/google_apis/$abi/system.img" "$sdk/emulator/emulator" "$sdk/platform-tools/adb" \
  -- "$image" emulator platform-tools
if [ "$(uname -s)-$abi" = Linux-x86_64 ]; then named_emulator; fi
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
# snapshot was saved with. And it has a front camera, as the phone its system image describes
# (android.hardware.camera.front) does: without one, CameraX looks for it again and again, for
# 6 seconds, before an app's camera shows.
config="$ANDROID_AVD_HOME/parcels-pixel.avd/config.ini"
grep -v -e '^hw.camera.back=' -e '^hw.camera.front=' "$config" > "$config.new"
printf 'hw.camera.back=environment\nhw.camera.front=emulated\n' >> "$config.new"
mv "$config.new" "$config"
printf 'version = 1\nscene.mode = mesh3d:Toren1BD.obj\n' > "$ANDROID_AVD_HOME/parcels-pixel.avd/environment.ini"

# Its boot snapshot: booted once and saved, every emulator axx starts of it boots from it in
# seconds instead of booting cold. axx starts them read-only: they save nothing, so each starts
# as the snapshot left it.
say "parcels-pixel: booting it for its snapshot"
# Its console saves the snapshot, and answers when it is saved; the emulator saves nothing as it
# stops (-no-snapshot-save): on CI's runner, one asked to stop went on idle now and then, its
# snapshot not begun.
args=(-avd parcels-pixel -port 5580 -no-window -no-audio -no-boot-anim -no-snapshot-save)
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
# Android says it has booted while its first boot still goes on: for a minute or more it unlocks
# the user's storage (and starts the home screen again as it does, in front of whatever app is
# there), scans its storage for media, and starts its services' apps, Google Play services among
# them, which start themselves again. A snapshot saved now has every emulator started from it
# go through that again, under each run's first scenario: the system's file picker lists a
# folder again as the scan updates it, a web view's page goes empty for seconds, taps are slow
# enough to be taken for long presses. So the snapshot is saved once the device has come to
# rest: its user unlocked, and its processors idle (four fifths of the time or more) for
# fifteen seconds on end. Four minutes at most: a device that never rests is saved as it is,
# and the script says so.
say "parcels-pixel: booted; waiting until it has finished its first boot"
# idle prints the share of the next five seconds, in hundredths, that the device's processors
# spend idle, from the kernel's own count (/proc/stat: the fourth number after "cpu").
idle() {
  limited 20 "$adb" -s emulator-5580 shell 'head -n 1 /proc/stat; sleep 5; head -n 1 /proc/stat' 2> /dev/null | tr -d '\r' |
    awk 'NR == 1 { for (i = 2; i <= NF; i++) before += $i; was = $5 }
         NR == 2 { for (i = 2; i <= NF; i++) after += $i; if (after > before) printf "%d", 100 * ($5 - was) / (after - before) }'
}
rested=0
waited=$SECONDS
deadline=$((SECONDS + 240))
while [ "$SECONDS" -lt "$deadline" ]; do
  kill -0 "$emulator" 2> /dev/null || break
  user=$(limited 10 "$adb" -s emulator-5580 shell am get-started-user-state 0 2> /dev/null | tr -d '\r' || true)
  share=$(idle || true)
  if [[ "$user" == *RUNNING_UNLOCKED* ]] && [ "${share:-0}" -ge 80 ]; then
    rested=$((rested + 1))
  else
    rested=0
  fi
  [ "$rested" -ge 3 ] && break
done
if [ "$rested" -ge 3 ]; then
  say "parcels-pixel: at rest $((SECONDS - waited))s after it booted"
else
  say "parcels-pixel: not at rest $((SECONDS - waited))s after it booted (its user: ${user:-unknown}; its processors idle ${share:-?}% of the last five seconds): its snapshot is saved as it is" >&2
fi
# Five minutes at most to save it (a save takes seconds).
say "parcels-pixel: saving its snapshot"
log="$ANDROID_AVD_HOME/parcels-pixel-snapshot.log"
snapshot="$ANDROID_AVD_HOME/parcels-pixel.avd/snapshots/default_boot"
answer=$(limited 300 "$adb" -s emulator-5580 emu avd snapshot save default_boot 2>&1) || true
if ! grep -qx 'OK' <<< "$(tr -d '\r' <<< "$answer")" || [ ! -s "$snapshot/ram.bin" ]; then
  say "parcels-pixel did not save its boot snapshot (its console answered: ${answer:-nothing}); the end of the emulator's log ($log):" >&2
  tail -n 40 "$log" >&2 || true
  # What the emulator's processes are doing (their state, and what each waits for), and how far
  # its snapshot got.
  ps -o pid,ppid,stat,wchan,etime,command -p "$(pgrep -d, -f 'emulator|qemu')" >&2 || true
  ls -la "$ANDROID_AVD_HOME/parcels-pixel.avd/snapshots/"* >&2 2> /dev/null || true
  kill -9 "$emulator" 2> /dev/null || true
  exit 1
fi
# Then it stops: a minute at most, or it is stopped (the snapshot is saved).
limited 30 "$adb" -s emulator-5580 emu kill > /dev/null 2>&1 || true
deadline=$((SECONDS + 60))
while kill -0 "$emulator" 2> /dev/null && [ "$SECONDS" -lt "$deadline" ]; do sleep 2; done
if kill -0 "$emulator" 2> /dev/null; then
  say "parcels-pixel did not stop in a minute, its snapshot saved: stopping it" >&2
  kill -9 "$emulator" 2> /dev/null || true
fi
wait "$emulator" 2> /dev/null || true
say "parcels-pixel: its boot snapshot is saved"
