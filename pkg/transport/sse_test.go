package transport

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestNewSSEClient tests SSE client creation.
func TestNewSSEClient(t *testing.T) {
	client := NewSSEClient("http://example.com/events")
	if client == nil {
		t.Fatal("NewSSEClient returned nil")
	}
	if client.url != "http://example.com/events" {
		t.Errorf("url = %q, want %q", client.url, "http://example.com/events")
	}
	if client.client == nil {
		t.Error("http client should be set")
	}
}

// TestNewSSEClientWithHTTPClient tests SSE client with custom HTTP client.
func TestNewSSEClientWithHTTPClient(t *testing.T) {
	httpClient := &http.Client{Timeout: 5 * time.Second}
	client := NewSSEClientWithHTTPClient("http://example.com/events", httpClient)

	if client.client != httpClient {
		t.Error("custom http client should be used")
	}
}

// TestSSEClientConnect tests SSE connection.
func TestSSEClientConnect(t *testing.T) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify headers
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Error("Accept header should be text/event-stream")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()

		// Keep connection open briefly
		time.Sleep(100 * time.Millisecond)
	}))
	defer server.Close()

	client := NewSSEClient(server.URL)
	err := client.Connect(context.Background())
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	if !client.IsConnected() {
		t.Error("should be connected")
	}

	client.Close()
}

// TestSSEClientConnectClosed tests connecting after close.
func TestSSEClientConnectClosed(t *testing.T) {
	client := NewSSEClient("http://example.com/events")
	client.Close()

	err := client.Connect(context.Background())
	if err != ErrSSEClosed {
		t.Errorf("expected ErrSSEClosed, got %v", err)
	}
}

// TestSSEClientClose tests closing.
func TestSSEClientClose(t *testing.T) {
	client := NewSSEClient("http://example.com/events")

	if err := client.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	if client.IsConnected() {
		t.Error("should not be connected after Close")
	}

	// Double close should be safe
	if err := client.Close(); err != nil {
		t.Errorf("double Close should not error: %v", err)
	}
}

// TestSSEClientSend tests that Send returns error (read-only).
func TestSSEClientSend(t *testing.T) {
	client := NewSSEClient("http://example.com/events")

	err := client.Send(context.Background(), []byte("test"))
	if err == nil {
		t.Error("Send should return error for SSE client")
	}
}

// TestSSEClientReceive tests receiving events.
func TestSSEClientReceive(t *testing.T) {
	// Create test server that sends events
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()

		// Send an event
		w.Write([]byte("data: hello world\n\n"))
		w.(http.Flusher).Flush()
	}))
	defer server.Close()

	client := NewSSEClient(server.URL)
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	data, err := client.Receive(context.Background())
	if err != nil {
		t.Fatalf("Receive failed: %v", err)
	}

	if string(data) != "hello world" {
		t.Errorf("received = %q, want %q", string(data), "hello world")
	}
}

// TestSSEClientReceiveEvent tests receiving full events.
func TestSSEClientReceiveEvent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()

		// Send event with all fields
		w.Write([]byte("id: 123\nevent: message\ndata: test data\nretry: 5000\n\n"))
		w.(http.Flusher).Flush()
	}))
	defer server.Close()

	client := NewSSEClient(server.URL)
	client.Connect(context.Background())
	defer client.Close()

	event, err := client.ReceiveEvent(context.Background())
	if err != nil {
		t.Fatalf("ReceiveEvent failed: %v", err)
	}

	if event.ID != "123" {
		t.Errorf("ID = %q, want %q", event.ID, "123")
	}
	if event.Event != "message" {
		t.Errorf("Event = %q, want %q", event.Event, "message")
	}
	if string(event.Data) != "test data" {
		t.Errorf("Data = %q, want %q", string(event.Data), "test data")
	}
	if event.Retry != 5000 {
		t.Errorf("Retry = %d, want %d", event.Retry, 5000)
	}
}

// TestSSEClientReceiveMultiline tests receiving multiline data.
func TestSSEClientReceiveMultiline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()

		// Send multiline data
		w.Write([]byte("data: line1\ndata: line2\ndata: line3\n\n"))
		w.(http.Flusher).Flush()
	}))
	defer server.Close()

	client := NewSSEClient(server.URL)
	client.Connect(context.Background())
	defer client.Close()

	event, err := client.ReceiveEvent(context.Background())
	if err != nil {
		t.Fatalf("ReceiveEvent failed: %v", err)
	}

	expected := "line1\nline2\nline3"
	if string(event.Data) != expected {
		t.Errorf("Data = %q, want %q", string(event.Data), expected)
	}
}

