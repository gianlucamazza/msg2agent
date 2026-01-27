# TLS Configuration

This guide covers setting up TLS for secure communication in msg2agent.

## Overview

msg2agent supports TLS for:

- **Relay WebSocket connections** (wss://)
- **Agent WebSocket listener** (wss://)
- **Agent HTTP server** (https://)
- **Agent-to-relay connections** (with certificate verification)

## Generating Certificates

### Self-Signed Certificates (Development)

```bash
# Generate CA key and certificate
openssl genrsa -out ca.key 4096
openssl req -new -x509 -days 365 -key ca.key -out ca.crt \
    -subj "/CN=msg2agent-ca"

# Generate server key
openssl genrsa -out server.key 2048

# Generate server CSR
openssl req -new -key server.key -out server.csr \
    -subj "/CN=localhost"

# Create extensions file for SAN
cat > server.ext << EOF
authorityKeyIdentifier=keyid,issuer
basicConstraints=CA:FALSE
keyUsage = digitalSignature, nonRepudiation, keyEncipherment, dataEncipherment
subjectAltName = @alt_names

[alt_names]
DNS.1 = localhost
DNS.2 = relay
DNS.3 = *.local
IP.1 = 127.0.0.1
EOF

# Sign server certificate
openssl x509 -req -in server.csr -CA ca.crt -CAkey ca.key \
    -CAcreateserial -out server.crt -days 365 \
    -extfile server.ext
```

### Let's Encrypt Certificates (Production)

Using certbot:

```bash
# Install certbot
sudo apt install certbot

# Obtain certificate
sudo certbot certonly --standalone -d relay.msg2agent.xyz

# Certificates are saved to:
# /etc/letsencrypt/live/relay.msg2agent.xyz/fullchain.pem
# /etc/letsencrypt/live/relay.msg2agent.xyz/privkey.pem
```

## Relay TLS Configuration

### Command Line

```bash
./relay \
    -addr :8443 \
    -tls \
    -tls-cert /path/to/server.crt \
    -tls-key /path/to/server.key
```

### Environment Variables

```bash
export MSG2AGENT_RELAY_ADDR=":8443"
export MSG2AGENT_TLS="true"
export MSG2AGENT_TLS_CERT="/path/to/server.crt"
export MSG2AGENT_TLS_KEY="/path/to/server.key"

./relay
```

### Verification

```bash
# Test with curl (use -k for self-signed)
curl -k https://localhost:8443/health

# Test WebSocket with wscat
wscat -c wss://localhost:8443 --no-check
```

## Agent TLS Configuration

### WebSocket Listener TLS

Enable TLS for the agent's P2P WebSocket listener:

```bash
./agent \
    -name alice \
    -listen :8082 \
    -tls \
    -tls-cert /path/to/server.crt \
    -tls-key /path/to/server.key
```

### HTTP Server TLS

Enable TLS for the agent card HTTP server:

```bash
./agent \
    -name alice \
    -http :8443 \
    -http-tls \
    -tls-cert /path/to/server.crt \
    -tls-key /path/to/server.key
```

### Connecting to TLS Relay

When connecting to a TLS-enabled relay:

```bash
# With certificate verification
./agent -name alice -relay wss://relay.msg2agent.xyz:8443

# Skip verification (development only!)
./agent -name alice \
    -relay wss://localhost:8443 \
    -tls-skip-verify
```

## TLS Settings Reference

| Flag               | Environment Variable        | Description                                |
| ------------------ | --------------------------- | ------------------------------------------ |
| `-tls`             | `MSG2AGENT_TLS`             | Enable TLS for listener                    |
| `-tls-cert`        | `MSG2AGENT_TLS_CERT`        | Certificate file path                      |
| `-tls-key`         | `MSG2AGENT_TLS_KEY`         | Private key file path                      |
| `-http-tls`        | `MSG2AGENT_HTTP_TLS`        | Enable TLS for HTTP server (agent only)    |
| `-tls-skip-verify` | `MSG2AGENT_TLS_SKIP_VERIFY` | Skip certificate verification (agent only) |

## Security Recommendations

### Production

1. **Always use TLS** - Never run without TLS in production
2. **Use valid certificates** - Obtain certificates from a trusted CA
3. **Rotate certificates** - Set up automatic renewal with certbot
4. **Strong cipher suites** - The relay uses TLS 1.2 minimum
5. **Never skip verification** - `-tls-skip-verify` is for development only

### Certificate Management

```bash
# Check certificate expiration
openssl x509 -in server.crt -noout -enddate

# Verify certificate chain
openssl verify -CAfile ca.crt server.crt

# View certificate details
openssl x509 -in server.crt -noout -text
```

## Troubleshooting

### Common Errors

**"certificate signed by unknown authority"**

- Add the CA certificate to the system trust store
- Or use `-tls-skip-verify` for development

**"certificate has expired"**

- Renew the certificate with certbot or generate a new one

**"tls: private key does not match public key"**

- Ensure the key file matches the certificate

### Debug TLS Connections

```bash
# Test TLS handshake
openssl s_client -connect localhost:8443 -servername localhost

# Check supported ciphers
nmap --script ssl-enum-ciphers -p 8443 localhost
```
