# Billing Audit Chain — Incident Response Runbook

## Detection

An alert fires when `billing_audit_chain_tampered_total > 0`.

```yaml
# Example AlertManager rule
- alert: BillingAuditChainTampered
  expr: increase(billing_audit_chain_tampered_total[5m]) > 0
  severity: critical
  annotations:
    summary: "Audit chain tampering detected (tenant {{ $labels.tenant_id }})"
```

## Triage

Identify the first bad event and when it occurred:

```bash
billing-admin verify-audit --all-tenants
# or for a specific tenant:
billing-admin verify-audit --tenant <tenant_id>
```

Output includes `FirstBadID` and `FirstBadTime`. Note the tenant ID and timestamp.

## Investigation

Query events around the tampered range:

```bash
billing-admin query-events --tenant <tenant_id> --from <FirstBadTime-5m> --to <FirstBadTime+5m>
```

Correlate with application logs and SSH/audit logs for the billing DB host around
the same timestamp. Look for:

- Direct DB writes (`INSERT INTO usage_events`) not via the relay binary
- File-level access to the SQLite file (check OS audit logs if available)
- Unexpected relay restarts or process crashes that may have interrupted a transaction

## Containment

1. Revoke all active API keys for the affected tenant:
   ```bash
   billing-admin list-keys --tenant <tenant_id>
   billing-admin revoke-key --id <key_id>
   ```

2. Suspend the tenant to block further relay/MCP access:
   ```bash
   billing-admin suspend-tenant --id <tenant_id>
   ```

3. If the DB file itself may be compromised, take it offline:
   ```bash
   # In docker-compose.odroid.yml: remove billing DB volume mount and restart
   docker compose -f infrastructure/docker-compose.odroid.yml restart relay
   ```

## Recovery

Restore from the most recent verified backup:

```bash
# List available backups (see docs/operations/billing-backup.md)
ls -lt /backups/billing/

# Verify backup integrity before restoring
billing-admin verify-audit --db /backups/billing/billing-<date>.db --all-tenants

# Replace live DB (relay must be stopped first)
docker compose -f infrastructure/docker-compose.odroid.yml stop relay
cp /backups/billing/billing-<date>.db /data/billing.db
docker compose -f infrastructure/docker-compose.odroid.yml start relay
```

Reconcile any events that occurred between the backup and the tamper detection using
application request logs.

## Post-Incident

- **GDPR Art. 33**: If the affected tenant has EU-resident users, notify the Data
  Protection Officer within 72 hours of discovery.
- File a root-cause analysis and update this runbook with new findings.
- Consider enabling SQLite WAL mode + filesystem-level checksums if not already active.
