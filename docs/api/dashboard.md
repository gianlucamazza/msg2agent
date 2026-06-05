# msg2agent Dashboard API

Reference documentation for the Dashboard REST API (`cmd/dashboard`).

## Authentication

All `/api/dashboard/*` endpoints require a Bearer JWT in the `Authorization` header:

```
Authorization: Bearer <JWT>
```

### Obtaining a JWT

The dashboard SPA at `/app/` performs a **PKCE OAuth2 flow** against the relay's
authorization server. To get a token programmatically:

1. Start an authorization request to `<relay>/oauth/authorize` with:
   - `response_type=code`
   - `client_id=<your-client-id>`
   - `code_challenge=<S256-challenge>`
   - `code_challenge_method=S256`
   - `redirect_uri=<your-callback>`
2. Exchange the authorization code at `<relay>/oauth/token` for an access token.
3. Include the access token as `Authorization: Bearer <token>` on every API call.

The access token is a short-lived JWT (typically 1 hour). The SPA automatically
refreshes it using the stored refresh token. For CLI usage, re-run the PKCE flow
when the token expires.

---

## Endpoints

### System

#### GET /health

Health check. No authentication required.

```bash
curl https://dashboard.example.com/health
```

Response `200 OK`:
```
ok
```

Response `503 Service Unavailable` (billing store unreachable):
```
billing store unavailable: connection refused
```

---

#### GET /version

Returns build version, commit hash, and build date. No authentication required.

```bash
curl https://dashboard.example.com/version
```

Response `200 OK`:
```json
{
  "version": "v1.2.3",
  "commit": "a1b2c3d",
  "date": "2026-05-01"
}
```

---

### Account

#### GET /api/dashboard/me

Returns the full identity of the authenticated tenant, including plan, billing
status, quota limits, and DID/public-key data (when configured).

```bash
curl -H "Authorization: Bearer <JWT>" \
  https://dashboard.example.com/api/dashboard/me
```

Response `200 OK`:
```json
{
  "id": "ten_01j9abc123",
  "name": "Alice Smith",
  "email": "alice@example.com",
  "email_verified": true,
  "plan": "starter",
  "billing_status": "active",
  "current_period_end": "2026-06-01T00:00:00Z",
  "created_at": "2025-01-15T10:30:00Z",
  "quota": {
    "max_messages_per_month": 1000,
    "max_tool_calls_per_month": 5000,
    "max_api_keys": 5,
    "max_dids": 1,
    "rate_limit_msg_per_sec": 2.0,
    "rate_limit_burst_size": 5.0
  },
  "did": "did:key:z6MkhaXgBZ...",
  "signing_public_key": "MCowBQYDK2VwAyEA...",
  "encryption_public_key": "MCowBQYDK2VuAyEA..."
}
```

---

#### GET /api/dashboard/profile

Returns a trimmed profile view (no plan/quota).

```bash
curl -H "Authorization: Bearer <JWT>" \
  https://dashboard.example.com/api/dashboard/profile
```

Response `200 OK`:
```json
{
  "id": "ten_01j9abc123",
  "name": "Alice Smith",
  "email": "alice@example.com",
  "email_verified": true,
  "created_at": "2025-01-15T10:30:00Z"
}
```

---

#### PATCH /api/dashboard/profile

Updates the tenant's display name (1–128 characters).

```bash
curl -X PATCH \
  -H "Authorization: Bearer <JWT>" \
  -H "Content-Type: application/json" \
  -d '{"name": "Alice Smith"}' \
  https://dashboard.example.com/api/dashboard/profile
```

Response `200 OK`: same shape as GET /profile.

Response `400 Bad Request`:
```json
{"error": "name must not be empty"}
```

---

#### POST /api/email/resend

Resends the email verification message. This endpoint is served by the **relay**,
not the dashboard. Rate-limited to 1 request per email address per minute.
No authentication required.

```bash
curl -X POST \
  -H "Content-Type: application/json" \
  -d '{"email": "alice@example.com"}' \
  https://relay.example.com/api/email/resend
```

