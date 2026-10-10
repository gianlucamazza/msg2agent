# OpenClaw Plugin

The OpenClaw plugin is a bridge between [OpenClaw](https://github.com/openclaw/openclaw) (an open-source personal AI assistant) and a msg2agent relay. It allows OpenClaw to discover agents, send messages, and read incoming replies — all through MCP Streamable HTTP.

This plugin lives in this directory. It is **not published** on ClawHub or npm. Copy or point OpenClaw at these files and your own MCP server.

## Architecture

```
OpenClaw
  └─ msg2agent Plugin (index.ts)
       └─ MCP Streamable HTTP
            └─ msg2agent MCP Server (cmd/mcp-server)
                 └─ Agent (connected to relay)
                      └─ Relay Network
```

The plugin acts as an MCP client: it speaks JSON-RPC 2.0 over HTTP to the msg2agent MCP server, which in turn operates as a full agent on the relay network.

## Prerequisites

- **msg2agent MCP server** running with `-transport streamable-http` (see [MCP Server Configuration](../operations/configuration.md#mcp-server-configuration))
- **Relay** accessible from the MCP server
- **OpenClaw** installed and running

## Configuration

The plugin reads its MCP endpoint URL from (in order of priority):

1. `mcpUrl` in the plugin config (`openclaw.plugin.json`)
2. `MSG2AGENT_MCP_URL` environment variable
3. Default: `http://localhost:3010/mcp`

### Plugin config (`openclaw.plugin.json`)

```json
{
  "id": "msg2agent",
  "name": "msg2agent",
  "version": "0.1.0",
  "description": "Agent-to-agent communication via msg2agent relay.",
  "configSchema": {
    "type": "object",
    "properties": {
      "mcpUrl": {
        "type": "string",
        "description": "MCP streamable-http endpoint URL"
      }
    }
  }
}
```

Set `mcpUrl` to wherever the MCP server is listening, e.g. `http://<host-ip>:3010/mcp`.

## Available Tools

> **Note:** The `msg2agent_` prefix is added by the OpenClaw plugin and is not part of the underlying MCP tool names. The MCP server exposes tools as `list_agents`, `send_message`, `get_agent_info`, etc. OpenClaw prepends the plugin ID (`msg2agent`) to avoid naming conflicts with other plugins.

| Tool                    | MCP tool name    | Description                                | Parameters                                                           |
| ----------------------- | ---------------- | ------------------------------------------ | -------------------------------------------------------------------- |
| `msg2agent_list_agents` | `list_agents`    | Discover all agents on the relay           | `capability?` — optional filter (e.g. `echo`, `ping`)                |
| `msg2agent_send`        | `send_message`   | Send a JSON-RPC message to an agent        | `to` — recipient DID, `method` — RPC method, `params?` — JSON string |
| `msg2agent_agent_info`  | `get_agent_info` | Get agent card, DID document, capabilities | `did` — agent DID to inspect                                         |
| `msg2agent_self_info`   | `get_self_info`  | Get this node's own DID and status         | _(none)_                                                             |
| `msg2agent_inbox`       | `list_messages`  | Read incoming messages from other agents   | `unread_only?` — filter unread only                                  |
| `msg2agent_inbox_clear` | _(plugin-only)_  | Reset the MCP session                      | _(none)_                                                             |

## Usage Flow

A typical interaction in OpenClaw:

1. **Discover** — call `msg2agent_list_agents` to see who is online
2. **Inspect** — call `msg2agent_agent_info` with a DID to learn about an agent's skills
3. **Send** — call `msg2agent_send` to invoke a method on the target agent
4. **Check replies** — call `msg2agent_inbox` to read any incoming messages

## Quick Start

### Local development

```bash
# 1. Start the relay
./relay -addr :8080 -log-level debug

# 2. Start the MCP server in streamable-http mode
go build -o mcp-server ./cmd/mcp-server
./mcp-server \
  -name openclaw \
  -relay ws://localhost:8080 \
  -transport streamable-http \
  -addr :3001

# 3. Configure the plugin with mcpUrl = http://localhost:3001/mcp
```

### Docker Compose (self-host)

See [`infrastructure/docker-compose.selfhost.yml`](../../infrastructure/docker-compose.selfhost.yml) for a compose stack with relay, MCP server, and dashboard.

```bash
docker compose -f infrastructure/docker-compose.selfhost.yml up -d
```

The MCP server will be available at `http://<host>:3010/mcp`.

## Publishing to ClawHub (not done)

This plugin is **not listed** on [ClawHub](https://clawhub.ai) or npm. OpenClaw's registry is `clawhub.ai` (not `clawhub.io`). The steps below are a draft for a future publish; they do not mean a listing exists.

### Prepare a listing

1. **Run the MCP server** on an endpoint you control (local HTTP is enough).
2. **Enable API key auth** in your deployment (see `MSG2AGENT_BILLING_DB`) if you want keyed access.
3. **Update `openclaw.plugin.json`** — set `mcpUrl` to that endpoint (for example `http://localhost:3001/mcp`).

### Submit to ClawHub

Use the current OpenClaw / ClawHub publish flow from [docs.openclaw.ai/clawhub](https://docs.openclaw.ai/clawhub) (the `@clawhub/cli` package on npm is a different, unrelated project). Typical shape:

```bash
# See current OpenClaw docs for the supported CLI and flags.
clawhub login
clawhub skill publish ./docs/openclaw-plugin \
                --slug msg2agent \
                --name msg2agent \
                --version 0.1.0
```

### Pricing suggestions (hypothetical)

These are listing ideas only. This repo is self-host only and does not sell hosted tiers.

| Tier | Price | What to include |
|---|---|---|
| Free / self-hosted | $0 | Skill lists self-hosted setup |

## Files

- [`openclaw.plugin.json`](openclaw.plugin.json) — Plugin manifest and config schema
- [`index.ts`](index.ts) — Plugin implementation (MCP client + tool registrations)
