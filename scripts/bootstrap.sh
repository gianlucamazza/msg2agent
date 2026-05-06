#!/usr/bin/env bash
# bootstrap.sh — first-run setup for msg2agent
#
# Creates: relay identity, billing DB, first tenant, first API key.
# Idempotent — safe to re-run; existing resources are preserved.
#
# Usage:
#   ./scripts/bootstrap.sh
#   BOOTSTRAP_NAME="Alice" BOOTSTRAP_EMAIL="alice@example.com" ./scripts/bootstrap.sh
#
# Optional env:
#   BOOTSTRAP_NAME     Tenant display name (prompted if missing)
#   BOOTSTRAP_EMAIL    Tenant email address (prompted if missing)
#   BOOTSTRAP_PLAN     Billing plan: free|starter|team|enterprise (default: free)
#   MSG2AGENT_DIR      Data directory (default: ~/.msg2agent)
#   BILLING_DB         Billing DB path (default: $MSG2AGENT_DIR/billing.db)
#   WRITE_ENV_LOCAL    Write API key to .env.local if set to "true" (default: false)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
BUILD_DIR="$PROJECT_ROOT/build"
MSG2AGENT_DIR="${MSG2AGENT_DIR:-$HOME/.msg2agent}"
BILLING_DB="${BILLING_DB:-$MSG2AGENT_DIR/billing.db}"
BOOTSTRAP_PLAN="${BOOTSTRAP_PLAN:-free}"
WRITE_ENV_LOCAL="${WRITE_ENV_LOCAL:-false}"

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
CYAN='\033[0;36m'
NC='\033[0m'

log_info() { echo -e "${GREEN}[bootstrap]${NC} $1"; }
log_warn() { echo -e "${YELLOW}[bootstrap]${NC} $1"; }
log_error() { echo -e "${RED}[bootstrap]${NC} $1" >&2; }

# --- Prereq check ---

