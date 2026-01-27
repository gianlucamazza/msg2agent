# Monitoring Guide

This guide covers setting up Prometheus and Grafana for monitoring msg2agent components.

## Metrics Overview

### Relay Metrics

The relay exposes Prometheus metrics at `/metrics` on its main port.

| Metric                                   | Type    | Description                  |
| ---------------------------------------- | ------- | ---------------------------- |
| `msg2agent_relay_connections_total`      | Counter | Total WebSocket connections  |
| `msg2agent_relay_connections_active`     | Gauge   | Currently active connections |
| `msg2agent_relay_messages_total`         | Counter | Total messages processed     |
| `msg2agent_relay_messages_dropped_total` | Counter | Messages dropped (no route)  |
| `msg2agent_relay_message_bytes_total`    | Counter | Total bytes transferred      |
| `msg2agent_relay_rate_limited_total`     | Counter | Rate-limited requests        |

### Agent Metrics

Agents expose metrics on a separate port (default: 9090).

| Metric                                        | Type      | Description                 |
| --------------------------------------------- | --------- | --------------------------- |
| `msg2agent_agent_messages_sent_total`         | Counter   | Messages sent (by type)     |
| `msg2agent_agent_messages_received_total`     | Counter   | Messages received (by type) |
| `msg2agent_agent_handler_calls_total`         | Counter   | Handler invocations         |
| `msg2agent_agent_handler_duration_seconds`    | Histogram | Handler execution time      |
| `msg2agent_agent_active_connections`          | Gauge     | Active peer connections     |
| `msg2agent_agent_pending_messages`            | Gauge     | Messages awaiting delivery  |
| `msg2agent_agent_encryption_operations_total` | Counter   | Encryption ops (by type)    |

## Prometheus Configuration

### Basic Setup

```yaml
# prometheus.yml
global:
  scrape_interval: 15s
  evaluation_interval: 15s

scrape_configs:
  - job_name: "relay"
    static_configs:
      - targets: ["relay:8080"]
    metrics_path: /metrics

  - job_name: "agents"
    static_configs:
      - targets:
          - "agent-alice:9090"
          - "agent-bob:9090"
    metrics_path: /metrics
```

### Kubernetes Service Discovery

```yaml
# prometheus.yml for Kubernetes
scrape_configs:
  - job_name: "msg2agent"
    kubernetes_sd_configs:
      - role: pod
    relabel_configs:
      - source_labels: [__meta_kubernetes_pod_annotation_prometheus_io_scrape]
        action: keep
        regex: true
      - source_labels: [__meta_kubernetes_pod_annotation_prometheus_io_path]
        action: replace
        target_label: __metrics_path__
        regex: (.+)
      - source_labels:
          [__address__, __meta_kubernetes_pod_annotation_prometheus_io_port]
        action: replace
        regex: ([^:]+)(?::\d+)?;(\d+)
        replacement: $1:$2
        target_label: __address__
      - source_labels: [__meta_kubernetes_pod_label_app]
        action: keep
        regex: msg2agent
```

## Grafana Dashboards

### Relay Dashboard

Create a dashboard with these panels:

#### Connection Overview

```json
{
  "title": "Active Connections",
  "type": "stat",
  "targets": [
    {
      "expr": "msg2agent_relay_connections_active",
      "legendFormat": "Active"
    }
  ]
}
```

#### Message Throughput

```json
{
  "title": "Messages per Second",
  "type": "graph",
  "targets": [
    {
      "expr": "rate(msg2agent_relay_messages_total[5m])",
      "legendFormat": "Messages/s"
    }
  ]
}
```

#### Drop Rate

```json
{
  "title": "Message Drop Rate",
  "type": "graph",
  "targets": [
    {
      "expr": "rate(msg2agent_relay_messages_dropped_total[5m])",
      "legendFormat": "Dropped/s"
    }
  ]
}
```

### Agent Dashboard

#### Handler Latency

```json
{
  "title": "Handler Latency (p99)",
  "type": "graph",
  "targets": [
    {
      "expr": "histogram_quantile(0.99, rate(msg2agent_agent_handler_duration_seconds_bucket[5m]))",
      "legendFormat": "{{method}} p99"
    }
  ]
}
```

#### Message Flow

```json
{
  "title": "Message Flow",
  "type": "graph",
  "targets": [
    {
      "expr": "rate(msg2agent_agent_messages_sent_total[5m])",
      "legendFormat": "Sent {{type}}"
    },
    {
      "expr": "rate(msg2agent_agent_messages_received_total[5m])",
      "legendFormat": "Received {{type}}"
    }
  ]
}
```

## Alerting Rules

### Prometheus Alert Rules

