/**
 * msg2agent OpenClaw Plugin
 *
 * Exposes msg2agent network capabilities as Claude-callable tools:
 *   - msg2agent_list_agents  — discover agents on the relay
 *   - msg2agent_send         — send JSON-RPC message to an agent
 *   - msg2agent_agent_info   — get details about a specific agent
 *   - msg2agent_self_info    — get this node's DID and status
 *   - msg2agent_inbox        — read incoming messages (polling)
 *   - msg2agent_inbox_clear  — mark all inbox messages as read
 *
 * Transport: MCP Streamable HTTP (mcp-go v0.43.2)
 * Endpoint:  http://localhost:3010/mcp  (configurable via plugin config)
 */

import { Type } from "@sinclair/typebox";

// ──────────────────────────────────────────────
// MCP HTTP Client
// ──────────────────────────────────────────────

interface MCPClientState {
  sessionId: string | null;
  requestId: number;
  initialized: boolean;
}

function createClient(baseUrl: string): {
  callTool: (name: string, args?: Record<string, unknown>) => Promise<string>;
  readResource: (uri: string) => Promise<string>;
  reset: () => void;
} {
  const state: MCPClientState = {
    sessionId: null,
    requestId: 0,
    initialized: false,
  };

  function nextId(): number {
    return ++state.requestId;
  }

  function headers(): Record<string, string> {
    const h: Record<string, string> = {
      "Content-Type": "application/json",
      "Accept": "application/json, text/event-stream",
    };
    if (state.sessionId) h["Mcp-Session-Id"] = state.sessionId;
    return h;
  }

  async function parseResponse(response: Response): Promise<unknown> {
    const ct = response.headers.get("content-type") || "";
    if (ct.includes("text/event-stream")) {
      const text = await response.text();
      for (const line of text.split("\n")) {
        if (line.startsWith("data: ") && !line.includes("[DONE]")) {
          try {
            return JSON.parse(line.slice(6));
          } catch {
            // skip malformed lines
          }
        }
      }
      throw new Error("No parseable data in SSE stream");
    }
    return response.json();
  }

  async function init(): Promise<void> {
    const response = await fetch(baseUrl, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "Accept": "application/json, text/event-stream",
      },
      body: JSON.stringify({
        jsonrpc: "2.0",
        id: nextId(),
        method: "initialize",
        params: {
          protocolVersion: "2024-11-05",
          capabilities: {},
          clientInfo: { name: "openclaw", version: "1.0.0" },
        },
      }),
    });

    if (!response.ok) {
      throw new Error(`MCP init failed: ${response.status} ${response.statusText}`);
    }

    const sid = response.headers.get("mcp-session-id");
    if (sid) state.sessionId = sid;

    await parseResponse(response);
    state.initialized = true;

    // Send initialized notification (fire-and-forget)
    fetch(baseUrl, {
      method: "POST",
      headers: headers(),
      body: JSON.stringify({ jsonrpc: "2.0", method: "notifications/initialized" }),
    }).catch(() => {});
  }

  async function ensureInit(): Promise<void> {
    if (!state.initialized) await init();
  }

  async function rpc(body: Record<string, unknown>): Promise<unknown> {
    await ensureInit();
    const response = await fetch(baseUrl, {
      method: "POST",
      headers: headers(),
      body: JSON.stringify(body),
    });
    if (!response.ok) {
      // Session might have expired — reset and retry once
      if (response.status === 404 || response.status === 400) {
        state.initialized = false;
        state.sessionId = null;
        await init();
        const retry = await fetch(baseUrl, {
          method: "POST",
          headers: headers(),
          body: JSON.stringify(body),
        });
        return parseResponse(retry);
      }
      throw new Error(`MCP request failed: ${response.status} ${response.statusText}`);
    }
    return parseResponse(response);
  }

  function extractText(data: unknown): string {
    const d = data as Record<string, unknown>;
    if (d?.error) {
      throw new Error(`MCP error: ${JSON.stringify(d.error)}`);
    }
    const result = d?.result as Record<string, unknown>;
    const content = result?.content as Array<Record<string, unknown>>;
    if (Array.isArray(content) && content.length > 0) {
      return content.map((c) => (c.text as string) || JSON.stringify(c)).join("\n");
    }
    // Resource read response
    const contents = result?.contents as Array<Record<string, unknown>>;
    if (Array.isArray(contents) && contents.length > 0) {
      return (contents[0].text as string) || JSON.stringify(contents[0]);
    }
    return JSON.stringify(result ?? data);
  }

  return {
    async callTool(name: string, args: Record<string, unknown> = {}): Promise<string> {
      const data = await rpc({
        jsonrpc: "2.0",
        id: nextId(),
        method: "tools/call",
        params: { name, arguments: args },
      });
      return extractText(data);
    },

    async readResource(uri: string): Promise<string> {
      const data = await rpc({
        jsonrpc: "2.0",
        id: nextId(),
        method: "resources/read",
        params: { uri },
      });
      return extractText(data);
    },

    reset(): void {
      state.sessionId = null;
      state.initialized = false;
    },
  };
}

// ──────────────────────────────────────────────
// Plugin Entry Point
// ──────────────────────────────────────────────

