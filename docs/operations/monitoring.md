# Monitoring Guide

This guide covers setting up Prometheus and Grafana for monitoring msg2agent components.

## Metrics Overview

### Relay Metrics

The relay exposes Prometheus metrics at `/metrics` on its main port. All relay metrics use the `relay_` prefix.

| Metric                                      | Type    | Labels   | Description                                                                |
| ------------------------------------------- | ------- | -------- | -------------------------------------------------------------------------- |
| `relay_connections_total`                   | Counter |          | Total WebSocket connections accepted                                       |
| `relay_connections_current`                 | Gauge   |          | Current number of active connections                                       |
| `relay_connections_rejected_total`          | Counter |          | Connections rejected due to limit                                          |
| `relay_messages_routed_total`               | Counter |          | Messages successfully routed                                               |
| `relay_messages_dropped_total`              | Counter | `reason` | Messages dropped (e.g. `recipient_not_found`, `buffer_full`, `queue_full`) |
| `relay_messages_queued_total`               | Counter |          | Messages queued for offline delivery                                       |
| `relay_messages_delivered_from_queue_total` | Counter |          | Queued messages delivered when recipient came online                       |
| `relay_rate_limit_hits_total`               | Counter | `type`   | Rate limit hits (e.g. `message`, `register`, `discover`)                   |
| `relay_registrations_total`                 | Counter |          | Agent registrations                                                        |
| `relay_discoveries_total`                   | Counter |          | Discovery requests                                                         |
| `relay_errors_total`                        | Counter | `type`   | Errors (e.g. `websocket_accept`, `discovery`)                              |

### Agent Metrics

Agents expose metrics on a separate port (configured via `-metrics`). Agent metrics use a configurable namespace (default: `agent`), so metric names follow the pattern `{namespace}_metric_name`.

| Metric                          | Type      | Labels               | Description                                 |
| ------------------------------- | --------- | -------------------- | ------------------------------------------- |
| `{ns}_messages_sent_total`      | Counter   | `method`, `peer_did` | Messages sent                               |
| `{ns}_messages_received_total`  | Counter   | `method`, `peer_did` | Messages received                           |
| `{ns}_message_errors_total`     | Counter   | `reason`             | Message errors                              |
| `{ns}_peers_connected`          | Gauge     |                      | Current number of connected peers           |
| `{ns}_peers_total`              | Counter   |                      | Total peer connections (lifetime)           |
| `{ns}_handler_calls_total`      | Counter   | `method`             | Handler invocations                         |
| `{ns}_handler_duration_seconds` | Histogram | `method`             | Handler execution time                      |
| `{ns}_handler_errors_total`     | Counter   | `method`             | Handler errors                              |
| `{ns}_encryption_ops_total`     | Counter   | `status`             | Encryption operations (`success`/`failure`) |
| `{ns}_decryption_ops_total`     | Counter   | `status`             | Decryption operations (`success`/`failure`) |
| `{ns}_signature_ops_total`      | Counter   | `status`             | Signature operations (`success`/`failure`)  |
| `{ns}_duplicates_dropped_total` | Counter   |                      | Duplicate messages dropped                  |
| `{ns}_tasks_created_total`      | Counter   | `task_state`         | A2A tasks created                           |
| `{ns}_tasks_completed_total`    | Counter   | `task_state`         | A2A tasks completed                         |
| `{ns}_task_duration_seconds`    | Histogram | `task_state`         | A2A task duration                           |

> **Note:** The default namespace is `agent`, so metrics appear as `agent_messages_sent_total`, etc. Custom namespaces can be set via `telemetry.NewAgentMetrics(namespace)`.

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
      "expr": "relay_connections_current",
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
      "expr": "rate(relay_messages_routed_total[5m])",
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
      "expr": "rate(relay_messages_dropped_total[5m])",
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
      "expr": "histogram_quantile(0.99, rate(agent_handler_duration_seconds_bucket[5m]))",
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
      "expr": "rate(agent_messages_sent_total[5m])",
      "legendFormat": "Sent {{method}}"
    },
    {
      "expr": "rate(agent_messages_received_total[5m])",
      "legendFormat": "Received {{method}}"
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
        expr: rate(relay_messages_dropped_total[5m]) > 10
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "High message drop rate"
          description: "Relay dropping {{ $value }} messages/s"

      - alert: HighLatency
        expr: histogram_quantile(0.99, rate(agent_handler_duration_seconds_bucket[5m])) > 1
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "High handler latency"
          description: "Handler {{ $labels.method }} p99 latency is {{ $value }}s"

      - alert: ConnectionSpike
        expr: rate(relay_connections_total[1m]) > 100
        for: 2m
        labels:
          severity: warning
        annotations:
          summary: "Connection spike detected"
          description: "{{ $value }} new connections/second"

      - alert: RateLimitTriggered
        expr: rate(relay_rate_limit_hits_total[5m]) > 0
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
rate(relay_connections_total[5m])

# Peak connections in last hour
max_over_time(relay_connections_current[1h])

# Connection churn (new connections minus current)
rate(relay_connections_total[5m])
```

### Message Analysis

```promql
# Total throughput
sum(rate(relay_messages_routed_total[5m]))

# Drop percentage
100 * rate(relay_messages_dropped_total[5m]) / rate(relay_messages_routed_total[5m])

# Queue backlog rate
rate(relay_messages_queued_total[5m]) - rate(relay_messages_delivered_from_queue_total[5m])
```

### Performance Analysis

```promql
# Handler latency percentiles
histogram_quantile(0.50, rate(agent_handler_duration_seconds_bucket[5m]))
histogram_quantile(0.95, rate(agent_handler_duration_seconds_bucket[5m]))
histogram_quantile(0.99, rate(agent_handler_duration_seconds_bucket[5m]))

# Slowest handlers
topk(5, histogram_quantile(0.99, rate(agent_handler_duration_seconds_bucket[5m])))
```
