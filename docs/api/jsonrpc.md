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

## Relay Methods

These methods are handled by the relay hub over the WebSocket connection.

### relay.register

Register an agent with the relay. Requires DID ownership proof by default.

**Request:**

```json
{
  "jsonrpc": "2.0",
  "method": "relay.register",
  "params": {
    "id": "agent-uuid",
    "did": "did:wba:example.com:agent:alice",
    "display_name": "alice",
    "public_keys": [...],
    "endpoints": [...],
    "capabilities": [...],
    "proof": "<base64-signature>",
    "timestamp": 1706180400
  },
  "id": "1"
}
```

The `proof` field is a signature of `"<DID>:<timestamp>"` using the agent's Ed25519 signing key. The timestamp must be within 5 minutes of the relay's clock. Proof can be disabled with the `-skip-did-proof` flag (not recommended for production).

**Response:**

```json
{
  "jsonrpc": "2.0",
  "result": { "status": "registered" },
  "id": "1"
}
```

### relay.discover

Discover registered agents, optionally filtered by capability.

**Request:**

```json
{
  "jsonrpc": "2.0",
  "method": "relay.discover",
  "params": { "capability": "chat" },
  "id": "2"
}
```

**Response:** Returns an array of registered agent records.

### Presence Methods

#### relay.presence.update

Update the calling agent's presence status.

**Params:**

| Name        | Type   | Required | Description                                        |
| ----------- | ------ | -------- | -------------------------------------------------- |
| status      | string | yes      | One of: `online`, `offline`, `busy`, `away`, `dnd` |
| status_text | string | no       | Human-readable status text (max 256 chars)         |

#### relay.presence.subscribe

Subscribe to presence changes for one or more agents.

**Params:**

| Name | Type     | Required | Description                  |
| ---- | -------- | -------- | ---------------------------- |
| dids | string[] | yes      | List of DIDs to subscribe to |

Subscribers receive `relay.presence.update` notifications when a target's status changes.

#### relay.presence.unsubscribe

Unsubscribe from presence changes.

**Params:**

| Name | Type     | Required | Description                      |
| ---- | -------- | -------- | -------------------------------- |
| dids | string[] | yes      | List of DIDs to unsubscribe from |

#### relay.presence.query

Query current presence for one or more agents.

**Params:**

| Name | Type     | Required | Description           |
| ---- | -------- | -------- | --------------------- |
| dids | string[] | yes      | List of DIDs to query |

**Response:** A map of DID to presence info (`status`, `status_text`).

### Channel Methods

Channels allow group messaging between agents. Channel URIs use the format `channel:domain:name`.

#### relay.channel.create

Create a new channel. The caller becomes the channel owner.

**Params:**

| Name        | Type   | Required | Description                                    |
| ----------- | ------ | -------- | ---------------------------------------------- |
| name        | string | yes      | Channel name (used as `channel:domain:name`)   |
| type        | string | yes      | Channel type: `group`, `broadcast`, or `topic` |
| description | string | no       | Human-readable description                     |

**Response:** `{ "id": "channel-uuid", "name": "channel-name" }`

#### relay.channel.join

Join an existing channel.

**Params:**

| Name       | Type   | Required | Description                            |
| ---------- | ------ | -------- | -------------------------------------- |
| channel_id | string | no       | Channel UUID (one of id/name required) |
| name       | string | no       | Channel name                           |

#### relay.channel.leave

Leave a channel. The channel owner cannot leave (must delete instead).

**Params:** Same as `relay.channel.join`.

#### relay.channel.list

List channels the calling agent is a member of.

**Response:** Array of channel info objects (`id`, `name`, `type`, `owner`, `description`, `member_count`).

#### relay.channel.members

List members of a channel. Only members can see the member list.

**Params:** Same as `relay.channel.join`.

**Response:** Array of member objects (`did`, `role`, `joined_at`).

#### relay.channel.delete

Delete a channel. Only the channel owner can delete.

**Params:** Same as `relay.channel.join`.

All members receive a `relay.channel.deleted` notification.

#### relay.channel.sender_key

