# Configuration Guide

This guide covers all configuration options for msg2agent components.

## Relay Configuration

### Command Line Flags

| Flag               | Default  | Description                                 |
| ------------------ | -------- | ------------------------------------------- |
| `-addr`            | `:8080`  | Listen address                              |
| `-tls`             | `false`  | Enable TLS                                  |
| `-tls-cert`        |          | TLS certificate file                        |
| `-tls-key`         |          | TLS private key file                        |
| `-store`           | `memory` | Store type: `memory`, `file`, `sqlite`      |
| `-store-file`      |          | Store file path (file/sqlite stores)        |
| `-max-connections` | `1000`   | Maximum concurrent connections              |
| `-msg-rate`        | `100`    | Message rate limit per client (msg/sec)     |
| `-log-level`       | `info`   | Log level: `debug`, `info`, `warn`, `error` |
| `-otlp-endpoint`   |          | OpenTelemetry OTLP endpoint                 |
| `-trace-stdout`    | `false`  | Output traces to stdout                     |

### Environment Variables

All flags can be set via environment variables with `MSG2AGENT_` prefix:

| Environment Variable        | Flag Equivalent    |
| --------------------------- | ------------------ |
| `MSG2AGENT_RELAY_ADDR`      | `-addr`            |
| `MSG2AGENT_TLS`             | `-tls`             |
| `MSG2AGENT_TLS_CERT`        | `-tls-cert`        |
| `MSG2AGENT_TLS_KEY`         | `-tls-key`         |
| `MSG2AGENT_STORE`           | `-store`           |
| `MSG2AGENT_STORE_FILE`      | `-store-file`      |
| `MSG2AGENT_MAX_CONNECTIONS` | `-max-connections` |
| `MSG2AGENT_MSG_RATE`        | `-msg-rate`        |
| `MSG2AGENT_LOG_LEVEL`       | `-log-level`       |
| `MSG2AGENT_OTLP_ENDPOINT`   | `-otlp-endpoint`   |
| `MSG2AGENT_TRACE_STDOUT`    | `-trace-stdout`    |

### Example Configurations

**Development:**

```bash
./relay -addr :8080 -log-level debug -trace-stdout
```

**Production with TLS and SQLite:**

```bash
./relay \
  -addr :8443 \
  -tls \
  -tls-cert /etc/msg2agent/server.crt \
  -tls-key /etc/msg2agent/server.key \
  -store sqlite \
  -store-file /var/lib/msg2agent/relay.db \
  -max-connections 5000 \
  -log-level info \
  -otlp-endpoint http://jaeger:4318
```

**Using environment variables:**

```bash
export MSG2AGENT_RELAY_ADDR=":8443"
export MSG2AGENT_TLS="true"
export MSG2AGENT_TLS_CERT="/etc/msg2agent/server.crt"
export MSG2AGENT_TLS_KEY="/etc/msg2agent/server.key"
export MSG2AGENT_STORE="sqlite"
export MSG2AGENT_STORE_FILE="/var/lib/msg2agent/relay.db"
./relay
```

## Agent Configuration

### Command Line Flags

| Flag                  | Default     | Description                        |
| --------------------- | ----------- | ---------------------------------- |
| `-name`               |             | Agent name (required)              |
| `-domain`             | `localhost` | Domain for DID                     |
| `-relay`              |             | Relay WebSocket URL                |
| `-http`               |             | HTTP server address for agent card |
| `-listen`             |             | P2P WebSocket listener address     |
| `-metrics`            |             | Metrics server address             |
| `-tls`                | `false`     | Enable TLS for listener            |
| `-http-tls`           | `false`     | Enable TLS for HTTP server         |
| `-tls-cert`           |             | TLS certificate file               |
| `-tls-key`            |             | TLS private key file               |
| `-tls-skip-verify`    | `false`     | Skip TLS verification (dev only)   |
| `-require-encryption` | `false`     | Require message encryption         |
| `-log-level`          | `info`      | Log level                          |
| `-otlp-endpoint`      |             | OpenTelemetry endpoint             |
| `-trace-stdout`       | `false`     | Output traces to stdout            |

### Environment Variables

| Environment Variable           | Flag Equivalent       |
| ------------------------------ | --------------------- |
| `MSG2AGENT_NAME`               | `-name`               |
| `MSG2AGENT_DOMAIN`             | `-domain`             |
| `MSG2AGENT_RELAY`              | `-relay`              |
| `MSG2AGENT_HTTP`               | `-http`               |
| `MSG2AGENT_LISTEN`             | `-listen`             |
| `MSG2AGENT_METRICS`            | `-metrics`            |
| `MSG2AGENT_TLS`                | `-tls`                |
| `MSG2AGENT_HTTP_TLS`           | `-http-tls`           |
| `MSG2AGENT_TLS_CERT`           | `-tls-cert`           |
| `MSG2AGENT_TLS_KEY`            | `-tls-key`            |
| `MSG2AGENT_TLS_SKIP_VERIFY`    | `-tls-skip-verify`    |
| `MSG2AGENT_REQUIRE_ENCRYPTION` | `-require-encryption` |
| `MSG2AGENT_LOG_LEVEL`          | `-log-level`          |
| `MSG2AGENT_OTLP_ENDPOINT`      | `-otlp-endpoint`      |

