#!/bin/sh
# Builds build/depot-desk.jar, the depot desk in Swing. Needs a JDK (17 or later).
set -eu
cd "$(dirname "$0")"
rm -rf build
mkdir -p build/classes
# Java 17 classes, which any JDK from 17 on runs, whichever JDK builds them.
javac --release 17 -d build/classes DepotDesk.java
cp ../depot.png build/classes/
jar --create --file build/depot-desk.jar --main-class DepotDesk -C build/classes .
echo "$PWD/build/depot-desk.jar"
