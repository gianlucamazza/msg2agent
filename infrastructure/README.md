# msg2agent Cloud Infrastructure

This directory contains the infrastructure configuration for deploying msg2agent as a cloud SaaS.

## Architecture

```
Internet → Nginx (TLS) → MCP server (port 3001)
                       → Relay hub (port 8080) via /ws and /api
```

Three services run together via `docker-compose.cloud.yml`:

| Service | Image | Port | Purpose |
|---------|-------|------|---------|
| `relay` | `${RELAY_IMAGE}` | 8080 (internal) | A2A relay hub + WebSocket JSON-RPC |
| `mcp` | `${MCP_IMAGE}` | 3001 (internal) | MCP streamable-HTTP gateway with billing |
| `nginx` | `nginx:alpine` | 80, 443 | TLS termination, routing |

## Quick Start

```bash
cp .env.cloud.example .env.cloud
# Edit .env.cloud with your values
docker compose -f infrastructure/docker-compose.cloud.yml --env-file .env.cloud up -d
```

## Required Environment Variables

See `.env.cloud.example` for the full list. Key variables:

| Variable | Description |
|----------|-------------|
| `DOMAIN` | Public domain name (e.g. `relay.msg2agent.io`) |
| `RELAY_IMAGE` | Docker image for the relay service |
| `MCP_IMAGE` | Docker image for the MCP gateway |

## Files

| File | Purpose |
|------|---------|
| `docker-compose.cloud.yml` | Three-service SaaS stack (relay + mcp + nginx) |
| `nginx/cloud.conf` | Nginx TLS termination + routing config |
| `agentcard-msg2agent.json` | A2A AgentCard for Marketplace/discovery |
| `grafana/billing-dashboard.json` | Grafana dashboard for billing metrics |
| `terraform/` | GCP infrastructure provisioning |

## AgentCard

`agentcard-msg2agent.json` is the A2A Agent Card served at
`https://${DOMAIN}/.well-known/agent.json`. It declares:

- **Skills**: discovery, secure-messaging, task-orchestration, inbox
- **Security**: OAuth2 (Google OIDC) and API key (`sk_live_` prefix)
- **Endpoint**: `https://${DOMAIN}/`

Update the `url` field in the JSON before deploying to a new domain.

## TLS

The nginx config assumes certificates are mounted at:
- `/etc/nginx/ssl/cert.pem`
- `/etc/nginx/ssl/key.pem`

Use Let's Encrypt (certbot) or your own CA. Mount the certs directory into the nginx container via `docker-compose.cloud.yml` volumes.
