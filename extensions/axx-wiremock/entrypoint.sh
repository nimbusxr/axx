#!/bin/sh
# SPDX-License-Identifier: Apache-2.0
#
# Runs WireMock standalone with the axx OpenAPI validation extension, and with WireMock's gRPC
# extension when WireMock's root dir has a grpc folder: that extension does not start without
# one. Arguments are WireMock's; JAVA_OPTS adds JVM options.
root=.
prev=
for arg in "$@"; do
  case $prev in --root-dir) root=$arg ;; esac
  case $arg in --root-dir=*) root=${arg#--root-dir=} ;; esac
  prev=$arg
done
cp='/var/wiremock/wiremock-standalone.jar:/var/wiremock/lib/*'
if [ -d "$root/grpc" ]; then
  cp="$cp:/var/wiremock/grpc/*"
fi
# shellcheck disable=SC2086 # JAVA_OPTS holds several options
exec java $JAVA_OPTS -cp "$cp:/var/wiremock/extensions/*" wiremock.Run "$@"
