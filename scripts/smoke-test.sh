#!/usr/bin/env bash
# smoke-test.sh — post-deploy verification using curl only.
#
# Checks health, readiness, metrics, and an authenticated MCP call.
# Exits non-zero if any required check fails.
#
# Usage:
#   ./scripts/smoke-test.sh
#   RELAY_URL=https://relay.example.com API_KEY=sk_live_... ./scripts/smoke-test.sh
#
# Environment:
#   RELAY_URL      Relay base URL (default: http://localhost:8080)
#   MCP_URL        MCP server base URL (default: http://localhost:8081)
#   DASHBOARD_URL  Dashboard base URL (optional; skipped if empty)
#   API_KEY        Bearer token for authenticated checks (optional)

set -euo pipefail

RELAY_URL="${RELAY_URL:-http://localhost:8080}"
MCP_URL="${MCP_URL:-http://localhost:8081}"
DASHBOARD_URL="${DASHBOARD_URL:-}"
API_KEY="${API_KEY:-}"

GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m'

PASS=0
FAIL=0

pass() {
	echo -e "  ${GREEN}✓${NC} $1"
	((PASS++)) || true
}
fail() {
	echo -e "  ${RED}✗${NC} $1"
	((FAIL++)) || true
}
skip() { echo -e "  ${YELLOW}~${NC} $1 (skipped)"; }

check_http() {
	local label="$1"
	local url="$2"
	local expected_code="${3:-200}"
	local body_match="${4:-}"

	local response
	response="$(curl -sf -o /tmp/smoke_body -w "%{http_code}" --max-time 5 "$url" 2>/dev/null || echo "000")"

	if [[ "$response" != "$expected_code" ]]; then
		fail "$label — HTTP $response (expected $expected_code) [$url]"
		return
	fi

	if [[ -n "$body_match" ]] && ! grep -q "$body_match" /tmp/smoke_body 2>/dev/null; then
		fail "$label — body missing '$body_match' [$url]"
		return
	fi

	pass "$label"
}

echo ""
echo "msg2agent smoke test"
echo "  relay     : $RELAY_URL"
echo "  mcp       : $MCP_URL"
echo "  dashboard : ${DASHBOARD_URL:-<not configured>}"
echo "  api_key   : ${API_KEY:+<set>}${API_KEY:-<not set>}"
echo ""

# ── Relay ─────────────────────────────────────────────────────────────────────

echo "Relay"
check_http "GET /health" "$RELAY_URL/health" 200 "ok"
check_http "GET /ready" "$RELAY_URL/ready" 200 "relay_did"
check_http "GET /metrics" "$RELAY_URL/metrics" 200 "relay_websocket_connections"

# ── MCP Server ────────────────────────────────────────────────────────────────

echo ""
echo "MCP Server"
check_http "GET /health" "$MCP_URL/health" 200
check_http "GET /metrics" "$MCP_URL/metrics" 200 "billing_usage_events_total"

if [[ -n "$API_KEY" ]]; then
	payload='{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}'
	response="$(curl -sf -o /tmp/smoke_mcp -w "%{http_code}" \
		--max-time 5 \
		-X POST "$MCP_URL/mcp" \
		-H "Content-Type: application/json" \
		-H "Authorization: Bearer $API_KEY" \
		-d "$payload" 2>/dev/null || echo "000")"
	if [[ "$response" == "200" ]] && grep -q '"tools"' /tmp/smoke_mcp 2>/dev/null; then
		pass "POST /mcp tools/list — authenticated"
	else
		fail "POST /mcp tools/list — HTTP $response"
	fi
else
	skip "POST /mcp tools/list (set API_KEY to enable)"
fi

# ── Dashboard ─────────────────────────────────────────────────────────────────

echo ""
echo "Dashboard"
if [[ -n "$DASHBOARD_URL" ]]; then
	check_http "GET /health" "$DASHBOARD_URL/health" 200
else
	skip "dashboard checks (set DASHBOARD_URL to enable)"
fi

# ── Summary ───────────────────────────────────────────────────────────────────

echo ""
echo "──────────────────────────────────"
echo -e "  Passed : ${GREEN}$PASS${NC}"
echo -e "  Failed : ${RED}$FAIL${NC}"
echo "──────────────────────────────────"
echo ""

if [[ "$FAIL" -gt 0 ]]; then
	exit 1
fi
