#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Smoke-tests a built axx-mailpit image: a rule refuses only the mail it matches, and stops
# once it is deleted.
#   usage: ./smoke-test.sh [image]        (default: axx-mailpit:dev)
set -euo pipefail

image=${1:-axx-mailpit:dev}
id=$(docker run -d -p 127.0.0.1::8025 -p 127.0.0.1::1025 "$image")
trap 'docker rm -f "$id" >/dev/null 2>&1 || true' EXIT
fail() { echo "FAIL: $*" >&2; docker logs "$id" >&2 || true; exit 1; }

for _ in $(seq 1 60); do
  [[ $(docker inspect -f '{{.State.Health.Status}}' "$id") == healthy ]] && break
  sleep 1
done
api="http://$(docker port "$id" 8025/tcp | head -1)"
smtp=$(docker port "$id" 1025/tcp | head -1)

# rcpt <address>: the SMTP reply to RCPT TO for that address.
rcpt() {
  python3 - "$smtp" "$1" <<'PY'
import smtplib, sys
host, port = sys.argv[1].rsplit(":", 1)
s = smtplib.SMTP(host, int(port), timeout=10)
s.ehlo()
s.mail("no-reply@parcels.example")
code, _ = s.rcpt(sys.argv[2])
print(code)
s.quit()
PY
}

rule=$(curl -fsS -X POST "$api/api/v1/chaos/rules" -H 'Content-Type: application/json' \
  -d '{"Stage": "recipient", "Match": "*@hawthorn-home.example", "ErrorCode": 451}')
ruleID=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["ID"])' <<<"$rule")
[[ -n $ruleID ]] || fail "no rule ID in $rule"

[[ $(rcpt orders@hawthorn-home.example) == 451 ]] || fail "the rule's recipient was not refused"
[[ $(rcpt orders@wisteria-way.example) == 250 ]] || fail "another recipient was refused"
curl -fsS "$api/api/v1/chaos/rules" | grep -q "$ruleID" || fail "the rule is not listed"

curl -fsS -X DELETE "$api/api/v1/chaos/rules/$ruleID" >/dev/null || fail "the rule could not be deleted"
[[ $(rcpt orders@hawthorn-home.example) == 250 ]] || fail "a deleted rule still refuses"
status=$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$api/api/v1/chaos/rules" -H 'Content-Type: application/json' \
  -d '{"Stage": "authentication", "Match": "*", "ErrorCode": 535}')
[[ $status == 400 ]] || fail "a rule for another stage was answered $status, not 400"

echo "axx-mailpit smoke test passed"
