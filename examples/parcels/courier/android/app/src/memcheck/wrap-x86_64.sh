#!/system/bin/sh
# SPDX-License-Identifier: Apache-2.0
#
# What Android starts the app through, in a build that checks memory (-Pmemcheck=true), on an
# x86-64 emulator: under AddressSanitizer, whose runtime the app brings beside this script. The
# NDK's own script for it (wrap.sh/asan.sh), with the runtime told that the app is debuggable.
#
# An app started through a script is not started debuggable, as one Android starts itself is: the
# runtime is told so here, as agents (ART TI) attach in full only to a debuggable one.
HERE=$(cd "$(dirname "$0")" && pwd)
cmd=$1
shift
export ASAN_OPTIONS=log_to_syslog=false,allow_user_segv_handler=1
ASAN_LIB=$(ls "$HERE"/libclang_rt.asan-*-android.so)
if [ -f "$HERE/libc++_shared.so" ]; then
  # The app's C++ runtime after the checker's: https://github.com/android-ndk/ndk/issues/988
  export LD_PRELOAD="$ASAN_LIB $HERE/libc++_shared.so"
else
  export LD_PRELOAD="$ASAN_LIB"
fi
exec "$cmd" -Xcompiler-option --debuggable "$@"
