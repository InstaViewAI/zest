#!/usr/bin/env bash
# Exercises all four escalation flows end to end against stub Slack/Zendesk
# upstreams. Requires no real credentials.
set -euo pipefail

PORT="${PORT:-8126}"
STUB_PORT=8777
BASE="http://localhost:${PORT}/api/v1"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMP="$(mktemp -d)"

cleanup() {
	[[ -n "${APP_PID:-}"  ]] && kill "$APP_PID"  2>/dev/null || true
	[[ -n "${STUB_PID:-}" ]] && kill "$STUB_PID" 2>/dev/null || true
	rm -rf "$TMP"
}
trap cleanup EXIT

wait_for() {
	for _ in $(seq 1 60); do
		curl -sf "$1" >/dev/null 2>&1 && return 0
		perl -e 'select(undef,undef,undef,0.25)'
	done
	echo "timed out waiting for $1" >&2
	return 1
}

cd "$ROOT"

echo "==> starting stub upstreams"
go run ./scripts/stub > "$TMP/stub.log" 2>&1 &
STUB_PID=$!

echo "==> starting zest on :${PORT}"
# config.local.yaml supplies everything else; this points it at the stubs.
cat > "$TMP/secret.e2e.yaml" <<EOF
server:
  port: ${PORT}
slack:
  base_url: http://127.0.0.1:${STUB_PORT}/slack
  token: stub-token
  channel_id: C123
zendesk:
  base_url: http://127.0.0.1:${STUB_PORT}/zendesk
  subdomain: acme
  email: dev@example.com
  api_token: stub-token
  webhook_secret: ""
escalation:
  thread_ts_field_id: 111
  channel_id_field_id: 222
report:
  weekly_enabled: false
EOF
env ENVIRONMENT=local \
	CONFIG_FILE_PATH="config/config.local.yaml" \
	SECRET_FILE_PATH="$TMP/secret.e2e.yaml" \
	go run ./cmd > "$TMP/app.log" 2>&1 &
APP_PID=$!

wait_for "http://localhost:${PORT}/ping"

echo
echo "==> flow 1: escalation (creates the thread, writes the join key)"
curl -sS -X POST "$BASE/webhooks/zendesk/escalation" -H 'Content-Type: application/json' -d '{
  "ticket_id":"4242","subject":"Camera offline",
  "ticket_url":"https://acme.zendesk.com/agent/tickets/4242",
  "status":"open","priority":"urgent","brand":"InstaView",
  "requester_name":"Jamie Lee","requester_email":"jamie@example.com",
  "agent_name":"Support Agent","agent_email":"agent@example.com",
  "device_type":"Doorbell","device_model":"IV-200","device_ids":"D-1,D-2",
  "describe_issue":"Feed drops every few minutes",
  "description":"Started after the 4.2 firmware update.",
  "notes":"Customer is on the enterprise plan."}'
echo

echo "==> flow 2: comment (join key on the payload)"
curl -sS -X POST "$BASE/webhooks/zendesk/comment" -H 'Content-Type: application/json' -d '{
  "ticket_id":"4242","comment":"Still failing after a reboot","author":"Jamie Lee",
  "channel_id":"C123","ts":"1725200000.000100"}'
echo

echo "==> flow 2b: comment with NO join key (recovered from the ticket)"
curl -sS -X POST "$BASE/webhooks/zendesk/comment" -H 'Content-Type: application/json' -d '{
  "ticket_id":"4242","comment":"Payload carries no thread ts"}'
echo

echo "==> flow 3: status change"
curl -sS -X POST "$BASE/webhooks/zendesk/status" -H 'Content-Type: application/json' -d '{
  "ticket_id":"4242","status":"solved","previous_status":"pending","assignee":"Eng Team",
  "channel_id":"C123","ts":"1725200000.000100"}'
echo

echo "==> flow 4: stale-ticket report"
curl -sS -X POST "$BASE/reports/stale"
echo

echo
echo "================ what the service sent upstream ================"
cat "$TMP/stub.log"
