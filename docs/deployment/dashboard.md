# Dashboard Deployment Guide

## Architecture

The dashboard is a single Go binary (`cmd/dashboard/`) that serves:

- **Embedded static assets** — `index.html`, `app.js`, `style.css` compiled into the binary via `//go:embed web`.
- **REST API** — `/api/dashboard/*` endpoints backed by the billing SQLite store.
- **Stripe proxy** — checkout and portal requests are forwarded to the relay service, which holds the Stripe secret key.

```
Browser → nginx /app/ → dashboard:8082
                         ├── /api/dashboard/* (OAuth2-protected)
                         └── /                (embedded SPA)
```

The dashboard shares the `billing.db` SQLite file with the MCP server via a Docker volume (`billing-data`).

## OAuth2 Setup (Google)

1. Create an OAuth 2.0 Client ID in [Google Cloud Console](https://console.cloud.google.com/apis/credentials).
2. Set **Authorised redirect URIs** to `https://<your-domain>/app/callback` (or your IdP callback URL).
3. Set these environment variables (in `.env` or your secret manager):

```
MSG2AGENT_OAUTH2_ISSUER_URL=https://accounts.google.com
MSG2AGENT_OAUTH2_AUDIENCE=<your-google-client-id>
MSG2AGENT_OAUTH2_JWKS_URL=https://www.googleapis.com/oauth2/v3/certs
MSG2AGENT_OAUTH_AUTO_PROVISION=free
```

`MSG2AGENT_OAUTH_AUTO_PROVISION=free` auto-creates a free-tier tenant on first login. Remove it to require manual provisioning.

## Custom Domain

Add a DNS record pointing to your server and update `nginx/cloud.conf`:

```nginx
server_name dashboard.example.com;
```

Or keep the `/app/` prefix on the existing domain — no changes needed beyond the already-configured nginx `location /app/` block.

## Stripe Proxy

Checkout and portal requests are proxied to the relay at `MSG2AGENT_RELAY_URL`. The relay holds the `STRIPE_SECRET_KEY`. An optional `MSG2AGENT_SERVICE_TOKEN` env var is forwarded as `X-Service-Token` for relay-side request validation.

## SQLite Sharing Limitation

Both the dashboard and the MCP server mount the same `billing-data` volume. SQLite's WAL mode (`busy_timeout=5s`) handles concurrent reads well, but write contention between services may cause brief delays under high load. For production at scale, migrate to PostgreSQL by setting `MSG2AGENT_BILLING_DRIVER=postgres` and providing a connection string.

## Starting the Dashboard

```bash
docker compose -f infrastructure/docker-compose.cloud.yml up -d dashboard
```

Health check: `curl http://localhost:8082/health` → `ok`
