# Development Setup

This guide covers setting up msg2agent for local development.

## Prerequisites

- Go 1.24+
- Docker and Docker Compose
- Make
- OpenSSL (for TLS certificate generation)

## Quick Start

```bash
# Clone the repository
git clone https://github.com/gianluca/msg2agent.git
cd msg2agent

# Build binaries
make build

# Start development environment (native)
make dev

# Or use Docker Compose
make dev-up
```

## Development Modes

### Native Development

Run services directly on your machine without Docker:

```bash
# Build and start relay + 2 agents
make dev

# Start only specific components
./scripts/dev-run.sh relay        # Only relay
./scripts/dev-run.sh agent alice  # Only alice agent
```

Default ports (native mode):

| Service       | Port | Description               |
| ------------- | ---- | ------------------------- |
| Relay WS      | 8080 | WebSocket endpoint        |
| Relay Health  | 8080 | /health, /ready, /metrics |
| Alice HTTP    | 8081 | Agent card                |
| Alice P2P     | 8082 | Direct WebSocket          |
| Alice Metrics | 9091 | Prometheus metrics        |
| Bob HTTP      | 8083 | Agent card                |
| Bob P2P       | 8084 | Direct WebSocket          |
| Bob Metrics   | 9092 | Prometheus metrics        |

### Docker Development

Use Docker Compose for isolated development:

```bash
# Start base environment (relay + alice + bob)
make dev-up

# View logs
make dev-logs

# Check container status
make dev-ps

# Stop and cleanup
make dev-down
```

## Docker Compose Profiles

### Base Configuration

```bash
docker compose up -d
```

Services: relay, alice, bob (in-memory store)

### SQLite Persistence

```bash
make compose-sqlite
# Or manually:
docker compose -f docker-compose.yml -f docker-compose.sqlite.yml up -d
```

Adds persistent SQLite storage for the relay.

### TLS Enabled

```bash
make compose-tls
# Or manually:
./scripts/setup-certs.sh  # Generate certificates first
docker compose -f docker-compose.yml -f docker-compose.tls.yml up -d
```

Enables TLS for all services. Certificates are stored in `testdata/certs/`.

### P2P Mode (No Relay)

```bash
make compose-p2p
# Or:
docker compose -f docker-compose.p2p.yml up -d
```

Runs agents in direct P2P mode without a relay hub.

### Observability Stack

```bash
make compose-observability
# Or:
docker compose -f docker-compose.yml -f docker-compose.observability.yml up -d
```

Adds:

- Prometheus: http://localhost:9090
- Jaeger: http://localhost:16686

## Environment Variables

Copy `.env.example` to `.env` and customize:

```bash
cp .env.example .env
```

Key variables:

| Variable                  | Default     | Description           |
| ------------------------- | ----------- | --------------------- |
| `MSG2AGENT_RELAY_ADDR`    | `:8080`     | Relay listen address  |
| `MSG2AGENT_NAME`          | `agent`     | Agent name            |
| `MSG2AGENT_DOMAIN`        | `localhost` | Domain for DID        |
| `MSG2AGENT_RELAY`         | (empty)     | Relay WebSocket URL   |
| `MSG2AGENT_STORE`         | `memory`    | Store type            |
| `MSG2AGENT_LOG_LEVEL`     | `debug`     | Log level             |
| `MSG2AGENT_TLS`           | `false`     | Enable TLS            |
| `MSG2AGENT_OTLP_ENDPOINT` | (empty)     | OTLP tracing endpoint |

## TLS Certificates

Generate test certificates:

```bash
./scripts/setup-certs.sh

# Regenerate (clean first)
./scripts/setup-certs.sh --clean
```

Generated files in `testdata/certs/`:

- `ca.crt` / `ca.key` - Certificate Authority
- `server.crt` / `server.key` - Server certificate
- `client.crt` / `client.key` - Client certificate (for mTLS)

## Building

```bash
# Build all binaries
make build

# Build specific binary
make build-relay
make build-agent
make build-mcp

# Build Docker images
make docker-build
```

Output binaries are in `./build/`:

- `relay` - Relay hub server
- `agent` - Agent executable
- `mcp-server` - MCP server for AI integration

## IDE Setup

### VS Code

Recommended extensions:

- Go (golang.go)
- Docker (ms-azuretools.vscode-docker)

### GoLand

Import the project and Go modules will be automatically configured.

## Troubleshooting

### Port already in use

```bash
# Find process using port
lsof -i :8080

# Kill it
kill -9 <PID>

# Or use different ports
RELAY_PORT=9080 make dev
```

### Docker network issues

```bash
# Recreate networks
docker compose down -v
docker network prune
docker compose up -d
```

### Build failures

```bash
# Clean build artifacts
make clean

# Update dependencies
go mod tidy

# Rebuild
make build
```

## Next Steps

- [Testing Guide](testing.md) - Learn about testing strategies
- [Architecture](../architecture.md) - Understand the system design
- [API Reference](../api/) - API documentation
