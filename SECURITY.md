# Security Policy

## Reporting a Vulnerability

**Do not open a public issue for security vulnerabilities.**

Email **security@msg2agent.xyz** with the following information:

- Description of the vulnerability and its potential impact
- Steps to reproduce or proof-of-concept
- Affected versions

You will receive an acknowledgement within 48 hours. We follow a **90-day disclosure timeline**: we aim to release a fix within 90 days of the initial report, after which coordinated public disclosure may occur regardless of patch status.

## Supported Versions

| Version | Supported |
|---------|-----------|
| v0.1.x  | Yes       |
| < v0.1  | No        |

Only the latest patch release of the current minor version receives security fixes.

## Threat Model

### Trust Boundary

The **relay** is the central trust boundary. All agent-to-agent communication flows through the relay, which is responsible for authentication, routing, and audit logging.

### Authentication

Agents authenticate via **Decentralized Identifiers (DID)**. The relay maintains a DID allowlist; connections from unregistered DIDs are rejected. Each message is signed by the sender; the relay verifies the signature and enforces that the `From` field matches the authenticated DID.

### API Key Storage

API keys are stored as **SHA-256 hashes**. Plaintext keys are never persisted. Key comparison is performed using constant-time equality to prevent timing attacks.

### Audit Integrity

The relay maintains a tamper-evident audit chain. Each audit record includes a hash of the previous record; any gap or hash mismatch indicates tampering and is flagged at query time.

### Security Headers

The relay enforces HTTP security headers on all endpoints (HSTS, X-Frame-Options, X-Content-Type-Options, Content-Security-Policy) to reduce exposure from browser-based attack vectors.

## Additional Documentation

For a detailed security architecture overview, see `docs/` (architecture and operations guides).
