FROM golang:1.24-alpine AS builder
WORKDIR /build
# Install gcc for cgo (required by mattn/go-sqlite3 if used; modernc/sqlite is pure Go)
RUN apk add --no-cache gcc musl-dev
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -o dashboard ./cmd/dashboard/

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /build/dashboard .
EXPOSE 8082
ENTRYPOINT ["./dashboard"]
