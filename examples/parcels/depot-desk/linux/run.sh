#!/bin/sh
# Runs the depot desk's feature against one Linux build, on X11 or on Wayland, in this image:
#   depot-desk <build> <x11|wayland> [axx run arguments]
# The desk's sources are read from /src (../, mounted read-only), the build is made in a copy,
# and axx is the one on PATH (mounted). With /out mounted, the screenshots and what the run
# leaves (.axx) are copied there.
set -eu
build=$1 display=$2
shift 2
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
status=0
axx run "$@" || status=$?
if [ -d /out ]; then
  cp -R screenshots /out/ 2> /dev/null || true
  cp -R .axx /out/ 2> /dev/null || true
fi
exit $status
