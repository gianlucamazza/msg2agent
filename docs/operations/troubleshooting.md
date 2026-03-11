# Troubleshooting Guide

This guide covers common issues and their solutions when running msg2agent.

## Connection Issues

### Agent Cannot Connect to Relay

**Symptoms:**

- Agent logs show "connection refused" or "timeout"
- Agent repeatedly tries to reconnect

**Possible Causes and Solutions:**

1. **Relay not running**

   ```bash
   # Check if relay is running
   curl http://localhost:8080/health

   # Start relay
   ./relay -addr :8080
   ```

2. **Wrong relay address**

   ```bash
   # Check agent is using correct URL
   ./agent -name alice -relay ws://localhost:8080

   # For TLS relay
   ./agent -name alice -relay wss://localhost:8443
   ```

3. **Firewall blocking connection**

   ```bash
   # Check if port is open
   nc -zv localhost 8080

   # Check firewall rules
   sudo iptables -L -n | grep 8080
   ```

4. **TLS certificate issues**

   ```bash
   # For development, skip verification
   ./agent -name alice -relay wss://localhost:8443 -tls-skip-verify

   # Verify certificate
   openssl s_client -connect localhost:8443 -servername localhost
   ```

### WebSocket Connection Drops

**Symptoms:**

- Frequent disconnections
- "connection reset by peer" errors

**Solutions:**

1. **Increase timeouts**

   ```bash
   # Check if reverse proxy is timing out
   # nginx.conf
   proxy_read_timeout 3600;
   proxy_send_timeout 3600;
   ```

2. **Enable WebSocket keepalive**
   The relay sends pings automatically. If connections still drop, check network equipment.

3. **Check for rate limiting**
   ```bash
   # Monitor rate limit metrics
   curl -s http://localhost:8080/metrics | grep rate_limited
   ```

## Message Delivery Issues

### Messages Not Being Delivered

**Symptoms:**

- Sender reports success but recipient doesn't receive
- Message appears in relay logs but not delivered

**Debugging Steps:**

1. **Check if recipient is connected**

   ```bash
   # Check relay connections
   curl http://localhost:8080/ready
   ```

2. **Verify DID format**

   ```bash
   # DIDs must be valid
   # Format: did:wba:<domain>:<type>:<name>
   # Example: did:wba:example.com:agent:alice
   ```

3. **Check relay logs**

   ```bash
   # Run relay with debug logging
   MSG2AGENT_LOG_LEVEL=debug ./relay -addr :8080

   # Look for routing errors
   # "no route for message" - recipient not connected
   # "message dropped" - routing failed
   ```

4. **Verify encryption keys**
   ```bash
   # Both agents must have exchanged keys
   # Check for "encryption failed" in logs
   ```

### High Message Latency

**Symptoms:**

- Messages take too long to deliver
- Handler timeouts

**Solutions:**

1. **Check handler duration**

   ```bash
   curl -s http://localhost:9090/metrics | grep handler_duration
   ```

2. **Profile handlers**
   - Add tracing to identify slow handlers
   - Check for blocking operations in handlers

3. **Check system resources**

   ```bash
   # CPU and memory
   top -p $(pgrep relay)

   # Network
   ss -s
   ```

## TLS Issues

### Certificate Verification Failures

**Error:** `x509: certificate signed by unknown authority`

**Solutions:**

1. **Add CA to system trust store**

   ```bash
   # Arch Linux
   sudo cp ca.crt /etc/ca-certificates/trust-source/anchors/
   sudo update-ca-trust

   # Ubuntu/Debian
   sudo cp ca.crt /usr/local/share/ca-certificates/
   sudo update-ca-certificates
   ```

2. **Use skip-verify for development**
   ```bash
   ./agent -name alice -relay wss://localhost:8443 -tls-skip-verify
   ```

### Certificate Expired

**Error:** `x509: certificate has expired or is not yet valid`

**Solution:**

```bash
# Check expiration
openssl x509 -in server.crt -noout -enddate

# Regenerate certificate
openssl req -new -x509 -days 365 -key server.key -out server.crt
```

### Key Mismatch

**Error:** `tls: private key does not match public key`

**Solution:**

```bash
# Verify key matches certificate
openssl x509 -noout -modulus -in server.crt | md5sum
openssl rsa -noout -modulus -in server.key | md5sum
# Both should output the same hash
```

## Database Issues

### SQLite Locking

**Error:** `database is locked`

**Causes:**

- Multiple writers to same database file
- Long-running transactions

**Solutions:**

1. **Use single relay instance** (current limitation)

2. **Enable WAL mode** (done automatically)

   ```sql
   PRAGMA journal_mode=WAL;
   ```

3. **Increase busy timeout**
   The relay uses a 5-second busy timeout by default.

### Database Corruption

**Symptoms:**

- Startup errors about malformed database
- Queries returning unexpected results