// TestSSEClientRemoteAddr tests remote address.
func TestSSEClientRemoteAddr(t *testing.T) {
	client := NewSSEClient("http://example.com/events")
	if client.RemoteAddr() != "http://example.com/events" {
		t.Errorf("RemoteAddr = %q, want %q", client.RemoteAddr(), "http://example.com/events")
	}
}

// TestNewSSEWriter tests SSE writer creation.
func TestNewSSEWriter(t *testing.T) {
	w := httptest.NewRecorder()
	writer, err := NewSSEWriter(w)
	if err != nil {
		t.Fatalf("NewSSEWriter failed: %v", err)
	}

	if writer == nil {
		t.Fatal("writer should not be nil")
	}

	// Check headers
	if w.Header().Get("Content-Type") != "text/event-stream" {
		t.Error("Content-Type header should be text/event-stream")
	}
	if w.Header().Get("Cache-Control") != "no-cache" {
		t.Error("Cache-Control header should be no-cache")
	}
}

// TestSSEWriterWriteEvent tests writing events.
func TestSSEWriterWriteEvent(t *testing.T) {
	w := httptest.NewRecorder()
	writer, _ := NewSSEWriter(w)

	event := &SSEEvent{
		ID:    "1",
		Event: "message",
		Data:  []byte("hello"),
	}

	if err := writer.WriteEvent(event); err != nil {
		t.Fatalf("WriteEvent failed: %v", err)
	}

	body := w.Body.String()
	if !strings.Contains(body, "id: 1") {
		t.Error("output should contain id")
	}
	if !strings.Contains(body, "event: message") {
		t.Error("output should contain event")
	}
	if !strings.Contains(body, "data: hello") {
		t.Error("output should contain data")
	}
}

// TestSSEWriterWriteData tests writing simple data.
func TestSSEWriterWriteData(t *testing.T) {
	w := httptest.NewRecorder()
	writer, _ := NewSSEWriter(w)

	if err := writer.WriteData([]byte("test")); err != nil {
		t.Fatalf("WriteData failed: %v", err)
	}

	body := w.Body.String()
	if !strings.Contains(body, "data: test") {
		t.Error("output should contain data")
	}
}

// TestSSEWriterWriteMultilineData tests writing multiline data.
func TestSSEWriterWriteMultilineData(t *testing.T) {
	w := httptest.NewRecorder()
	writer, _ := NewSSEWriter(w)

	event := &SSEEvent{
		Data: []byte("line1\nline2"),
	}

	if err := writer.WriteEvent(event); err != nil {
		t.Fatalf("WriteEvent failed: %v", err)
	}

	body := w.Body.String()
	if !strings.Contains(body, "data: line1\n") {
		t.Error("output should contain first data line")
	}
	if !strings.Contains(body, "data: line2\n") {
		t.Error("output should contain second data line")
	}
}

// TestSSEWriterClose tests closing.
func TestSSEWriterClose(t *testing.T) {
	w := httptest.NewRecorder()
	writer, _ := NewSSEWriter(w)

	if err := writer.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Write after close should fail
	err := writer.WriteData([]byte("test"))
	if err != ErrSSEClosed {
		t.Errorf("expected ErrSSEClosed, got %v", err)
	}
}

// TestNewSSEServer tests SSE server creation.
func TestNewSSEServer(t *testing.T) {
	server := NewSSEServer(nil)
	if server == nil {
		t.Fatal("NewSSEServer returned nil")
	}
	if server.clients == nil {
		t.Error("clients map should be initialized")
	}
}

// TestSSEServerServeHTTP tests handling SSE requests.
func TestSSEServerServeHTTP(t *testing.T) {
	eventReceived := make(chan bool, 1)

	sseServer := NewSSEServer(func(ctx context.Context, w *SSEWriter, r *http.Request) error {
		w.WriteData([]byte("hello"))
		eventReceived <- true
		return nil
	})

	server := httptest.NewServer(sseServer)
	defer server.Close()

	// Make request
	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	// Wait for event
	select {
	case <-eventReceived:
		// OK
	case <-time.After(time.Second):
		t.Error("timeout waiting for event")
	}
}

