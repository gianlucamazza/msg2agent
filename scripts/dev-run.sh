#!/usr/bin/env bash
# dev-run.sh - Start msg2agent development environment without Docker
#
# Usage:
#   ./scripts/dev-run.sh              # Start relay + alice + bob
#   ./scripts/dev-run.sh relay        # Start only relay
#   ./scripts/dev-run.sh agent alice  # Start only alice agent
#
# Environment:
#   LOG_LEVEL    - Log level (debug, info, warn, error). Default: debug
#   RELAY_PORT   - Relay port. Default: 8080
#   BUILD_FIRST  - Set to "1" to rebuild before starting. Default: 0

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
BUILD_DIR="$PROJECT_ROOT/build"

# Configuration
LOG_LEVEL="${LOG_LEVEL:-debug}"
RELAY_PORT="${RELAY_PORT:-8080}"
BUILD_FIRST="${BUILD_FIRST:-0}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log_info() { echo -e "${GREEN}[INFO]${NC} $1"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }
log_section() { echo -e "\n${BLUE}=== $1 ===${NC}"; }

# Cleanup function
cleanup() {
    log_section "Cleaning up"
    if [[ -n "${RELAY_PID:-}" ]]; then
        log_info "Stopping relay (PID: $RELAY_PID)"
        kill "$RELAY_PID" 2>/dev/null || true
    fi
    if [[ -n "${ALICE_PID:-}" ]]; then
        log_info "Stopping alice (PID: $ALICE_PID)"
        kill "$ALICE_PID" 2>/dev/null || true
    fi
    if [[ -n "${BOB_PID:-}" ]]; then
        log_info "Stopping bob (PID: $BOB_PID)"
        kill "$BOB_PID" 2>/dev/null || true
    fi
    wait 2>/dev/null || true
    log_info "All processes stopped"
}

trap cleanup EXIT INT TERM

# Build if needed
build_if_needed() {
    if [[ "$BUILD_FIRST" == "1" ]] || [[ ! -f "$BUILD_DIR/relay" ]] || [[ ! -f "$BUILD_DIR/agent" ]]; then
        log_section "Building binaries"
        cd "$PROJECT_ROOT"
        make build
        log_info "Build complete"
    fi
}

# Start relay hub
start_relay() {
    log_info "Starting relay on :$RELAY_PORT"
    "$BUILD_DIR/relay" \
        -addr ":$RELAY_PORT" \
        -log-level "$LOG_LEVEL" \
        -store memory &
    RELAY_PID=$!
    sleep 1

    if ! kill -0 "$RELAY_PID" 2>/dev/null; then
        log_error "Relay failed to start"
        exit 1
    fi
    log_info "Relay started (PID: $RELAY_PID)"
}

# Start an agent
start_agent() {
    local name="$1"
    local http_port="$2"
    local p2p_port="$3"
    local metrics_port="$4"

    log_info "Starting agent '$name' (HTTP: $http_port, P2P: $p2p_port, Metrics: $metrics_port)"
    "$BUILD_DIR/agent" \
        -name "$name" \
        -domain "$name.local" \
        -relay "ws://localhost:$RELAY_PORT" \
        -http ":$http_port" \
        -listen ":$p2p_port" \
        -metrics ":$metrics_port" \
        -log-level "$LOG_LEVEL" &

    eval "${name^^}_PID=$!"
    sleep 1
    log_info "Agent '$name' started (PID: ${!${name^^}_PID:-unknown})"
}

# Wait for service health
wait_for_health() {
    local url="$1"
    local name="$2"
    local max_attempts="${3:-30}"

    log_info "Waiting for $name to be healthy..."
    for i in $(seq 1 "$max_attempts"); do
        if curl -sf "$url" > /dev/null 2>&1; then
            log_info "$name is healthy"
            return 0
        fi
        sleep 1
    done
    log_error "$name failed to become healthy"
    return 1
}

# Main
main() {
    cd "$PROJECT_ROOT"

    local mode="${1:-all}"

    build_if_needed

    case "$mode" in
        relay)
            log_section "Starting Relay Only"
            start_relay
            wait_for_health "http://localhost:$RELAY_PORT/health" "Relay"
            log_section "Relay Running"
            echo "  Health: http://localhost:$RELAY_PORT/health"
            echo "  Metrics: http://localhost:$RELAY_PORT/metrics"
            echo ""
            echo "Press Ctrl+C to stop"
            wait
            ;;

        agent)
            local agent_name="${2:-alice}"
            log_section "Starting Agent: $agent_name"
            case "$agent_name" in
                alice)
                    start_agent "alice" 8081 8082 9091
                    wait_for_health "http://localhost:9091/health" "Alice"
                    echo "  Agent Card: http://localhost:8081/.well-known/agent.json"
                    ;;
                bob)
                    start_agent "bob" 8083 8084 9092
                    wait_for_health "http://localhost:9092/health" "Bob"
                    echo "  Agent Card: http://localhost:8083/.well-known/agent.json"
                    ;;
                *)
                    log_error "Unknown agent: $agent_name"
                    exit 1
                    ;;
            esac
            echo ""
            echo "Press Ctrl+C to stop"
            wait
            ;;

        all|*)
            log_section "Starting Full Development Environment"

            # Start relay
            start_relay
            wait_for_health "http://localhost:$RELAY_PORT/health" "Relay"

            # Start agents
            start_agent "alice" 8081 8082 9091
            start_agent "bob" 8083 8084 9092

            # Wait for agents
            wait_for_health "http://localhost:9091/health" "Alice"
            wait_for_health "http://localhost:9092/health" "Bob"

            log_section "Development Environment Ready"
            echo ""
            echo "Services:"
            echo "  Relay:        ws://localhost:$RELAY_PORT"
            echo "  Relay Health: http://localhost:$RELAY_PORT/health"
            echo ""
            echo "  Alice Card:    http://localhost:8081/.well-known/agent.json"
            echo "  Alice Metrics: http://localhost:9091/metrics"
            echo ""
            echo "  Bob Card:      http://localhost:8083/.well-known/agent.json"
            echo "  Bob Metrics:   http://localhost:9092/metrics"
            echo ""
            echo "Press Ctrl+C to stop all services"
            wait
            ;;
    esac
}

main "$@"
