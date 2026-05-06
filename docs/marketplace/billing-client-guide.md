# Client Integration Guide — Billing & Rate Limits

This guide covers how to use a cloud-hosted msg2agent relay that enforces tenant billing. For self-hosted deployments without `--billing-db`, skip this guide — no authentication is required.

## Getting an API key

1. Your operator (or you, if you host your own relay) issues a key via the admin CLI:
   ```sh
   billing-admin -db billing.db issue-key --tenant t_01HXYZ... --name my-agent
   ```
2. The plaintext key is printed **once**. Store it in your secrets manager.

## Authenticating requests

Pass the key in every request as a Bearer token:

```
Authorization: Bearer msg2a_AbCdEfGh...
```

### MCP server

```sh
mcp-server \
  --relay wss://relay.msg2agent.io \
  --name my-agent \
  --transport streamable-http \
  --addr :3001
```

Set the environment variable before starting:

```sh
export MSG2AGENT_API_KEY=msg2a_AbCdEfGh...
```

Or pass the key in the WebSocket handshake when connecting a custom agent:

```go
header := http.Header{}
header.Set("Authorization", "Bearer " + apiKey)
conn, _, err := websocket.DefaultDialer.DialContext(ctx, relayURL, header)
```

### curl example

```sh
curl -H "Authorization: Bearer msg2a_AbCdEfGh..." \
     https://relay.msg2agent.io/health
```

## Error codes and client behaviour

| JSON-RPC code | Meaning            | Action                                    |
| ------------- | ------------------ | ----------------------------------------- |
| `-32007`      | Rate limited       | Exponential back-off, retry               |
| `-32008`      | Quota exceeded     | Do **not** retry; notify user or operator |
| `HTTP 401`    | Missing/bad key    | Check key; do not retry                   |
| `HTTP 403`    | Tenant suspended   | Contact your operator                     |

### Exponential back-off for -32007

When you receive `-32007`, wait before retrying:

```go
// Go example using CallRelayWithRetry (built into pkg/agent)
result, err := agent.CallRelayWithRetry(ctx, "relay.register", params)
// Automatically retries up to 5 times with exponential back-off (cap 30s).

// Manual back-off pattern
base := 500 * time.Millisecond
for attempt := range 5 {
    result, err := client.Call(ctx, method, params)
    if err == nil { break }
    if !isRateLimited(err) { return err } // don't retry other errors
    delay := min(time.Duration(1<<attempt)*base, 30*time.Second)
    time.Sleep(delay)
}
```

```python
# Python example
import time, random

def call_with_backoff(fn, max_attempts=5):
    for attempt in range(max_attempts):
        try:
            return fn()
        except RateLimitedError:
            if attempt == max_attempts - 1:
                raise
            delay = min(0.5 * (2 ** attempt), 30)
            time.sleep(delay + random.uniform(0, 0.1))  # jitter
```

### Handling -32008 (quota exceeded)

Do **not** retry quota-exceeded errors — they will fail until the billing period resets (1st of next calendar month UTC) or the plan is upgraded.

The error response includes a `data` field with upgrade information:

```json
{
  "error": {
    "code": -32008,
    "message": "DID quota exceeded for this tenant",
    "data": {
      "plan": "starter",
      "current": 5,
      "limit": 5,
      "upgrade_hint": "billing-admin set-plan --tenant t_01HXYZ... --plan team"
    }
  }
}
```

Surface this to your user or trigger an alert — the operator needs to upgrade the plan.

## Monthly quota limits

| Plan       | Messages/month | Tool calls/month | DIDs | Rate (msg/s) |
|------------|----------------|-----------------|------|-------------|
| free       | 1,000          | 5,000           | 3    | 5           |
| starter    | 10,000         | 50,000          | 5    | 10          |
| team       | 200,000        | 1,000,000       | 50   | 100         |
| enterprise | 1B             | 1B              | 100k | 10,000      |

Billing periods reset on the 1st of each calendar month (UTC).

## Rotating keys

Issue a new key first, deploy it, then revoke the old one:

```sh
billing-admin -db billing.db issue-key --tenant t_01HXYZ... --name production-v2
# Deploy the new key to your agents
billing-admin -db billing.db revoke-key --id k_01HABC...
```

In-flight requests using the old key at revocation time complete normally. All subsequent requests with the revoked key return HTTP 401.

## Reporting issues

Contact your relay operator. For the open-source relay, open a GitHub issue at `github.com/gianlucamazza/msg2agent`.
