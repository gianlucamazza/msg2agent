package transport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// TestNewWebSocketTransport tests creation of WebSocket transport.
func TestNewWebSocketTransport(t *testing.T) {
	config := DefaultConfig("ws://localhost:8080")
	transport := NewWebSocketTransport(config)

	if transport == nil {
		t.Fatal("NewWebSocketTransport returned nil")
	}
	if transport.remoteAddr != "ws://localhost:8080" {
		t.Errorf("remoteAddr = %q, want %q", transport.remoteAddr, "ws://localhost:8080")
	}
	if transport.IsConnected() {
		t.Error("new transport should not be connected")
	}
}

// TestDefaultConfig tests default configuration values.
func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig("ws://test:9000")

	if config.Address != "ws://test:9000" {
		t.Errorf("Address = %q, want %q", config.Address, "ws://test:9000")
	}
	if config.MaxMessageSize != 1<<20 {
		t.Errorf("MaxMessageSize = %d, want %d", config.MaxMessageSize, 1<<20)
	}
	if config.ReadBufferSize != 4096 {
		t.Errorf("ReadBufferSize = %d, want %d", config.ReadBufferSize, 4096)
	}
	if config.WriteBufferSize != 4096 {
		t.Errorf("WriteBufferSize = %d, want %d", config.WriteBufferSize, 4096)
	}
	if config.TLSEnabled {
		t.Error("TLSEnabled should be false by default")
	}
}

// TestWebSocketTransportConnectDisconnect tests connection lifecycle.
func TestWebSocketTransportConnectDisconnect(t *testing.T) {
	server := newTestWSServer(t)
	defer server.Close()

	addr := "ws" + strings.TrimPrefix(server.URL, "http")
	config := DefaultConfig(addr)
	transport := NewWebSocketTransport(config)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Connect
	if err := transport.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	if !transport.IsConnected() {
		t.Error("transport should be connected after Connect")
	}

	// Connect again should be no-op
	if err := transport.Connect(ctx); err != nil {
		t.Fatalf("second Connect should not fail: %v", err)
	}

	// Close
	if err := transport.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if transport.IsConnected() {
		t.Error("transport should not be connected after Close")
	}

	// Close again should be no-op
	if err := transport.Close(); err != nil {
		t.Fatalf("second Close should not fail: %v", err)
	}
}

// TestWebSocketTransportSendReceive tests message exchange.
func TestWebSocketTransportSendReceive(t *testing.T) {
	echoCh := make(chan []byte, 1)
	server := newTestWSEchoServer(t, echoCh)
	defer server.Close()

	addr := "ws" + strings.TrimPrefix(server.URL, "http")
	config := DefaultConfig(addr)
	transport := NewWebSocketTransport(config)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := transport.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer transport.Close()

	testMsg := []byte("hello world")
	if err := transport.Send(ctx, testMsg); err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	received, err := transport.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive failed: %v", err)
	}
	if string(received) != string(testMsg) {
		t.Errorf("received = %q, want %q", received, testMsg)
	}
}

// TestWebSocketTransportNotConnected tests operations when not connected.
func TestWebSocketTransportNotConnected(t *testing.T) {
	config := DefaultConfig("ws://localhost:9999")
	transport := NewWebSocketTransport(config)

	ctx := context.Background()

	err := transport.Send(ctx, []byte("test"))
	if err != ErrNotConnected {
		t.Errorf("Send on disconnected transport: got %v, want ErrNotConnected", err)
	}

	_, err = transport.Receive(ctx)
	if err != ErrNotConnected {
		t.Errorf("Receive on disconnected transport: got %v, want ErrNotConnected", err)
	}
}

// TestWebSocketTransportRemoteAddr tests RemoteAddr method.
func TestWebSocketTransportRemoteAddr(t *testing.T) {
	addr := "ws://example.com:8080/path"
	config := DefaultConfig(addr)
	transport := NewWebSocketTransport(config)

	if transport.RemoteAddr() != addr {
		t.Errorf("RemoteAddr = %q, want %q", transport.RemoteAddr(), addr)
	}
}