export default function register(api: {
  registerTool: (
    tool: {
      name: string;
      description: string;
      parameters: unknown;
      execute: (...args: unknown[]) => Promise<{ content: Array<{ type: string; text: string }> }>;
    },
    opts?: { optional?: boolean },
  ) => void;
  config?: { mcpUrl?: string };
}) {
  const mcpUrl: string =
    (api.config as Record<string, string> | undefined)?.mcpUrl ||
    process.env.MSG2AGENT_MCP_URL ||
    "http://localhost:3010/mcp";

  const client = createClient(mcpUrl);

  function ok(text: string) {
    return { content: [{ type: "text" as const, text }] };
  }

  function err(e: unknown) {
    const msg = e instanceof Error ? e.message : String(e);
    return { content: [{ type: "text" as const, text: `❌ msg2agent error: ${msg}` }] };
  }

  // ── list_agents ──────────────────────────────
  api.registerTool(
    {
      name: "msg2agent_list_agents",
      description:
        "Discover all agents registered on the msg2agent relay network. " +
        "Returns each agent's DID, name, capabilities, endpoints, and status. " +
        "Use this to find who you can communicate with.",
      parameters: Type.Object({
        capability: Type.Optional(
          Type.String({ description: "Optional capability filter (e.g. echo, ping)" }),
        ),
      }),
      async execute(_id: unknown, params: { capability?: string }) {
        try {
          const text = await client.callTool("list_agents", {
            capability: params.capability ?? "",
          });
          return ok(text);
        } catch (e) {
          return err(e);
        }
      },
    },
    { optional: true },
  );

  // ── send_message ─────────────────────────────
  api.registerTool(
    {
      name: "msg2agent_send",
      description:
        "Send a JSON-RPC message to another agent on the network identified by its DID. " +
        "The agent must be registered on the same relay. " +
        "Common methods: 'ping', 'echo', 'message/send'. " +
        "Returns the agent's JSON-RPC response.",
      parameters: Type.Object({
        to: Type.String({
          description:
            "Full DID of the recipient agent " +
            "(e.g. did:wba:msg2agent.home.gianlucamazza.it:agent:bob)",
        }),
        method: Type.String({
          description: "JSON-RPC method to invoke on the target (e.g. ping, echo, message/send)",
        }),
        params: Type.Optional(
          Type.String({ description: 'JSON-encoded parameters object. Default: "{}"' }),
        ),
      }),
      async execute(
        _id: unknown,
        p: { to: string; method: string; params?: string },
      ) {
        try {
          const text = await client.callTool("send_message", {
            to: p.to,
            method: p.method,
            params: p.params ?? "{}",
          });
          return ok(text);
        } catch (e) {
          return err(e);
        }
      },
    },
    { optional: true },
  );

  // ── agent_info ───────────────────────────────
  api.registerTool(
    {
      name: "msg2agent_agent_info",
      description:
        "Get detailed information about a specific agent: its A2A agent card, " +
        "DID document, capabilities, skills, and service endpoints.",
      parameters: Type.Object({
        did: Type.String({ description: "Full DID of the agent to inspect" }),
      }),
      async execute(_id: unknown, params: { did: string }) {
        try {
          const text = await client.callTool("get_agent_info", { did: params.did });
          return ok(text);
        } catch (e) {
          return err(e);
        }
      },
    },
    { optional: true },
  );

  // ── self_info ────────────────────────────────
  api.registerTool(
    {
      name: "msg2agent_self_info",
      description:
        "Get identity and status of the OpenClaw msg2agent node: " +
        "its own DID, display name, capabilities, and relay endpoints.",
      parameters: Type.Object({}),
      async execute() {
        try {
          const text = await client.callTool("get_self_info");
          return ok(text);
        } catch (e) {
          return err(e);
        }
      },
    },
    { optional: true },
  );

  // ── inbox (read) ─────────────────────────────
  api.registerTool(
    {
      name: "msg2agent_inbox",
      description:
        "Read incoming messages received by the OpenClaw agent from other agents. " +
        "Returns a list of messages with sender DID, method, body, and timestamp. " +
        "Use this to check for replies or unsolicited messages from the network.",
      parameters: Type.Object({
        unread_only: Type.Optional(
          Type.Boolean({ description: "If true, return only unread messages. Default: false" }),
        ),
      }),
      async execute(_id: unknown, params: { unread_only?: boolean }) {
        try {
          // The inbox resource returns all messages; we filter client-side if needed
          const raw = await client.readResource("msg2agent://inbox");
          if (!params.unread_only) return ok(raw);

          try {
            const msgs = JSON.parse(raw) as Array<Record<string, unknown>>;
            const unread = msgs.filter((m) => !m.read);
            return ok(unread.length ? JSON.stringify(unread, null, 2) : "📭 No unread messages.");
          } catch {
            return ok(raw);
          }
        } catch (e) {
          return err(e);
        }
      },
    },
    { optional: true },
  );

  // ── inbox_clear ──────────────────────────────
  api.registerTool(
    {
      name: "msg2agent_inbox_clear",
      description:
        "Reset the MCP session with the msg2agent server. " +
        "Use this if the connection seems stale or after receiving all pending inbox messages.",
      parameters: Type.Object({}),
      async execute() {
        client.reset();
        return ok("✅ msg2agent session reset. Next tool call will re-initialize.");
      },
    },
    { optional: true },
  );
}
