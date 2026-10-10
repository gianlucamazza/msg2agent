#!/usr/bin/env bash
# check-abandoned-domains.sh — fail if former hosted product hostnames reappear.
#
# Those public hostnames are not being renewed (January 2027). A later
# registrant could receive traffic if this repo still pointed at them.
# Defaults, docs, emails, and links must stay on self-host / example.com /
# localhost / info@gianlucamazza.it.
#
# The literal hostnames are assembled at runtime so this script is not itself
# a hit for a plain-text search.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
cd "$PROJECT_ROOT"

# Assemble "name.tld" then escape the dot so a space between the labels is not a hit.
apex="$(printf '%s.%s' msg2agent xyz)"
alias="$(printf '%s.%s' promptkids app)"
pattern="$(printf '%s|%s' "${apex//./\\.}" "${alias//./\\.}")"

if command -v rg >/dev/null 2>&1; then
	hits="$(rg -n --hidden --no-ignore \
		-g '!**/.git/**' \
		-g '!**/node_modules/**' \
		-g '!**/vendor/**' \
		-g '!**/build/**' \
		-g '!**/dist/**' \
		-e "$pattern" || true)"
else
	hits="$(git ls-files -z | xargs -0 grep -nE -e "$pattern" || true)"
fi

if [[ -n "$hits" ]]; then
	echo "Abandoned hosted hostnames must not appear in the tree:" >&2
	echo "$hits" >&2
	exit 1
fi

echo "No abandoned hosted hostnames found."
