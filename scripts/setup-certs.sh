#!/usr/bin/env bash
# setup-certs.sh - Generate TLS certificates for testing
#
# Usage:
#   ./scripts/setup-certs.sh          # Generate certs in testdata/certs/
#   ./scripts/setup-certs.sh --clean  # Remove existing certs first
#
# Generated files:
#   testdata/certs/ca.crt       - CA certificate
#   testdata/certs/ca.key       - CA private key
#   testdata/certs/server.crt   - Server certificate (for relay and agents)
#   testdata/certs/server.key   - Server private key
#   testdata/certs/client.crt   - Client certificate (optional)
#   testdata/certs/client.key   - Client private key (optional)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
CERT_DIR="$PROJECT_ROOT/testdata/certs"

# Certificate configuration
DAYS_VALID=365
KEY_SIZE=2048
COUNTRY="US"
STATE="California"
LOCALITY="San Francisco"
ORGANIZATION="msg2agent"
ORG_UNIT="Development"
COMMON_NAME="msg2agent.local"

# SANs for the server certificate
SANS="DNS:localhost,DNS:relay,DNS:alice,DNS:bob,DNS:*.local,IP:127.0.0.1,IP:::1"

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log_info() { echo -e "${GREEN}[INFO]${NC} $1"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }

# Clean existing certs
clean_certs() {
	if [[ -d "$CERT_DIR" ]]; then
		log_info "Removing existing certificates..."
		rm -rf "$CERT_DIR"
	fi
}

# Create directory
setup_dir() {
	mkdir -p "$CERT_DIR"
	chmod 700 "$CERT_DIR"
}

# Generate CA
generate_ca() {
	log_info "Generating CA certificate..."

	openssl genrsa -out "$CERT_DIR/ca.key" "$KEY_SIZE" 2>/dev/null

	openssl req -new -x509 \
		-key "$CERT_DIR/ca.key" \
		-out "$CERT_DIR/ca.crt" \
		-days "$DAYS_VALID" \
		-subj "/C=$COUNTRY/ST=$STATE/L=$LOCALITY/O=$ORGANIZATION/OU=$ORG_UNIT/CN=$ORGANIZATION CA" \
		2>/dev/null

	chmod 600 "$CERT_DIR/ca.key"
	log_info "CA certificate generated: $CERT_DIR/ca.crt"
}

# Generate server certificate
generate_server_cert() {
	log_info "Generating server certificate..."

	# Create CSR config with SANs
	cat >"$CERT_DIR/server.cnf" <<EOF
[req]
default_bits = $KEY_SIZE
prompt = no
default_md = sha256
req_extensions = req_ext
distinguished_name = dn

[dn]
C = $COUNTRY
ST = $STATE
L = $LOCALITY
O = $ORGANIZATION
OU = $ORG_UNIT
CN = $COMMON_NAME

[req_ext]
subjectAltName = $SANS

[v3_ext]
authorityKeyIdentifier = keyid,issuer
basicConstraints = CA:FALSE
keyUsage = digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth, clientAuth
subjectAltName = $SANS
EOF

	# Generate private key
	openssl genrsa -out "$CERT_DIR/server.key" "$KEY_SIZE" 2>/dev/null

	# Generate CSR
	openssl req -new \
		-key "$CERT_DIR/server.key" \
		-out "$CERT_DIR/server.csr" \
		-config "$CERT_DIR/server.cnf" \
		2>/dev/null

	# Sign with CA
	openssl x509 -req \
		-in "$CERT_DIR/server.csr" \
		-CA "$CERT_DIR/ca.crt" \
		-CAkey "$CERT_DIR/ca.key" \
		-CAcreateserial \
		-out "$CERT_DIR/server.crt" \
		-days "$DAYS_VALID" \
		-extensions v3_ext \
		-extfile "$CERT_DIR/server.cnf" \
		2>/dev/null

	# Cleanup intermediate files
	rm -f "$CERT_DIR/server.csr" "$CERT_DIR/server.cnf" "$CERT_DIR/ca.srl"
	chmod 600 "$CERT_DIR/server.key"

	log_info "Server certificate generated: $CERT_DIR/server.crt"
}

# Generate client certificate (optional, for mTLS)
generate_client_cert() {
	log_info "Generating client certificate..."

	# Generate private key
	openssl genrsa -out "$CERT_DIR/client.key" "$KEY_SIZE" 2>/dev/null

	# Generate CSR
	openssl req -new \
		-key "$CERT_DIR/client.key" \
		-out "$CERT_DIR/client.csr" \
		-subj "/C=$COUNTRY/ST=$STATE/L=$LOCALITY/O=$ORGANIZATION/OU=$ORG_UNIT/CN=client" \
		2>/dev/null

	# Sign with CA
	openssl x509 -req \
		-in "$CERT_DIR/client.csr" \
		-CA "$CERT_DIR/ca.crt" \
		-CAkey "$CERT_DIR/ca.key" \
		-CAcreateserial \
		-out "$CERT_DIR/client.crt" \
		-days "$DAYS_VALID" \
		2>/dev/null

	# Cleanup intermediate files
	rm -f "$CERT_DIR/client.csr" "$CERT_DIR/ca.srl"
	chmod 600 "$CERT_DIR/client.key"

	log_info "Client certificate generated: $CERT_DIR/client.crt"
}

# Verify certificates
verify_certs() {
	log_info "Verifying certificates..."

	# Verify server cert against CA
	if openssl verify -CAfile "$CERT_DIR/ca.crt" "$CERT_DIR/server.crt" >/dev/null 2>&1; then
		log_info "Server certificate verified successfully"
	else
		log_warn "Server certificate verification failed"
		return 1
	fi

	# Verify client cert against CA
	if openssl verify -CAfile "$CERT_DIR/ca.crt" "$CERT_DIR/client.crt" >/dev/null 2>&1; then
		log_info "Client certificate verified successfully"
	else
		log_warn "Client certificate verification failed"
		return 1
	fi

	# Show certificate info
	echo ""
	echo "Certificate details:"
	echo "  CA:     $CERT_DIR/ca.crt"
	echo "  Server: $CERT_DIR/server.crt"
	echo "  Client: $CERT_DIR/client.crt"
	echo ""
	echo "Server certificate SANs:"
	openssl x509 -in "$CERT_DIR/server.crt" -noout -ext subjectAltName 2>/dev/null | tail -1
}

# Main
main() {
	if [[ "${1:-}" == "--clean" ]]; then
		clean_certs
	fi

	if [[ -f "$CERT_DIR/server.crt" ]]; then
		log_warn "Certificates already exist in $CERT_DIR"
		log_warn "Use --clean to regenerate"
		exit 0
	fi

	setup_dir
	generate_ca
	generate_server_cert
	generate_client_cert
	verify_certs

	echo ""
	log_info "TLS certificates generated successfully!"
	echo ""
	echo "Usage with Docker Compose:"
	echo "  docker compose -f docker-compose.yml -f docker-compose.tls.yml up -d"
	echo ""
	echo "Usage with native binaries:"
	echo "  ./build/relay -tls -tls-cert testdata/certs/server.crt -tls-key testdata/certs/server.key"
}

main "$@"
