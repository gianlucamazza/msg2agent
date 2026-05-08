# Configuration Guide

This guide covers all configuration options for msg2agent components.

## Relay Configuration

### Command Line Flags

| Flag                        | Default  | Description                                                                           |
| --------------------------- | -------- | ------------------------------------------------------------------------------------- |
| `-addr`                     | `:8080`  | Listen address                                                                        |
| `-tls`                      | `false`  | Enable TLS                                                                            |
| `-tls-cert`                 |          | TLS certificate file                                                                  |
| `-tls-key`                  |          | TLS private key file                                                                  |
| `-store`                    | `memory` | Store type: `memory`, `file`, `sqlite`                                                |
| `-store-file`               |          | Store file path (file/sqlite stores)                                                  |
| `-max-connections`          | `1000`   | Maximum concurrent connections                                                        |
| `-msg-rate`                 | `100`    | Message rate limit per client (msg/sec)                                               |
| `-log-level`                | `info`   | Log level: `debug`, `info`, `warn`, `error`                                           |
| `-otlp-endpoint`            |          | OpenTelemetry OTLP endpoint                                                           |
| `-trace-stdout`             | `false`  | Output traces to stdout                                                               |
| `-cors-origins`             |          | Comma-separated list of allowed CORS origins                                          |
| `-skip-did-proof`           | `false`  | Skip DID ownership verification (not recommended) (env: `MSG2AGENT_SKIP_DID_PROOF`)  |
| `-allowed-dids`             |          | Comma-separated allowlist of DIDs that may register (empty = open relay)              |
| `-billing-db`               |          | Path to billing SQLite DB; enables API key auth on WebSocket register                 |
| `-billing-driver`           | `sqlite` | Billing store driver: `sqlite`, `postgres`                                            |
| `-oauth2-issuer-url`        |          | OAuth2/OIDC issuer URL for JWT validation on relay WebSocket                          |
| `-oauth2-audience`          |          | Expected OAuth2 token audience                                                        |
| `-oauth2-jwks-url`          |          | JWKS endpoint URL override (default: `<issuer>/.well-known/jwks.json`)               |
| `-audit-verifier-interval`  | `6h`     | Interval for background audit chain verification (0 = disabled)                       |
| `-enable-signup`            | `false`  | Enable self-service tenant signup endpoint `POST /api/tenants` (requires billing-db)  |

### Environment Variables

All flags can be set via environment variables with `MSG2AGENT_` prefix:

| Environment Variable          | Flag Equivalent            |
| ----------------------------- | -------------------------- |
| `MSG2AGENT_RELAY_ADDR`        | `-addr`                    |
| `MSG2AGENT_TLS`               | `-tls`                     |
| `MSG2AGENT_TLS_CERT`          | `-tls-cert`                |
| `MSG2AGENT_TLS_KEY`           | `-tls-key`                 |
| `MSG2AGENT_STORE`             | `-store`                   |
| `MSG2AGENT_STORE_FILE`        | `-store-file`              |
| `MSG2AGENT_MAX_CONNECTIONS`   | `-max-connections`         |
| `MSG2AGENT_MSG_RATE`          | `-msg-rate`                |
| `MSG2AGENT_LOG_LEVEL`         | `-log-level`               |
| `MSG2AGENT_OTLP_ENDPOINT`     | `-otlp-endpoint`           |
| `MSG2AGENT_TRACE_STDOUT`      | `-trace-stdout`            |
| `MSG2AGENT_CORS_ORIGINS`      | `-cors-origins`            |
| `MSG2AGENT_SKIP_DID_PROOF`    | `-skip-did-proof`          |
| `MSG2AGENT_ALLOWED_DIDS`      | `-allowed-dids`            |
| `MSG2AGENT_BILLING_DB`        | `-billing-db`              |
| `MSG2AGENT_OAUTH2_ISSUER_URL` | `-oauth2-issuer-url`       |
| `MSG2AGENT_OAUTH2_AUDIENCE`   | `-oauth2-audience`         |

