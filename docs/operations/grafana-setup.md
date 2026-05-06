# Grafana Dashboard Setup

## Prerequisites

- Grafana 10.x running (or compatible 9.x)
- Prometheus datasource configured and scraping the relay `/metrics` endpoint

## Import the Dashboard

1. Open Grafana → Dashboards → Import
2. Upload `infrastructure/grafana/billing-dashboard.json`
3. Select your Prometheus datasource when prompted
4. Click **Import**

## Panels

| Panel | Metric | Alert threshold |
|-------|--------|-----------------|
| Billable Events Rate | `billing_usage_events_total` | — |
| Quota Usage Ratio | `billing_quota_usage_ratio` | Warn ≥ 0.8, Critical ≥ 1.0 |
| Quota Exceeded Events | `billing_quota_exceeded_total` | > 0 in last 1h |
| Audit Events Dropped | `billing_audit_dropped_total` | > 0 → investigate channel capacity |
| Rate Limited Requests | `billing_rate_limited_total` | Sustained > 10/min may indicate attack |
| Audit Chain Tampers | `billing_audit_chain_tampered_total` | > 0 → immediate incident (see audit-incident-response.md) |

## Recommended Alerts (Prometheus AlertManager)

```yaml
groups:
  - name: billing
    rules:
      - alert: QuotaUsageHigh
        expr: billing_quota_usage_ratio > 0.9
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Tenant {{ $labels.tenant_id }} near quota limit"

      - alert: AuditChainTampered
        expr: billing_audit_chain_tampered_total > 0
        for: 0m
        labels:
          severity: critical
        annotations:
          summary: "Audit chain tampering detected for tenant {{ $labels.tenant_id }}"
```
