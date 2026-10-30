#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# The tests CI's Mac jobs run (ios, coreml), built for macOS on Apple silicon as axx builds for it
# (no cgo): each test binary in its package's folder, where scripts/gotest.sh runs it, and
# test2json for macOS (bin/test2json), which scripts/gotest.sh runs it through
# (GOTEST_TEST2JSON). With a path, they are put in a tar there too, which keeps their modes.
#   usage: scripts/mac-tests.sh [tar]
set -euo pipefail

export GOOS=darwin GOARCH=arm64 CGO_ENABLED=0
built=(bin/test2json)
go build -o bin/test2json cmd/test2json
for pkg in packs/mobile/ios packs/models/ios packs/models/macos packs/models/core/format/coreml packs/models/core/format/foundationmodels; do
  go test -c -tags integration -o "$pkg/$(basename "$pkg").test" "./$pkg"
  built+=("$pkg/$(basename "$pkg").test")
done
if [ -n "${1:-}" ]; then
  tar -cf "$1" "${built[@]}"
fi
