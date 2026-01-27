# Glossary

Definitions of terms used in msg2agent documentation.

## A

### A2A (Agent-to-Agent)

A protocol specification by Google for agent interoperability. Defines how agents discover each other, exchange messages, and manage tasks. See [Architecture](architecture.md#a2a-agent-to-agent).

### ACL (Access Control List)

A security mechanism that defines which agents can invoke which methods. Uses principal patterns (DIDs or wildcards) to grant or deny access.

### Agent

An autonomous software entity with a unique identity (DID) that can send and receive messages. Agents register handlers for JSON-RPC methods and can connect to a relay or listen for direct connections.

### Agent Card

A JSON document describing an agent's capabilities, skills, and endpoints. Served at `/.well-known/agent.json`. Used for discovery in the A2A protocol.

## D

### DID (Decentralized Identifier)

A W3C standard for self-sovereign identity. In msg2agent, DIDs follow the format `did:wba:domain:agent:name` where `wba` stands for Web-Based Agent.

### DID Document

A JSON document containing the public keys and service endpoints associated with a DID. Used to verify signatures and establish encrypted connections.

### DIDComm

A messaging protocol built on DIDs. Messages are wrapped in envelopes that specify sender, recipients, and message type. msg2agent uses DIDComm 2.0 envelope format.

## E

### Ed25519

An elliptic curve digital signature algorithm used for message authentication. Each agent has an Ed25519 key pair for signing messages.

### Encryption

Optional message body encryption using X25519-XChaCha20-Poly1305. When enabled (`-require-encryption`), all message bodies are encrypted for the recipient.

## J

### JSON-RPC

A remote procedure call protocol using JSON. msg2agent uses JSON-RPC 2.0 for all method invocations. Requests contain `method`, `params`, and `id`; responses contain `result` or `error`.

## M

### MCP (Model Context Protocol)

A protocol for integrating tools with AI assistants. msg2agent provides an MCP adapter that exposes agent functionality as tools (e.g., `send_message`, `list_agents`).

### Message Envelope

The DIDComm wrapper around a JSON-RPC payload. Contains `id`, `type`, `from`, `to`, `created_time`, and `body` fields.

## O

### OTLP (OpenTelemetry Protocol)

A protocol for transmitting telemetry data (traces, metrics). msg2agent can export traces to OTLP-compatible backends like Jaeger.

## P

### P2P (Peer-to-Peer)

Direct agent-to-agent connections without going through a relay. Agents can listen on a WebSocket port (`-listen`) for incoming connections.

## R

### Relay

The central message routing hub. Agents connect to the relay via WebSocket, register their identity, and exchange messages. The relay routes messages by DID.

### Registry

The storage component within the relay that tracks registered agents, their DIDs, public keys, and connection status. Supports memory, file, and SQLite backends.

## S

### Signature

Cryptographic proof of message authenticity. All messages are signed with Ed25519. Recipients verify signatures using the sender's public key from their DID Document.

### Skill

A capability advertised in an agent's agent card. Skills describe what an agent can do (e.g., "Echo", "Summarize"). Part of the A2A protocol.

### SSE (Server-Sent Events)

A transport for streaming responses from server to client. Used for A2A task subscriptions where the server pushes updates.

### Store

The persistence layer for the relay registry. Options:

- **Memory**: Non-persistent, in-memory storage
- **File**: JSON file persistence
- **SQLite**: SQL database with WAL mode

## T

### Task

A unit of work in the A2A protocol. Tasks have states (`submitted`, `working`, `input_required`, `completed`, `failed`, `canceled`) and maintain a history of messages.

### TLS (Transport Layer Security)

Encryption for network connections. Enable with `-tls` flag. Required for production deployments. See [TLS Setup](deployment/tls-setup.md).

### Transport

The communication layer abstraction. Supports WebSocket (relay/P2P), stdio (MCP), and SSE (streaming).

## W

### WebSocket

A protocol for bidirectional communication over a single TCP connection. Primary transport for agent-relay and P2P connections.

## X

### X25519

An elliptic curve Diffie-Hellman function for key agreement. Used with XChaCha20-Poly1305 for message encryption. Each agent has an X25519 key pair for encryption.

### XChaCha20-Poly1305

An authenticated encryption algorithm. Combined with X25519 key agreement for end-to-end message encryption.