// TestWebSocketTransportConnectionRefused tests connection failure.
func TestWebSocketTransportConnectionRefused(t *testing.T) {
	config := DefaultConfig("ws://127.0.0.1:59999") // Unlikely to be listening
	transport := NewWebSocketTransport(config)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := transport.Connect(ctx)
	if err == nil {
		transport.Close()
		t.Fatal("Connect should fail when server is not running")
	}
}

// TestWebSocketTransportContextCancellation tests context cancellation.
func TestWebSocketTransportContextCancellation(t *testing.T) {
	server := newTestWSSlowServer(t, 5*time.Second)
	defer server.Close()

	addr := "ws" + strings.TrimPrefix(server.URL, "http")
	config := DefaultConfig(addr)
	transport := NewWebSocketTransport(config)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := transport.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer transport.Close()

	// Cancel context during receive
	receiveCtx, receiveCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer receiveCancel()

	_, err := transport.Receive(receiveCtx)
	if err == nil {
		t.Error("Receive should fail when context is cancelled")
	}
}

// TestNewWebSocketListener tests listener creation.
func TestNewWebSocketListener(t *testing.T) {
	config := DefaultConfig(":0")
	listener := NewWebSocketListener(config)

	if listener == nil {
		t.Fatal("NewWebSocketListener returned nil")
	}
	if listener.Addr() != ":0" {
		t.Errorf("Addr = %q, want %q", listener.Addr(), ":0")
	}
}

// TestWebSocketListenerAccept tests accepting connections.
func TestWebSocketListenerAccept(t *testing.T) {
	config := DefaultConfig("127.0.0.1:0")
	listener := NewWebSocketListener(config)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Start listener
	if err := listener.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer listener.Close()

	// Connect client in background
	clientConfig := DefaultConfig("ws://" + listener.Addr())
	client := NewWebSocketTransport(clientConfig)

	connectDone := make(chan error, 1)
	go func() {
		connectDone <- client.Connect(ctx)
	}()

	// Accept the connection
	accepted, err := listener.Accept(ctx)
	if err != nil {
		t.Fatalf("Accept failed: %v", err)
	}

	if err := <-connectDone; err != nil {
		t.Fatalf("Client Connect failed: %v", err)
	}

	// Verify both transports work
	testMsg := []byte("ping")
	if err := client.Send(ctx, testMsg); err != nil {
		t.Fatalf("Client Send failed: %v", err)
	}

	received, err := accepted.Receive(ctx)
	if err != nil {
		t.Fatalf("Accepted Receive failed: %v", err)
	}
	if string(received) != string(testMsg) {
		t.Errorf("received = %q, want %q", received, testMsg)
	}

	client.Close()
	accepted.Close()
}

// TestWebSocketListenerClose tests listener close behavior.
func TestWebSocketListenerClose(t *testing.T) {
	config := DefaultConfig("127.0.0.1:0")
	listener := NewWebSocketListener(config)

	ctx := context.Background()
	if err := listener.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Give server time to start
	time.Sleep(50 * time.Millisecond)

	// Close listener
	if err := listener.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Accept should return error
	acceptCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()

	_, err := listener.Accept(acceptCtx)
	if err != ErrClosed && err != context.DeadlineExceeded {
		t.Errorf("Accept after Close: got %v, want ErrClosed or DeadlineExceeded", err)
	}

	// Close again should be no-op
	if err := listener.Close(); err != nil {
		t.Fatalf("second Close should not fail: %v", err)
	}
}

