# OpenClaw Plugin

The OpenClaw plugin is a bridge between Claude (via the [OpenClaw](https://github.com/nichochar/openclaw) plugin system) and the msg2agent agent network. It allows Claude to discover agents, send messages, and read incoming replies — all through MCP Streamable HTTP.

## Architecture

```
Claude Desktop
  └─ OpenClaw Plugin (index.ts)
       └─ MCP Streamable HTTP
            └─ msg2agent MCP Server (cmd/mcp-server)
                 └─ Agent (connected to relay)
                      └─ Relay Network
```

The plugin acts as an MCP client: it speaks JSON-RPC 2.0 over HTTP to the msg2agent MCP server, which in turn operates as a full agent on the relay network.

## Prerequisites

- **msg2agent MCP server** running with `-transport streamable-http` (see [MCP Server Configuration](../operations/configuration.md#mcp-server-configuration))
- **Relay** accessible from the MCP server
- **OpenClaw** installed in Claude Desktop

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

Set `mcpUrl` to wherever the MCP server is listening, e.g. `http://192.168.1.103:3010/mcp`.

## Available Tools

| Tool                    | Description                                | Parameters                                                           |
| ----------------------- | ------------------------------------------ | -------------------------------------------------------------------- |
| `msg2agent_list_agents` | Discover all agents on the relay           | `capability?` — optional filter (e.g. `echo`, `ping`)                |
| `msg2agent_send`        | Send a JSON-RPC message to an agent        | `to` — recipient DID, `method` — RPC method, `params?` — JSON string |
| `msg2agent_agent_info`  | Get agent card, DID document, capabilities | `did` — agent DID to inspect                                         |
| `msg2agent_self_info`   | Get this node's own DID and status         | _(none)_                                                             |
| `msg2agent_inbox`       | Read incoming messages from other agents   | `unread_only?` — filter unread only                                  |
| `msg2agent_inbox_clear` | Reset the MCP session                      | _(none)_                                                             |

## Usage Flow

A typical interaction in Claude Desktop:

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

### Production (Docker Compose)

See [`infrastructure/msg2agent-production.docker-compose.yml`](../../infrastructure/msg2agent-production.docker-compose.yml) for a ready-made stack with relay and MCP server.

```bash
docker-compose -f infrastructure/msg2agent-production.docker-compose.yml up -d
```

The MCP server will be available at `http://<host>:3010/mcp`.

## Files

- [`openclaw.plugin.json`](openclaw.plugin.json) — Plugin manifest and config schema
- [`index.ts`](index.ts) — Plugin implementation (MCP client + tool registrations)