Additional relay-only environment variables (no flag equivalent):

| Environment Variable        | Description                                                            |
| --------------------------- | ---------------------------------------------------------------------- |
| `BILLING_PG_DSN`            | Postgres DSN used when `-billing-driver postgres` is active            |
| `BILLING_WEBHOOK_URL`       | Webhook URL for quota notifications (see Billing Webhook section)      |
| `MSG2AGENT_OAUTH_AUTO_PROVISION` | Plan name (e.g. `PlanFree`) to auto-provision on first OAuth2 login |

### Example Configurations

**Development:**

```bash
./relay -addr :8080 -log-level debug -trace-stdout
```

**Production with TLS and SQLite:**

```bash
./relay \
  -addr :8443 \
  -tls \
  -tls-cert /etc/msg2agent/server.crt \
  -tls-key /etc/msg2agent/server.key \
  -store sqlite \
  -store-file /var/lib/msg2agent/relay.db \
  -max-connections 5000 \
  -log-level info \
  -otlp-endpoint http://jaeger:4318
```

**With billing and OAuth2:**

```bash
./relay \
  -addr :8443 \
  -tls \
  -tls-cert /etc/msg2agent/server.crt \
  -tls-key /etc/msg2agent/server.key \
  -billing-db /var/lib/msg2agent/billing.db \
  -oauth2-issuer-url https://your-idp.example.com \
  -oauth2-audience your-api-audience \
  -audit-verifier-interval 6h
```

**Using environment variables:**

```bash
export MSG2AGENT_RELAY_ADDR=":8443"
export MSG2AGENT_TLS="true"
export MSG2AGENT_TLS_CERT="/etc/msg2agent/server.crt"
export MSG2AGENT_TLS_KEY="/etc/msg2agent/server.key"
export MSG2AGENT_STORE="sqlite"
export MSG2AGENT_STORE_FILE="/var/lib/msg2agent/relay.db"
./relay
```

## Agent Runtime

The agent runtime is implemented as the reusable Go package `pkg/agent`. The
shipped process that embeds it is `cmd/mcp-server`, which connects to the relay,
registers a DID, and exposes the network through MCP transports. There is no
current standalone binary or Docker target named `agent`.

Custom agents should import `pkg/agent`, configure `agent.Config`, register
method handlers, and connect to the relay with a WebSocket URL. Operational
configuration for the shipped agent-facing binary is documented in the MCP
server section below.

## MCP Server Configuration

The MCP server (`cmd/mcp-server`) bridges AI assistants to the msg2agent network. It runs a full agent internally and exposes its capabilities via the MCP protocol.

### Command Line Flags

| Flag                       | Default               | Description                                                    |
| -------------------------- | --------------------- | -------------------------------------------------------------- |
| `-name`                    | `mcp-agent`           | Agent name                                                     |
| `-domain`                  | `localhost`           | Domain for DID                                                 |
| `-relay`                   | `ws://localhost:8080` | Relay WebSocket URL                                            |
| `-transport`               | `stdio`               | MCP transport: `stdio`, `sse`, `streamable-http`               |
| `-addr`                    | `:8081`               | Listen address for SSE/HTTP transports                         |
| `-identity-file`           |                       | Path to identity key file (default: `~/.msg2agent/<name>.key`) |
| `-billing-db`              |                       | Path to billing DB; enables API key auth when set              |
| `-billing-driver`          | `sqlite`              | Billing store driver: `sqlite`, `postgres`                     |
| `-allow-anon`              | `false`               | Allow unauthenticated MCP requests (only valid without billing-db) |
| `-oauth2-issuer-url`       |                       | OAuth2/OIDC issuer URL                                         |
| `-oauth2-audience`         |                       | Expected OAuth2 token audience                                 |
| `-oauth2-jwks-url`         |                       | JWKS endpoint URL override                                     |
| `-shutdown-timeout`        | `30s`                 | Graceful shutdown timeout for HTTP server                      |
| `-audit-verifier-interval` | `6h`                  | Interval for background audit chain verification (0 = disabled)|

