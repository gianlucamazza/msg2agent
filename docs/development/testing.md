# Testing Guide

This guide covers testing strategies and tools for msg2agent.

## Test Categories

### Unit Tests

Test individual packages in isolation:

```bash
make test-unit
# Or directly:
go test -v -race ./pkg/... ./cmd/...
```

### Integration Tests

Test component interactions:

```bash
make test-integration
# Or:
go test -v -race -tags=integration ./test/...
```

### End-to-End Tests

Test the full system with running services:

```bash
# Build binaries first
make build

# Run E2E tests
make test-e2e
# Or:
go test -v -tags=e2e ./test/...
```

### All Tests

Run the complete test suite:

```bash
make test-all
```

## Test Scenarios

Interactive test scenarios validate specific use cases:

### Relay Messaging

Test messaging through the relay hub:

```bash
# With Docker
make scenario-relay

# With native binaries
./scripts/scenarios/relay-messaging.sh --native
```

Tests:

- Relay health/readiness
- Agent registration
- Agent card accessibility
- DID validation

### P2P Direct

Test direct agent-to-agent communication:

```bash
# With Docker
make scenario-p2p

# With native binaries
./scripts/scenarios/p2p-direct.sh --native
```

Tests:

- Agent card endpoints
- Health checks
- Metrics endpoints
- Direct WebSocket connectivity

### TLS Mode

Test TLS-encrypted communication:

```bash
make scenario-tls
```

Tests:

- Certificate generation
- TLS relay connectivity
- Agent TLS connections

### MCP Integration

Test MCP server stdio interface:

```bash
make scenario-mcp
```

Tests:

- MCP server binary
- JSON-RPC initialize
- Tool listing

## Package-Specific Tests

### Security Tests

Test cryptographic and security packages:

```bash
make test-security
# Tests: pkg/security/... pkg/crypto/...
```

### A2A Adapter Tests

Test Agent-to-Agent protocol adapter:

```bash
make test-a2a
# Tests: adapters/a2a/...
```

### MCP Adapter Tests

Test Model Context Protocol adapter:

```bash
make test-mcp
# Tests: adapters/mcp/...
```

## Coverage

Generate test coverage report:

```bash
make test-coverage
```

Output:

- `coverage.out` - Raw coverage data
- `coverage.html` - HTML report (open in browser)

View coverage by package:

```bash
go tool cover -func=coverage.out
```

## Load Testing

Basic load testing:

```bash
make test-load

# With custom parameters
./scripts/test-load.sh --clients 50 --messages 1000
```

For comprehensive load testing, consider:

- [k6](https://k6.io/)
- [wrk](https://github.com/wg/wrk)
- [vegeta](https://github.com/tsenart/vegeta)

## Benchmarks

Run Go benchmarks:

```bash
# All benchmarks
go test -bench=. ./pkg/...

# Specific package
go test -bench=. ./pkg/messaging/...

# With memory stats
go test -bench=. -benchmem ./pkg/crypto/...
```

## CI Pipeline

Run the full CI pipeline locally:

```bash
make ci
```

This runs:

1. `check` - fmt, vet, lint
2. `test-all` - all test categories

Docker-based E2E in CI:

```bash
make ci-e2e
```

## Writing Tests

### Test File Organization

```
pkg/
  messaging/
    message.go
    message_test.go      # Unit tests
test/
  integration/
    messaging_test.go    # Integration tests (tag: integration)
  e2e/
    scenario_test.go     # E2E tests (tag: e2e)
```

### Build Tags

Use build tags for test categories:

```go
//go:build integration

package integration

// Integration tests here
```

### Mock Transports

The codebase includes mock transports for testing:

```go
import "github.com/gianluca/msg2agent/pkg/transport/mock"

// Create mock transport
mt := mock.NewTransport()

// Use in tests
agent, _ := agent.New(agent.Config{
    Transport: mt,
})
```

### Table-Driven Tests

Preferred test pattern:

```go
func TestMessageValidation(t *testing.T) {
    tests := []struct {
        name    string
        msg     Message
        wantErr bool
    }{
        {"valid message", Message{From: "a", To: "b"}, false},
        {"missing from", Message{To: "b"}, true},
        {"missing to", Message{From: "a"}, true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := tt.msg.Validate()
            if (err != nil) != tt.wantErr {
                t.Errorf("got err=%v, wantErr=%v", err, tt.wantErr)
            }
        })
    }
}
```

## Test Data

Test fixtures are in `testdata/`:

```
testdata/
  certs/           # TLS certificates (generated)
  prometheus.yml   # Prometheus config
```

## Debugging Tests

### Verbose Output

```bash
go test -v ./pkg/messaging/...
```

### Run Single Test

```bash
go test -v -run TestMessageRouting ./pkg/messaging/...
```

### With Race Detector

```bash
go test -race ./pkg/...
```

### Debug with Delve

```bash
dlv test ./pkg/messaging/...
```

## Observability in Tests

### Test Tracing

Enable stdout tracing for debugging:

```bash
MSG2AGENT_TRACE_STDOUT=true ./build/agent ...
```

### Metrics in Tests

Check metrics endpoints:

```bash
curl http://localhost:8080/metrics  # Relay
curl http://localhost:9091/metrics  # Agent
```

## Common Issues

### Race Conditions

Always run with `-race` flag:

```bash
go test -race ./...
```

### Flaky Tests

For time-sensitive tests, use proper synchronization:

```go
// Bad
time.Sleep(100 * time.Millisecond)

// Good
select {
case <-done:
case <-time.After(5 * time.Second):
    t.Fatal("timeout")
}
```

### Port Conflicts

Tests should use dynamic ports or unique ports per test:

```go
listener, _ := net.Listen("tcp", ":0")
port := listener.Addr().(*net.TCPAddr).Port
```
