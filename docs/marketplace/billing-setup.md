# Billing Setup Guide

This guide covers how to configure and operate the billing system for a cloud-hosted msg2agent deployment. For self-hosted deployments (no `--billing-db` flag), skip this guide — all quota and auth checks are no-ops.

## Architecture overview

```
Client → Bearer msg2a_... → APIKeyMiddleware (auth only)
                          → MCPToolMeterMiddleware (MCP tool billing)
                          → A2A meterA2ARequest (A2A billing)
Relay  → WS Authorization header → per-tenant rate limit + MaxAgentDIDs quota
```

Audit events are written asynchronously (channel buffer 1000, flushed every 5 s or 100 events). The billing DB is the source of truth; in-memory counters are restored from `usage_aggregates` on restart.

## Plans and quotas

| Plan       | Max DIDs | Msg/month | Tool calls/month | Rate (msg/s) | Burst |
|------------|----------|-----------|-----------------|-------------|-------|
| free       | 3        | 1,000     | 5,000           | 5           | 20    |
| starter    | 5        | 10,000    | 50,000          | 10          | 50    |
| team       | 50       | 200,000   | 1,000,000       | 100         | 500   |
| enterprise | 100,000  | 1B        | 1B              | 10,000      | 50,000|

## Initial setup

### 1. Start the relay with billing

```sh
relay --billing-db /var/lib/msg2agent/billing.db \
      --db /var/lib/msg2agent/agents.db \
      --addr :443
```

### 2. Start the MCP server with billing

```sh
mcp-server --billing-db /var/lib/msg2agent/billing.db \
           --addr :8080
```

`--allow-anon` is accepted but emits a startup warning when combined with `--billing-db`.

## Admin operations

All operations use the `billing-admin` CLI:

```sh
billing-admin -db /var/lib/msg2agent/billing.db <command> [flags]
```

### Create a tenant

```sh
billing-admin -db billing.db create-tenant \
  --name "Acme Corp" \
  --email "ops@acme.com" \
  --plan starter
# Output: tenant created
#   ID:    t_01HXYZ...
#   Name:  Acme Corp
#   Plan:  starter
```

### Issue an API key

The plaintext key is printed **once only**. Store it securely (e.g. a secrets manager).

```sh
billing-admin -db billing.db issue-key \
  --tenant t_01HXYZ... \
  --name production
# Output:
# API key issued (shown only once — store it securely):
#
#   msg2a_AbCdEfGh...
#
# Key ID:  k_01HABC...
# Label:   production
# Prefix:  AbCdEfGh
```

Clients use the key in every request:

```
Authorization: Bearer msg2a_AbCdEfGh...
```

### Rotate a key

```sh
# Issue a new key first, then revoke the old one.
billing-admin -db billing.db issue-key --tenant t_01HXYZ... --name production-v2
billing-admin -db billing.db revoke-key --id k_01HABC...
```

### Suspend a tenant

```sh
billing-admin -db billing.db suspend-tenant --id t_01HXYZ...
```

Suspended tenants receive HTTP 403 on all authenticated endpoints. Their data is preserved (`TenantStatusDeleted` is a tombstone — no hard deletes).

### Upgrade a tenant plan

Update the plan and quota directly in the DB and evict the rate-limiter bucket:

```sh
sqlite3 /var/lib/msg2agent/billing.db \
  "UPDATE tenants SET plan='team', quota_json=json_object(
     'MaxAgentDIDs',50,
     'MaxMessagesPerMonth',200000,
     'MaxToolCallsPerMonth',1000000,
     'RateLimitMsgPerSec',100,
     'RateLimitBurstSize',500
   ), updated_at=datetime('now') WHERE id='t_01HXYZ...';"
```

The rate-limiter bucket is evicted lazily on the next request (or at relay restart). For immediate effect, restart the relay.

## Usage reporting

### List current period usage

```sh
billing-admin -db billing.db list-usage --period 2026-05
# TENANT          PERIOD   EVENT       COUNT
# t_01HXYZ...     2026-05  message     4321
# t_01HXYZ...     2026-05  tool_call   12500
```

### Export CSV

```sh
billing-admin -db billing.db export-csv --period 2026-05 > usage-2026-05.csv
# tenant_id,period,event,count
# t_01HXYZ...,2026-05,message,4321
# t_01HXYZ...,2026-05,tool_call,12500
```

All periods:

```sh
billing-admin -db billing.db export-csv > usage-all.csv
```

### Dispute resolution

For dispute readiness, query raw events directly:

```sql
SELECT tenant_id, event, tool_name, request_id, ts
FROM usage_events
WHERE tenant_id = 't_01HXYZ...'
  AND ts BETWEEN '2026-05-01' AND '2026-06-01'
ORDER BY ts;
```

## Rate limit behavior

Clients hitting the per-message rate limit receive JSON-RPC error `-32007` (CodeRateLimited). Clients should implement exponential back-off with a cap (e.g. 30 s max).

Clients hitting the monthly quota receive:
- MCP: `tool result error "quota exceeded"` (no HTTP 4xx, stays in MCP protocol)
- Relay: JSON-RPC error `-32008` (CodeQuotaExceeded)

## Network model

The relay operates as a **shared network**: all registered DIDs are visible to all connected agents via `relay.discover` and `relay.lookup`, regardless of tenant. Tenant isolation applies only to:

- `MaxAgentDIDs` quota (registration enforcement)
- Per-tenant rate limits on message relay
- Billing event recording

This is intentional — msg2agent is a communication network, not a siloed SaaS platform.

## Security notes

- API keys are hashed with SHA-256 before storage. The plaintext is never persisted.
- Revoked keys are rejected immediately on the next request. In-flight requests at the time of revocation complete normally (eventual consistency, by design).
- The `/health` endpoint bypasses authentication and should not be exposed to the public internet without a WAF rule.
