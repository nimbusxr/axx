#!/usr/bin/env bash
# Benchmarks axx with hyperfine. Results go to bench/results/<mode>.{md,json}.
#
#   bench/run.sh startup        CLI start-up: version, steps, validate (no infrastructure)
#   bench/run.sh suite          the example suite with its stack already running
#                               (framework overhead + real work, the dev loop)
#   bench/run.sh e2e            one cold run: start the stack, run everything, tear down
#
# `suite` expects the stack to be up:
#   docker compose -f examples/parcels/infra/compose.yaml up -d --build --wait
# and empties its databases before every run (the scenarios register fixed references).
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
out="$root/bench/results"
mkdir -p "$out"
mode=${1:-startup}
runs=${RUNS:-10}

command -v hyperfine >/dev/null || { echo "bench: hyperfine is required (mise install)" >&2; exit 2; }

axx="$root/bin/axx"
(cd "$root" && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "$axx" ./cmd/axx)
example="$root/examples/parcels/acceptance"
compose="docker compose -f $root/examples/parcels/infra/compose.yaml"

hf() { hyperfine --style basic --export-markdown "$out/$mode.md" --export-json "$out/$mode.json" "$@"; }

case "$mode" in
startup)
  cd "$example"
  hf --warmup 3 --runs "$runs" \
    -n "axx version" "$axx version" \
    -n "axx steps --json" "$axx steps --json" \
    -n "axx validate (example)" "$axx validate"
  ;;
suite)
  cd "$example"
  "$axx" fixtures generate >/dev/null
  reset="$compose exec -T postgres psql -q -U parcels -d parcels -c 'TRUNCATE parcels.parcels, parcels.manifest_lines'"
  reset+=" && $compose exec -T mongo mongosh -u parcels -p parcels --quiet --eval 'db.getSiblingDB(\"parcels\").dropDatabase()'"
  hf --warmup 1 --runs "$runs" --prepare "$reset" -n "axx run --no-start" "$axx run --no-start --format progress"
  ;;
e2e)
  cd "$example"
  hf --runs "${RUNS:-3}" -n "axx run (cold, stack started and stopped)" "$axx run --format progress"
  ;;
*)
  echo "usage: $0 startup|suite|e2e" >&2
  exit 2
  ;;
esac
echo "bench: results in ${out#"$root"/}/$mode.md"