### Environment Variables

The MCP server reads all `MSG2AGENT_*` env vars as fallbacks when the corresponding flag is not set explicitly:

| Environment Variable          | Flag Equivalent      | Notes                                                     |
| ----------------------------- | -------------------- | --------------------------------------------------------- |
| `MSG2AGENT_NAME`              | `-name`              |                                                           |
| `MSG2AGENT_DOMAIN`            | `-domain`            |                                                           |
| `MSG2AGENT_RELAY_URL`         | `-relay`             |                                                           |
| `MSG2AGENT_HTTP_ADDR`         | `-addr`              |                                                           |
| `MSG2AGENT_BILLING_DB`        | `-billing-db`        |                                                           |
| `MSG2AGENT_BILLING_DRIVER`    | `-billing-driver`    |                                                           |
| `MSG2AGENT_OAUTH2_ISSUER_URL` | `-oauth2-issuer-url` |                                                           |
| `MSG2AGENT_OAUTH2_AUDIENCE`   | `-oauth2-audience`   |                                                           |
| `MSG2AGENT_OAUTH2_JWKS_URL`   | `-oauth2-jwks-url`   |                                                           |
| `MSG2AGENT_OAUTH_AUTO_PROVISION` | (no flag)         | Plan name for OAuth2 auto-provisioning (e.g. `PlanFree`) |

### Example Configurations

**Stdio mode (Claude Code / Claude Desktop MCP):**

```bash
./mcp-server -name my-agent -relay ws://localhost:8080
```

Or in `.mcp.json`:

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

**Streamable HTTP mode (OpenClaw plugin):**

```bash
./mcp-server \
  -name openclaw \
  -domain example.com \
  -relay ws://relay:8080 \
  -transport streamable-http \
  -addr :3001
```

**With billing and OAuth2:**

```bash
./mcp-server \
  -name secure-agent \
  -relay wss://relay.example.com \
  -transport streamable-http \
  -addr :3001 \
  -billing-db /var/lib/msg2agent/billing.db \
  -oauth2-issuer-url https://your-idp.example.com \
  -oauth2-audience your-api-audience
```

**SSE mode:**

```bash
./mcp-server \
  -name sse-agent \
  -relay ws://localhost:8080 \
  -transport sse \
  -addr :8081
```

## Billing Admin CLI

The `billing-admin` tool manages tenants, API keys, and usage data. It always requires a `-db` flag pointing to the SQLite database.

```text
billing-admin -db <path> <command> [flags]

Commands:
  create-tenant   Create a new billing tenant
  list-tenants    List all tenants
  suspend-tenant  Suspend a tenant by ID
  issue-key       Issue an API key for a tenant
  revoke-key      Revoke an API key by ID
  list-keys       List API keys for a tenant
  list-usage      Show usage aggregates for a tenant/period
  export-csv      Export usage CSV to stdout
  purge-events    Delete raw audit events older than a date
  query-events    Query raw audit events for a tenant
  backup          Write a consistent snapshot of the billing DB
  verify          Print a health summary of the billing DB
  verify-audit    Walk the audit hash chain and report any tampering
```

## Billing Configuration

### Billing Store Drivers

| Driver     | DSN / Path         | Notes                                     |
| ---------- | ------------------ | ----------------------------------------- |
| `sqlite`   | File path          | Default; single-node; WAL mode enabled    |
| `postgres` | Postgres DSN       | Multi-node; requires `BILLING_PG_DSN`     |

**SQLite example:**

```bash
./relay -billing-db /var/lib/msg2agent/billing.db -billing-driver sqlite
```

**Postgres example:**

```bash
export BILLING_PG_DSN="postgres://user:pass@localhost:5432/billing?sslmode=require"
./relay -billing-driver postgres -billing-db "$BILLING_PG_DSN"
```

