# msg2agent Infrastructure

Self-host Compose files for a home-lab msg2agent stack (self-host example).

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
docker compose -f infrastructure/docker-compose.selfhost.yml up -d
```

## Files

| File | Purpose |
|------|---------|
| `docker-compose.selfhost.yml` | Compose stack — source of truth, deployed by Drone CI |
| `agentcard-msg2agent.json` | A2A AgentCard served at `/.well-known/agent.json` |
| `connector-manifest.json` | Optional JSON the relay can serve via `-connector-manifest` |
| `grafana/billing-dashboard.json` | Grafana dashboard for billing metrics |

## AgentCard and connector manifest

`agentcard-msg2agent.json` and `connector-manifest.json` are **templates**.
They use `relay.example.com` placeholders — replace every URL and DID domain
with the public host of your own relay before passing them to `--agent-card`
or `--connector-manifest`. There is no hosted connector listing.

The relay serves the AgentCard at `https://<your-domain>/.well-known/agent.json`
via `--agent-card`, and the connector file at
`/.well-known/mcp-connector.json` via `--connector-manifest`.

## Environment Variables

See `docker-compose.selfhost.yml` for the full list. Required secrets (passed via `.env`):

| Variable | Description |
|----------|-------------|
| `STRIPE_SECRET_KEY` | Stripe secret key (`sk_live_*` for production) |
| `STRIPE_WEBHOOK_SECRET` | Stripe webhook signing secret |
| `STRIPE_PRICE_FREE/STARTER/TEAM/ENTERPRISE` | Stripe price IDs |
| `MSG2AGENT_SERVICE_TOKEN` | Internal service-to-service bearer token |
| `MSG2AGENT_OAUTH2_ISSUER_URL` | OAuth2 issuer (optional) |