// TestSSEServerBroadcast tests broadcasting to clients.
func TestSSEServerBroadcast(t *testing.T) {
	sseServer := NewSSEServer(func(ctx context.Context, w *SSEWriter, r *http.Request) error {
		<-ctx.Done()
		return nil
	})

	server := httptest.NewServer(sseServer)
	defer server.Close()

	// Connect client
	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	// Wait for client to register
	time.Sleep(50 * time.Millisecond)

	// Check client count
	if sseServer.ClientCount() != 1 {
		t.Errorf("ClientCount = %d, want 1", sseServer.ClientCount())
	}

	// Broadcast
	sseServer.BroadcastData([]byte("broadcast message"))
}

// TestSSEServerClientCount tests client counting.
func TestSSEServerClientCount(t *testing.T) {
	sseServer := NewSSEServer(nil)

	if sseServer.ClientCount() != 0 {
		t.Error("initial client count should be 0")
	}
}

// TestSSEEvent tests SSE event structure.
func TestSSEEvent(t *testing.T) {
	event := &SSEEvent{
		ID:    "42",
		Event: "update",
		Data:  []byte(`{"status":"ok"}`),
		Retry: 3000,
	}

	if event.ID != "42" {
		t.Error("ID mismatch")
	}
	if event.Event != "update" {
		t.Error("Event mismatch")
	}
	if string(event.Data) != `{"status":"ok"}` {
		t.Error("Data mismatch")
	}
	if event.Retry != 3000 {
		t.Error("Retry mismatch")
	}
}

// TestNewSSETransport tests SSE transport creation.
func TestNewSSETransport(t *testing.T) {
	transport := NewSSETransport("http://example.com/events")
	if transport == nil {
		t.Fatal("NewSSETransport returned nil")
	}
	if transport.SSEClient == nil {
		t.Error("SSEClient should be set")
	}
}

// TestSSEHeartbeat tests heartbeat functionality.
func TestSSEHeartbeat(t *testing.T) {
	w := httptest.NewRecorder()
	writer, _ := NewSSEWriter(w)

	heartbeat := NewSSEHeartbeat(writer, 10*time.Millisecond)
	heartbeat.Start()

	// Wait for at least one heartbeat
	time.Sleep(25 * time.Millisecond)

	// Stop waits for goroutine to fully exit
	heartbeat.Stop()

	// Now safe to read without race
	body := w.Body.String()
	if !strings.Contains(body, ": heartbeat") {
		t.Error("output should contain heartbeat comment")
	}
}

// mockResponseWriter for testing no-flusher scenario
type mockResponseWriter struct {
	header http.Header
	buf    bytes.Buffer
}

func (m *mockResponseWriter) Header() http.Header {
	if m.header == nil {
		m.header = make(http.Header)
	}
	return m.header
}

func (m *mockResponseWriter) Write(b []byte) (int, error) {
	return m.buf.Write(b)
}

func (m *mockResponseWriter) WriteHeader(statusCode int) {}

// TestNewSSEWriterNoFlusher tests error when ResponseWriter doesn't support flushing.
func TestNewSSEWriterNoFlusher(t *testing.T) {
	w := &mockResponseWriter{}
	_, err := NewSSEWriter(w)
	if err != ErrNoFlusher {
		t.Errorf("expected ErrNoFlusher, got %v", err)
	}
}

// TestSSEWriterRetry tests writing retry field.
func TestSSEWriterRetry(t *testing.T) {
	w := httptest.NewRecorder()
	writer, _ := NewSSEWriter(w)

	event := &SSEEvent{
		Data:  []byte("test"),
		Retry: 5000,
	}

	writer.WriteEvent(event)

	body := w.Body.String()
	if !strings.Contains(body, "retry: 5000") {
		t.Error("output should contain retry field")
	}
}

// TestSSEConcurrentWrites tests concurrent write safety.
func TestSSEConcurrentWrites(t *testing.T) {
	w := httptest.NewRecorder()
	writer, _ := NewSSEWriter(w)

	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			_ = writer.WriteData([]byte("message"))
		})
	}
	wg.Wait()

	// Just verify no panics occurred
	if w.Body.Len() == 0 {
		t.Error("expected some output")
	}
}

// TestSSEClientReceiveClosed tests receiving on closed client.
func TestSSEClientReceiveClosed(t *testing.T) {
	client := NewSSEClient("http://example.com/events")
	client.Close()

	_, err := client.Receive(context.Background())
	if err != ErrSSEClosed {
		t.Errorf("expected ErrSSEClosed, got %v", err)
	}
}

// TestSSEClientReceiveContextCanceled tests context cancellation.
func TestSSEClientReceiveContextCanceled(t *testing.T) {
	client := NewSSEClient("http://example.com/events")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.Receive(ctx)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}