Distribute a sender key for E2E encryption in a group channel. Each member distributes their sender key so other members can decrypt their messages.

**Params:**

| Name          | Type   | Required | Description                            |
| ------------- | ------ | -------- | -------------------------------------- |
| channel_id    | string | no       | Channel UUID (one of id/name required) |
| name          | string | no       | Channel name                           |
| chain_key     | bytes  | yes      | Chain key for message encryption       |
| signature_key | bytes  | yes      | Signature key for verification         |

Other members receive a `relay.channel.sender_key` notification with the sender's DID and keys.

## Message Types

The message envelope (`pkg/messaging/message.go`) supports these message types:

| Type           | Description                    |
| -------------- | ------------------------------ |
| `request`      | RPC request (expects response) |
| `response`     | RPC response                   |
| `notification` | One-way notification           |
| `stream`       | Streaming data                 |
| `error`        | Error response                 |
| `chat`         | Conversational message         |
| `typing`       | Typing indicator               |
| `receipt`      | Delivery/read receipt          |
| `presence`     | Online status update           |
| `reaction`     | Emoji reaction to a message    |

### Message Envelope Fields

```json
{
  "id": "uuid-v7",
  "correlation_id": "uuid (for responses)",
  "from": "did:wba:...",
  "to": "did:wba:...",
  "type": "request",
  "method": "echo",
  "body": {},
  "signature": "<base64>",
  "timestamp": "2025-01-25T10:00:00Z",
  "encrypted": false,
  "thread_id": "uuid (conversation thread)",
  "parent_id": "uuid (for nested replies)",
  "thread_seq_no": 1,
  "request_ack": false
}
```

### Threading

Messages can be grouped into conversation threads:

- `thread_id` groups messages in a conversation. The first message in a thread sets `thread_id` to its own `id`.
- `parent_id` enables nested replies within a thread.
- `thread_seq_no` provides ordering within a thread.

### Delivery Acknowledgments

Set `request_ack: true` to receive a `relay.ack` notification with delivery status:

```json
{
  "message_id": "uuid",
  "delivered": true,
  "status": "delivered",
  "timestamp": "2025-01-25T10:00:00Z"
}
```

Possible statuses: `delivered`, `queued` (offline), `recipient not found`, `buffer full`, `queue failed`.

## Agent REST Endpoints

When the agent is started with `-http`, these additional HTTP endpoints are available:

### POST /send-chat

Send a chat message to another agent.

**Request body:** `{ "to": "did:wba:...", "text": "Hello" }`

**Response:** `{ "status": "sent", "id": "msg-uuid", "thread_id": "thread-uuid" }`

### POST /send-async

Fire-and-forget message with delivery acknowledgment.

**Request body:** `{ "to": "did:wba:...", "method": "echo", "params": {...} }`

**Response:** `{ "status": "sent", "id": "msg-uuid" }`

### POST /call

Synchronous RPC call to another agent.

**Request body:** `{ "to": "did:wba:...", "method": "echo", "params": {...} }`

**Response:** `{ "id": "msg-uuid", "result": "..." }`

### POST /typing

Send a typing indicator.

**Request body:** `{ "to": "did:wba:...", "typing": true }`

### GET /discover

List all agents registered on the relay.

### GET /chat-history

Get the agent's chat message history.

## MCP Tools

The MCP server exposes these tools for AI assistant integration:

### list_agents

List available agents on the network.

**Parameters:**

| Name       | Type   | Required | Description                      |
| ---------- | ------ | -------- | -------------------------------- |
| capability | string | no       | Optional capability to filter by |

### send_message

Send a message to another agent.

**Parameters:**

| Name   | Type   | Required | Description                |
| ------ | ------ | -------- | -------------------------- |
| to     | string | yes      | DID of the recipient agent |
| method | string | yes      | Method to call             |
| params | string | yes      | JSON string of parameters  |

### get_agent_info

Get detailed information about a specific agent including capabilities and endpoints.

**Parameters:**

| Name | Type   | Required | Description               |
| ---- | ------ | -------- | ------------------------- |
| did  | string | yes      | DID of the agent to query |

