package transport

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

var (
	ErrSSEClosed = errors.New("SSE connection closed")
	ErrSSEWrite  = errors.New("SSE write failed")
	ErrNoFlusher = errors.New("response writer does not support flushing")
)

// SSEEvent represents a Server-Sent Event.
type SSEEvent struct {
	ID    string
	Event string
	Data  []byte
	Retry int
}

// SSEClient is a client for receiving Server-Sent Events.
type SSEClient struct {
	url       string
	client    *http.Client
	response  *http.Response
	reader    *bufio.Reader
	mu        sync.Mutex
	closed    bool
	closeCh   chan struct{}
	connected bool
}

// NewSSEClient creates a new SSE client.
func NewSSEClient(url string) *SSEClient {
	return &SSEClient{
		url: url,
		client: &http.Client{
			Timeout: 0, // No timeout for SSE connections
		},
		closeCh: make(chan struct{}),
	}
}

// NewSSEClientWithHTTPClient creates an SSE client with a custom HTTP client.
func NewSSEClientWithHTTPClient(url string, client *http.Client) *SSEClient {
	return &SSEClient{
		url:     url,
		client:  client,
		closeCh: make(chan struct{}),
	}
}

// Connect establishes the SSE connection.
func (c *SSEClient) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return ErrSSEClosed
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Connection", "keep-alive")

	resp, err := c.client.Do(req) //nolint:gosec // URL from trusted configuration
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close() // Best effort cleanup
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	c.response = resp
	c.reader = bufio.NewReader(resp.Body)
	c.connected = true

	return nil
}

// Close closes the SSE connection.
func (c *SSEClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}

	c.closed = true
	c.connected = false
	close(c.closeCh)

	if c.response != nil {
		return c.response.Body.Close()
	}

	return nil
}

// Send is not supported for SSE clients (read-only).
func (c *SSEClient) Send(ctx context.Context, data []byte) error {
	return errors.New("SSE client is read-only")
}

// Receive receives the next SSE event as raw data.
func (c *SSEClient) Receive(ctx context.Context) ([]byte, error) {
	event, err := c.ReceiveEvent(ctx)
	if err != nil {
		return nil, err
	}
	return event.Data, nil
}

// ReceiveEvent receives and parses the next SSE event.
func (c *SSEClient) ReceiveEvent(ctx context.Context) (*SSEEvent, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.closeCh:
		return nil, ErrSSEClosed
	default:
	}

	c.mu.Lock()
	reader := c.reader
	closed := c.closed
	c.mu.Unlock()

	if closed || reader == nil {
		return nil, ErrSSEClosed
	}

	event := &SSEEvent{}
	var dataBuffer bytes.Buffer

	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				return nil, ErrSSEClosed
			}
			return nil, err
		}

		// Trim trailing newlines
		line = bytes.TrimRight(line, "\r\n")

		// Empty line signals end of event
		if len(line) == 0 {
			if dataBuffer.Len() > 0 {
				event.Data = dataBuffer.Bytes()
				return event, nil
			}
			continue
		}

		// Parse field
		if bytes.HasPrefix(line, []byte("data:")) {
			data := bytes.TrimPrefix(line, []byte("data:"))
			data = bytes.TrimPrefix(data, []byte(" "))
			if dataBuffer.Len() > 0 {
				dataBuffer.WriteByte('\n')
			}
			dataBuffer.Write(data)
		} else if bytes.HasPrefix(line, []byte("event:")) {
			event.Event = string(bytes.TrimSpace(bytes.TrimPrefix(line, []byte("event:"))))
		} else if bytes.HasPrefix(line, []byte("id:")) {
			event.ID = string(bytes.TrimSpace(bytes.TrimPrefix(line, []byte("id:"))))
		} else if bytes.HasPrefix(line, []byte("retry:")) {
			retryStr := string(bytes.TrimSpace(bytes.TrimPrefix(line, []byte("retry:"))))
			event.Retry = atoi(retryStr)
		}
		// Ignore comments (lines starting with :)
	}
}

// IsConnected returns true if the client is connected.
func (c *SSEClient) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected && !c.closed
}

// RemoteAddr returns the SSE endpoint URL.
func (c *SSEClient) RemoteAddr() string {
	return c.url
}

// SSEWriter writes Server-Sent Events to an http.ResponseWriter.
type SSEWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
	mu      sync.Mutex
	closed  bool
}

// NewSSEWriter creates a new SSE writer.
func NewSSEWriter(w http.ResponseWriter) (*SSEWriter, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, ErrNoFlusher
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	return &SSEWriter{
		w:       w,
		flusher: flusher,
	}, nil
}

