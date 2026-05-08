# Docker Deployment

This guide covers deploying msg2agent components using Docker and Docker Compose.

## Building Images

The repository root `Dockerfile` builds all shipped binaries in a shared builder stage
and exposes one runtime target per component:

```bash
docker build --target relay -t msg2agent-relay:latest .
docker build --target mcp-server -t msg2agent-mcp-server:latest .
docker build --target billing-admin -t msg2agent-billing-admin:latest .
docker build --target dashboard -t msg2agent-dashboard:latest .
```

`make docker-build` runs the same target set with version tags.

## Docker Compose

### Basic Setup

The checked-in `docker-compose.yml` starts the current runtime services:
`relay`, `mcp-server` in streamable HTTP mode, and `dashboard`. The agent runtime
is implemented as `pkg/agent` and is embedded by `mcp-server`; there is no
standalone Docker target named `agent`.

```yaml
services:
  relay:
    image: msg2agent-relay:latest
    ports:
      - "8080:8080"
    environment:
      - MSG2AGENT_LOG_LEVEL=info
      - MSG2AGENT_MAX_CONNECTIONS=1000
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://localhost:8080/health"]
      interval: 10s
      timeout: 5s
      retries: 3

  mcp-server:
    image: msg2agent-mcp-server:latest
    command: ["-name", "mcp-agent", "-domain", "mcp.local", "-relay", "ws://relay:8080", "-transport", "streamable-http", "-addr", ":3001"]
    depends_on:
      relay:
        condition: service_healthy
    ports:
      - "3001:3001"

  dashboard:
    image: msg2agent-dashboard:latest
    ports:
      - "8082:8082"
```

### With SQLite Persistence

```yaml
# docker-compose.sqlite.yml
version: "3.8"

services:
  relay:
    image: msg2agent-relay:latest
    ports:
      - "8080:8080"
    volumes:
      - relay-data:/data
    environment:
      - MSG2AGENT_LOG_LEVEL=info
      - MSG2AGENT_STORE=sqlite
      - MSG2AGENT_STORE_FILE=/data/relay.db
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://localhost:8080/health"]
      interval: 10s
      timeout: 5s
      retries: 3

volumes:
  relay-data:
```

### With TLS

```yaml
# docker-compose.tls.yml
version: "3.8"

services:
  relay:
    image: msg2agent-relay:latest
    ports:
      - "8443:8443"
    volumes:
      - ./certs:/certs:ro
    environment:
      - MSG2AGENT_RELAY_ADDR=:8443
      - MSG2AGENT_TLS=true
      - MSG2AGENT_TLS_CERT=/certs/server.crt
      - MSG2AGENT_TLS_KEY=/certs/server.key
      - MSG2AGENT_LOG_LEVEL=info
```

## Running

```bash
# Build and start all services
make dev-up

# View logs
make dev-logs

# Check health
curl http://localhost:8080/health
curl http://localhost:3001/health
curl http://localhost:8082/health

# Stop services
make dev-down
```

## Health Checks

The compose services expose these default health endpoints:

| Service      | Endpoint                 | Purpose            |
| ------------ | ------------------------ | ------------------ |
| relay        | `http://localhost:8080/health` | Liveness probe     |
| relay        | `http://localhost:8080/ready`  | Readiness probe    |
| relay        | `http://localhost:8080/metrics` | Prometheus metrics |
| mcp-server   | `http://localhost:3001/health` | Liveness probe     |
| dashboard    | `http://localhost:8082/health` | Liveness probe     |

## Environment Variables

See the full list of environment variables in the [Configuration Guide](../operations/configuration.md).