**Recovery:**

```bash
# Backup corrupted database
cp relay.db relay.db.bak

# Try integrity check
sqlite3 relay.db "PRAGMA integrity_check;"

# Export and reimport
sqlite3 relay.db ".dump" > backup.sql
rm relay.db
sqlite3 relay.db < backup.sql
```

## Performance Issues

### High CPU Usage

**Debugging:**

```bash
# Check relay metrics for high connection/message counts
curl -s http://localhost:8080/metrics | grep relay_connections_current
curl -s http://localhost:8080/metrics | grep relay_messages_routed_total

# Note: /debug/pprof is not exposed by default.
# To profile, build with pprof enabled or use external tools:
# go tool pprof http://localhost:6060/debug/pprof/profile?seconds=30

# Common causes:
# - Too many connections
# - Excessive logging
# - Large message payloads
```

**Solutions:**

1. **Reduce log level**

   ```bash
   MSG2AGENT_LOG_LEVEL=warn ./relay -addr :8080
   ```

2. **Limit connections**
   ```bash
   ./relay -addr :8080 -max-connections 500
   ```

### High Memory Usage

**Debugging:**

```bash
# Check metrics for connection and queue pressure
curl -s http://localhost:8080/metrics | grep relay_connections_current
curl -s http://localhost:8080/metrics | grep relay_messages_queued_total

# Note: /debug/pprof is not exposed by default.
# To profile memory, build with pprof enabled or use external tools:
# go tool pprof http://localhost:6060/debug/pprof/heap
```

**Solutions:**

1. **Enable message size limits** (configured by default)

2. **Monitor pending messages**

   ```bash
   curl -s http://localhost:9090/metrics | grep pending_messages
   ```

3. **Set resource limits in Docker/K8s**
   ```yaml
   resources:
     limits:
       memory: 256Mi
   ```

## Startup Issues

### Port Already in Use

**Error:** `listen tcp :8080: bind: address already in use`

**Solutions:**

```bash
# Find process using port
lsof -i :8080
# or
ss -tlnp | grep 8080

# Kill the process or use different port
./relay -addr :8081
```

### Permission Denied

**Error:** `listen tcp :443: bind: permission denied`

**Solutions:**

```bash
# Use port > 1024
./relay -addr :8443

# Or grant capability (not recommended for production)
sudo setcap 'cap_net_bind_service=+ep' ./relay
```

### Missing Configuration

**Error:** `TLS enabled but certificate not provided`

**Solution:**

```bash
# Provide both cert and key
./relay -addr :8443 -tls -tls-cert server.crt -tls-key server.key
```

## Logging

### Enabling Debug Logs

```bash
# Environment variable
MSG2AGENT_LOG_LEVEL=debug ./relay -addr :8080

# Or flag
./relay -addr :8080 -log-level debug
```

### Log Levels

| Level   | Description                       |
| ------- | --------------------------------- |
| `debug` | Verbose, includes message routing |
| `info`  | Standard operational logs         |
| `warn`  | Warnings and degraded operations  |
| `error` | Errors only                       |

### Structured Logging

Logs are in JSON format when output is not a TTY:

```json
{
  "time": "2025-01-25T10:00:00Z",
  "level": "INFO",
  "msg": "client connected",
  "client_id": "abc123"
}
```

## Tracing

### Enable Tracing for Debugging

```bash
# Stdout tracing (development)
./relay -addr :8080 -trace-stdout

# OTLP tracing (with Jaeger)
./relay -addr :8080 -otlp-endpoint http://localhost:4318
```

### Finding Slow Operations

1. Open Jaeger UI (http://localhost:16686)
2. Select service "msg2agent-relay"
3. Sort by duration
4. Examine spans for bottlenecks

## Common Error Messages

| Error                       | Cause                   | Solution                                            |
| --------------------------- | ----------------------- | --------------------------------------------------- |
| `invalid DID format`        | Malformed DID string    | Check DID follows `did:wba:domain:type:name` format |
| `encryption failed`         | Missing peer public key | Ensure agents have exchanged keys                   |
| `handler not found`         | Unknown method          | Verify handler is registered                        |
| `rate limit exceeded`       | Too many requests       | Reduce request rate or increase limits              |
| `context deadline exceeded` | Operation timeout       | Check network, increase timeout                     |
| `connection refused`        | Service not running     | Start the service                                   |
| `no route for message`      | Recipient not connected | Verify recipient is online                          |

## Getting Help

If you can't resolve an issue:

1. **Gather information**
   - Version: `./relay --version`
   - Logs with debug level
   - Metrics output
   - Tracing data if available

2. **Check existing issues**
   - https://github.com/anthropics/claude-code/issues

3. **Report the issue**
   - Include all gathered information
   - Describe steps to reproduce
   - Note environment (OS, Go version, etc.)