### Billing Webhook

Set `BILLING_WEBHOOK_URL` to receive POST requests when tenants cross quota thresholds:

```bash
export BILLING_WEBHOOK_URL="https://your-backend.example.com/hooks/billing"
```

Webhook payload (JSON):

```json
{
  "tenant_id": "t_abc123",
  "plan": "starter",
  "period": "2026-05",
  "event": "message",
  "event_type": "quota_warning",
  "current": 8500,
  "limit": 10000,
  "ratio": 0.85,
  "timestamp": "2026-05-01T12:00:00Z"
}
```

Retry behaviour:

- **2xx**: counted as success, no retry
- **4xx**: no retry (client error)
- **5xx**: up to 3 total attempts with 1 s / 2 s exponential back-off; event dropped after all retries

Quota warning threshold defaults to 0.8 (80%). Override with:

```bash
export BILLING_QUOTA_WARN_RATIO=0.9
```

### Postgres Backup

The `billing-admin backup` command uses `pg_dump` when running against PostgreSQL.
Set `MSG2AGENT_PG_DUMP` to the path of the `pg_dump` binary if it is not on `PATH`:

```bash
export MSG2AGENT_PG_DUMP=/usr/lib/postgresql/15/bin/pg_dump
```

### Quota Overrides

Default per-plan quotas can be overridden at runtime via `BILLING_QUOTAS_FILE` (JSON):

```bash
export BILLING_QUOTAS_FILE=/etc/msg2agent/quotas.json
```

Format:

```json
{
  "free":       { "MaxAgentDIDs": 5, "MaxMessagesPerMonth": 2000 },
  "starter":    { "MaxAgentDIDs": 10 },
  "team":       {},
  "enterprise": {}
}
```

Only non-zero fields override the built-in defaults.

## Store Configuration

### Memory Store (Default)

- No persistence
- Data lost on restart
- Good for development/testing

```bash
./relay -store memory
```

### File Store

- JSON file persistence
- Simple but not concurrent-safe
- Good for single-instance deployments

```bash
./relay -store file -store-file /var/lib/msg2agent/agents.json
```

### SQLite Store

- Full SQL persistence
- WAL mode for performance
- Good for production single-instance

```bash
./relay -store sqlite -store-file /var/lib/msg2agent/relay.db
```

SQLite features:

- Automatic migrations
- WAL journal mode
- 5-second busy timeout
- Foreign key support

## Logging Configuration

### Log Levels

| Level   | Description                   | Use Case               |
| ------- | ----------------------------- | ---------------------- |
| `debug` | All messages including traces | Development, debugging |
| `info`  | Operational messages          | Normal production      |
| `warn`  | Warnings and potential issues | Quiet production       |
| `error` | Errors only                   | Minimal logging        |

### Structured Logging

Output is JSON when stdout is not a TTY:

```json
{
  "time": "2025-01-25T10:00:00Z",
  "level": "INFO",
  "msg": "server started",
  "addr": ":8080"
}
```

Human-readable format when stdout is a TTY:

```
2025/01/25 10:00:00 INFO server started addr=:8080
```

## TLS Configuration

### Generating Certificates

See [TLS Setup Guide](../deployment/tls-setup.md) for detailed instructions.

### Certificate Requirements

- X.509 format
- PEM encoded
- Key must match certificate
- SAN extension recommended

### Minimum TLS Version

The relay enforces TLS 1.2 minimum.

## Metrics Configuration

### Prometheus Metrics

The relay and streamable HTTP MCP server expose Prometheus metrics at `/metrics`:

```bash
# Relay metrics
curl http://localhost:8080/metrics

# MCP server metrics
./mcp-server -transport streamable-http -addr :3001 -relay ws://localhost:8080
curl http://localhost:3001/metrics
```

### OpenTelemetry Tracing

