#!/bin/sh
# Runs the depot desk's feature against one Linux build, on X11 or on Wayland, in this image:
#   depot-desk <build> <x11|wayland|x11-hosted> [axx run arguments]
# With x11-hosted, an axx host starts and drives the desk for the run, as it does for a run in an
# agent's sandbox, and what stands in for its person approves the desk.
# The desk's sources are read from /src (../, mounted read-only), the build is made in a copy,
# and axx is the one on PATH (mounted). With /out mounted, the screenshots and what the run
# leaves (.axx) are copied there.
set -eu
build=$1 display=$2
shift 2
hosted=
case $display in
  *-hosted) display=${display%-hosted} hosted=1 ;;
esac
mkdir -p /work/desk
rsync -a --delete --exclude node_modules --exclude build --exclude target --exclude .dart_tool \
  --exclude gen --exclude .axx --exclude flutter/linux --exclude flutter/macos --exclude flutter/windows \
  /src/ /work/desk/
cd /work/desk
case $build in
  swing) sh swing/build.sh > /dev/null ;;
  # Electron fetches its binary only when asked (its install.js).
  electron) npm ci --prefix electron --no-audit --no-fund > /dev/null && node electron/node_modules/electron/install.js ;;
  tauri) sh tauri/build.sh ;;
  flutter) sh flutter/build.sh ;;
esac
profile=linux-$build
if [ "$display" = wayland ]; then
  # GNOME Shell needs a system bus; without logind (as in a container), no /run/systemd/seats.
  mkdir -p /run/dbus
  dbus-daemon --system --fork
  profile=$profile,wayland
fi
cd acceptance
set -- --profile "$profile" "$@"
if [ "$build" = flutter ]; then
  set -- "$@" -D "desk.app=../flutter/build/linux/$(dpkg --print-architecture | sed 's/amd64/x64/')/release/bundle/depot_desk"
fi
if [ -n "$hosted" ]; then
  # The host talks as it does to an editor that started it: its events as lines of JSON, the
  # answers on its standard input. Each approval it asks for is approved, and kept in its log.
  mkfifo /tmp/host.in
  : > /tmp/host.log
  axx host --ide < /tmp/host.in >> /tmp/host.log 2>&1 &
  host=$!
  exec 3> /tmp/host.in
  tail -n +1 -f /tmp/host.log | while IFS= read -r line; do
    case $line in
      *'"event":"approval"'*) printf '{"approve":%s}\n' "$(printf '%s' "$line" | sed 's/.*"id":\([0-9][0-9]*\).*/\1/')" >&3 ;;
    esac
  done &
  for _ in $(seq 100); do [ -f .axx/host.json ] && break; sleep 0.1; done
  [ -f .axx/host.json ] || { echo "the axx host did not start:"; cat /tmp/host.log; exit 1; }
fi
status=0
axx run "$@" || status=$?
if [ -n "$hosted" ]; then
  kill "$host" 2> /dev/null || true
  wait "$host" 2> /dev/null || true
  # The run must have had its desk from the host, once its person approved it.
  grep -q '"event":"approval"' /tmp/host.log || { echo "the axx host was not asked to approve the desk:"; cat /tmp/host.log; status=1; }
  grep -q 'started the depot app' /tmp/host.log || { echo "the axx host did not start the desk:"; cat /tmp/host.log; status=1; }
  [ -d /out ] && cp /tmp/host.log /out/host.log
fi
if [ -d /out ]; then
  cp -R screenshots /out/ 2> /dev/null || true
  cp -R .axx /out/ 2> /dev/null || true
fi
exit $status