// WriteEvent writes an SSE event.
func (s *SSEWriter) WriteEvent(event *SSEEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrSSEClosed
	}

	var buf bytes.Buffer

	if event.ID != "" {
		fmt.Fprintf(&buf, "id: %s\n", event.ID)
	}
	if event.Event != "" {
		fmt.Fprintf(&buf, "event: %s\n", event.Event)
	}
	if event.Retry > 0 {
		fmt.Fprintf(&buf, "retry: %d\n", event.Retry)
	}

	// Write data lines
	lines := bytes.Split(event.Data, []byte("\n"))
	for _, line := range lines {
		fmt.Fprintf(&buf, "data: %s\n", line)
	}

	buf.WriteString("\n")

	if _, err := s.w.Write(buf.Bytes()); err != nil {
		return err
	}

	s.flusher.Flush()
	return nil
}

// WriteData writes a simple data event.
func (s *SSEWriter) WriteData(data []byte) error {
	return s.WriteEvent(&SSEEvent{Data: data})
}

// Close marks the writer as closed.
func (s *SSEWriter) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

// SSEServer is an HTTP handler that manages SSE connections.
type SSEServer struct {
	clients   map[*SSEWriter]struct{}
	clientsMu sync.RWMutex
	handler   func(ctx context.Context, w *SSEWriter, r *http.Request) error
}

// NewSSEServer creates a new SSE server.
func NewSSEServer(handler func(ctx context.Context, w *SSEWriter, r *http.Request) error) *SSEServer {
	return &SSEServer{
		clients: make(map[*SSEWriter]struct{}),
		handler: handler,
	}
}

// ServeHTTP handles SSE connection requests.
func (s *SSEServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	writer, err := NewSSEWriter(w)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.clientsMu.Lock()
	s.clients[writer] = struct{}{}
	s.clientsMu.Unlock()

	defer func() {
		s.clientsMu.Lock()
		delete(s.clients, writer)
		s.clientsMu.Unlock()
		_ = writer.Close() // Best effort cleanup
	}()

	ctx := r.Context()

	if s.handler != nil {
		_ = s.handler(ctx, writer, r) // Handler errors are not propagated in SSE
	} else {
		// Default: keep connection open until client disconnects
		<-ctx.Done()
	}
}

// Broadcast sends an event to all connected clients.
func (s *SSEServer) Broadcast(event *SSEEvent) {
	s.clientsMu.RLock()
	clients := make([]*SSEWriter, 0, len(s.clients))
	for c := range s.clients {
		clients = append(clients, c)
	}
	s.clientsMu.RUnlock()

	for _, client := range clients {
		_ = client.WriteEvent(event) // Best effort broadcast
	}
}

// BroadcastData sends data to all connected clients.
func (s *SSEServer) BroadcastData(data []byte) {
	s.Broadcast(&SSEEvent{Data: data})
}

// ClientCount returns the number of connected clients.
func (s *SSEServer) ClientCount() int {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()
	return len(s.clients)
}

// SSETransport wraps SSEClient to implement the Transport interface.
// Note: This is read-only; Send operations will fail.
type SSETransport struct {
	*SSEClient
}

// NewSSETransport creates a new SSE transport.
func NewSSETransport(url string) *SSETransport {
	return &SSETransport{
		SSEClient: NewSSEClient(url),
	}
}

// SSEHeartbeat sends periodic heartbeat comments to keep connections alive.
type SSEHeartbeat struct {
	writer   *SSEWriter
	interval time.Duration
	stopCh   chan struct{}
	done     chan struct{}
}

// NewSSEHeartbeat creates a new heartbeat sender.
func NewSSEHeartbeat(writer *SSEWriter, interval time.Duration) *SSEHeartbeat {
	return &SSEHeartbeat{
		writer:   writer,
		interval: interval,
		stopCh:   make(chan struct{}),
		done:     make(chan struct{}),
	}
}

// Start starts sending heartbeats.
func (h *SSEHeartbeat) Start() {
	go func() {
		defer close(h.done)
		ticker := time.NewTicker(h.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				// Send comment as heartbeat
				h.writer.mu.Lock()
				if !h.writer.closed {
					_, _ = h.writer.w.Write([]byte(": heartbeat\n\n"))
					h.writer.flusher.Flush()
				}
				h.writer.mu.Unlock()
			case <-h.stopCh:
				return
			}
		}
	}()
}

// Stop stops sending heartbeats and waits for the goroutine to exit.
func (h *SSEHeartbeat) Stop() {
	close(h.stopCh)
	<-h.done
}
