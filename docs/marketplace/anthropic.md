# Publishing on the Anthropic Claude Marketplace

[Claude Marketplace](https://claude.com/platform/marketplace) (launched March 2026) lets enterprises purchase third-party Claude-powered software against their existing Anthropic commitments. **Anthropic does not take a revenue cut** — you keep 100% of what customers pay you.

## What you're publishing

msg2agent is distributed as a **remote MCP server** (Connector) that Claude connects to over HTTPS. Users add it as a Custom Connector from `claude.com/connectors` or from the Marketplace listing. The MCP tools (`list_agents`, `send_message`, `submit_task`, etc.) become available directly inside Claude conversations.

## Prerequisites

Before submitting, you need:

- A publicly reachable MCP server over HTTPS with a valid TLS certificate
- OAuth 2.0 authentication on the MCP endpoint (Anthropic requires it for Marketplace listings)
- A billing backend issuing API keys per tenant (see `pkg/billing/`)
- A privacy policy and terms of service URL

## Step 1 — Deploy a hosted MCP server

```bash
docker compose -f infrastructure/docker-compose.odroid.yml up -d
```

The stack exposes:
- `https://your-domain.com/mcp` — MCP Streamable HTTP endpoint (API key protected)
- `https://your-domain.com/health` — Health check
- `https://your-domain.com/.well-known/agent.json` — A2A AgentCard (public)

## Step 2 — Enable API key auth on the MCP endpoint

Set these environment variables in `docker-compose.odroid.yml`:

```yaml
MSG2AGENT_BILLING_DB: /data/billing.db
MSG2AGENT_AUTH_JWKS_URL: https://your-auth-provider.com/.well-known/jwks.json
MSG2AGENT_AUTH_ISSUER: https://your-auth-provider.com
MSG2AGENT_AUTH_AUDIENCE: https://relay.your-domain.com
```

Or use API key auth (simpler, supported by the `pkg/billing/middleware.go`):

```yaml
MSG2AGENT_API_KEY_AUTH: "true"
MSG2AGENT_BILLING_DB: /data/billing.db
```

## Step 3 — Test with Claude Code

Add manually in Claude Desktop → Settings → Connectors → Add custom connector → URL: `https://your-domain.com/mcp`.

Verify all 14 tools appear and `get_self_info` returns your agent's DID.

## Step 4 — Submit to Claude Marketplace

1. Go to [claude.com/platform/marketplace](https://claude.com/platform/marketplace) → "Submit a listing"
2. Fill in:
   - **Name**: msg2agent
   - **Category**: Developer Tools / Agent Infrastructure
   - **MCP endpoint URL**: `https://your-domain.com/mcp`
   - **Auth type**: API Key (Bearer) or OAuth2
   - **Short description** (140 chars):
     > Trustless cross-org agent network. Connect Claude to any DID-based agent — discover, message, delegate tasks.
   - **Long description**: paste content from `README.md`
   - **Privacy policy URL** and **Terms of service URL**
3. Submit for quality/security review (typically 5–10 business days)

## Pricing model (you set it)

Anthropic doesn't enforce pricing — you charge customers directly. Suggested model:

| Plan | Price | Limits |
|---|---|---|
| Starter | $19/mo | 5 agent DIDs, 10k messages/month |
| Team | $99/mo | 50 agents, 200k messages/month |
| Enterprise | Custom | Unlimited, SLA, SAML/SSO |

Enterprise customers can pay via existing Anthropic commitment spend.

## Connector manifest (for `claude.com/connectors` directory)

Prepare `connector-manifest.json` for the self-serve Connectors directory (separate from Marketplace):

```json
{
  "id": "msg2agent",
  "name": "msg2agent",
  "description": "Trustless agent-to-agent communication with W3C DID identity. Connect Claude to discover and message other AI agents securely.",
  "url": "https://your-domain.com/mcp",
  "auth": {
    "type": "api_key",
    "header": "Authorization",
    "scheme": "Bearer"
  },
  "category": "developer_tools",
  "logo_url": "https://your-domain.com/static/img/logo.png"
}
```

Submit via [claude.com/connectors/submit](https://claude.com/connectors/submit).