Response `200 OK`: empty body (also returned when email is unknown or already
verified — prevents enumeration).

Response `429 Too Many Requests`:
```json
{"error": "too many requests; try again in a minute"}
```

---

### API Keys

#### GET /api/dashboard/keys

Lists all API keys for the tenant. Supports `?limit=` (default 100, max 500)
and `?offset=` for pagination.

```bash
curl -H "Authorization: Bearer <JWT>" \
  "https://dashboard.example.com/api/dashboard/keys?limit=20&offset=0"
```

Response `200 OK`:
```json
{
  "items": [
    {
      "id": "key_01j9xyz789",
      "label": "Production server",
      "key_prefix": "m2a_live_abc1",
      "created_at": "2025-03-10T08:00:00Z",
      "last_used": "2026-05-20T14:22:00Z",
      "revoked_at": null
    }
  ],
  "total": 1,
  "limit": 20,
  "offset": 0
}
```

---

#### POST /api/dashboard/keys

Creates a new API key. The full key value is returned only once; store it
immediately.

```bash
curl -X POST \
  -H "Authorization: Bearer <JWT>" \
  -H "Content-Type: application/json" \
  -d '{"label": "CI pipeline"}' \
  https://dashboard.example.com/api/dashboard/keys
```

Response `201 Created`:
```json
{
  "id": "key_01j9xyz789",
  "key": "m2a_live_abc1def2ghi3jkl4mno5pqr6stu7vwx8yz90",
  "label": "CI pipeline"
}
```

Response `429 Too Many Requests` (rate limit hit): includes `Retry-After` header.

---

#### DELETE /api/dashboard/keys/{id}

Revokes an API key permanently.

```bash
curl -X DELETE \
  -H "Authorization: Bearer <JWT>" \
  https://dashboard.example.com/api/dashboard/keys/key_01j9xyz789
```

Response `204 No Content`.

Response `404 Not Found`:
```json
{"error": "key not found"}
```

---

#### PATCH /api/dashboard/keys/{id}

Renames an API key label (1–64 characters).

```bash
curl -X PATCH \
  -H "Authorization: Bearer <JWT>" \
  -H "Content-Type: application/json" \
  -d '{"label": "Staging server"}' \
  https://dashboard.example.com/api/dashboard/keys/key_01j9xyz789
```

Response `200 OK`:
```json
{"id": "key_01j9xyz789", "label": "Staging server"}
```

---

### Usage

#### GET /api/dashboard/usage

Returns paginated usage aggregates (event counts per period). Filter by
`?period=YYYY-MM` for a specific month.

```bash
curl -H "Authorization: Bearer <JWT>" \
  "https://dashboard.example.com/api/dashboard/usage?period=2026-05"
```

Response `200 OK`:
```json
{
  "items": [
    {"period": "2026-05", "event": "messages",   "count": 234},
    {"period": "2026-05", "event": "tool_calls", "count": 87}
  ],
  "total": 2,
  "limit": 100,
  "offset": 0
}
```

---

#### GET /api/dashboard/usage.csv

Downloads usage data as a CSV file. Supports `?period=YYYY-MM`.

```bash
curl -H "Authorization: Bearer <JWT>" \
  "https://dashboard.example.com/api/dashboard/usage.csv?period=2026-05" \
  -o usage.csv
```

Response `200 OK` (`Content-Type: text/csv`):
```
period,event,count
2026-05,messages,234
2026-05,tool_calls,87
```

---

#### GET /api/dashboard/usage/by-tool

Returns a paginated breakdown of tool call counts. Supports `?period=YYYY-MM`.
Returns `501` if the admin store is not configured.

```bash
curl -H "Authorization: Bearer <JWT>" \
  "https://dashboard.example.com/api/dashboard/usage/by-tool?period=2026-05"
```

Response `200 OK`:
```json
{
  "items": [
    {"tool_name": "search_web", "count": 45},
    {"tool_name": "read_file",  "count": 42}
  ],
  "total": 2,
  "limit": 100,
  "offset": 0
}
```

