# Documentation

Welcome to the msg2agent documentation.

## Quick Navigation

### I want to...

**Understand the system**

- [Architecture](architecture.md) - System design, components, message flow, billing, OAuth, observability
- [Glossary](glossary.md) - Definitions of DID, A2A, DIDComm, MCP, and other terms

**Run locally**

- [Getting Started](getting-started.md) - Build and run your first agents

**Deploy to production**

- [Docker](deployment/docker.md) - Docker and Docker Compose deployment
- [Kubernetes](deployment/kubernetes.md) - Kubernetes manifests and setup
- [TLS Setup](deployment/tls-setup.md) - Certificate generation and configuration
- [Dashboard](deployment/dashboard.md) - Operator dashboard deployment and OAuth setup
- [Google Cloud Run](deployment/google-cloud-run.md) - Deploy relay to Cloud Run

**Configure and operate**

- [Configuration](operations/configuration.md) - All flags and environment variables
- [Monitoring](operations/monitoring.md) - Prometheus, Grafana, OpenTelemetry
- [Troubleshooting](operations/troubleshooting.md) - Common issues and solutions

**Develop and test**

- [Development Setup](development/setup.md) - Local development environment
- [Testing](development/testing.md) - Running tests

**Integrate with the API**

- [JSON-RPC API](api/jsonrpc.md) - Complete API reference
- [OpenClaw Plugin](openclaw-plugin/README.md) - OpenClaw plugin for msg2agent
- [MCP Server Configuration](operations/configuration.md#mcp-server-configuration) - MCP server flags and transports

**Pay & operate**

- [Billing Setup](marketplace/billing-setup.md) - Plans, quotas, API keys, OAuth flows
- [Billing Client Guide](marketplace/billing-client-guide.md) - API key usage, error codes, rate-limit back-off
- [Stripe Configuration](operations/stripe.md) - Stripe env vars, webhook flow, plan IDs
- [Billing Postgres Migration](operations/billing-postgres.md) - Migrate billing store from SQLite to Postgres
- [Billing Backup](operations/billing-backup.md) - RPO/RTO targets, VACUUM INTO backup, restore
- [Audit Incident Response](operations/audit-incident-response.md) - Tampering detection and mitigation runbook
- [Grafana Setup](operations/grafana-setup.md) - Import billing dashboard, AlertManager rules

**Publish on marketplaces**

- [Anthropic Marketplace](marketplace/anthropic.md) - Submit to Claude Marketplace
- [Billing Client Guide](marketplace/billing-client-guide.md) - Integration guide for marketplace clients

## Documentation Index

```
docs/
├── README.md                      # This file
├── getting-started.md             # Quickstart tutorial
├── architecture.md                # System design (relay, MCP, billing, OAuth, observability)
├── glossary.md                    # Term definitions
├── api/
│   └── jsonrpc.md                 # JSON-RPC API reference
├── deployment/
│   ├── docker.md                  # Docker deployment
│   ├── kubernetes.md              # Kubernetes deployment
│   ├── tls-setup.md               # TLS certificate setup
│   ├── dashboard.md               # Operator dashboard deployment
│   └── google-cloud-run.md        # Google Cloud Run deployment
├── development/
│   ├── setup.md                   # Local development setup
│   └── testing.md                 # Testing guide
├── marketplace/
│   ├── anthropic.md               # Claude Marketplace submission
│   ├── billing-setup.md           # Plans, quotas, API keys, OAuth flows
│   └── billing-client-guide.md    # API key usage and error handling
├── openclaw-plugin/
│   └── README.md                  # OpenClaw plugin guide
└── operations/
    ├── configuration.md           # Configuration reference
    ├── monitoring.md              # Metrics and tracing
    ├── troubleshooting.md         # Problem resolution
    ├── stripe.md                  # Stripe configuration
    ├── billing-postgres.md        # Billing Postgres migration
    ├── billing-backup.md          # Backup and restore
    ├── audit-incident-response.md # Audit chain incident response
    └── grafana-setup.md           # Grafana dashboard setup
```

## Conventions

- Configuration examples use environment variables with `MSG2AGENT_` prefix
- All ports shown are defaults and can be changed
- Commands assume Linux/macOS; adjust paths for Windows
