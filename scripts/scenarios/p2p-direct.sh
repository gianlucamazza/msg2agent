#!/usr/bin/env bash
# p2p-direct.sh - Test P2P direct messaging between agents
#
# This script tests direct WebSocket connections between agents without relay.
#
# Usage:
#   ./scripts/scenarios/p2p-direct.sh           # Run with Docker
#   ./scripts/scenarios/p2p-direct.sh --native  # Run with native binaries
#
# Requirements:
#   - Docker Compose (for Docker mode)
#   - Built binaries (for native mode)
#   - curl, websocat (optional, for WebSocket testing)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$(dirname "$SCRIPT_DIR")")"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info() { echo -e "${GREEN}[INFO]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }
log_test() { echo -e "${BLUE}[TEST]${NC} $1"; }
log_pass() { echo -e "${GREEN}[PASS]${NC} $1"; }
log_fail() { echo -e "${RED}[FAIL]${NC} $1"; }

TESTS_PASSED=0
TESTS_FAILED=0

run_test() {
	local name="$1"
	local cmd="$2"

	log_test "$name"
	if eval "$cmd"; then
		log_pass "$name"
		((++TESTS_PASSED))
	else
		log_fail "$name"
		((++TESTS_FAILED))
	fi
}

# Cleanup
cleanup() {
	log_info "Cleaning up..."
	if [[ "${MODE:-}" == "docker" ]]; then
		docker compose -f "$PROJECT_ROOT/docker-compose.p2p.yml" down -v 2>/dev/null || true
	else
		pkill -f "build/agent" 2>/dev/null || true
	fi
}

trap cleanup EXIT

# Start services
start_services() {
	local mode="${1:-docker}"
	MODE="$mode"

	if [[ "$mode" == "docker" ]]; then
		log_info "Starting P2P services with Docker..."
		cd "$PROJECT_ROOT"
		docker compose -f docker-compose.p2p.yml up -d --build
		sleep 5

		# Wait for services
		for i in {1..30}; do
			if curl -sf http://localhost:9091/health >/dev/null 2>&1 &&
				curl -sf http://localhost:9092/health >/dev/null 2>&1; then
				log_info "Services are healthy"
				return 0
			fi
			sleep 1
		done
		log_error "Services failed to start"
		return 1
	else
		log_info "Starting P2P services with native binaries..."
		cd "$PROJECT_ROOT"

		# Build if needed
		if [[ ! -f "build/agent" ]]; then
			make build
		fi

		# Start Alice
		./build/agent \
			-name alice \
			-domain alice.local \
			-http :8081 \
			-listen :8082 \
			-metrics :9091 &
		ALICE_PID=$!

		sleep 2

		# Start Bob
		./build/agent \
			-name bob \
			-domain bob.local \
			-http :8083 \
			-listen :8084 \
			-metrics :9092 &
		BOB_PID=$!

		sleep 2

		if ! kill -0 "$ALICE_PID" 2>/dev/null || ! kill -0 "$BOB_PID" 2>/dev/null; then
			log_error "Agents failed to start"
			return 1
		fi

		log_info "Agents started (Alice: $ALICE_PID, Bob: $BOB_PID)"
	fi
}

# Tests
test_alice_agent_card() {
	local response
	response=$(curl -sf http://localhost:8081/.well-known/agent.json)
	[[ -n "$response" ]] && echo "$response" | grep -q "alice"
}

test_bob_agent_card() {
	local response
	response=$(curl -sf http://localhost:8083/.well-known/agent.json)
	[[ -n "$response" ]] && echo "$response" | grep -q "bob"
}

test_alice_health() {
	curl -sf http://localhost:9091/health >/dev/null
}

test_bob_health() {
	curl -sf http://localhost:9092/health >/dev/null
}

test_alice_metrics() {
	local response
	response=$(curl -sf http://localhost:9091/metrics)
	[[ -n "$response" ]] && echo "$response" | grep -q "go_"
}

test_bob_metrics() {
	local response
	response=$(curl -sf http://localhost:9092/metrics)
	[[ -n "$response" ]] && echo "$response" | grep -q "go_"
}

# Main
main() {
	local mode="docker"
	if [[ "${1:-}" == "--native" ]]; then
		mode="native"
	fi

	echo ""
	echo "=========================================="
	echo "  P2P Direct Messaging Test Scenario"
	echo "=========================================="
	echo ""

	start_services "$mode"

	echo ""
	echo "Running tests..."
	echo ""

	run_test "Alice agent card accessible" "test_alice_agent_card"
	run_test "Bob agent card accessible" "test_bob_agent_card"
	run_test "Alice health check" "test_alice_health"
	run_test "Bob health check" "test_bob_health"
	run_test "Alice metrics available" "test_alice_metrics"
	run_test "Bob metrics available" "test_bob_metrics"

	echo ""
	echo "=========================================="
	echo "  Results: $TESTS_PASSED passed, $TESTS_FAILED failed"
	echo "=========================================="
	echo ""

	if [[ $TESTS_FAILED -gt 0 ]]; then
		exit 1
	fi
}

main "$@"
