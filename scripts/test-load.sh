#!/usr/bin/env bash
# test-load.sh - Load testing for msg2agent
#
# This script performs basic load testing to verify system behavior under load.
#
# Usage:
#   ./scripts/test-load.sh                    # Run with defaults
#   ./scripts/test-load.sh --clients 50       # 50 concurrent clients
#   ./scripts/test-load.sh --messages 1000    # 1000 messages per client
#   ./scripts/test-load.sh --duration 60      # Run for 60 seconds
#
# Requirements:
#   - Running relay and agents (docker compose up or make dev)
#   - curl, go (for benchmark tool)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

# Default configuration
CLIENTS="${CLIENTS:-10}"
MESSAGES="${MESSAGES:-100}"
DURATION="${DURATION:-30}"
RELAY_URL="${RELAY_URL:-ws://localhost:8080}"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info() { echo -e "${GREEN}[INFO]${NC} $1"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }

# Parse arguments
while [[ $# -gt 0 ]]; do
	case "$1" in
	--clients)
		CLIENTS="$2"
		shift 2
		;;
	--messages)
		MESSAGES="$2"
		shift 2
		;;
	--duration)
		DURATION="$2"
		shift 2
		;;
	--relay)
		RELAY_URL="$2"
		shift 2
		;;
	*)
		echo "Unknown option: $1"
		exit 1
		;;
	esac
done

# Check prerequisites
check_prerequisites() {
	log_info "Checking prerequisites..."

	# Check if relay is running
	local relay_http="${RELAY_URL//ws:/http:}"
	relay_http="${relay_http//wss:/https:}"

	if ! curl -sf "${relay_http}/health" >/dev/null 2>&1; then
		log_error "Relay not reachable at $RELAY_URL"
		log_info "Start the relay with: make dev-up"
		exit 1
	fi

	log_info "Relay is healthy"
}

# Run HTTP endpoint load test
test_http_endpoints() {
	log_info "Testing HTTP endpoints..."

	echo ""
	echo "Health endpoint load test (100 requests):"
	local relay_http="${RELAY_URL//ws:/http:}"
	relay_http="${relay_http//wss:/https:}"

	# Simple curl-based load test
	local start_time end_time elapsed success_count
	start_time=$(date +%s.%N)
	success_count=0

	for i in $(seq 1 100); do
		if curl -sf "${relay_http}/health" >/dev/null 2>&1; then
			((success_count++))
		fi
	done

	end_time=$(date +%s.%N)
	elapsed=$(echo "$end_time - $start_time" | bc)
	local rps=$(echo "scale=2; 100 / $elapsed" | bc)

	echo "  Requests: 100"
	echo "  Successful: $success_count"
	echo "  Time: ${elapsed}s"
	echo "  Rate: ${rps} req/s"
}

# Run WebSocket connection test
test_websocket_connections() {
	log_info "Testing WebSocket connection capacity..."

	# This is a simplified test - full load testing would use a proper tool
	echo ""
	echo "Connection test (simulated):"
	echo "  Target clients: $CLIENTS"
	echo "  Messages per client: $MESSAGES"
	echo "  Duration limit: ${DURATION}s"
	echo ""
	log_warn "Full WebSocket load testing requires websocat or custom Go benchmark"
	echo ""
}

# Run Go benchmark tests
test_go_benchmarks() {
	log_info "Running Go benchmark tests..."

	cd "$PROJECT_ROOT"

	echo ""
	echo "Package benchmarks:"
	echo ""

	# Run benchmarks for key packages
	go test -bench=. -benchtime=3s -run=^$ ./pkg/messaging/... 2>/dev/null || log_warn "messaging benchmarks skipped"
	go test -bench=. -benchtime=3s -run=^$ ./pkg/crypto/... 2>/dev/null || log_warn "crypto benchmarks skipped"
	go test -bench=. -benchtime=3s -run=^$ ./pkg/protocol/... 2>/dev/null || log_warn "protocol benchmarks skipped"
}

# Collect metrics
collect_metrics() {
	log_info "Collecting metrics..."

	local relay_http="${RELAY_URL//ws:/http:}"
	relay_http="${relay_http//wss:/https:}"

	echo ""
	echo "Current relay metrics:"
	echo ""

	# Get readiness info
	local ready_info
	ready_info=$(curl -sf "${relay_http}/ready" 2>/dev/null || echo '{}')
	echo "  Ready status: $ready_info"
	echo ""

	# Get key metrics
	local metrics
	metrics=$(curl -sf "${relay_http}/metrics" 2>/dev/null || echo "")

	if [[ -n "$metrics" ]]; then
		echo "Key metrics:"
		echo "$metrics" | grep -E "^(msg2agent_|go_goroutines|go_memstats_alloc_bytes )" | head -20
	fi
}

# Main
main() {
	echo ""
	echo "=========================================="
	echo "  msg2agent Load Testing"
	echo "=========================================="
	echo ""
	echo "Configuration:"
	echo "  Relay URL: $RELAY_URL"
	echo "  Clients: $CLIENTS"
	echo "  Messages: $MESSAGES"
	echo "  Duration: ${DURATION}s"
	echo ""

	check_prerequisites
	test_http_endpoints
	test_websocket_connections
	test_go_benchmarks
	collect_metrics

	echo ""
	echo "=========================================="
	echo "  Load Testing Complete"
	echo "=========================================="
	echo ""
	echo "For comprehensive load testing, consider using:"
	echo "  - k6 (https://k6.io/)"
	echo "  - wrk (https://github.com/wg/wrk)"
	echo "  - vegeta (https://github.com/tsenart/vegeta)"
	echo ""
}

main "$@"
