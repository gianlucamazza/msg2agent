#!/usr/bin/env bash
# restore-billing.sh — restore a billing database snapshot.
#
# Companion to backup-billing.sh. Mode is auto-detected by file extension:
#   .db            SQLite backup → cp to BILLING_DB
#   .dump / .sql   Postgres dump → pg_restore / psql
#
# Usage:
#   ./scripts/restore-billing.sh <backup-file> [billing-db-path]
#
# SQLite example:
#   ./scripts/restore-billing.sh backups/billing-20260101T120000Z.db ~/.msg2agent/billing.db
#
# Postgres example:
#   BILLING_PG_DSN=postgres://msg2agent:secret@localhost/msg2agent \
#       ./scripts/restore-billing.sh backups/billing-20260101T120000Z.dump
#
# Environment:
#   BILLING_DB      SQLite destination path (default: ./billing.db)
#   BILLING_PG_DSN  Postgres DSN (required for .dump/.sql restores)

set -euo pipefail

BILLING_DB="${BILLING_DB:-./billing.db}"
BILLING_PG_DSN="${BILLING_PG_DSN:-}"

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

log_info() { echo -e "${GREEN}[restore-billing]${NC} $1"; }
log_warn() { echo -e "${YELLOW}[restore-billing]${NC} $1"; }
log_error() { echo -e "${RED}[restore-billing]${NC} $1" >&2; }

BACKUP_FILE="${1:-}"
if [[ -z "$BACKUP_FILE" ]]; then
	log_error "Usage: $0 <backup-file> [billing-db-path]"
	exit 1
fi

if [[ ! -f "$BACKUP_FILE" ]]; then
	log_error "Backup file not found: $BACKUP_FILE"
	exit 1
fi

# Override BILLING_DB from positional arg if provided.
if [[ -n "${2:-}" ]]; then
	BILLING_DB="$2"
fi

EXT="${BACKUP_FILE##*.}"

case "$EXT" in
db)
	log_warn "Restoring SQLite backup: $BACKUP_FILE → $BILLING_DB"
	if pgrep -f "relay\|mcp-server\|dashboard" &>/dev/null; then
		log_warn "WARNING: msg2agent processes appear to be running."
		log_warn "Stop them before restoring to avoid database corruption."
		read -rp "Continue anyway? [y/N] " confirm
		if [[ "${confirm,,}" != "y" ]]; then
			echo "Aborted."
			exit 0
		fi
	fi
	cp -p "$BACKUP_FILE" "$BILLING_DB"
	chmod 600 "$BILLING_DB"
	log_info "SQLite restore complete: $BILLING_DB"
	;;

dump | sql)
	if [[ -z "$BILLING_PG_DSN" ]]; then
		log_error "BILLING_PG_DSN is required for Postgres restore"
		exit 1
	fi
	if [[ "$EXT" == "dump" ]]; then
		if ! command -v pg_restore &>/dev/null; then
			log_error "pg_restore not found — install postgresql-client"
			exit 1
		fi
		log_info "Restoring Postgres dump: $BACKUP_FILE"
		pg_restore -d "$BILLING_PG_DSN" --clean --if-exists "$BACKUP_FILE"
	else
		if ! command -v psql &>/dev/null; then
			log_error "psql not found — install postgresql-client"
			exit 1
		fi
		log_info "Restoring Postgres SQL: $BACKUP_FILE"
		psql "$BILLING_PG_DSN" -f "$BACKUP_FILE"
	fi
	log_info "Postgres restore complete"
	;;

*)
	log_error "Unrecognised backup extension: .$EXT (expected .db, .dump, or .sql)"
	exit 1
	;;
esac
