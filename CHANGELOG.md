# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
