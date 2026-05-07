# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **Self-serve paid signup**: Starter ($19/mo) and Team ($99/mo) plans are now selectable on the pricing page. Signup creates a Stripe Checkout session and returns `checkout_url`; `BillingStatus` flips to `active` when `checkout.session.completed` fires.
- **Email verification**: after signup a magic-link is sent to the registered email (`/oauth/verify?token=…`). `Tenant.EmailVerifiedAt` is persisted on click. SMTP is configured via `MSG2AGENT_SMTP_*` env vars; missing config is a no-op (signup is never blocked).
- **Post-checkout activation polling**: the dashboard SPA polls `GET /api/dashboard/me` every 5 s for up to 120 s after a Stripe redirect, showing "Activating your plan…" until `billing_status` becomes `active`.
- **Enterprise CTA**: pricing page now shows a "Contact sales" link (`mailto:sales@msg2agent.xyz`) below the plan grid.
- `pkg/email`: new package with `Sender` interface, `NopSender`, and `SMTPSender` backed by `MSG2AGENT_SMTP_*` env vars.
- SQLite/Postgres migration v7: `tenants.email_verified_at` column and `email_verification_tokens` table.

### Changed

- **`past_due` enforcement**: tenants whose payment has failed (`BillingStatus=past_due`) now receive HTTP 402 (previously only `incomplete` was blocked). Stripe Customer Portal link is included in the response body.
- **Plan change via Customer Portal**: `customer.subscription.updated` now reverse-looks up the new Stripe Price ID and updates `tenant.Plan` and `tenant.Quota` accordingly. Previously only `BillingStatus` was updated.
- **Duplicate email guard**: `POST /api/tenants` returns HTTP 409 with a sign-in link if an active account already exists for the given email (prevents silent duplicate accounts).
- **Unknown Google sign-in**: instead of a hard 403 error page, signing in with a Google account that has no local tenant now redirects to `/pricing?email=…&reason=no-account` with the email pre-filled and an explanatory banner.
- Pricing page pre-selects the plan from `?plan=` URL param; landing page "Starter" and "Team" CTAs now link to `/pricing?plan=starter` / `/pricing?plan=team` instead of a waitlist email.
- Migrated production domain from `msg2agent.home.gianlucamazza.it` to `msg2agent.xyz`. Dashboard remains path-based at `/app/` under the apex.
- **Breaking (DID identity)**: gateway AgentCard DID changes from `did:wba:msg2agent.home.gianlucamazza.it:agent:gateway` to `did:wba:msg2agent.xyz:agent:gateway`. Consumers of `/.well-known/agent.json` must update their peer registries.
- Removed hardcoded domain from RFC 9728 metadata in relay and mcp-server — both now derive `resource` from `MSG2AGENT_OAUTH_AS_BASE_URL` at startup.

## [0.1.0] - 2026-05-06

### Added

- DID-based agent identity (`did:wba:domain:agent:name`) using W3C DID specification
- Relay hub with WebSocket transport, in-memory registry, and offline message queue
- MCP server adapter (stdio and streamable-HTTP transports) for AI assistant integration
- A2A protocol adapter for Google Agent-to-Agent interoperability
- Billing system: SaaS multi-tenant architecture with tenant isolation
- Stripe integration for subscription and usage-based billing
- API key authentication with scoped permissions and key rotation
- OAuth2/JWT authentication support
- SQLite and Postgres storage backends (pluggable)
- Usage metering with per-tenant quota enforcement
- Immutable audit chain for billing events
- Graceful shutdown with in-flight request draining
- Prometheus metrics endpoint (`/metrics`)
- OpenTelemetry tracing with OTLP and stdout exporters
- Security headers middleware (HSTS, CSP, X-Frame-Options, etc.)
- DID allowlist for access control
- Dependabot configuration for automated dependency updates
- `govulncheck` and `gosec` integration in CI pipeline
- Fuzz tests for message parsing and crypto primitives

[Unreleased]: https://github.com/gianlucamazza/msg2agent/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/gianlucamazza/msg2agent/releases/tag/v0.1.0