check_prereqs() {
	local missing=()
	for cmd in go make; do
		if ! command -v "$cmd" &>/dev/null; then
			missing+=("$cmd")
		fi
	done
	if [[ ${#missing[@]} -gt 0 ]]; then
		log_error "Missing required tools: ${missing[*]}"
		log_error "Install them and re-run bootstrap."
		exit 1
	fi
}

# --- Build ---

build_binaries() {
	log_info "Building binaries..."
	make -C "$PROJECT_ROOT" build --no-print-directory
	log_info "Binaries ready in $BUILD_DIR/"
}

# --- Identity ---

setup_identity() {
	mkdir -p "$MSG2AGENT_DIR"
	chmod 700 "$MSG2AGENT_DIR"

	local key_file="$MSG2AGENT_DIR/relay.key"
	if [[ -f "$key_file" ]]; then
		log_info "Relay identity already exists: $key_file"
	else
		log_info "Generating relay identity..."
		# Relay saves its identity on first start; we run it briefly with a 3s timeout.
		timeout 3 "$BUILD_DIR/relay" \
			--identity-file "$key_file" \
			--domain localhost \
			--name relay \
			--store memory \
			--addr :0 \
			2>/dev/null || true
		if [[ ! -f "$key_file" ]]; then
			log_error "Relay identity file not created at $key_file"
			log_error "Run: $BUILD_DIR/relay --identity-file $key_file --domain localhost --name relay"
			exit 1
		fi
		log_info "Identity saved: $key_file"
	fi

	RELAY_DID="$(grep -oP '"did"\s*:\s*"\K[^"]+' "$key_file" 2>/dev/null || echo 'unknown')"
	log_info "Relay DID: $RELAY_DID"
}

# --- Billing DB ---

setup_billing() {
	log_info "Initialising billing DB: $BILLING_DB"
	# Running list-tenants auto-migrates the schema via store factory.
	"$BUILD_DIR/billing-admin" -db "$BILLING_DB" list-tenants >/dev/null 2>&1 || true
	log_info "Billing DB ready"
}

# --- First tenant ---

setup_tenant() {
	local existing
	existing="$("$BUILD_DIR/billing-admin" -db "$BILLING_DB" list-tenants 2>/dev/null | tail -n +2 | grep -c . || true)"
	if [[ "$existing" -ge 1 ]]; then
		log_info "Tenant already exists — skipping creation"
		TENANT_ID="$("$BUILD_DIR/billing-admin" -db "$BILLING_DB" list-tenants 2>/dev/null | awk 'NR==2{print $1}')"
		return
	fi

	local name="${BOOTSTRAP_NAME:-}"
	local email="${BOOTSTRAP_EMAIL:-}"

	if [[ -z "$name" ]]; then
		read -rp "Tenant name [My Org]: " name
		name="${name:-My Org}"
	fi
	if [[ -z "$email" ]]; then
		read -rp "Tenant email [admin@example.com]: " email
		email="${email:-admin@example.com}"
	fi

	log_info "Creating tenant: $name <$email> (plan: $BOOTSTRAP_PLAN)"
	local output
	output="$("$BUILD_DIR/billing-admin" -db "$BILLING_DB" create-tenant \
		--name "$name" --email "$email" --plan "$BOOTSTRAP_PLAN")"
	echo "$output"
	TENANT_ID="$(echo "$output" | grep -oP 'ID:\s+\K\S+')"
}

# --- First API key ---

setup_api_key() {
	log_info "Issuing bootstrap API key for tenant $TENANT_ID..."
	local output
	output="$("$BUILD_DIR/billing-admin" -db "$BILLING_DB" issue-key \
		--tenant "$TENANT_ID" --name bootstrap)"
	API_KEY="$(echo "$output" | grep -oP 'sk_(live|test)_\S+')"

	echo ""
	echo -e "${CYAN}┌──────────────────────────────────────────────────────────────┐${NC}"
	echo -e "${CYAN}│  API KEY (shown once — store it securely)                    │${NC}"
	echo -e "${CYAN}│                                                              │${NC}"
	echo -e "${CYAN}│  $API_KEY${NC}"
	echo -e "${CYAN}│                                                              │${NC}"
	echo -e "${CYAN}└──────────────────────────────────────────────────────────────┘${NC}"
	echo ""

	if [[ "$WRITE_ENV_LOCAL" == "true" ]]; then
		local env_file="$PROJECT_ROOT/.env.local"
		echo "MSG2AGENT_API_KEY=$API_KEY" >"$env_file"
		chmod 600 "$env_file"
		log_info "API key written to $env_file (chmod 600)"
	else
		read -rp "Write API key to .env.local? [y/N] " write_local
		if [[ "${write_local,,}" == "y" ]]; then
			local env_file="$PROJECT_ROOT/.env.local"
			echo "MSG2AGENT_API_KEY=$API_KEY" >"$env_file"
			chmod 600 "$env_file"
			log_info "API key written to $env_file (chmod 600)"
		fi
	fi
}

# --- Summary ---

print_summary() {
	echo ""
	log_info "Bootstrap complete!"
	echo ""
	echo "  Relay identity : $MSG2AGENT_DIR/relay.key"
	echo "  Relay DID      : ${RELAY_DID:-unknown}"
	echo "  Billing DB     : $BILLING_DB"
	echo "  Tenant ID      : $TENANT_ID"
	echo ""
	echo "Next steps:"
	echo "  make dev                          # start relay + agents locally"
	echo "  make demo                         # add demo tenants + seed usage data"
	echo "  make smoke                        # verify all services are healthy"
	echo ""
	echo "  Or with Docker Compose:"
	echo "  docker compose up -d"
}

main() {
	check_prereqs
	build_binaries
	setup_identity
	setup_billing
	setup_tenant
	setup_api_key
	print_summary
}

main "$@"
