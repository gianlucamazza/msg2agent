# Load Tests

k6-based load tests for the msg2agent relay and MCP billing stack.

## Prerequisites

Install k6: https://grafana.com/docs/k6/latest/set-up/install-k6/

```bash
# Arch Linux
yay -S k6

# macOS
brew install k6
```

## Usage

### MCP Billing HTTP (tools/call)

```bash
make loadtest MCP_URL=http://localhost:3001 API_KEY=sk_live_yourkey
```

Or with multiple keys (one per line):
```bash
echo -e "sk_live_key1\nsk_live_key2" > loadtest/test-keys.txt
make loadtest-http MCP_URL=http://localhost:3001 API_KEYS_FILE=loadtest/test-keys.txt
```

### Relay WebSocket

```bash
make loadtest-ws RELAY_URL=ws://localhost:8080
```

## Thresholds

| Metric | Threshold |
|--------|-----------|
| `http_req_duration` p95 | < 200ms |
| `tool_call_duration_ms` p95 | < 100ms |
| `tool_call_success_rate` | > 95% |
| `quota_exceeded` count | < 100 |
| `auth_errors` count | == 0 |
| `ws_roundtrip_ms` p95 | < 500ms |

## Notes

- `billing_http.k6.js` requires a running MCP server with a valid API key.
- `relay_ws.k6.js` can run against an unauthenticated relay endpoint for baseline testing.
- Use `--out influxdb=http://localhost:8086/k6` to push metrics to InfluxDB + Grafana.
