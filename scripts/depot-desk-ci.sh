#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# The depot desk's feature against one Linux build of the desk, in the desk's image, as CI's
# desktop job runs it (the image's command is examples/parcels/depot-desk/linux/run.sh), with a
# limit on the whole run, the image's pull included. A run that ends in time ends this script
# with its status. One that does not is a failure that says what it can of where it stopped:
# the container's processes are listed, and axx is interrupted, which ends its run with the
# report, the traces and the screenshots, as Ctrl-C does. An axx that still does not end is
# stuck where no step's limit reaches: it is told to quit, and Go prints where each of its
# goroutines is. What is left then (a build, a desktop session) is killed.
#   usage: scripts/depot-desk-ci.sh IMAGE BUILD DISPLAY AXX OUT SECONDS
# AXX is the axx the run uses, built for Linux, and OUT the folder that gets the report
# (junit.xml, report.html), the screenshots and what the run leaves (.axx).
set -euo pipefail

image=$1 build=$2 display=$3 axx=$4 out=$5 limit=$6
started=$(date +%s)
src=$(cd "$(dirname "$0")/../examples/parcels/depot-desk" && pwd)
name=depot-desk-$build-$display-$$
mkdir -p "$out"

# GNOME Shell takes /run/systemd/seats for logind's, which a container has not.
docker run -d --name "$name" --shm-size=1g --tmpfs /run/systemd \
  -v "$src:/src:ro" -v "$axx:/usr/local/bin/axx:ro" -v "$out:/out" \
  "$image" depot-desk "$build" "$display" --format progress --format junit:/out/junit.xml \
  --format html:/out/report.html --set packs.desktop-core.traces=failed > /dev/null
trap 'docker rm -f "$name" > /dev/null 2>&1 || true' EXIT
docker logs -f "$name" &
logs=$!

# The container's exit status, once it ends.
exec 3< <(docker wait "$name")
left=$((limit - ($(date +%s) - started)))
if read -r -t "$((left > 1 ? left : 1))" status <&3; then
  wait "$logs" || true
  exit "$status"
fi

echo "::error::The depot desk's feature ($build, $display) did not end within $limit seconds. Its processes, and what axx says as it is interrupted, are in the job's log; what the run left is in the job's artifact."
docker top "$name" || true
# The signal to every axx in the container: the run's, and a hosted run's host.
signal() {
  docker exec "$name" sh -c 'for p in /proc/[0-9]*; do [ "$(cat "$p/comm" 2> /dev/null)" = axx ] && kill -s "$0" "${p#/proc/}"; done; true' "$1" || true
}
signal INT
if ! read -r -t 15 status <&3; then
  signal QUIT
  read -r -t 10 status <&3 || docker kill "$name" > /dev/null || true
fi
wait "$logs" || true
# What the run left, where run.sh did not get to copy it.
for kept in screenshots .axx; do
  [ -e "$out/$kept" ] || docker cp "$name:/work/desk/acceptance/$kept" "$out/" > /dev/null 2>&1 || true
done
exit 1
