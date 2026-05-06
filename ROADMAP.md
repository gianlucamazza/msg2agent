# Roadmap

## Shipped

### Roadmap I — Core Messaging
- DID-based agent identity (`did:wba:`)
- Ed25519 signing and X25519-XChaCha20-Poly1305 encryption
- Relay hub with WebSocket transport and offline queue
- JSON-RPC wire protocol

### Roadmap II — MCP and Interoperability
- MCP server adapter (stdio + streamable-HTTP)
- A2A protocol adapter for Google Agent-to-Agent interoperability
- Prometheus metrics and OpenTelemetry tracing
- Security headers, DID allowlist, govulncheck, gosec, fuzz tests

### Roadmap III — Billing and Project Completion
- SaaS multi-tenant billing: tenant isolation, API key auth, OAuth2/JWT
- Stripe integration: subscription and usage-based billing
- SQLite and Postgres backends (pluggable)
- Usage metering, quota enforcement, immutable audit chain
- Dashboard-ready REST API for tenant and usage management
- CHANGELOG, CONTRIBUTING, CODE_OF_CONDUCT

## Next

These items are planned but not yet scheduled:

- **Multi-region relay**: geo-distributed relay mesh with latency-aware routing
- **On-chain DID registry**: anchor `did:wba:` identifiers to a public ledger for trustless resolution
- **TypeScript SDK**: first-class client library for Node.js and browser environments
- **Mobile SDK**: iOS and Android client libraries for agent communication