---

### Audit

#### GET /api/dashboard/audit/verify

Walks the cryptographic hash chain of audit log entries for the tenant and
reports whether any entries have been tampered with. Returns `501` if the
admin store is not configured.

```bash
curl -H "Authorization: Bearer <JWT>" \
  https://dashboard.example.com/api/dashboard/audit/verify
```

Response `200 OK`:
```json
{
  "tenant_id": "ten_01j9abc123",
  "verified": 1500,
  "tampered": false,
  "first_bad_id": null
}
```

---

#### GET /api/dashboard/audit/events

Returns paginated audit events. Supports `?period=YYYY-MM` and `?limit=`
(default 500, max 1000). Returns `501` if the admin store is not configured.

```bash
curl -H "Authorization: Bearer <JWT>" \
  "https://dashboard.example.com/api/dashboard/audit/events?period=2026-05&limit=50"
```

Response `200 OK`:
```json
{
  "items": [
    {
      "id": "evt_01j9abc001",
      "event": "tool_call",
      "tool_name": "search_web",
      "request_id": "req_01j9xyz999",
      "timestamp": "2026-05-20T12:34:56Z"
    }
  ],
  "total": 1,
  "limit": 50,
  "offset": 0
}
```

---

### OAuth Clients

#### GET /api/dashboard/oauth-clients

Lists OAuth clients with at least one active refresh token for the tenant.

```bash
curl -H "Authorization: Bearer <JWT>" \
  https://dashboard.example.com/api/dashboard/oauth-clients
```

Response `200 OK`:
```json
[
  {
    "client_id": "claude-desktop",
    "client_name": "Claude Desktop",
    "created_at": "2025-11-01T09:00:00Z"
  }
]
```

---

#### DELETE /api/dashboard/oauth-clients/{id}

Revokes all refresh tokens for the specified client, disconnecting the app.

```bash
curl -X DELETE \
  -H "Authorization: Bearer <JWT>" \
  https://dashboard.example.com/api/dashboard/oauth-clients/claude-desktop
```

Response `204 No Content`.

---

### Billing

#### POST /api/dashboard/checkout

Creates a Stripe Checkout session via the relay. Returns a redirect URL.
Returns `503` if the relay is not configured.

```bash
curl -X POST \
  -H "Authorization: Bearer <JWT>" \
  -H "Content-Type: application/json" \
  -d '{
    "plan": "starter",
    "success_url": "https://app.example.com/app/?checkout=success",
    "cancel_url":  "https://app.example.com/app/?checkout=cancelled"
  }' \
  https://dashboard.example.com/api/dashboard/checkout
```

Response `200 OK`:
```json
{"url": "https://checkout.stripe.com/pay/cs_live_abc123"}
```

---

#### POST /api/dashboard/portal

Creates a Stripe Customer Portal session via the relay. Returns `503` if the
relay is not configured.

```bash
curl -X POST \
  -H "Authorization: Bearer <JWT>" \
  -H "Content-Type: application/json" \
  -d '{"return_url": "https://app.example.com/app/"}' \
  https://dashboard.example.com/api/dashboard/portal
```

Response `200 OK`:
```json
{"url": "https://billing.stripe.com/session/live_abc123"}
```

---

## Error format

All error responses use a consistent JSON envelope:

```json
{"error": "human-readable message"}
```

Common HTTP status codes:

| Code | Meaning |
|------|---------|
| 400  | Bad request / validation error |
| 401  | Missing or invalid JWT |
| 404  | Resource not found |
| 429  | Rate limit exceeded |
| 501  | Feature not configured (admin store or relay missing) |
| 503  | Dependency unavailable (billing store or relay unreachable) |

---

## Pagination

Paginated endpoints return a consistent envelope:

```json
{
  "items": [...],
  "total": 42,
  "limit": 100,
  "offset": 0
}
```

Default `limit` is `100`; maximum is `500` (or `1000` for audit events).
Pass `?limit=N&offset=M` to page through results.
