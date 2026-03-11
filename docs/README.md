# Documentation

Welcome to the msg2agent documentation.

## Quick Navigation

### I want to...

**Understand the system**

- [Architecture](architecture.md) - System design, components, message flow
- [Glossary](glossary.md) - Definitions of DID, A2A, DIDComm, and other terms

**Run locally**

- [Getting Started](getting-started.md) - Build and run your first agents

**Deploy to production**

- [Docker](deployment/docker.md) - Docker and Docker Compose deployment
- [Kubernetes](deployment/kubernetes.md) - Kubernetes manifests and setup
- [TLS Setup](deployment/tls-setup.md) - Certificate generation and configuration

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

## Documentation Index

```
docs/
├── README.md                 # This file
├── getting-started.md        # Quickstart tutorial
├── architecture.md           # System design
├── glossary.md               # Term definitions
├── api/
│   └── jsonrpc.md            # JSON-RPC API reference
├── deployment/
│   ├── docker.md             # Docker deployment
│   ├── kubernetes.md         # Kubernetes deployment
│   └── tls-setup.md          # TLS certificate setup
├── development/
│   ├── setup.md              # Local development setup
│   └── testing.md            # Testing guide
├── openclaw-plugin/
│   ├── README.md             # OpenClaw plugin guide
│   ├── openclaw.plugin.json  # Plugin manifest
│   └── index.ts              # Plugin implementation
└── operations/
    ├── configuration.md      # Configuration reference
    ├── monitoring.md         # Metrics and tracing
    └── troubleshooting.md    # Problem resolution
```

## Conventions

- Configuration examples use environment variables with `MSG2AGENT_` prefix
- All ports shown are defaults and can be changed
- Commands assume Linux/macOS; adjust paths for Windows
