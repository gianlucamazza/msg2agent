package transport

import (
	"context"
	"crypto/tls"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// WebSocketTransport implements Transport using WebSocket.
type WebSocketTransport struct {
	conn       *websocket.Conn
	remoteAddr string
	config     Config
	logger     *slog.Logger
	mu         sync.RWMutex
	closed     bool
	stopPing   chan struct{}
}

// NewWebSocketTransport creates a new WebSocket transport for client use.
func NewWebSocketTransport(config Config) *WebSocketTransport {
	return NewWebSocketTransportWithLogger(config, nil)
}

// NewWebSocketTransportWithLogger creates a new WebSocket transport with a logger.
func NewWebSocketTransportWithLogger(config Config, logger *slog.Logger) *WebSocketTransport {
	if logger == nil {
		logger = slog.Default()
	}
	return &WebSocketTransport{
		remoteAddr: config.Address,
		config:     config,
		logger:     logger,
	}
}

// Connect establishes a WebSocket connection.
func (t *WebSocketTransport) Connect(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.conn != nil {
		t.logger.Debug("websocket already connected", "addr", t.remoteAddr)
		return nil // Already connected
	}

	t.logger.Debug("websocket connecting", "addr", t.remoteAddr)

	opts := &websocket.DialOptions{
		HTTPHeader: http.Header{
			"User-Agent": []string{"msg2agent/1.0"},
		},
	}

	// Configure TLS skip verify if enabled (for testing)
	if t.config.TLSSkipVerify {
		opts.HTTPClient = &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true, //nolint:gosec // Explicitly allowed via config
				},
			},
		}
		t.logger.Warn("TLS verification disabled", "addr", t.remoteAddr)
	}

	conn, _, err := websocket.Dial(ctx, t.remoteAddr, opts)
	if err != nil {
		t.logger.Error("websocket connect failed", "addr", t.remoteAddr, "error", err)
		return err
	}

	conn.SetReadLimit(int64(t.config.MaxMessageSize))
	t.conn = conn
	t.closed = false
	t.stopPing = make(chan struct{})
	t.logger.Info("websocket connected", "addr", t.remoteAddr)

	// Start ping goroutine to keep connection alive
	go t.pingLoop()
	return nil
}

// pingLoop sends periodic keepalive messages to keep the connection alive.
// It sends application-level JSON-RPC notifications that the server will process.
func (t *WebSocketTransport) pingLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// JSON-RPC keepalive notification
	keepalive := []byte(`{"jsonrpc":"2.0","method":"keepalive"}`)

	for {
		select {
		case <-ticker.C:
			t.mu.RLock()
			conn := t.conn
			closed := t.closed
			t.mu.RUnlock()

			if conn == nil || closed {
				return
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := conn.Write(ctx, websocket.MessageBinary, keepalive)
			cancel()

			if err != nil {
				t.logger.Debug("websocket keepalive failed", "addr", t.remoteAddr, "error", err)
				return
			}
			t.logger.Debug("websocket keepalive sent", "addr", t.remoteAddr)
		case <-t.stopPing:
			return
		}
	}
}

// Close closes the WebSocket connection.
func (t *WebSocketTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.conn == nil || t.closed {
		return nil
	}

	t.logger.Debug("websocket closing", "addr", t.remoteAddr)
	t.closed = true

	// Stop the ping goroutine
	if t.stopPing != nil {
		close(t.stopPing)
	}

	err := t.conn.Close(websocket.StatusNormalClosure, "closing")
	if err != nil {
		t.logger.Warn("websocket close error", "addr", t.remoteAddr, "error", err)
	} else {
		t.logger.Info("websocket closed", "addr", t.remoteAddr)
	}
	return err
}

// Send sends a message over WebSocket.
func (t *WebSocketTransport) Send(ctx context.Context, data []byte) error {
	t.mu.RLock()
	conn := t.conn
	closed := t.closed
	t.mu.RUnlock()

	if conn == nil || closed {
		t.logger.Debug("websocket send failed: not connected", "addr", t.remoteAddr)
		return ErrNotConnected
	}

	err := conn.Write(ctx, websocket.MessageBinary, data)
	if err != nil {
		t.logger.Warn("websocket send error", "addr", t.remoteAddr, "size", len(data), "error", err)
	} else {
		t.logger.Debug("websocket sent", "addr", t.remoteAddr, "size", len(data))
	}
	return err
}

// Receive receives a message from WebSocket.
func (t *WebSocketTransport) Receive(ctx context.Context) ([]byte, error) {
	t.mu.RLock()
	conn := t.conn
	closed := t.closed
	t.mu.RUnlock()

	if conn == nil || closed {
		t.logger.Debug("websocket receive failed: not connected", "addr", t.remoteAddr)
		return nil, ErrNotConnected
	}

	_, data, err := conn.Read(ctx)
	if err != nil {
		t.logger.Debug("websocket receive error", "addr", t.remoteAddr, "error", err)
		return nil, err
	}
	t.logger.Debug("websocket received", "addr", t.remoteAddr, "size", len(data))
	return data, nil
}

