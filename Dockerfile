# syntax=docker/dockerfile:1

# =============================================================================
# Builder Stage
# =============================================================================
FROM golang:1.25-alpine AS builder

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
    -ldflags "-X github.com/gianlucamazza/msg2agent/pkg/buildinfo.Version=${VERSION} -X github.com/gianlucamazza/msg2agent/pkg/buildinfo.Commit=${COMMIT} -X github.com/gianlucamazza/msg2agent/pkg/buildinfo.Date=${DATE}" \
    -o /out/relay ./cmd/relay

RUN CGO_ENABLED=0 go build \
    -ldflags "-X github.com/gianlucamazza/msg2agent/pkg/buildinfo.Version=${VERSION} -X github.com/gianlucamazza/msg2agent/pkg/buildinfo.Commit=${COMMIT} -X github.com/gianlucamazza/msg2agent/pkg/buildinfo.Date=${DATE}" \
    -o /out/mcp-server ./cmd/mcp-server

RUN CGO_ENABLED=0 go build \
    -ldflags "-X github.com/gianlucamazza/msg2agent/pkg/buildinfo.Version=${VERSION} -X github.com/gianlucamazza/msg2agent/pkg/buildinfo.Commit=${COMMIT} -X github.com/gianlucamazza/msg2agent/pkg/buildinfo.Date=${DATE}" \
    -o /out/billing-admin ./cmd/billing-admin

RUN CGO_ENABLED=0 go build \
    -ldflags "-X github.com/gianlucamazza/msg2agent/pkg/buildinfo.Version=${VERSION} -X github.com/gianlucamazza/msg2agent/pkg/buildinfo.Commit=${COMMIT} -X github.com/gianlucamazza/msg2agent/pkg/buildinfo.Date=${DATE}" \
    -o /out/dashboard ./cmd/dashboard

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

# Create identity directory
RUN mkdir -p /home/msg2agent/.msg2agent

VOLUME ["/home/msg2agent/.msg2agent"]

# MCP server uses stdio, no ports exposed
# No health check for stdio mode

# Default command
ENTRYPOINT ["mcp-server"]
CMD []

# =============================================================================
# Billing Admin Image
# =============================================================================
FROM alpine:3.20 AS billing-admin

RUN apk add --no-cache ca-certificates tzdata

RUN addgroup -g 1000 msg2agent && \
    adduser -u 1000 -G msg2agent -s /bin/sh -D msg2agent

COPY --from=builder /out/billing-admin /usr/local/bin/billing-admin

USER msg2agent

VOLUME ["/data"]

ENTRYPOINT ["billing-admin"]
CMD ["-help"]

# =============================================================================
# Dashboard Image
# =============================================================================
FROM alpine:3.20 AS dashboard

RUN apk add --no-cache ca-certificates tzdata

RUN addgroup -g 1000 msg2agent && \
    adduser -u 1000 -G msg2agent -s /bin/sh -D msg2agent

RUN mkdir -p /data && chown msg2agent:msg2agent /data

COPY --from=builder /out/dashboard /usr/local/bin/dashboard

USER msg2agent

VOLUME ["/data"]

EXPOSE 8082

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8082/health || exit 1

ENTRYPOINT ["dashboard"]
CMD ["-addr", ":8082"]
