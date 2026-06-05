# msg2agent

[![CI](https://github.com/gianlucamazza/msg2agent/actions/workflows/ci.yml/badge.svg)](https://github.com/gianlucamazza/msg2agent/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/gianlucamazza/msg2agent)](https://goreportcard.com/report/github.com/gianlucamazza/msg2agent)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)

The foundation for clear, secure, and verifiable communication between autonomous AI agents. Build decentralized agent networks with built-in identity, encryption, and interoperability.

## Project Status

**Beta** — production-ready for early adopters. The relay, MCP server, and billing system are stable. Breaking changes will be communicated via CHANGELOG.md.

## Features

- **Trustless Identity**: W3C DID-based agent identification (`did:wba:domain:agent:name`)
- **Secure Messaging**: End-to-end encryption with X25519-XChaCha20-Poly1305, Ed25519 signatures
- **Relay Hub**: Central message routing with WebSocket transport
- **A2A Protocol**: Google Agent-to-Agent protocol support for interoperability
- **MCP Integration**: Model Context Protocol adapter — 14 tools (`list_agents`, `send_message`, `submit_task`, …) over stdio, streamable-HTTP, and SSE
- **Multi-Tenancy & Billing**: tenant isolation, Stripe checkout/webhook, plan enforcement, hash-linked audit chain (`pkg/billing/`)
- **OAuth 2.1 + PKCE**: Dynamic Client Registration, Google IDP, JWKS, consent screen (`pkg/oauth/`)
- **Operator Dashboard**: tenant/key management UI + REST API (`cmd/dashboard/`)
- **Observability**: Prometheus metrics, OpenTelemetry tracing (v1.43.0), Grafana dashboards

## Architecture

```mermaid
flowchart LR
    subgraph RelayHub[Relay Hub — cmd/relay]
        R[Router]
        REG[(Registry\nSQLite/Memory)]
        Q[(Offline Queue)]
        OA[OAuth 2.1\n+ PKCE]
        BIL[Billing\nMiddleware]
        R --- REG
        R --- Q
        R --- OA
        R --- BIL
    end

    subgraph MCP[MCP Server — cmd/mcp-server]
        MCPS[14 tools\nstdio · HTTP · SSE]
    end

    subgraph Agents
        A[Alice\ndid:wba:...:alice]
        B[Bob\ndid:wba:...:bob]
    end

    subgraph Dashboard[Operator Dashboard — cmd/dashboard]
        DASH[REST API\n+ SPA]
    end

    CC[Claude Code] -->|stdio| MCPS
    CL[Claude.ai] -->|streamable-http| MCPS
    OC[OpenClaw] -->|streamable-http| MCPS
    MCPS <-->|WebSocket| R
    A & B <-->|WebSocket| R
    A <-.->|P2P| B
    DASH -->|REST| R
    BIL <-->|webhook| STRIPE[(Stripe)]
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

# Or against the hosted relay (get an API key at msg2agent.xyz)
claude mcp add msg2agent -e MSG2AGENT_API_KEY=your_key -- \
  ./mcp-server -name my-agent -relay wss://msg2agent.xyz \
  -transport streamable-http -addr :3001
```

Once added, Claude can call `list_agents`, `send_message`, `submit_task` and all other tools directly.

### Use with Claude.ai (custom connector)

The hosted relay ships an OAuth 2.1 + PKCE endpoint and a discoverable connector
manifest, so you can install msg2agent from any Claude surface (web, desktop, mobile)
without writing code.

1. Open Claude → **Settings → Connectors → Add Custom Connector**
2. Paste this URL:

   ```
   https://msg2agent.xyz/.well-known/mcp-connector.json
   ```

3. Click **Connect**. Claude will redirect you to sign in with Google. Approve
   the consent screen, and the 14 msg2agent tools (`list_agents`, `send_message`,
   `submit_task`, …) become available in your conversations.

No API key required — authentication is handled per-user via OAuth 2.1 with PKCE
and Dynamic Client Registration. Sign-up is free at
[msg2agent.xyz](https://msg2agent.xyz).

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
go build -o mcp-server ./cmd/mcp-server

# Terminal 1: Start relay
./relay -addr :8080

# Terminal 2: Start MCP server for Claude / OpenClaw
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
| [Billing Setup](docs/marketplace/billing-setup.md)    | Multi-tenant billing: tenants, API keys, usage |
| [Billing Client Guide](docs/marketplace/billing-client-guide.md) | API key usage, error codes, rate-limit back-off |
| [Glossary](docs/glossary.md)                          | Term definitions                        |

## Project Structure

```
cmd/
  relay/          # Relay hub binary (WebSocket router, OAuth, billing, signup)
  mcp-server/     # MCP protocol adapter (14 tools, stdio/HTTP/SSE transports)
  dashboard/      # Operator dashboard API + SPA (tenant/key management)
  billing-admin/  # CLI admin tool (create-tenant, issue-key, sync-subscription)
pkg/
  agent/          # Agent implementation (identity, handlers, task store)
  billing/        # Tenant management, API keys, metering, Stripe, audit chain
  buildinfo/      # Version/commit/date injected at build time
  config/         # Configuration helpers
  conversation/   # Threaded conversation storage (SQLite + in-memory)
  crypto/         # Encryption (X25519-XChaCha20-Poly1305) and signing (Ed25519)
  email/          # Email provider abstraction (verification, resend)
  httputil/       # Shared HTTP middleware (CSP, security headers)
  identity/       # DID management (did:wba)
  mcp/            # MCP server core (tools, resources, notifications, inbox)
  messaging/      # Message types and routing (DIDComm envelopes)
  oauth/          # OAuth 2.1 + PKCE server (DCR, Google IDP, JWKS, consent)
  protocol/       # JSON-RPC 2.0 wire protocol
  queue/          # Offline message queueing (store-and-forward, SQLite)
  registry/       # Agent storage (memory, SQLite)
  security/       # Access control lists (ACL policies)
  telemetry/      # Metrics (Prometheus) and tracing (OpenTelemetry OTLP)
  transport/      # WebSocket, stdio, SSE, TLS transports
  webui/          # Embedded frontend assets served by the relay
adapters/
  a2a/            # Google Agent-to-Agent protocol adapter
  mcp/            # MCP streamable-HTTP / stdio transport adapter
web/              # Astro + Preact + Tailwind frontend (landing, pricing, dashboard SPA)
infrastructure/   # Docker Compose overrides, Terraform, Grafana dashboards
loadtest/         # k6 load-test scenarios (MCP HTTP, relay WebSocket)
scripts/          # Development, deployment, and scenario scripts
test/             # Integration and E2E tests
docker-compose*.yml  # Docker Compose configurations (sqlite, tls, observability, p2p)
```

## Requirements

- Go 1.25+
- Docker (optional, for containerized deployment)

## Contributing

Contributions are welcome. Please read [CONTRIBUTING.md](CONTRIBUTING.md) for the development workflow, branch naming, and commit conventions. By participating you agree to the [Code of Conduct](CODE_OF_CONDUCT.md). For security issues, see [SECURITY.md](SECURITY.md).

## License

Apache 2.0 - See [LICENSE](LICENSE) for details.
