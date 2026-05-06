# Billing Postgres Backend

msg2agent ships with an SQLite billing store by default. This document describes
when and how to migrate to Postgres for higher-scale deployments.

## When to switch

Consider Postgres when:

- **> 1 000 tenants** — SQLite's single-writer lock becomes a bottleneck
- **> 100 000 billable events/day** — WAL replay on restart is slow at this volume
- **Horizontal relay scaling** — multiple relay instances cannot share a single
  SQLite file; Postgres accepts concurrent connections from all instances
- **Managed backup requirements** — Postgres integrates with cloud-managed
  snapshots (Cloud SQL, RDS, Neon) without custom shell scripts

For single-server or self-hosted deployments, SQLite is simpler and has no
external dependency.

## Connection string (DSN)

Postgres uses the `pgx` driver. Standard libpq DSN format:

```
postgres://user:password@host:5432/dbname?sslmode=require
```

Environment variable: `BILLING_PG_DSN` (picked up by both `relay` and
`mcp-server` when `--billing-driver=postgres` is set).

## Connection pool tuning

`PostgresStore` uses `database/sql` defaults. Override for high-concurrency:

```go
// These are defaults; tune for your workload.
db.SetMaxOpenConns(25)       // match Postgres max_connections / relay instances
db.SetMaxIdleConns(5)
db.SetConnMaxLifetime(5 * time.Minute)
```

## Enabling on the relay

```bash
relay \
  --billing-driver=postgres \
  --billing-db="postgres://billing:secret@db:5432/billing?sslmode=require"
```

The `--billing-db` flag accepts either a file path (SQLite) or a DSN (Postgres)
when `--billing-driver=postgres` is set.

## Schema migrations

Migrations run automatically at startup via `PostgresStore.migrate()`. The
`schema_migrations` table tracks applied versions. Adding new migrations to
`pgMigrations` in `pkg/billing/postgres_store.go` is idempotent — already-applied
versions are skipped.

## Migrating from SQLite

There is no automated migration tool. The recommended path:

1. **Export data** from the SQLite store using `billing-admin`:
   ```bash
   billing-admin list-tenants --format=json > tenants.json
   billing-admin list-keys --all-tenants --format=json > keys.json
   ```

2. **Re-import** by calling the `NewStore("postgres", dsn)` factory and replaying
   the JSON via the admin tool or a one-off migration script. Usage event history
   does not migrate (events are append-only and billing cycles roll monthly).

3. **Verify** the migrated data:
   ```bash
   billing-admin verify-audit --all-tenants
   ```

## Backup

SQLite uses `billing-admin backup --dest path` (SQLite Online Backup API).

For Postgres, set `MSG2AGENT_PG_DUMP` to the path of `pg_dump` on the host:

```bash
export MSG2AGENT_PG_DUMP=/usr/bin/pg_dump
billing-admin backup --dest /backups/billing-$(date +%Y%m%d).dump
```

Without `MSG2AGENT_PG_DUMP` set, `Backup()` returns an error. In cloud
deployments, prefer the managed snapshot / PITR offered by your database
provider over shell-out backups.

## Behavioral differences from SQLite

| Aspect | SQLite | Postgres |
|---|---|---|
| Hash chain concurrency | Single-writer WAL (safe by design) | `pg_advisory_xact_lock` per tenant |
| `Backup()` | SQLite Online Backup API (built-in) | `pg_dump` shell-out |
| Timestamp storage | RFC 3339 text | `TIMESTAMPTZ` |
| Quota field | `TEXT` (JSON) | `JSONB` |

## Performance estimates

From k6 load tests (`make loadtest`) on a 2-vCPU instance:

| Endpoint | SQLite p95 | Postgres p95 |
|---|---|---|
| MCP tools/call | < 30 ms | < 25 ms |
| send_message relay | < 80 ms | < 60 ms |

Postgres shows better concurrency under load once the connection pool is warm.
SQLite is faster for cold single-request benchmarks due to zero network overhead.
