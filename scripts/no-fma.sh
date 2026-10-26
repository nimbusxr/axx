#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# A model stub is the same bit for bit on every machine, so nothing that builds one may have a
# multiply and an add that Go fuses into one instruction: arm64 has it (amd64 does not), and it
# rounds once where there were two roundings. This builds the models packs for arm64 with the
# compiler's own report of where it would fuse, and fails if there is any place (modelstest's
# aside: it only makes tests' inputs). Round the product on its own: float64(a*b) + c.
set -euo pipefail
cd "$(dirname "$0")/.."
sites=$(GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o /dev/null \
  -gcflags='github.com/nimbusxr/axx/packs/models/core/...=-d=fmahash=vy' ./packs/models/core/... 2>&1 |
  grep -o 'packs/models/core/[^ ]*\.go:[0-9]*:[0-9]*' | grep -v -e /modelstest/ -e _test.go | sort -u || true)
if [ -n "$sites" ]; then
  echo "a multiply and an add Go fuses on arm64, which would build a stub otherwise there: round the product on its own, float64(a*b) + c" >&2
  printf '%s\n' "$sites" >&2
  exit 1
fi
