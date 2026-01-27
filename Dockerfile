# syntax=docker/dockerfile:1

# =============================================================================
# Builder Stage
# =============================================================================
FROM golang:1.23-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /src

# Allow Go to download newer toolchain if needed
ENV GOTOOLCHAIN=auto

# Copy go mod files first for better layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build arguments for version injection
ARG VERSION=dev
ARG COMMIT=unknown
ARG DATE=unknown

# Build all binaries
RUN CGO_ENABLED=0 go build \
    -ldflags "-X main.Version=${VERSION} -X main.Commit=${COMMIT} -X main.Date=${DATE}" \
    -o /out/relay ./cmd/relay

RUN CGO_ENABLED=0 go build \
    -ldflags "-X main.Version=${VERSION} -X main.Commit=${COMMIT} -X main.Date=${DATE}" \
    -o /out/agent ./cmd/agent

RUN CGO_ENABLED=0 go build \
    -ldflags "-X main.Version=${VERSION} -X main.Commit=${COMMIT} -X main.Date=${DATE}" \
    -o /out/mcp-server ./cmd/mcp-server

# =============================================================================
# Relay Image
# =============================================================================
FROM alpine:3.20 AS relay

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata

# Create non-root user
RUN addgroup -g 1000 msg2agent && \
    adduser -u 1000 -G msg2agent -s /bin/sh -D msg2agent

# Create data directory
RUN mkdir -p /data && chown msg2agent:msg2agent /data

# Copy binary
COPY --from=builder /out/relay /usr/local/bin/relay

# Switch to non-root user
USER msg2agent

# Expose port
EXPOSE 8080

# Volume for persistent data
VOLUME ["/data"]

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

# Default command
ENTRYPOINT ["relay"]
CMD ["-addr", ":8080", "-store", "sqlite", "-store-file", "/data/relay.db"]

# =============================================================================
# Agent Image
# =============================================================================
FROM alpine:3.20 AS agent

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata

# Create non-root user
RUN addgroup -g 1000 msg2agent && \
    adduser -u 1000 -G msg2agent -s /bin/sh -D msg2agent

# Copy binary
COPY --from=builder /out/agent /usr/local/bin/agent

# Switch to non-root user
USER msg2agent

# Expose ports: HTTP (8081), P2P (8082), Metrics (9090)
EXPOSE 8081 8082 9090

# Health check on metrics port
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:9090/health || exit 1

# Default command
ENTRYPOINT ["agent"]
CMD ["-http", ":8081", "-metrics", ":9090"]

# =============================================================================
# MCP Server Image
# =============================================================================
FROM alpine:3.20 AS mcp-server

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata

# Create non-root user
RUN addgroup -g 1000 msg2agent && \
    adduser -u 1000 -G msg2agent -s /bin/sh -D msg2agent

# Copy binary
COPY --from=builder /out/mcp-server /usr/local/bin/mcp-server

# Switch to non-root user
USER msg2agent

# MCP server uses stdio, no ports exposed
# No health check for stdio mode

# Default command
ENTRYPOINT ["mcp-server"]
CMD []
