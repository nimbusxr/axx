#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Smoke-tests a built axx-wiremock image against the stubs and spec in example/.
#   usage: ./smoke-test.sh [image]        (default: axx-wiremock:dev)
set -euo pipefail

image=${1:-axx-wiremock:dev}
here=$(cd "$(dirname "$0")" && pwd)
containers=()
cleanup() { [[ ${#containers[@]} -eq 0 ]] || docker rm -f "${containers[@]}" >/dev/null 2>&1 || true; }
trap cleanup EXIT

fail() { echo "FAIL: $*" >&2; exit 1; }

# start <extra wiremock args...>: runs the image (with $env as extra docker run options),
# waits for its healthcheck, sets $base.
env=()
start() {
  local id port status=
  id=$(docker run -d -p 127.0.0.1::8080 ${env[@]+"${env[@]}"} \
    -v "$here/example/mappings:/home/wiremock/mappings:ro" \
    -v "$here/example/openapi:/var/openapi:ro" \
    "$image" "$@")
  containers+=("$id")
  for _ in $(seq 1 60); do
    status=$(docker inspect -f '{{.State.Health.Status}}' "$id")
    [[ $status == healthy ]] && break
    sleep 1
  done
  if [[ $status != healthy ]]; then
    docker logs "$id" >&2
    fail "container did not become healthy (status: $status)"
  fi
  port=$(docker port "$id" 8080/tcp | head -n1 | sed 's/.*://')
  base="http://127.0.0.1:$port"
}

# expect <status> <body substring|-> <curl args...>
expect() {
  local want=$1 want_body=$2 got body
  shift 2
  body=$(curl -sS -o - -w '\n%{http_code}' "$@")
  got=${body##*$'\n'}
  body=${body%$'\n'*}
  printf '%s %s -> %s %s\n' "${*: -1}" "${want}" "${got}" "${body:0:160}"
  [[ $got == "$want" ]] || fail "expected HTTP $want, got $got"
  [[ $want_body == - || $body == *"$want_body"* ]] || fail "body does not contain: $want_body"
}

check() {
  expect 200 '"name":"Ada"' "$base/users/42"
  expect 201 - -H 'Content-Type: application/json' -d '{"name":"Ada"}' "$base/users"
  expect 500 "required property 'name' not found" -H 'Content-Type: application/json' -d '{}' "$base/users"
  expect 500 'integer' "$base/users/7"
  expect 200 pong "$base/ping"
}

echo "== $image, auto-registered extension"
start --verbose
check

echo "== $image, extension named with --extensions"
start --extensions us.nimbusxr.axx.wiremock.openapi.OpenApiValidatorExtension
check

echo "== $image, admin endpoint"
expect 200 '"format" : 1' "$base/__admin/openapi-validation"

echo "== $image, report mode: the stub answers, the journal records the finding"
env=(-e OPENAPI_VALIDATION_MODE=report)
start --verbose
expect 201 - -H 'Content-Type: application/json' -d '{}' "$base/users"
expect 200 "required property 'name' not found" "$base/__admin/requests"

echo "smoke test passed"