### get_self_info

Get information about this agent (self). No parameters.

### query_capabilities

Find agents that support specific capabilities.

**Parameters:**

| Name         | Type   | Required | Description                                   |
| ------------ | ------ | -------- | --------------------------------------------- |
| capabilities | string | yes      | Comma-separated list of required capabilities |

### submit_task

Submit a new A2A task to an agent. Returns the task with ID and initial status.

**Parameters:**

| Name       | Type   | Required | Description                                              |
| ---------- | ------ | -------- | -------------------------------------------------------- |
| agent_did  | string | yes      | DID of the target agent                                  |
| message    | string | yes      | JSON message to send to the agent                        |
| session_id | string | no       | Optional session ID to continue an existing conversation |

### get_task_status

Get the status of an A2A task by its ID.

**Parameters:**

| Name      | Type   | Required | Description                         |
| --------- | ------ | -------- | ----------------------------------- |
| task_id   | string | yes      | ID of the task                      |
| agent_did | string | yes      | DID of the agent that owns the task |

### cancel_task

Cancel an A2A task in progress.

**Parameters:**

| Name      | Type   | Required | Description                         |
| --------- | ------ | -------- | ----------------------------------- |
| task_id   | string | yes      | ID of the task to cancel            |
| agent_did | string | yes      | DID of the agent that owns the task |

### send_task_input

Send input to an A2A task that is waiting for user input.

**Parameters:**

| Name      | Type   | Required | Description                         |
| --------- | ------ | -------- | ----------------------------------- |
| task_id   | string | yes      | ID of the task                      |
| agent_did | string | yes      | DID of the agent that owns the task |
| message   | string | yes      | JSON message with the user input    |

### list_tasks

List all locally tracked A2A tasks. No parameters.

### list_messages

List messages in the inbox.

**Parameters:**

| Name        | Type    | Required | Description                          |
| ----------- | ------- | -------- | ------------------------------------ |
| unread_only | boolean | no       | If true, only return unread messages |

### read_message

Read a specific message by ID. Marks the message as read.

**Parameters:**

| Name | Type   | Required | Description               |
| ---- | ------ | -------- | ------------------------- |
| id   | string | yes      | ID of the message to read |

### delete_message

Delete a message from the inbox by ID.

**Parameters:**

| Name | Type   | Required | Description                 |
| ---- | ------ | -------- | --------------------------- |
| id   | string | yes      | ID of the message to delete |

### message_count

Get the count of messages in the inbox.

**Parameters:**

| Name        | Type    | Required | Description                         |
| ----------- | ------- | -------- | ----------------------------------- |
| unread_only | boolean | no       | If true, only count unread messages |

## MCP Resources

The MCP server exposes these resources:

| URI                      | Description                                        |
| ------------------------ | -------------------------------------------------- |
| `msg2agent://inbox`      | List of incoming messages from other agents        |
| `msg2agent://tasks`      | List of A2A tasks being tracked                    |
| `msg2agent://inbox/{id}` | Get a specific inbox message by ID (marks as read) |
| `msg2agent://tasks/{id}` | Get details of a specific tracked task             |

## Error Codes

| Code   | Message               | Description                                   |
| ------ | --------------------- | --------------------------------------------- |
| -32700 | Parse error           | Invalid JSON                                  |
| -32600 | Invalid Request       | Invalid JSON-RPC request                      |
| -32601 | Method not found      | Method doesn't exist                          |
| -32602 | Invalid params        | Invalid method parameters                     |
| -32603 | Internal error        | Internal JSON-RPC error                       |
| -32001 | Access denied         | ACL check failed                              |
| -32002 | Routing error         | Message routing failed                        |
| -32003 | Signature invalid     | Signature verification failed                 |
| -32004 | Decryption failed     | Message decryption failed                     |
| -32005 | Sender not registered | Sender not registered with relay              |
| -32006 | Sender mismatch       | Message `from` field doesn't match client DID |
| -32007 | Rate limited          | Rate limit exceeded                           |

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
