#!/usr/bin/env bash
# letsencrypt-renew.sh — cron-friendly Let's Encrypt certificate renewal.
#
# Renews certificates via certbot and reloads nginx. Silent on success so
# cron does not send mail when nothing changed.
#
# Recommended crontab (runs at 03:17 on the 1st and 15th of each month):
#   17 3 1,15 * * /opt/msg2agent/scripts/letsencrypt-renew.sh
#
# Usage:
#   ./scripts/letsencrypt-renew.sh
#
# Environment:
#   COMPOSE_FILE   docker-compose file for nginx reload
#                  (default: infrastructure/docker-compose.cloud.yml)
#   LOG_FILE       Append-only log path (default: /var/log/msg2agent/letsencrypt.log)
#   NGINX_RELOAD   Command to reload nginx (auto-detected: native or compose)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
COMPOSE_FILE="${COMPOSE_FILE:-$PROJECT_ROOT/infrastructure/docker-compose.cloud.yml}"
LOG_FILE="${LOG_FILE:-/var/log/msg2agent/letsencrypt.log}"

mkdir -p "$(dirname "$LOG_FILE")"

log() { echo "$(date -u +%Y-%m-%dT%H:%M:%SZ) $*" | tee -a "$LOG_FILE"; }

if ! command -v certbot &>/dev/null; then
	log "ERROR: certbot not found — install certbot and re-run"
	exit 1
fi

log "Starting certificate renewal check"

# Renew; certbot exits 0 whether or not a renewal happened.
if certbot renew --quiet 2>>"$LOG_FILE"; then
	log "certbot renew completed"
else
	log "ERROR: certbot renew failed (exit $?)"
	exit 1
fi

# Reload nginx to pick up new certificates.
if [[ -n "${NGINX_RELOAD:-}" ]]; then
	log "Reloading nginx via custom command: $NGINX_RELOAD"
	eval "$NGINX_RELOAD" 2>>"$LOG_FILE"
elif command -v nginx &>/dev/null && nginx -t -q 2>/dev/null; then
	log "Reloading native nginx"
	nginx -s reload 2>>"$LOG_FILE"
elif [[ -f "$COMPOSE_FILE" ]] && command -v docker &>/dev/null; then
	log "Reloading nginx via docker compose ($COMPOSE_FILE)"
	docker compose -f "$COMPOSE_FILE" exec nginx nginx -s reload 2>>"$LOG_FILE" || true
else
	log "WARN: could not reload nginx — restart it manually to activate new certificates"
fi

log "Certificate renewal check complete"
