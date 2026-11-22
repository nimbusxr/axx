#!/system/bin/sh
# SPDX-License-Identifier: Apache-2.0
#
# What Android starts the app through, in a build that checks memory (-Pmemcheck=true), on arm64:
# under HWAddressSanitizer, whose runtime is the device's own (Android 14 and later).
#
# An app started through a script is not started debuggable, as one Android starts itself is: the
# runtime is told so here, as agents (ART TI) attach in full only to a debuggable one.
cmd=$1
shift
LD_HWASAN=1 exec "$cmd" -Xcompiler-option --debuggable "$@"