```yaml
# alerts.yml
groups:
  - name: msg2agent
    rules:
      - alert: RelayDown
        expr: up{job="relay"} == 0
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "Relay is down"
          description: "Relay {{ $labels.instance }} has been down for more than 1 minute."

      - alert: HighDropRate
        expr: rate(msg2agent_relay_messages_dropped_total[5m]) > 10
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "High message drop rate"
          description: "Relay dropping {{ $value }} messages/s"

      - alert: HighLatency
        expr: histogram_quantile(0.99, rate(msg2agent_agent_handler_duration_seconds_bucket[5m])) > 1
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "High handler latency"
          description: "Handler {{ $labels.method }} p99 latency is {{ $value }}s"

      - alert: ConnectionSpike
        expr: rate(msg2agent_relay_connections_total[1m]) > 100
        for: 2m
        labels:
          severity: warning
        annotations:
          summary: "Connection spike detected"
          description: "{{ $value }} new connections/second"

      - alert: RateLimitTriggered
        expr: rate(msg2agent_relay_rate_limited_total[5m]) > 0
        for: 1m
        labels:
          severity: info
        annotations:
          summary: "Rate limiting active"
          description: "Rate limiting {{ $value }} requests/s"
```

## Docker Compose with Monitoring

```yaml
# docker-compose.monitoring.yml
version: "3.8"

services:
  relay:
    image: msg2agent/relay:latest
    ports:
      - "8080:8080"

  prometheus:
    image: prom/prometheus:latest
    ports:
      - "9090:9090"
    volumes:
      - ./prometheus.yml:/etc/prometheus/prometheus.yml
      - ./alerts.yml:/etc/prometheus/alerts.yml
      - prometheus-data:/prometheus
    command:
      - "--config.file=/etc/prometheus/prometheus.yml"
      - "--storage.tsdb.path=/prometheus"

  grafana:
    image: grafana/grafana:latest
    ports:
      - "3000:3000"
    environment:
      - GF_SECURITY_ADMIN_PASSWORD=admin
    volumes:
      - grafana-data:/var/lib/grafana

volumes:
  prometheus-data:
  grafana-data:
```

## OpenTelemetry Tracing

### Jaeger Setup

```yaml
# docker-compose.tracing.yml
version: "3.8"

services:
  jaeger:
    image: jaegertracing/all-in-one:latest
    ports:
      - "16686:16686" # UI
      - "4318:4318" # OTLP HTTP
    environment:
      - COLLECTOR_OTLP_ENABLED=true

  relay:
    image: msg2agent/relay:latest
    environment:
      - MSG2AGENT_OTLP_ENDPOINT=http://jaeger:4318
    depends_on:
      - jaeger
```

### Starting Components with Tracing

```bash
# Relay with OTLP tracing
./relay -addr :8080 -otlp-endpoint http://localhost:4318

# Agent with OTLP tracing
./agent -name alice -relay ws://localhost:8080 -otlp-endpoint http://localhost:4318

# Debug: stdout tracing
./relay -addr :8080 -trace-stdout
```

### Viewing Traces

1. Open Jaeger UI at http://localhost:16686
2. Select service "msg2agent-relay" or "msg2agent-agent"
3. Find traces by operation name or time range

## Health Checks

See [Configuration Guide](configuration.md#health-endpoints) for the full list of health endpoints.

### Checking Health

```bash
# Relay health
curl http://localhost:8080/health
# Response: ok

# Relay readiness
curl http://localhost:8080/ready
# Response: {"status":"ready","connections":5}

# Agent health
curl http://localhost:9090/health
# Response: ok
```

## Useful Queries

### Connection Analysis

```promql
# Connection rate over time
rate(msg2agent_relay_connections_total[5m])

# Peak connections in last hour
max_over_time(msg2agent_relay_connections_active[1h])

# Connection churn
rate(msg2agent_relay_connections_total[5m]) - rate(msg2agent_relay_connections_active[5m])
```

### Message Analysis

```promql
# Total throughput
sum(rate(msg2agent_relay_messages_total[5m]))

# Bytes per second
sum(rate(msg2agent_relay_message_bytes_total[5m]))

# Drop percentage
100 * rate(msg2agent_relay_messages_dropped_total[5m]) / rate(msg2agent_relay_messages_total[5m])
```

### Performance Analysis

```promql
# Handler latency percentiles
histogram_quantile(0.50, rate(msg2agent_agent_handler_duration_seconds_bucket[5m]))
histogram_quantile(0.95, rate(msg2agent_agent_handler_duration_seconds_bucket[5m]))
histogram_quantile(0.99, rate(msg2agent_agent_handler_duration_seconds_bucket[5m]))

# Slowest handlers
topk(5, histogram_quantile(0.99, rate(msg2agent_agent_handler_duration_seconds_bucket[5m])))
```
