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

### Roadmap IV — Self-serve Paid Funnel
- Self-serve paid signup (Starter/Team) via Stripe Checkout — previously gated as "coming soon"
- `past_due` enforcement: tenants with failed payments receive HTTP 402 until payment is updated
- Plan changes via Stripe Customer Portal now update `tenant.Plan` and quota in real time
- Duplicate email guard on signup: 409 Conflict with sign-in link instead of silent second account
- Google sign-in with unknown email redirects to pricing page with email pre-filled
- Post-checkout activation polling in the dashboard SPA (spinner until `billing_status=active`)
- Email verification via magic-link (`/oauth/verify?token=…`); `Tenant.EmailVerifiedAt` persisted
- SMTP sender package (`pkg/email`) wired to signup flow; no-op when SMTP is not configured

## Next

These items are planned but not yet scheduled:

- **Stripe metered usage / overage billing**: push per-tenant tool-call and message counts as `subscription_item.usage_record` events to Stripe, enabling overage charges beyond plan hard caps. Current behaviour: hard caps (HTTP 429). Implementation requires metered Stripe prices and a background flush from `pkg/billing/meter.go`.
- **Resend verification email**: `POST /api/dashboard/email/resend` endpoint (rate-limited 1/min) for tenants who lost the original magic-link.
- **Team plan collaborators**: invite flow gated on `Tenant.EmailVerifiedAt != nil`.
- **Multi-region relay**: geo-distributed relay mesh with latency-aware routing
- **On-chain DID registry**: anchor `did:wba:` identifiers to a public ledger for trustless resolution
- **TypeScript SDK**: first-class client library for Node.js and browser environments
- **Mobile SDK**: iOS and Android client libraries for agent communication