// TestWebSocketListenerAcceptContextCancel tests context cancellation on Accept.
func TestWebSocketListenerAcceptContextCancel(t *testing.T) {
	config := DefaultConfig("127.0.0.1:0")
	listener := NewWebSocketListener(config)

	ctx := context.Background()
	if err := listener.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer listener.Close()

	// Give server time to start
	time.Sleep(50 * time.Millisecond)

	// Accept with short timeout
	acceptCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()

	_, err := listener.Accept(acceptCtx)
	if err != context.DeadlineExceeded {
		t.Errorf("Accept with cancelled context: got %v, want DeadlineExceeded", err)
	}
}

// TestWebSocketConcurrentSendReceive tests concurrent message operations.
func TestWebSocketConcurrentSendReceive(t *testing.T) {
	echoCh := make(chan []byte, 100)
	server := newTestWSEchoServer(t, echoCh)
	defer server.Close()

	addr := "ws" + strings.TrimPrefix(server.URL, "http")
	config := DefaultConfig(addr)
	transport := NewWebSocketTransport(config)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := transport.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer transport.Close()

	const numMessages = 50
	var wg sync.WaitGroup
	errors := make(chan error, numMessages*2)

	// Sender goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < numMessages; i++ {
			msg := []byte("message")
			if err := transport.Send(ctx, msg); err != nil {
				errors <- err
				return
			}
		}
	}()

	// Receiver goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < numMessages; i++ {
			_, err := transport.Receive(ctx)
			if err != nil {
				errors <- err
				return
			}
		}
	}()

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("concurrent operation failed: %v", err)
	}
}

// TestWebSocketMultipleClients tests multiple clients connecting.
func TestWebSocketMultipleClients(t *testing.T) {
	config := DefaultConfig("127.0.0.1:0")
	listener := NewWebSocketListener(config)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := listener.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer listener.Close()

	const numClients = 3
	var wg sync.WaitGroup
	clients := make([]*WebSocketTransport, numClients)
	accepted := make([]Transport, numClients)
	errors := make(chan error, numClients*2)

	// Connect clients
	for i := range numClients {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			clientConfig := DefaultConfig("ws://" + listener.Addr())
			clients[i] = NewWebSocketTransport(clientConfig)
			if err := clients[i].Connect(ctx); err != nil {
				errors <- err
			}
		}(i)
	}

	// Accept connections
	for i := range numClients {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			conn, err := listener.Accept(ctx)
			if err != nil {
				errors <- err
				return
			}
			accepted[i] = conn
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Fatalf("connection failed: %v", err)
	}

	// Clean up
	for _, c := range clients {
		if c != nil {
			c.Close()
		}
	}
	for _, a := range accepted {
		if a != nil {
			a.Close()
		}
	}
}

// TestWebSocketTransportIsConnectedThreadSafe tests thread safety of IsConnected.
func TestWebSocketTransportIsConnectedThreadSafe(t *testing.T) {
	server := newTestWSServer(t)
	defer server.Close()

	addr := "ws" + strings.TrimPrefix(server.URL, "http")
	config := DefaultConfig(addr)
	transport := NewWebSocketTransport(config)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := transport.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = transport.IsConnected()
		}()
	}

	// Close while checking
	go func() {
		time.Sleep(10 * time.Millisecond)
		transport.Close()
	}()

	wg.Wait()
}

// Helper: creates a simple WebSocket test server.
func newTestWSServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")

		// Keep connection open
		ctx := r.Context()
		for {
			_, _, err := conn.Read(ctx)
			if err != nil {
				return
			}
		}
	}))
}

// Helper: creates an echo WebSocket server.
func newTestWSEchoServer(t *testing.T, echoCh chan []byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")

		ctx := r.Context()
		for {
			msgType, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			if err := conn.Write(ctx, msgType, data); err != nil {
				return
			}
			select {
			case echoCh <- data:
			default:
			}
		}
	}))
}

// Helper: creates a slow WebSocket server that doesn't respond.
func newTestWSSlowServer(t *testing.T, delay time.Duration) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")

		// Sleep without responding
		time.Sleep(delay)
	}))
}
