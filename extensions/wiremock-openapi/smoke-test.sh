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
    -v "$here/example/grpc:/home/wiremock/grpc:ro" \
    -v "$here/example/graphql:/var/graphql:ro" \
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

echo "== $image, gRPC: a unary call to a stub of example/grpc's Ping service"
# An empty request message is a gRPC frame of five zero bytes; the answer's frame holds the
# stub's status, and its trailers grpc-status 0.
answer=$(printf '\0\0\0\0\0' | curl -sS --http2-prior-knowledge -D - --data-binary @- \
  -H 'content-type: application/grpc' -H 'te: trailers' "$base/smoke.v1.Ping/Ping" | tr -d '\0' | tr -c '[:print:]\n' ' ')
printf '%s\n' "$answer" | grep -q 'grpc-status: 0' || fail "the gRPC call did not answer OK: $answer"
printf '%s\n' "$answer" | grep -q 'pong-grpc' || fail "the gRPC call did not answer the stub: $answer"
echo "/smoke.v1.Ping/Ping -> OK pong-grpc"

echo "== $image, GraphQL: a subgraph mocked field by field from example/graphql/shops.graphql"
env=(-e GRAPHQL_SCHEMA_SOURCE=/var/graphql/shops.graphql)
start --verbose
expect 200 'type Shop' -H 'Content-Type: application/json' -d '{"query": "{ _service { sdl } }"}' "$base/graphql"
expect 200 '"name":"Maple Home"' -H 'Content-Type: application/json' \
  -d '{"query": "query ($r: [_Any!]!) { _entities(representations: $r) { ... on Shop { name tier } } }", "variables": {"r": [{"__typename": "Shop", "id": "maple-crafts"}]}}' "$base/graphql"
expect 200 '"_entities":[null]' -H 'Content-Type: application/json' \
  -d '{"query": "query ($r: [_Any!]!) { _entities(representations: $r) { ... on Shop { name } } }", "variables": {"r": [{"__typename": "Shop", "id": "hawthorn-home"}]}}' "$base/graphql"
env=()

echo "== $image, models: example/mappings/parcel-assistant.json answered in each provider's format"
start --verbose
ask='"messages": [{"role": "user", "content": "Where is PX-SMOKE-8001?"}]'
expect 200 '"content":"Your parcel PX-SMOKE-8001 is out for delivery."' -H 'Content-Type: application/json' \
  -d "{\"model\": \"gpt-4.1-mini\", $ask}" "$base/v1/chat/completions"
expect 200 'data: [DONE]' -H 'Content-Type: application/json' \
  -d "{\"model\": \"gpt-4.1-mini\", \"stream\": true, $ask}" "$base/v1/chat/completions"
expect 200 'event: message_stop' -H 'Content-Type: application/json' -H 'anthropic-version: 2023-06-01' \
  -d "{\"model\": \"claude-sonnet-4-5\", \"max_tokens\": 256, \"stream\": true, $ask}" "$base/v1/messages"
expect 200 '"text":"Your parcel PX-SMOKE-8001 is out for delivery."' -H 'Content-Type: application/json' \
  -d '{"contents": [{"role": "user", "parts": [{"text": "Where is PX-SMOKE-8001?"}]}]}' "$base/v1beta/models/gemini-2.5-flash:generateContent"
expect 200 '"done":true' -H 'Content-Type: application/json' -d "{\"model\": \"llama3.2\", $ask}" "$base/api/chat"
type=$(curl -sS -o /dev/null -w '%{content_type}' -H 'Content-Type: application/json' \
  -d '{"messages": [{"role": "user", "content": [{"text": "Where is PX-SMOKE-8001?"}]}]}' "$base/model/eu.amazon.nova-lite-v1%3A0/converse-stream")
[[ $type == application/vnd.amazon.eventstream ]] || fail "Bedrock's converse-stream answered $type, not an AWS event stream"
echo "/model/eu.amazon.nova-lite-v1:0/converse-stream -> 200 $type"

echo "== $image, a root dir without a grpc folder: REST only"
start --root-dir /tmp
expect 404 - -H 'content-type: application/grpc' --http2-prior-knowledge --data-binary @/dev/null "$base/smoke.v1.Ping/Ping"

echo "== $image, admin endpoint"
expect 200 '"format" : 1' "$base/__admin/openapi-validation"

echo "== $image, report mode: the stub answers, the journal records the finding"
env=(-e OPENAPI_VALIDATION_MODE=report)
start --verbose
expect 201 - -H 'Content-Type: application/json' -d '{}' "$base/users"
expect 200 "required property 'name' not found" "$base/__admin/requests"

echo "smoke test passed"
