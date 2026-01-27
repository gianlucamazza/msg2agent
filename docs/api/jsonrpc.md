# JSON-RPC API Reference

This document describes the JSON-RPC methods available in msg2agent.

## Overview

msg2agent uses JSON-RPC 2.0 for all inter-agent communication. Messages are wrapped in the DIDComm envelope format.

### Message Envelope

```json
{
  "id": "unique-message-id",
  "type": "https://didcomm.org/basicmessage/2.0/message",
  "from": "did:wba:example.com:agent:alice",
  "to": ["did:wba:example.com:agent:bob"],
  "created_time": 1706180400,
  "body": {
    "jsonrpc": "2.0",
    "method": "method_name",
    "params": {},
    "id": "request-id"
  }
}
```

## Core Methods

### ping

Health check method.

**Request:**

```json
{
  "jsonrpc": "2.0",
  "method": "ping",
  "id": "1"
}
```

**Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "pong": true,
    "timestamp": 1706180400
  },
  "id": "1"
}
```

### echo

Echo back the provided message.

**Request:**

```json
{
  "jsonrpc": "2.0",
  "method": "echo",
  "params": {
    "message": "Hello, World!"
  },
  "id": "2"
}
```

**Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "echo": "Hello, World!"
  },
  "id": "2"
}
```

## A2A Protocol Methods

### message/send

Send a message to another agent (A2A protocol).

**Request:**

```json
{
  "jsonrpc": "2.0",
  "method": "message/send",
  "params": {
    "message": {
      "role": "user",
      "parts": [
        {
          "type": "text",
          "text": "Hello from Alice!"
        }
      ]
    }
  },
  "id": "3"
}
```

**Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "id": "task-uuid",
    "status": {
      "state": "completed"
    }
  },
  "id": "3"
}
```

### tasks/get

Get task status.

**Request:**

```json
{
  "jsonrpc": "2.0",
  "method": "tasks/get",
  "params": {
    "id": "task-uuid"
  },
  "id": "4"
}
```

**Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "id": "task-uuid",
    "status": {
      "state": "completed",
      "timestamp": "2025-01-25T10:00:00Z"
    },
    "history": [
      {
        "role": "user",
        "parts": [{ "type": "text", "text": "Hello" }]
      },
      {
        "role": "agent",
        "parts": [{ "type": "text", "text": "Hello back!" }]
      }
    ]
  },
  "id": "4"
}
```

### tasks/cancel

Cancel a running task.

**Request:**

```json
{
  "jsonrpc": "2.0",
  "method": "tasks/cancel",
  "params": {
    "id": "task-uuid"
  },
  "id": "5"
}
```

**Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "id": "task-uuid",
    "status": {
      "state": "canceled"
    }
  },
  "id": "5"
}
```

### message/stream

Stream a message and receive task updates via SSE (Server-Sent Events).

**Request:**

```json
{
  "jsonrpc": "2.0",
  "method": "message/stream",
  "params": {
    "message": {
      "role": "user",
      "parts": [{ "type": "text", "text": "Process this" }]
    },
    "sessionId": "optional-session-id"
  },
  "id": "6"
}
```

**Response (SSE stream):**

Multiple events are sent as the task progresses:

```json
// Initial status event
{"type": "status", "task": {"id": "task-uuid", "status": {"state": "submitted"}}}

// Working status event
{"type": "status", "task": {"id": "task-uuid", "status": {"state": "working"}}}

// Artifact event (if applicable)
{"type": "artifact", "artifact": {"type": "text", "data": "...", "index": 0, "lastChunk": true}}

// Final status event
{"type": "status", "task": {"id": "task-uuid", "status": {"state": "completed"}}, "final": true}
```

### tasks/resubscribe

Re-subscribe to an existing task's updates.

**Request:**

```json
{
  "jsonrpc": "2.0",
  "method": "tasks/resubscribe",
  "params": {
    "id": "task-uuid"
  },
  "id": "7"
}
```

**Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "id": "task-uuid",
    "status": {
      "state": "working"
    }
  },
  "id": "7"
}
```

## Discovery Methods

### agent.info

Get agent information (agent card).

**Request:**

```json
{
  "jsonrpc": "2.0",
  "method": "agent.info",
  "id": "8"
}
```

**Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "name": "alice",
    "description": "Alice's agent",
    "url": "https://example.com/.well-known/agent.json",
    "version": "1.0.0",
    "capabilities": {
      "streaming": true,
      "pushNotifications": false,
      "stateTransitionHistory": true
    },
    "skills": [
      {
        "id": "echo",
        "name": "Echo",
        "description": "Echoes messages back"
      }
    ]
  },
  "id": "8"
}
```

## MCP Methods

The MCP adapter exposes these tools:

### send_message

Send a message to another agent.

**Tool Input:**

```json
{
  "to_did": "did:wba:example.com:agent:bob",
  "method": "echo",
  "params": { "message": "Hello" }
}
```

**Tool Output:**

```json
{
  "result": { "echo": "Hello" },
  "from": "did:wba:example.com:agent:bob"
}
```

### discover_agent

Discover an agent by DID.

**Tool Input:**

```json
{
  "did": "did:wba:example.com:agent:bob"
}
```

**Tool Output:**

```json
{
  "name": "bob",
  "did": "did:wba:example.com:agent:bob",
  "capabilities": ["echo", "ping"]
}
```

### list_agents

List all known agents.

**Tool Input:**

```json
{}
```

**Tool Output:**

```json
{
  "agents": [
    { "did": "did:wba:example.com:agent:alice", "name": "alice" },
    { "did": "did:wba:example.com:agent:bob", "name": "bob" }
  ]
}
```

### register_handler

Register a method handler.

**Tool Input:**

```json
{
  "method": "custom_method"
}
```

**Tool Output:**

```json
{
  "registered": true,
  "method": "custom_method"
}
```

### get_identity

Get agent's identity information.

**Tool Input:**

```json
{}
```

**Tool Output:**

```json
{
  "did": "did:wba:example.com:agent:alice",
  "name": "alice",
  "domain": "example.com"
}
```

### start_task

Start an A2A task.

**Tool Input:**

```json
{
  "to_did": "did:wba:example.com:agent:bob",
  "message": "Process this request"
}
```

**Tool Output:**

```json
{
  "task_id": "task-uuid",
  "state": "submitted"
}
```

## Error Codes

| Code   | Message           | Description               |
| ------ | ----------------- | ------------------------- |
| -32700 | Parse error       | Invalid JSON              |
| -32600 | Invalid Request   | Invalid JSON-RPC request  |
| -32601 | Method not found  | Method doesn't exist      |
| -32602 | Invalid params    | Invalid method parameters |
| -32603 | Internal error    | Internal JSON-RPC error   |
| -32000 | Agent not found   | Target agent not found    |
| -32001 | Encryption failed | Message encryption failed |
| -32002 | Task not found    | Task ID not found         |
| -32003 | Task canceled     | Task was canceled         |
| -32004 | Rate limited      | Too many requests         |

### Error Response Example

```json
{
  "jsonrpc": "2.0",
  "error": {
    "code": -32601,
    "message": "Method not found",
    "data": {
      "method": "unknown_method"
    }
  },
  "id": "1"
}
```

## Task States

| State            | Description                      |
| ---------------- | -------------------------------- |
| `submitted`      | Task received, not yet processed |
| `working`        | Task is being processed          |
| `input_required` | Waiting for user input           |
| `completed`      | Task finished successfully       |
| `failed`         | Task failed with error           |
| `canceled`       | Task was canceled                |

## WebSocket Protocol

### Connection

Connect to relay or agent WebSocket endpoint:

```
ws://localhost:8080        # Relay
wss://localhost:8443       # Relay with TLS
ws://agent:8082            # Direct P2P
```

### Message Format

All messages are JSON-RPC wrapped in DIDComm envelopes:

```json
{
  "id": "msg-id",
  "type": "https://didcomm.org/basicmessage/2.0/message",
  "from": "did:wba:...",
  "to": ["did:wba:..."],
  "body": { "jsonrpc": "2.0", ... }
}
```

### Encrypted Messages

When encryption is enabled, the body is encrypted using X25519-XChaCha20-Poly1305:

```json
{
  "id": "msg-id",
  "type": "https://didcomm.org/encrypted/2.0/message",
  "from": "did:wba:...",
  "to": ["did:wba:..."],
  "body": {
    "protected": "base64url...",
    "recipients": [...],
    "iv": "base64url...",
    "ciphertext": "base64url...",
    "tag": "base64url..."
  }
}
```

## HTTP Endpoints

### Agent Card

```
GET /.well-known/agent.json
```

Returns the agent's A2A agent card:

```json
{
  "name": "alice",
  "url": "https://example.com/.well-known/agent.json",
  "version": "1.0.0",
  "capabilities": {...},
  "skills": [...]
}
```

### Health Check

```
GET /health
```

Returns `ok` if healthy.

### Readiness

```
GET /ready
```

Returns JSON status:

```json
{
  "status": "ready",
  "connections": 5
}
```

### Metrics

```
GET /metrics
```

Returns Prometheus metrics format.
