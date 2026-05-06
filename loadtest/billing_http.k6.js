/**
 * msg2agent billing HTTP load test
 * Tests MCP streamable-HTTP endpoint with API key auth.
 *
 * Usage:
 *   k6 run loadtest/billing_http.k6.js \
 *     -e MCP_URL=http://localhost:3001 \
 *     -e API_KEYS_FILE=loadtest/test-keys.txt
 */
import http from 'k6/http';
import { check, sleep } from 'k6';
import { Counter, Rate, Trend } from 'k6/metrics';

// Custom metrics
const toolCallErrors = new Counter('tool_call_errors');
const quotaExceeded = new Counter('quota_exceeded');
const authErrors = new Counter('auth_errors');
const toolCallDuration = new Trend('tool_call_duration_ms', true);
const successRate = new Rate('tool_call_success_rate');

export const options = {
  stages: [
    { duration: '30s', target: 20 },   // ramp up
    { duration: '2m', target: 100 },   // sustained load
    { duration: '1m', target: 200 },   // peak
    { duration: '30s', target: 0 },    // ramp down
  ],
  thresholds: {
    http_req_duration: ['p(95)<200'],
    tool_call_duration_ms: ['p(95)<100'],
    tool_call_success_rate: ['rate>0.95'],
    quota_exceeded: ['count<100'],
    auth_errors: ['count==0'],
  },
};

// Load API keys from env or fall back to a single test key
function loadKeys() {
  const file = __ENV.API_KEYS_FILE;
  if (!file) {
    const k = __ENV.API_KEY || '';
    return k ? [k] : [];
  }
  // In k6, open() reads a file relative to the script
  try {
    const content = open(file);
    return content.split('\n').map(l => l.trim()).filter(l => l.length > 0);
  } catch (_) {
    return [];
  }
}

const keys = loadKeys();

function pickKey() {
  if (keys.length === 0) return __ENV.API_KEY || 'sk_test_placeholder';
  return keys[Math.floor(Math.random() * keys.length)];
}

const mcpUrl = __ENV.MCP_URL || 'http://localhost:3001';

function callTool(apiKey, toolName, toolArgs) {
  const body = JSON.stringify({
    jsonrpc: '2.0',
    id: `${__VU}-${__ITER}`,
    method: 'tools/call',
    params: { name: toolName, arguments: toolArgs },
  });

  const params = {
    headers: {
      'Content-Type': 'application/json',
      'Authorization': `Bearer ${apiKey}`,
    },
    tags: { tool: toolName },
  };

  const start = Date.now();
  const res = http.post(`${mcpUrl}/mcp`, body, params);
  toolCallDuration.add(Date.now() - start);

  const ok = check(res, {
    'status 200': r => r.status === 200,
    'has result': r => {
      try {
        const j = JSON.parse(r.body);
        return j.result !== undefined || j.error !== undefined;
      } catch (_) { return false; }
    },
  });

  if (res.status === 401 || res.status === 403) { authErrors.add(1); }
  if (res.status === 429) { quotaExceeded.add(1); }
  if (!ok) { toolCallErrors.add(1); }
  successRate.add(ok ? 1 : 0);
}

export default function () {
  const key = pickKey();
  // 60% list_agents (tool call), 40% send_message (message event)
  if (Math.random() < 0.6) {
    callTool(key, 'list_agents', {});
  } else {
    callTool(key, 'send_message', {
      to: 'did:wba:localhost:agent:target',
      method: 'ping',
      params: { ts: Date.now() },
    });
  }
  sleep(0.1);
}
