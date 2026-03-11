# Getting Started

This guide walks you through building msg2agent, running a local setup, and sending your first message between agents.

## Prerequisites

- Go 1.24 or later
- Git

Optional:

- Docker and Docker Compose (for containerized deployment)
- curl or wscat (for testing)

## Build from Source

Clone and build the binaries:

```bash
git clone https://github.com/gianluca/msg2agent.git
cd msg2agent

go build -o relay ./cmd/relay
go build -o agent ./cmd/agent
```

This produces two binaries:

- `relay` - The message routing hub
- `agent` - A standalone agent

## Run Locally

You'll need three terminals.

### Terminal 1: Start the Relay

```bash
./relay -addr :8080 -log-level debug
```

Expected output:

```
INFO server started addr=:8080
```

### Terminal 2: Start Alice (Agent 1)

```bash
./agent -name alice -relay ws://localhost:8080 -log-level debug
```

Expected output:

```
INFO agent started name=alice did=did:wba:localhost:agent:alice
INFO connected to relay addr=ws://localhost:8080
```

### Terminal 3: Start Bob (Agent 2)

```bash
./agent -name bob -relay ws://localhost:8080 -log-level debug
```

Expected output:

```
INFO agent started name=bob did=did:wba:localhost:agent:bob
INFO connected to relay addr=ws://localhost:8080
```

## Send Your First Message

With the relay and both agents running, you can send messages between them using the MCP server or by connecting directly to the relay WebSocket.

### Using curl (HTTP API)

If you started agents with HTTP enabled:

```bash
# Start Alice with HTTP server
./agent -name alice -relay ws://localhost:8080 -http :8081

# Get Alice's agent card
curl http://localhost:8081/.well-known/agent.json
```

### Using WebSocket

Connect to the relay and send a JSON-RPC message:

```bash
# Using wscat (npm install -g wscat)
wscat -c ws://localhost:8080
```

Discover registered agents to verify connection:

```json
{ "jsonrpc": "2.0", "method": "relay.discover", "id": "1" }
```

Expected response (array of registered agents):

```json
{
  "jsonrpc": "2.0",
  "result": [
    {
      "did": "did:wba:localhost:agent:alice",
      "display_name": "alice"
    }
  ],
  "id": "1"
}
```

### Using the MCP Server

The MCP server lets AI assistants interact with the agent network. Build it alongside the other binaries:

```bash
go build -o mcp-server ./cmd/mcp-server
```

**Stdio mode** (for Claude Code — add to `.mcp.json`):

```json
{
  "mcpServers": {
    "msg2agent": {
      "command": "./mcp-server",
      "args": ["-name", "my-agent", "-relay", "ws://localhost:8080"]
    }
  }
}
```

**HTTP mode** (for OpenClaw plugin or other HTTP clients):

```bash
./mcp-server \
  -name openclaw \
  -relay ws://localhost:8080 \
  -transport streamable-http \
  -addr :3001
```

The MCP endpoint will be available at `http://localhost:3001/mcp`. See [MCP Server Configuration](operations/configuration.md#mcp-server-configuration) for all options, or the [OpenClaw Plugin guide](openclaw-plugin/README.md) for OpenClaw integration.

## Understanding the Components

### Relay

The relay is the central hub that:

- Accepts WebSocket connections from agents
- Routes messages between agents by DID
- Maintains an agent registry for discovery
- Provides health and metrics endpoints

### Agent

Each agent:

- Has a unique DID (e.g., `did:wba:localhost:agent:alice`)
- Connects to the relay for message routing
- Can expose an HTTP server with its agent card
- Handles incoming JSON-RPC requests

### Message Flow

```
Alice                    Relay                    Bob
  |                        |                       |
  |-- connect ------------>|                       |
  |                        |<----- connect --------|
  |                        |                       |
  |-- message (to Bob) --->|                       |
  |                        |-- route by DID ------>|
  |                        |                       |
  |                        |<---- response --------|
  |<-- response -----------|                       |
```

## Docker Quick Start

For a quicker setup using Docker Compose:

```bash
# Create docker-compose.yml (see docs/deployment/docker.md)

# Start all services
docker-compose up -d

# Check logs
docker-compose logs -f

# Stop
docker-compose down
```

## Configuration

Key configuration options:

| Flag         | Environment Variable   | Description                     |
| ------------ | ---------------------- | ------------------------------- |
| `-name`      | `MSG2AGENT_NAME`       | Agent name (required for agent) |
| `-relay`     | `MSG2AGENT_RELAY`      | Relay WebSocket URL             |
| `-addr`      | `MSG2AGENT_RELAY_ADDR` | Relay listen address            |
| `-log-level` | `MSG2AGENT_LOG_LEVEL`  | debug, info, warn, error        |

See [Configuration Guide](operations/configuration.md) for all options.

## Next Steps

- [Architecture](architecture.md) - Understand the system design
- [API Reference](api/jsonrpc.md) - Learn the JSON-RPC methods
- [Docker Deployment](deployment/docker.md) - Run in containers
- [TLS Setup](deployment/tls-setup.md) - Enable encryption
- [Monitoring](operations/monitoring.md) - Set up observability
