# Billing Database Backup & Restore

The billing database (`billing.db`) is the source of truth for invoicing. `usage_aggregates` is what gets exported to CSV for billing customers. Losing the file mid-month means losing revenue data.

## RPO / RTO targets

| Target | Approach |
|---|---|
| RPO ~1 hour | Hourly cron via `backup-billing.sh` |
| RTO < 5 minutes | Copy snapshot file + restart relay |

## Backup

`billing-admin backup` uses SQLite's `VACUUM INTO` — a consistent online backup that doesn't block readers or writers.

```bash
billing-admin -db /data/billing.db backup -out /var/backups/billing/billing-$(date +%Y%m%d).db
```

### Automated cron (hourly)

```cron
0 * * * * /opt/msg2agent/scripts/backup-billing.sh \
  -d /data/billing.db \
  -o /var/backups/billing \
  -r 30 \
  >> /var/log/billing-backup.log 2>&1
```

The script also calls `billing-admin verify` on the snapshot and rotates files older than 30 days.

## Verify

Prints a health summary of any billing DB:

```bash
billing-admin -db /var/backups/billing/billing-20260501.db verify
# schema version : 1
# tenants        : 12
# active keys    : 34
# aggregates     : 156
```

## Restore procedure

1. Stop the relay:
   ```bash
   systemctl stop msg2agent-relay
   # or: docker compose stop relay
   ```
2. Replace the live database with the snapshot:
   ```bash
   cp /var/backups/billing/billing-<timestamp>.db /data/billing.db
   ```
3. Verify the restored file:
   ```bash
   billing-admin -db /data/billing.db verify
   ```
4. Restart the relay. The in-memory meter will be restored from `usage_aggregates` on startup via `RestoreFromAggregates`.

## Disaster recovery (total loss)

If `billing.db` is lost without a snapshot:

- `usage_aggregates` is gone — monthly totals must be reconstructed from raw `usage_events` if those were exported separately.
- Tenant and API-key records are lost — re-create tenants and re-issue keys.
- **Mitigation**: enable the hourly cron and replicate the backup directory to object storage (S3, GCS, etc.).

## What is NOT preserved by purge

`billing-admin purge-events` deletes raw `usage_events` rows but leaves `usage_aggregates` intact. Invoicing is always based on `usage_aggregates`, so purging raw events is safe for billing accuracy. Take a backup before purging if you need the raw audit log for compliance.
