# Docker Deployment

This guide covers deploying msg2agent components using Docker and Docker Compose.

## Building Images

### Relay Image

```dockerfile
# Dockerfile.relay
FROM golang:1.23-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /relay ./cmd/relay

FROM alpine:3.19
RUN apk --no-cache add ca-certificates
COPY --from=builder /relay /usr/local/bin/relay
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/relay"]
CMD ["-addr", ":8080"]
```

Build the image:

```bash
docker build -f Dockerfile.relay -t msg2agent/relay:latest .
```

## Docker Compose

### Basic Setup

```yaml
# docker-compose.yml
version: "3.8"

services:
  relay:
    image: msg2agent/relay:latest
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
    image: msg2agent/mcp-server:latest
    depends_on:
      relay:
        condition: service_healthy
    environment:
      - MSG2AGENT_NAME=my-agent
      - MSG2AGENT_DOMAIN=example.com
      - MSG2AGENT_RELAY_URL=ws://relay:8080
      - MSG2AGENT_HTTP_ADDR=:3001
    ports:
      - "3001:3001"
```

### With SQLite Persistence

```yaml
# docker-compose.sqlite.yml
version: "3.8"

services:
  relay:
    image: msg2agent/relay:latest
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
    image: msg2agent/relay:latest
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
# Start all services
docker-compose up -d

# View logs
docker-compose logs -f relay

# Check health
curl http://localhost:8080/health

# Stop services
docker-compose down
```

## Health Checks

The relay exposes these endpoints:

| Endpoint   | Purpose            | Response          |
| ---------- | ------------------ | ----------------- |
| `/health`  | Liveness probe     | `ok`              |
| `/ready`   | Readiness probe    | JSON with status  |
| `/metrics` | Prometheus metrics | Prometheus format |

## Environment Variables

See the full list of environment variables in the [Configuration Guide](../operations/configuration.md).