```bash
# OTLP HTTP endpoint (Jaeger, Tempo, etc.)
./relay -otlp-endpoint http://localhost:4318

# Stdout for debugging
./relay -trace-stdout
```

## Health Endpoints

### Relay

| Endpoint  | Purpose   | Response         |
| --------- | --------- | ---------------- |
| `/health` | Liveness  | `ok`             |
| `/ready`  | Readiness | JSON with status |

### MCP Server (streamable HTTP)

| Endpoint   | Purpose   | Response         |
| ---------- | --------- | ---------------- |
| `/health`  | Liveness  | `ok`             |
| `/ready`   | Readiness | JSON with DID    |
| `/metrics` | Metrics   | Prometheus format |

## Resource Limits

### Relay Limits

| Resource        | Default                  | Notes                 |
| --------------- | ------------------------ | --------------------- |
| Max connections | 1000                     | Per-relay limit       |
| Message size    | 1MB                      | Enforced by WebSocket |
| Rate limit      | 100 msg/sec (burst: 200) | Per-client            |

### Agent Limits

| Resource       | Default | Notes         |
| -------------- | ------- | ------------- |
| TaskStore size | 10000   | Maximum tasks |
| Task TTL       | 24h     | Auto-cleanup  |
| Dedup cache    | 10000   | Message IDs   |
| Dedup TTL      | 5min    | Cache expiry  |

## Security Implications

This section documents the security tradeoffs of key configuration knobs.

### `--allow-anon` (mcp-server only)

Allows unauthenticated MCP requests. This flag is **mutually exclusive** with `--billing-db`: if both are specified the process exits with code 1.

**Use only in development or local testing.** In any deployment with billing enabled, anonymous access bypasses all quota and rate-limit enforcement.

### `--skip-did-proof` / `MSG2AGENT_SKIP_DID_PROOF` (relay)

Disables DID ownership proof verification during agent registration. Without this check, any client can claim any DID.

**Never set in production.** Valid use case: integration tests where agents do not carry real key material.

### `MSG2AGENT_OAUTH_AUTO_PROVISION`

When set to a plan name (e.g. `PlanFree`), any holder of a valid OIDC token that is not yet registered automatically gets a new tenant created under that plan on first connection.

**Leave unset in production** unless self-service signup is explicitly desired. If set, any user with a valid token at your OAuth2 provider becomes a billable tenant immediately — there is no approval step. Combine with `--enable-signup` only when you have quota-budget controls in place.

### `--enable-signup` (relay)

Enables the unauthenticated `POST /api/tenants` self-service endpoint. Requires `--billing-db` to be set (the relay exits with code 1 if `--enable-signup` is set without `--billing-db`).

Rate-limit the `/api/tenants` endpoint at your load balancer to prevent abuse.

### `--allowed-dids` / `MSG2AGENT_ALLOWED_DIDS` (relay)

When set, only the listed DIDs may register. An empty value means the relay accepts any valid DID:WBA.

**Always configure in production** multi-tenant deployments unless combined with billing-level tenant enforcement.

## Security Recommendations

### Production Checklist

- [ ] Enable TLS (`-tls`)
- [ ] Use valid certificates (not self-signed)
- [ ] Require encryption in custom `pkg/agent` deployments when peer support is available
- [ ] Set appropriate rate limits
- [ ] Use SQLite or external DB for persistence
- [ ] Configure log level to `info` or `warn`
- [ ] Set up monitoring with Prometheus
- [ ] Enable tracing with OpenTelemetry
- [ ] Do **not** set `MSG2AGENT_SKIP_DID_PROOF`
- [ ] Do **not** set `MSG2AGENT_OAUTH_AUTO_PROVISION` unless self-service signup is intended
- [ ] Do **not** set `--allow-anon` when billing is enabled

### Development Checklist

- [ ] Use `-tls-skip-verify` only in dev
- [ ] Use memory store for fast iteration
- [ ] Enable debug logging
- [ ] Use stdout tracing