### Example Configurations

**Development:**

```bash
./agent -name alice -relay ws://localhost:8080 -log-level debug
```

**Production:**

```bash
./agent \
  -name production-agent \
  -domain example.com \
  -relay wss://relay.example.com:8443 \
  -http :8081 \
  -http-tls \
  -metrics :9090 \
  -tls-cert /etc/msg2agent/agent.crt \
  -tls-key /etc/msg2agent/agent.key \
  -require-encryption \
  -log-level info \
  -otlp-endpoint http://jaeger:4318
```

**P2P mode (no relay):**

```bash
./agent \
  -name alice \
  -domain example.com \
  -listen :8082 \
  -tls \
  -tls-cert server.crt \
  -tls-key server.key
```

## Store Configuration

### Memory Store (Default)

- No persistence
- Data lost on restart
- Good for development/testing

```bash
./relay -store memory
```

### File Store

- JSON file persistence
- Simple but not concurrent-safe
- Good for single-instance deployments

```bash
./relay -store file -store-file /var/lib/msg2agent/agents.json
```

### SQLite Store

- Full SQL persistence
- WAL mode for performance
- Good for production single-instance

```bash
./relay -store sqlite -store-file /var/lib/msg2agent/relay.db
```

SQLite features:

- Automatic migrations
- WAL journal mode
- 5-second busy timeout
- Foreign key support

## Logging Configuration

### Log Levels

| Level   | Description                   | Use Case               |
| ------- | ----------------------------- | ---------------------- |
| `debug` | All messages including traces | Development, debugging |
| `info`  | Operational messages          | Normal production      |
| `warn`  | Warnings and potential issues | Quiet production       |
| `error` | Errors only                   | Minimal logging        |

### Structured Logging

Output is JSON when stdout is not a TTY:

```json
{
  "time": "2025-01-25T10:00:00Z",
  "level": "INFO",
  "msg": "server started",
  "addr": ":8080"
}
```

Human-readable format when stdout is a TTY:

```
2025/01/25 10:00:00 INFO server started addr=:8080
```

## TLS Configuration

### Generating Certificates

See [TLS Setup Guide](../deployment/tls-setup.md) for detailed instructions.

### Certificate Requirements

- X.509 format
- PEM encoded
- Key must match certificate
- SAN extension recommended

### Minimum TLS Version

The relay enforces TLS 1.2 minimum.

## Metrics Configuration

### Prometheus Metrics

Both relay and agents expose metrics at `/metrics`:

```bash
# Relay metrics
curl http://localhost:8080/metrics

# Agent metrics (separate port)
./agent -name alice -metrics :9090
curl http://localhost:9090/metrics
```

### OpenTelemetry Tracing

```bash
# OTLP HTTP endpoint (Jaeger, Tempo, etc.)
./relay -otlp-endpoint http://localhost:4318

# Stdout for debugging
./relay -trace-stdout
```

## Health Endpoints

### Relay

| Endpoint  | Purpose   | Response         |
| --------- | --------- | ---------------- |
| `/health` | Liveness  | `ok`             |
| `/ready`  | Readiness | JSON with status |

### Agent (on metrics port)

| Endpoint  | Purpose      | Response |
| --------- | ------------ | -------- |
| `/health` | Liveness     | `ok`     |
| `/ready`  | Readiness    | `ok`     |
| `/live`   | K8s liveness | `ok`     |

## Resource Limits

### Relay Limits

| Resource        | Default | Notes                 |
| --------------- | ------- | --------------------- |
| Max connections | 1000    | Per-relay limit       |
| Message size    | 1MB     | Enforced by WebSocket |
| Rate limit      | 100/min | Per-client            |

### Agent Limits

| Resource       | Default | Notes         |
| -------------- | ------- | ------------- |
| TaskStore size | 10000   | Maximum tasks |
| Task TTL       | 24h     | Auto-cleanup  |
| Dedup cache    | 10000   | Message IDs   |
| Dedup TTL      | 5min    | Cache expiry  |

## Security Recommendations

### Production Checklist

- [ ] Enable TLS (`-tls`)
- [ ] Use valid certificates (not self-signed)
- [ ] Enable encryption (`-require-encryption`)
- [ ] Set appropriate rate limits
- [ ] Use SQLite or external DB for persistence
- [ ] Configure log level to `info` or `warn`
- [ ] Set up monitoring with Prometheus
- [ ] Enable tracing with OpenTelemetry

### Development Checklist

- [ ] Use `-tls-skip-verify` only in dev
- [ ] Use memory store for fast iteration
- [ ] Enable debug logging
- [ ] Use stdout tracing
