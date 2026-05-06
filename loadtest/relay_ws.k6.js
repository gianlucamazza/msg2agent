/**
 * msg2agent relay WebSocket load test
 * Connects agents to the relay hub and sends JSON-RPC messages.
 *
 * Usage:
 *   k6 run loadtest/relay_ws.k6.js \
 *     -e RELAY_URL=ws://localhost:8080
 */
import ws from 'k6/ws';
import { check, sleep } from 'k6';
import { Counter, Trend } from 'k6/metrics';

const wsErrors = new Counter('ws_errors');
const msgSent = new Counter('ws_messages_sent');
const msgRoundtrip = new Trend('ws_roundtrip_ms', true);

export const options = {
  stages: [
    { duration: '15s', target: 10 },
    { duration: '1m', target: 50 },
    { duration: '30s', target: 0 },
  ],
  thresholds: {
    ws_errors: ['count<10'],
    ws_roundtrip_ms: ['p(95)<500'],
  },
};

const relayUrl = __ENV.RELAY_URL || 'ws://localhost:8080';

export default function () {
  const agentID = `load-agent-${__VU}-${__ITER}`;
  const did = `did:wba:localhost:agent:${agentID}`;

  const res = ws.connect(relayUrl, {}, function (socket) {
    socket.on('open', () => {
      // Register agent
      socket.send(JSON.stringify({
        jsonrpc: '2.0',
        id: 1,
        method: 'relay.register',
        params: {
          id: agentID,
          did: did,
          display_name: agentID,
          public_keys: [],
          endpoints: [],
          capabilities: [],
          status: 'online',
        },
      }));
    });

    socket.on('message', (data) => {
      try {
        const msg = JSON.parse(data);
        if (msg.id === 1 && msg.result) {
          // Registration successful — send a few pings
          for (let i = 0; i < 5; i++) {
            const start = Date.now();
            socket.send(JSON.stringify({
              jsonrpc: '2.0',
              id: 100 + i,
              method: 'relay.discover',
              params: null,
            }));
            msgSent.add(1);
            msgRoundtrip.add(Date.now() - start);
          }
          sleep(0.5);
          socket.close();
        }
      } catch (_) {
        wsErrors.add(1);
      }
    });

    socket.on('error', () => { wsErrors.add(1); });

    socket.setTimeout(() => { socket.close(); }, 5000);
  });

  check(res, { 'ws connected': r => r && r.status === 101 });
  sleep(1);
}