// IsConnected returns true if connected.
func (t *WebSocketTransport) IsConnected() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.conn != nil && !t.closed
}

// RemoteAddr returns the remote address.
func (t *WebSocketTransport) RemoteAddr() string {
	return t.remoteAddr
}

// WebSocketListener implements Listener for WebSocket connections.
type WebSocketListener struct {
	server   *http.Server
	listener net.Listener
	addr     string
	connCh   chan *websocket.Conn
	config   Config
	logger   *slog.Logger
	mu       sync.Mutex
	closed   bool
}

// NewWebSocketListener creates a new WebSocket listener.
func NewWebSocketListener(config Config) *WebSocketListener {
	return NewWebSocketListenerWithLogger(config, nil)
}

// NewWebSocketListenerWithLogger creates a new WebSocket listener with a logger.
func NewWebSocketListenerWithLogger(config Config, logger *slog.Logger) *WebSocketListener {
	if logger == nil {
		logger = slog.Default()
	}
	return &WebSocketListener{
		addr:   config.Address,
		connCh: make(chan *websocket.Conn, 16),
		config: config,
		logger: logger,
	}
}

// Start starts the listener.
func (l *WebSocketListener) Start(ctx context.Context) error {
	l.logger.Debug("websocket listener starting", "addr", l.addr)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Configure CORS: use allowed origins or reject cross-origin requests
		acceptOpts := &websocket.AcceptOptions{}
		if len(l.config.AllowedOrigins) > 0 {
			acceptOpts.OriginPatterns = l.config.AllowedOrigins
		}
		// When AllowedOrigins is nil/empty, OriginPatterns defaults to same-origin only

		conn, err := websocket.Accept(w, r, acceptOpts)
		if err != nil {
			l.logger.Warn("websocket accept failed", "remote", r.RemoteAddr, "error", err)
			return
		}
		conn.SetReadLimit(int64(l.config.MaxMessageSize))
		l.logger.Debug("websocket connection accepted", "remote", r.RemoteAddr)

		select {
		case l.connCh <- conn:
		case <-ctx.Done():
			l.logger.Debug("websocket connection rejected: shutting down", "remote", r.RemoteAddr)
			_ = conn.Close(websocket.StatusGoingAway, "server shutting down") // Best effort
		}
	})

	// Create the network listener first to get the actual bound address
	var ln net.Listener
	var err error

	if l.config.TLSEnabled {
		var cert tls.Certificate
		cert, err = tls.LoadX509KeyPair(l.config.TLSCertFile, l.config.TLSKeyFile)
		if err != nil {
			l.logger.Error("failed to load TLS certificate", "error", err)
			return err
		}
		tlsConfig := &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		}
		ln, err = tls.Listen("tcp", l.addr, tlsConfig)
		if err != nil {
			l.logger.Error("TLS listen failed", "addr", l.addr, "error", err)
			return err
		}
	} else {
		ln, err = net.Listen("tcp", l.addr)
		if err != nil {
			l.logger.Error("listen failed", "addr", l.addr, "error", err)
			return err
		}
	}

	l.listener = ln
	l.addr = ln.Addr().String() // Update addr to actual bound address

	l.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		if err := l.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			l.logger.Error("websocket server error", "error", err)
		}
	}()

	l.logger.Info("websocket listener started", "addr", l.addr)
	return nil
}

// Accept accepts a new connection.
func (l *WebSocketListener) Accept(ctx context.Context) (Transport, error) {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		l.logger.Debug("websocket accept failed: listener closed")
		return nil, ErrClosed
	}
	l.mu.Unlock()

	select {
	case conn, ok := <-l.connCh:
		if !ok || conn == nil {
			l.logger.Debug("websocket accept failed: channel closed")
			return nil, ErrClosed
		}
		l.logger.Debug("websocket accepted connection")
		return &WebSocketTransport{
			conn:       conn,
			remoteAddr: conn.Subprotocol(),
			config:     l.config,
			logger:     l.logger,
		}, nil
	case <-ctx.Done():
		l.logger.Debug("websocket accept canceled", "error", ctx.Err())
		return nil, ctx.Err()
	}
}

// Close closes the listener.
func (l *WebSocketListener) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.closed {
		return nil
	}

	l.logger.Debug("websocket listener closing", "addr", l.addr)
	l.closed = true
	close(l.connCh)

	// Close underlying listener first
	if l.listener != nil {
		_ = l.listener.Close() // Best effort
	}

	if l.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		err := l.server.Shutdown(ctx)
		if err != nil {
			l.logger.Warn("websocket listener shutdown error", "error", err)
		} else {
			l.logger.Info("websocket listener closed", "addr", l.addr)
		}
		return err
	}
	l.logger.Info("websocket listener closed", "addr", l.addr)
	return nil
}

// Addr returns the listener address.
func (l *WebSocketListener) Addr() string {
	return l.addr
}
