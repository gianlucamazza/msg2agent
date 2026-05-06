#!/usr/bin/env bash
# backup-billing.sh — create a consistent snapshot of the billing database.
#
# Usage: backup-billing.sh [options]
#   -d <path>   Path to billing.db (default: $BILLING_DB or ./billing.db)
#   -o <dir>    Output directory for snapshots (default: ./backups)
#   -r <days>   Retention: delete snapshots older than N days (default: 30)
#   -b <binary> Path to billing-admin binary (default: billing-admin on PATH)
#
# Cron example (hourly, RPO ~1h):
#   0 * * * * /opt/msg2agent/scripts/backup-billing.sh -d /data/billing.db -o /var/backups/billing >> /var/log/billing-backup.log 2>&1

set -euo pipefail

DB="${BILLING_DB:-./billing.db}"
OUT_DIR="./backups"
RETENTION_DAYS=30
BILLING_ADMIN="billing-admin"

while getopts "d:o:r:b:" opt; do
	case $opt in
	d) DB="$OPTARG" ;;
	o) OUT_DIR="$OPTARG" ;;
	r) RETENTION_DAYS="$OPTARG" ;;
	b) BILLING_ADMIN="$OPTARG" ;;
	*)
		echo "unknown flag -$OPTARG" >&2
		exit 1
		;;
	esac
done

LOCKFILE="/tmp/backup-billing.lock"
exec 200>"$LOCKFILE"
if ! flock -n 200; then
	echo "$(date -u +%Y-%m-%dT%H:%M:%SZ) backup already running, skipping" >&2
	exit 0
fi

mkdir -p "$OUT_DIR"

TIMESTAMP="$(date -u +%Y%m%dT%H%M%SZ)"
DEST="${OUT_DIR}/billing-${TIMESTAMP}.db"

echo "$(date -u +%Y-%m-%dT%H:%M:%SZ) starting backup: $DB → $DEST"
"$BILLING_ADMIN" -db "$DB" backup -out "$DEST"
echo "$(date -u +%Y-%m-%dT%H:%M:%SZ) backup complete ($(du -sh "$DEST" | cut -f1))"

# Verify the snapshot.
"$BILLING_ADMIN" -db "$DEST" verify
echo "$(date -u +%Y-%m-%dT%H:%M:%SZ) snapshot verified"

# Rotate: remove files older than RETENTION_DAYS.
find "$OUT_DIR" -maxdepth 1 -name "billing-*.db" -mtime "+${RETENTION_DAYS}" -delete
echo "$(date -u +%Y-%m-%dT%H:%M:%SZ) rotated snapshots older than ${RETENTION_DAYS} days"
