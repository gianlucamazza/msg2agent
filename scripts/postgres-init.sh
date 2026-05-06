#!/usr/bin/env bash
# postgres-init.sh — idempotent Postgres DB + role bootstrap for msg2agent.
#
# Creates the database and role if they do not already exist, then grants
# the permissions the Go billing store needs. Table migration is handled
# by the Go store on first startup (pgMigrations in postgres_store.go).
#
# Usage:
#   DBPASS=secret ./scripts/postgres-init.sh
#   DBNAME=mydb DBUSER=myuser DBPASS=secret ./scripts/postgres-init.sh
#
# Required environment:
#   DBPASS        Password for the application role (no default)
#
# Optional environment:
#   PGHOST        Postgres host (default: localhost)
#   PGPORT        Postgres port (default: 5432)
#   PGSUPERUSER   Superuser to connect as (default: postgres)
#   PGSUPERPASS   Superuser password (if needed; uses .pgpass / peer auth if empty)
#   DBNAME        Database name to create (default: msg2agent)
#   DBUSER        Role name to create (default: msg2agent)

set -euo pipefail

PGHOST="${PGHOST:-localhost}"
PGPORT="${PGPORT:-5432}"
PGSUPERUSER="${PGSUPERUSER:-postgres}"
PGSUPERPASS="${PGSUPERPASS:-}"
DBNAME="${DBNAME:-msg2agent}"
DBUSER="${DBUSER:-msg2agent}"

if [[ -z "${DBPASS:-}" ]]; then
	echo "error: DBPASS is required" >&2
	exit 1
fi

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log_info() { echo -e "${GREEN}[postgres-init]${NC} $1"; }
log_warn() { echo -e "${YELLOW}[postgres-init]${NC} $1"; }

if ! command -v psql &>/dev/null; then
	echo "error: psql not found — install postgresql-client" >&2
	exit 1
fi

export PGHOST PGPORT
if [[ -n "$PGSUPERPASS" ]]; then
	export PGPASSWORD="$PGSUPERPASS"
fi

psql_super() {
	psql -U "$PGSUPERUSER" -v ON_ERROR_STOP=1 "$@"
}

log_info "Connecting to Postgres at $PGHOST:$PGPORT as $PGSUPERUSER"

# --- Create role (idempotent) ---

log_info "Ensuring role '$DBUSER' exists..."
psql_super -d postgres <<SQL
DO \$\$
BEGIN
    IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = '$DBUSER') THEN
        CREATE ROLE "$DBUSER" LOGIN PASSWORD '$DBPASS';
        RAISE NOTICE 'Role $DBUSER created.';
    ELSE
        ALTER ROLE "$DBUSER" PASSWORD '$DBPASS';
        RAISE NOTICE 'Role $DBUSER already exists — password updated.';
    END IF;
END
\$\$;
SQL

# --- Create database (idempotent via SELECT ... \gexec) ---

log_info "Ensuring database '$DBNAME' exists..."
psql_super -d postgres -tc \
	"SELECT 'CREATE DATABASE \"$DBNAME\" OWNER \"$DBUSER\"' WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = '$DBNAME')" |
	psql_super -d postgres

# --- Grants ---

log_info "Granting privileges to '$DBUSER' on '$DBNAME'..."
psql_super -d "$DBNAME" <<SQL
GRANT CONNECT ON DATABASE "$DBNAME" TO "$DBUSER";
GRANT USAGE, CREATE ON SCHEMA public TO "$DBUSER";
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT ALL ON TABLES TO "$DBUSER";
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT ALL ON SEQUENCES TO "$DBUSER";
SQL

# --- Print DSN ---

echo ""
log_info "Database ready. Add this to your environment:"
echo ""
echo "  BILLING_PG_DSN=postgres://$DBUSER:$DBPASS@$PGHOST:$PGPORT/$DBNAME?sslmode=disable"
echo ""
log_warn "Change sslmode=disable to sslmode=require in production."
