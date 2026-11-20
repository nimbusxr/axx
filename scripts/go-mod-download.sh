#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Downloads the modules go.mod names that Go's module cache lacks, before a CI job builds: those a
# pull request updates, those the cache's job never built, or all of them when the cache was not
# restored. The go command asks for a module once, so one lookup of the proxy's host that fails,
# or one download that breaks off, failed the build. This asks three times, 10 and then 20 seconds
# apart, and keeps what each try downloaded. With every module in the cache it downloads nothing
# and takes a fraction of a second.
set -uo pipefail

for attempt in 1 2 3; do
  go mod download && exit 0
  if [ "$attempt" -lt 3 ]; then
    wait=$((attempt * 10))
    echo "go mod download failed (try $attempt of 3): trying again in ${wait}s" >&2
    sleep "$wait"
  fi
done
echo "go mod download failed three times" >&2
exit 1
