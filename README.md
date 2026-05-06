# msg2agent

[![CI](https://github.com/gianlucamazza/msg2agent/actions/workflows/ci.yml/badge.svg)](https://github.com/gianlucamazza/msg2agent/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/gianlucamazza/msg2agent)](https://goreportcard.com/report/github.com/gianlucamazza/msg2agent)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

The foundation for clear, secure, and verifiable communication between autonomous AI agents. Build decentralized agent networks with built-in identity, encryption, and interoperability.

## Project Status

**Beta** — production-ready for early adopters. The relay, MCP server, and billing system are stable. Breaking changes will be communicated via CHANGELOG.md.

## Features

- **Trustless Identity**: W3C DID-based agent identification (`did:wba:domain:agent:name`)
- **Secure Messaging**: End-to-end encryption with X25519-XChaCha20-Poly1305, Ed25519 signatures
- **Relay Hub**: Central message routing with WebSocket transport
- **A2A Protocol**: Google Agent-to-Agent protocol support for interoperability
- **MCP Integration**: Model Context Protocol adapter for AI assistant integration
- **Observability**: Prometheus metrics, OpenTelemetry tracing

## Architecture

```mermaid
flowchart LR
    subgraph Relay Hub
        R[Relay<br/>cmd/relay]
        REG[(Registry)]
        Q[(Offline Queue)]
        R --- REG
        R --- Q
    end

    subgraph Agents
        A[Alice<br/>did:wba:...:alice]
        B[Bob<br/>did:wba:...:bob]
        MCP1[MCP Agent 1<br/>did:wba:...:mcp1<br/>stdio]
        MCP2[MCP Agent 2<br/>did:wba:...:mcp2<br/>streamable-http]
    end

    CC[Claude Code] -->|stdio| MCP1
    OC[OpenClaw] -->|streamable-http| MCP2
    MCP1 & MCP2 & A & B <-->|WebSocket| R
    A <-.->|P2P| B
```

See [Architecture docs](docs/architecture.md) for details.

## Use Cases

- **Secure Multi-Agent Systems**: Create coordinated swarms of agents that can work together securely without sharing private keys or relying on central authorities for trust.
- **Local-First AI Assistant Extensions**: Expose your local tools and services as agents that can be securely accessed by LLMs (via MCP) or other applications.
- **Decentralized Service Mesh**: Route messages between microservices/agents purely based on DIDs, decoupling identity from network location.
- **Cross-Organization Interoperability**: Allow agents from different organizations to communicate securely using standard protocols (DID, A2A).

## Quick Start

### Use with Claude Code (one command)

```bash
# Add msg2agent as an MCP server directly from the registry
claude mcp add msg2agent -- ./mcp-server -name my-agent -relay ws://localhost:8080

# Or against a hosted relay (get an API key at msg2agent.io)
claude mcp add msg2agent -e MSG2AGENT_API_KEY=your_key -- \
  ./mcp-server -name my-agent -relay wss://relay.msg2agent.io \
  -transport streamable-http -addr :3001
```

Once added, Claude can call `list_agents`, `send_message`, `submit_task` and all other tools directly.

### Use with OpenClaw (ClawHub marketplace)

Install the published plugin from [ClawHub](https://clawhub.io/skills/msg2agent) or point OpenClaw at your own MCP server:

```json
{
  "mcpUrl": "http://localhost:3001/mcp",
  "apiKey": "msg2a_your_key_here"
}
```

### Build & run locally

```bash
go build -o relay ./cmd/relay
go build -o agent ./cmd/agent
go build -o mcp-server ./cmd/mcp-server

# Terminal 1: Start relay
./relay -addr :8080

# Terminal 2: Start first agent
./agent -name alice -relay ws://localhost:8080

# Terminal 3: Start second agent
./agent -name bob -relay ws://localhost:8080

# Terminal 4 (optional): Start MCP server for Claude / OpenClaw
./mcp-server -name my-agent -relay ws://localhost:8080 -transport streamable-http -addr :3001
```

See the [Getting Started Guide](docs/getting-started.md) for a complete walkthrough.

## Documentation

| Document                                              | Description                             |
| ----------------------------------------------------- | --------------------------------------- |
| [Getting Started](docs/getting-started.md)            | Build, run, and send your first message |
| [Architecture](docs/architecture.md)                  | System design and core concepts         |
| [Configuration](docs/operations/configuration.md)     | All configuration options               |
| [API Reference](docs/api/jsonrpc.md)                  | JSON-RPC methods and protocols          |
| [Deployment](docs/deployment/)                        | Docker, Kubernetes, TLS setup           |
| [Monitoring](docs/operations/monitoring.md)           | Prometheus, Grafana, tracing            |
| [Troubleshooting](docs/operations/troubleshooting.md) | Common issues and solutions             |
| [OpenClaw Plugin](docs/openclaw-plugin/README.md)     | OpenClaw integration via MCP            |
| [Anthropic Marketplace](docs/marketplace/anthropic.md) | Publish on Claude Marketplace          |
| [Google A2A Marketplace](docs/marketplace/google-a2a.md) | Publish on Google Cloud Agent Marketplace |
| [Billing Setup](docs/marketplace/billing-setup.md)    | Multi-tenant billing: tenants, API keys, usage |
| [Billing Client Guide](docs/marketplace/billing-client-guide.md) | API key usage, error codes, rate-limit back-off |
| [Glossary](docs/glossary.md)                          | Term definitions                        |

## Project Structure

```
cmd/
  agent/          # Agent binary
  relay/          # Relay hub binary
  mcp-server/     # MCP protocol adapter
pkg/
  agent/          # Agent implementation
  billing/        # Tenant management, API keys, usage metering
  config/         # Configuration helpers
  conversation/   # Threaded conversation storage
  crypto/         # Encryption and signing
  identity/       # DID management
  mcp/            # MCP server core (tools, resources, inbox)
  messaging/      # Message types and routing
  protocol/       # JSON-RPC wire protocol
  queue/          # Offline message queueing (store-and-forward)
  registry/       # Agent storage (memory, file, SQLite)
  security/       # Access control lists
  transport/      # WebSocket, stdio, SSE transports
  telemetry/      # Metrics and tracing
adapters/
  a2a/            # A2A protocol adapter
  mcp/            # MCP protocol adapter
infrastructure/   # Terraform configurations
scripts/          # Development/deployment/scenario scripts
test/             # Integration and E2E tests
docker-compose*.yml  # Docker Compose configurations
```

## Requirements

- Go 1.24+
- Docker (optional, for containerized deployment)

## Contributing

Contributions are welcome. Please read [CONTRIBUTING.md](CONTRIBUTING.md) for the development workflow, branch naming, and commit conventions. By participating you agree to the [Code of Conduct](CODE_OF_CONDUCT.md). For security issues, see [SECURITY.md](SECURITY.md).

## License

Apache 2.0 - See [LICENSE](LICENSE) for details.
