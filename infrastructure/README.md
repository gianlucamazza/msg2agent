# msg2agent Infrastructure

Production deployment files for the msg2agent stack on odroid (home lab).

## Architecture

```
Internet → Nginx Proxy Manager (TLS + Let's Encrypt)
               → Relay hub        (port 8080, WebSocket)
               → MCP server       (port 3001, streamable-HTTP)
               → Dashboard        (port 8082, web UI)
```

Three services managed by Docker Compose:

| Service | Image | Port | Purpose |
|---------|-------|------|---------|
| `relay` | `msg2agent-relay` | 8080 (internal) | A2A relay hub + billing webhook |
| `mcp-server` | `msg2agent-mcp-server` | 3001 (internal) | MCP streamable-HTTP gateway with billing |
| `dashboard` | `msg2agent-dashboard` | 8082 (internal) | Tenant/API key management UI |

## Quick Start

```bash
cp .env.example .env
# Edit .env with your values (Stripe keys, OAuth2, service token)
docker compose -f infrastructure/docker-compose.odroid.yml up -d
```

## Files

| File | Purpose |
|------|---------|
| `docker-compose.odroid.yml` | Production stack — source of truth, deployed by Drone CI |
| `agentcard-msg2agent.json` | A2A AgentCard served at `/.well-known/agent.json` |
| `connector-manifest.json` | Anthropic Connectors directory manifest |
| `grafana/billing-dashboard.json` | Grafana dashboard for billing metrics |

## AgentCard

`agentcard-msg2agent.json` declares the agent's DID, capabilities, and endpoint.
The relay serves it at `https://<domain>/.well-known/agent.json` via the `--agent-card` flag.

## Environment Variables

See `docker-compose.odroid.yml` for the full list. Required secrets (passed via `.env`):

| Variable | Description |
|----------|-------------|
| `STRIPE_SECRET_KEY` | Stripe secret key (`sk_live_*` for production) |
| `STRIPE_WEBHOOK_SECRET` | Stripe webhook signing secret |
| `STRIPE_PRICE_FREE/STARTER/TEAM/ENTERPRISE` | Stripe price IDs |
| `MSG2AGENT_SERVICE_TOKEN` | Internal service-to-service bearer token |
| `MSG2AGENT_OAUTH2_ISSUER_URL` | OAuth2 issuer (optional) |
