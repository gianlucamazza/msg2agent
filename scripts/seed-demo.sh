#!/usr/bin/env bash
# seed-demo.sh — populate demo tenants and usage events for dashboard testing.
#
# Creates 3 tenants (free, starter, team), issues one API key per tenant, and
# seeds 50 tool_call events per tenant so the usage chart has data to render.
#
# Requires bootstrap.sh to have run first (billing DB must exist).
#
# Usage:
#   ./scripts/seed-demo.sh
#   BILLING_DB=/path/to/billing.db ./scripts/seed-demo.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
BUILD_DIR="$PROJECT_ROOT/build"
MSG2AGENT_DIR="${MSG2AGENT_DIR:-$HOME/.msg2agent}"
BILLING_DB="${BILLING_DB:-$MSG2AGENT_DIR/billing.db}"
BILLING_ADMIN="$BUILD_DIR/billing-admin"

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log_info() { echo -e "${GREEN}[seed-demo]${NC} $1"; }
log_warn() { echo -e "${YELLOW}[seed-demo]${NC} $1"; }

if [[ ! -f "$BILLING_ADMIN" ]]; then
	echo "billing-admin binary not found at $BILLING_ADMIN — run 'make build' first" >&2
	exit 1
fi

if [[ ! -f "$BILLING_DB" ]]; then
	echo "Billing DB not found at $BILLING_DB — run 'make bootstrap' first" >&2
	exit 1
fi

DEMO_TENANTS=(
	"Demo Free|demo-free@example.com|free"
	"Demo Starter|demo-starter@example.com|starter"
	"Demo Team|demo-team@example.com|team"
)

for entry in "${DEMO_TENANTS[@]}"; do
	IFS='|' read -r name email plan <<<"$entry"

	# Skip if a tenant with this email already exists.
	if "$BILLING_ADMIN" -db "$BILLING_DB" list-tenants 2>/dev/null | grep -q "$email"; then
		log_warn "Tenant $email already exists — skipping"
		continue
	fi

	log_info "Creating tenant: $name ($plan)"
	output="$("$BILLING_ADMIN" -db "$BILLING_DB" create-tenant \
		--name "$name" --email "$email" --plan "$plan")"
	echo "$output"
	tenant_id="$(echo "$output" | grep -oP 'ID:\s+\K\S+')"

	log_info "Issuing API key for $tenant_id"
	"$BILLING_ADMIN" -db "$BILLING_DB" issue-key \
		--tenant "$tenant_id" --name "demo" >/dev/null

	log_info "Seeding 50 tool_call events for $tenant_id"
	"$BILLING_ADMIN" -db "$BILLING_DB" seed-events \
		--tenant "$tenant_id" --count 50 --event tool_call --tool demo_tool

	log_info "Seeding 10 task_submit events for $tenant_id"
	"$BILLING_ADMIN" -db "$BILLING_DB" seed-events \
		--tenant "$tenant_id" --count 10 --event task_submit --tool demo_tool
done

log_info "Demo data seeded. Run './build/billing-admin -db $BILLING_DB list-tenants' to verify."
