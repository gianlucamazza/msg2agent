// Package transport provides transport layer abstractions and implementations.
package transport

import (
	"context"
	"errors"
	"io"
)

var (
	ErrClosed           = errors.New("transport closed")
	ErrConnectionFailed = errors.New("connection failed")
	ErrTimeout          = errors.New("operation timed out")
	ErrNotConnected     = errors.New("not connected")
)

// Transport is the interface for a bidirectional message transport.
type Transport interface {
	// Connect establishes a connection to the remote endpoint.
	Connect(ctx context.Context) error

	// Close closes the transport.
	Close() error

	// Send sends a message to the remote endpoint.
	Send(ctx context.Context, data []byte) error

	// Receive receives a message from the remote endpoint.
	// It blocks until a message is available or context is canceled.
	Receive(ctx context.Context) ([]byte, error)

	// IsConnected returns true if the transport is connected.
	IsConnected() bool

	// RemoteAddr returns the address of the remote endpoint.
	RemoteAddr() string
}

// Listener is the interface for accepting incoming connections.
type Listener interface {
	// Accept waits for and returns the next connection.
	Accept(ctx context.Context) (Transport, error)

	// Close closes the listener.
	Close() error

	// Addr returns the listener's network address.
	Addr() string
}

// Handler is the function type for handling incoming messages.
type Handler func(ctx context.Context, data []byte) ([]byte, error)

// Server is a transport server that handles incoming connections.
type Server interface {
	// Serve starts accepting connections and handling messages.
	Serve(ctx context.Context) error

	// Close stops the server.
	Close() error

	// SetHandler sets the message handler.
	SetHandler(handler Handler)
}

// Dialer creates new transport connections.
type Dialer interface {
	Dial(ctx context.Context, addr string) (Transport, error)
}

// StreamTransport extends Transport with streaming capabilities.
type StreamTransport interface {
	Transport

	// OpenStream opens a new stream on the connection.
	OpenStream(ctx context.Context) (io.ReadWriteCloser, error)

	// AcceptStream accepts an incoming stream.
	AcceptStream(ctx context.Context) (io.ReadWriteCloser, error)
}

// Config holds common transport configuration.
type Config struct {
	Address         string
	MaxMessageSize  int
	ReadBufferSize  int
	WriteBufferSize int
	TLSEnabled      bool
	TLSCertFile     string
	TLSKeyFile      string
	TLSSkipVerify   bool     // Skip TLS certificate verification (for testing)
	AllowedOrigins  []string // Allowed CORS origins for WebSocket (nil = same-origin only)
}

// DefaultConfig returns a default transport configuration.
func DefaultConfig(addr string) Config {
	return Config{
		Address:         addr,
		MaxMessageSize:  1 << 20, // 1MB
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
	}
}
