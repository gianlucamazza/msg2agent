# Publishing on Google Cloud AI Agent Marketplace

Google Cloud's [AI Agent Marketplace](https://cloud.google.com/blog/topics/partners/google-cloud-ai-agent-marketplace) lets ISVs and technology partners sell A2A-compatible agents to Google Cloud customers. msg2agent is natively A2A-compliant (`adapters/a2a/`) with OAuth2/OIDC auth and `/.well-known/agent.json` already implemented.

## Why msg2agent fits

- `adapters/a2a/` implements the full A2A spec: `message/send`, `message/stream`, `tasks/get`, `tasks/list`, `tasks/cancel`, `tasks/resubscribe`
- `adapters/a2a/agent.go` serves `/.well-known/agent.json` (AgentCard) — required for Google validation
- `adapters/a2a/auth.go` includes `DefaultGoogleOAuth2Config()` for Google OIDC tokens
- 150+ organizations are in production routing real tasks over A2A (as of May 2026)

## Step 1 — Deploy with A2A endpoint

```bash
docker compose -f infrastructure/docker-compose.cloud.yml up -d
```

Your A2A server will be at `https://relay.your-domain.com/a2a`.

Check the agent card is reachable:

```bash
curl https://relay.your-domain.com/.well-known/agent.json | jq .
```

Expected output includes `"protocolVersions": ["1.0"]` and `"skills"`.

## Step 2 — Enable Google OAuth2

In `docker-compose.cloud.yml`, set the A2A server to validate Google tokens:

```yaml
MSG2AGENT_A2A_AUTH_AUDIENCE: https://relay.your-domain.com
MSG2AGENT_A2A_OAUTH2_ENABLED: "true"
```

This wires `DefaultGoogleOAuth2Config(audience)` from `adapters/a2a/auth.go:51` into the A2A handler.

Test with a Google service account:

```bash
TOKEN=$(gcloud auth print-identity-token --audiences=https://relay.your-domain.com)
curl -H "Authorization: Bearer $TOKEN" \
     -H "Content-Type: application/json" \
     -d '{"jsonrpc":"2.0","method":"message/send","params":{...},"id":1}' \
     https://relay.your-domain.com/a2a
```

## Step 3 — Validate with Google's A2A checker

Google provides a validation tool for the "Google Cloud Ready – Gemini Enterprise" badge:

```bash
# Install the A2A validator CLI
pip install google-cloud-a2a-validator

# Run validation
a2a-validate --url https://relay.your-domain.com \
             --audience https://relay.your-domain.com \
             --service-account your-sa@project.iam.gserviceaccount.com
```

All checks must pass before submission.

## Step 4 — Prepare the AgentCard

Edit `infrastructure/agentcard-msg2agent.json` with your production details, then verify it matches what `/.well-known/agent.json` returns:

```bash
diff <(curl -s https://relay.your-domain.com/.well-known/agent.json | jq -S .) \
     <(jq -S . infrastructure/agentcard-msg2agent.json)
```

## Step 5 — Submit to Google Cloud Marketplace

1. Join the [Google Cloud Partner Advantage program](https://cloud.google.com/partners)
2. Go to [Google Cloud Partner Portal](https://console.cloud.google.com/partner) → Products → Create listing
3. Select **AI Agent** as product type
4. Fill in:
   - **Agent endpoint**: `https://relay.your-domain.com`
   - **AgentCard URL**: `https://relay.your-domain.com/.well-known/agent.json`
   - **Auth type**: Google OAuth2 (OpenID Connect)
   - **Pricing**: choose Subscription, Usage-based, or Bring-your-own-license
   - **Skills**: copy from AgentCard `skills[]` array
5. Request "Google Cloud Ready – Gemini Enterprise" certification
6. Review takes 2–4 weeks

## Pricing options on Google Cloud Marketplace

| Model | How it works |
|---|---|
| **Subscription** | Fixed monthly fee billed through GCP. Customer sees it on their GCP invoice. |
| **Usage-based** | Per-message or per-task pricing reported via Google's Usage Reporting API. |
| **Private Offers** | Negotiate custom pricing with enterprise customers directly. |
| **Outcome-based** | Charge per completed task (requires webhook reporting back to GCP billing). |

Subscription is the simplest to start. Switch to usage-based once you have metering data from `pkg/billing/meter.go`.

## Maintaining the listing

- Update the AgentCard when you add new skills (new tool handlers in `pkg/mcp/server.go`)
- The A2A protocol version must stay at `"1.0"` or declare backward compatibility
- Google re-validates the agent card automatically every 30 days
