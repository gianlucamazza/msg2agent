package main

import (
	"encoding/json"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gianluca/msg2agent/pkg/messaging"
	"github.com/gianluca/msg2agent/pkg/protocol"
	"github.com/gianluca/msg2agent/pkg/registry"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func testHub() *RelayHub {
	cfg := DefaultRelayConfig()
	cfg.RequireDIDProof = false // Disable DID proof for tests
	hub := NewRelayHub(cfg, testLogger())
	return hub
}

func testClient(hub *RelayHub, id string, did string) *Client {
	cfg := hub.config
	return &Client{
		ID:              id,
		DID:             did,
		SendCh:          make(chan []byte, 256),
		hub:             hub,
		msgLimiter:      messaging.NewRateLimiter(cfg.MessageRateLimit, cfg.MessageBurstSize),
		registerLimiter: messaging.NewRateLimiter(cfg.RegisterRateLimit, 2),
		discoverLimiter: messaging.NewRateLimiter(cfg.DiscoverRateLimit, 20),
	}
}

func TestNewRelayHub(t *testing.T) {
	hub := testHub()

	if hub == nil {
		t.Fatal("NewRelayHub returned nil")
	}

	if hub.store == nil {
		t.Error("store should be initialized")
	}

	if hub.clients == nil {
		t.Error("clients map should be initialized")
	}

	if hub.logger == nil {
		t.Error("logger should be set")
	}

	// Verify config was applied
	if hub.config.MaxConnections != 1000 {
		t.Error("config should be applied")
	}
}

func TestRegisterUnregister(t *testing.T) {
	hub := testHub()

	client := &Client{
		ID:     "client-1",
		DID:    "did:wba:example.com:agent:alice",
		SendCh: make(chan []byte, 256),
		hub:    hub,
	}

	// Register
	hub.Register(client)

	hub.mu.RLock()
	if _, ok := hub.clients["client-1"]; !ok {
		t.Error("client should be registered by ID")
	}
	if _, ok := hub.clients["did:wba:example.com:agent:alice"]; !ok {
		t.Error("client should be registered by DID")
	}
	hub.mu.RUnlock()

	// Unregister
	hub.Unregister(client)

	hub.mu.RLock()
	if _, ok := hub.clients["client-1"]; ok {
		t.Error("client should be unregistered by ID")
	}
	if _, ok := hub.clients["did:wba:example.com:agent:alice"]; ok {
		t.Error("client should be unregistered by DID")
	}
	hub.mu.RUnlock()
}

func TestRegisterWithoutDID(t *testing.T) {
	hub := testHub()

	client := &Client{
		ID:     "client-no-did",
		DID:    "", // Empty DID
		SendCh: make(chan []byte, 256),
		hub:    hub,
	}

	hub.Register(client)

	hub.mu.RLock()
	if _, ok := hub.clients["client-no-did"]; !ok {
		t.Error("client should be registered by ID")
	}
	// Empty DID should not create an entry
	if _, ok := hub.clients[""]; ok {
		t.Error("empty DID should not create an entry")
	}
	hub.mu.RUnlock()
}

func TestRoute(t *testing.T) {
	hub := testHub()

	// Create and register recipient
	recipient := &Client{
		ID:     "recipient-1",
		DID:    "did:wba:example.com:agent:bob",
		SendCh: make(chan []byte, 256),
		hub:    hub,
	}
	hub.Register(recipient)

	// Create message to route
	msg := &messaging.Message{
		From:   "did:wba:example.com:agent:alice",
		To:     "did:wba:example.com:agent:bob",
		Method: "test.method",
	}

	data := []byte(`{"test": "data"}`)

	// Route the message
	err := hub.Route(msg, data)
	if err != nil {
		t.Fatalf("Route failed: %v", err)
	}

	// Check that recipient received the message
	select {
	case received := <-recipient.SendCh:
		if string(received) != string(data) {
			t.Errorf("received = %q, want %q", received, data)
		}
	default:
		t.Error("recipient should have received message")
	}
}

func TestRouteRecipientNotFound(t *testing.T) {
	// Disable offline queue to test error case
	cfg := DefaultRelayConfig()
	cfg.EnableOfflineQueue = false
	hub := NewRelayHubWithStore(cfg, registry.NewMemoryStore(), nil, testLogger())
	defer hub.Stop()

	msg := &messaging.Message{
		From: "did:wba:example.com:agent:alice",
		To:   "did:wba:example.com:agent:unknown",
	}

	err := hub.Route(msg, []byte(`{}`))
	if err == nil {
		t.Error("Route should fail for unknown recipient when queue disabled")
	}
}

func TestRouteRecipientOfflineQueued(t *testing.T) {
	// Test that messages are queued when recipient is offline
	hub := testHub()
	defer hub.Stop()

	msg := &messaging.Message{
		ID:   uuid.Must(uuid.NewV7()),
		From: "did:wba:example.com:agent:alice",
		To:   "did:wba:example.com:agent:offline",
	}

	err := hub.Route(msg, []byte(`{"test":"data"}`))
	if err != nil {
		t.Errorf("Route should succeed with offline queue: %v", err)
	}

	// Verify message was queued
	size, err := hub.queue.GetQueueSize("did:wba:example.com:agent:offline")
	if err != nil {
		t.Fatalf("failed to get queue size: %v", err)
	}
	if size != 1 {
		t.Errorf("expected 1 queued message, got %d", size)
	}
}

func TestRouteBufferFull(t *testing.T) {
	hub := testHub()

	// Create recipient with tiny buffer
	recipient := &Client{
		ID:     "full-buffer-client",
		DID:    "did:wba:example.com:agent:bob",
		SendCh: make(chan []byte, 1), // Buffer size 1
		hub:    hub,
	}
	hub.Register(recipient)

	// Fill the buffer
	recipient.SendCh <- []byte("filling")

	msg := &messaging.Message{
		From: "did:wba:example.com:agent:alice",
		To:   "did:wba:example.com:agent:bob",
	}

	// Route should fail when buffer is full
	err := hub.Route(msg, []byte(`{}`))
	if err == nil {
		t.Error("Route should fail when buffer is full")
	}
}

func TestBroadcast(t *testing.T) {
	hub := testHub()

	// Create multiple clients
	clients := make([]*Client, 3)
	for i := 0; i < 3; i++ {
		clients[i] = &Client{
			ID:     "client-" + string(rune('a'+i)),
			DID:    "did:wba:example.com:agent:" + string(rune('a'+i)),
			SendCh: make(chan []byte, 256),
			hub:    hub,
		}
		hub.Register(clients[i])
	}

	// Broadcast from client-a
	data := []byte(`{"broadcast": "message"}`)
	hub.Broadcast("client-a", data)

	// client-a (sender) should NOT receive
	select {
	case <-clients[0].SendCh:
		t.Error("sender should not receive broadcast")
	default:
		// OK
	}

	// client-b and client-c should receive
	for i := 1; i < 3; i++ {
		select {
		case received := <-clients[i].SendCh:
			if string(received) != string(data) {
				t.Errorf("client %d: received = %q, want %q", i, received, data)
			}
		default:
			t.Errorf("client %d should have received broadcast", i)
		}
	}
}

func TestBroadcastNoDuplicates(t *testing.T) {
	hub := testHub()

	// Create client registered by both ID and DID
	client := &Client{
		ID:     "test-client",
		DID:    "did:wba:example.com:agent:bob",
		SendCh: make(chan []byte, 256),
		hub:    hub,
	}
	hub.Register(client)

	// Broadcast
	hub.Broadcast("sender", []byte(`test`))

	// Should only receive once (not twice for ID and DID)
	count := 0
	for {
		select {
		case <-client.SendCh:
			count++
		default:
			goto done
		}
	}
done:

	if count != 1 {
		t.Errorf("received %d messages, want 1 (no duplicates)", count)
	}
}

// mockConn simulates a websocket connection for testing
type mockConn struct {
	writeData [][]byte
	closed    bool
}

func TestHandleRegisterDirect(t *testing.T) {
	hub := testHub()

	client := testClient(hub, "register-test", "")
	hub.Register(client)

	// Create registration request
	agent := registry.Agent{
		DID:         "did:wba:example.com:agent:alice",
		DisplayName: "Alice",
		Capabilities: []registry.Capability{
			{Name: "chat", Description: "Chat capability"},
		},
	}

	req, _ := protocol.NewRequest("1", "relay.register", agent)

	// Call handleRegister directly
	client.handleRegister(req)

	// Check client DID was updated
	if client.DID != "did:wba:example.com:agent:alice" {
		t.Errorf("client.DID = %q, want 'did:wba:example.com:agent:alice'", client.DID)
	}

	// Check agent was stored
	stored, err := hub.store.GetByDID("did:wba:example.com:agent:alice")
	if err != nil {
		t.Fatalf("agent not stored: %v", err)
	}
	if stored.DisplayName != "Alice" {
		t.Errorf("stored agent DisplayName = %q, want 'Alice'", stored.DisplayName)
	}

	// Check response was sent
	select {
	case data := <-client.SendCh:
		var resp protocol.JSONRPCResponse
		json.Unmarshal(data, &resp)
		if resp.Error != nil {
			t.Errorf("unexpected error: %v", resp.Error)
		}
	default:
		t.Error("should have received response")
	}
}

func TestHandleRegisterInvalidParams(t *testing.T) {
	hub := testHub()

	client := testClient(hub, "invalid-register", "")
	hub.Register(client)

	// Create request with invalid params
	req := &protocol.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      "1",
		Method:  "relay.register",
		Params:  json.RawMessage(`"not an object"`),
	}

	client.handleRegister(req)

	// Check error response was sent
	select {
	case data := <-client.SendCh:
		var resp protocol.JSONRPCResponse
		json.Unmarshal(data, &resp)
		if resp.Error == nil {
			t.Error("expected error response")
		}
		if resp.Error.Code != protocol.CodeInvalidParams {
			t.Errorf("error code = %d, want %d", resp.Error.Code, protocol.CodeInvalidParams)
		}
	default:
		t.Error("should have received error response")
	}
}

func TestHandleDiscoverAll(t *testing.T) {
	hub := testHub()

	// Add some agents to the store - use NewAgent to get proper IDs
	alice := registry.NewAgent("did:wba:example.com:agent:alice", "Alice")
	bob := registry.NewAgent("did:wba:example.com:agent:bob", "Bob")
	hub.store.Put(alice)
	hub.store.Put(bob)

	client := testClient(hub, "discover-test", "")
	hub.Register(client)

	// Create discover request (no filter)
	req, _ := protocol.NewRequest("1", "relay.discover", nil)

	client.handleDiscover(req)

	// Check response
	select {
	case data := <-client.SendCh:
		var resp protocol.JSONRPCResponse
		json.Unmarshal(data, &resp)
		if resp.Error != nil {
			t.Errorf("unexpected error: %v", resp.Error)
		}

		var result []*registry.Agent
		json.Unmarshal(resp.Result, &result)
		if len(result) != 2 {
			t.Errorf("got %d agents, want 2", len(result))
		}
	default:
		t.Error("should have received response")
	}
}

func TestHandleDiscoverWithCapability(t *testing.T) {
	hub := testHub()

	// Add agents with different capabilities - use NewAgent for proper IDs
	alice := registry.NewAgent("did:wba:example.com:agent:alice", "Alice")
	alice.Capabilities = []registry.Capability{{Name: "translate"}}
	hub.store.Put(alice)

	bob := registry.NewAgent("did:wba:example.com:agent:bob", "Bob")
	bob.Capabilities = []registry.Capability{{Name: "chat"}}
	hub.store.Put(bob)

	client := testClient(hub, "discover-cap-test", "")
	hub.Register(client)

	// Create discover request with capability filter
	req, _ := protocol.NewRequest("1", "relay.discover", map[string]string{"capability": "translate"})

	client.handleDiscover(req)

	// Check response
	select {
	case data := <-client.SendCh:
		var resp protocol.JSONRPCResponse
		json.Unmarshal(data, &resp)
		if resp.Error != nil {
			t.Errorf("unexpected error: %v", resp.Error)
		}

		var result []*registry.Agent
		json.Unmarshal(resp.Result, &result)
		if len(result) != 1 {
			t.Errorf("got %d agents, want 1", len(result))
		}
		if len(result) > 0 && result[0].DID != "did:wba:example.com:agent:alice" {
			t.Errorf("wrong agent returned: %s", result[0].DID)
		}
	default:
		t.Error("should have received response")
	}
}

func TestHandleLookup(t *testing.T) {
	hub := testHub()

	alice := registry.NewAgent("did:wba:example.com:agent:alice", "Alice")
	hub.store.Put(alice)

	client := testClient(hub, "lookup-test", "")
	hub.Register(client)

	req, _ := protocol.NewRequest("1", "relay.lookup", map[string]string{"did": "did:wba:example.com:agent:alice"})
	client.handleLookup(req)

	select {
	case data := <-client.SendCh:
		var resp protocol.JSONRPCResponse
		json.Unmarshal(data, &resp)
		if resp.Error != nil {
			t.Errorf("unexpected error: %v", resp.Error)
		}

		var result registry.Agent
		json.Unmarshal(resp.Result, &result)
		if result.DID != "did:wba:example.com:agent:alice" {
			t.Errorf("got DID %s, want did:wba:example.com:agent:alice", result.DID)
		}
	default:
		t.Error("should have received response")
	}
}

func TestHandleLookupNotFound(t *testing.T) {
	hub := testHub()

	client := testClient(hub, "lookup-notfound-test", "")
	hub.Register(client)

	req, _ := protocol.NewRequest("1", "relay.lookup", map[string]string{"did": "did:wba:example.com:agent:unknown"})
	client.handleLookup(req)

	select {
	case data := <-client.SendCh:
		var resp protocol.JSONRPCResponse
		json.Unmarshal(data, &resp)
		if resp.Error == nil {
			t.Error("expected error for unknown agent")
		}
	default:
		t.Error("should have received response")
	}
}

func TestHandleLookupMissingDID(t *testing.T) {
	hub := testHub()

	client := testClient(hub, "lookup-missing-test", "")
	hub.Register(client)

	req, _ := protocol.NewRequest("1", "relay.lookup", nil)
	client.handleLookup(req)

	select {
	case data := <-client.SendCh:
		var resp protocol.JSONRPCResponse
		json.Unmarshal(data, &resp)
		if resp.Error == nil {
			t.Error("expected error for missing DID")
		}
	default:
		t.Error("should have received response")
	}
}

func TestHandleMessageRouting(t *testing.T) {
	hub := testHub()

	// Create and register sender
	sender := testClient(hub, "sender", "did:wba:example.com:agent:alice")
	hub.Register(sender)

	// Create and register recipient
	recipient := testClient(hub, "recipient", "did:wba:example.com:agent:bob")
	hub.Register(recipient)

	// Create a message request
	msg := messaging.Message{
		From:   "did:wba:example.com:agent:alice",
		To:     "did:wba:example.com:agent:bob",
		Method: "test.method",
	}
	req, _ := protocol.NewRequest("1", "test.method", msg)
	data, _ := protocol.Encode(req)

	// Handle the message
	sender.handleMessage(data)

	// Recipient should receive the routed message
	select {
	case received := <-recipient.SendCh:
		if string(received) != string(data) {
			t.Logf("received different data (may be reformatted)")
		}
	default:
		t.Error("recipient should have received routed message")
	}
}

func TestHandleMessageInvalidJSON(t *testing.T) {
	hub := testHub()

	client := testClient(hub, "invalid-json", "")
	hub.Register(client)

	// Should not panic on invalid JSON
	client.handleMessage([]byte("not valid json"))

	// No response expected for invalid messages
}

func TestHandleMessageInvalidParams(t *testing.T) {
	hub := testHub()

	sender := testClient(hub, "invalid-params", "did:wba:example.com:agent:alice")
	hub.Register(sender)

	// Create request with invalid params
	req := &protocol.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      "1",
		Method:  "some.method",
		Params:  json.RawMessage(`"not a message object"`),
	}
	data, _ := protocol.Encode(req)

	sender.handleMessage(data)

	// Should get error response
	select {
	case respData := <-sender.SendCh:
		var resp protocol.JSONRPCResponse
		json.Unmarshal(respData, &resp)
		if resp.Error == nil {
			t.Error("expected error response")
		}
	default:
		t.Error("should have received error response")
	}
}

func TestHandleMessageNotRegistered(t *testing.T) {
	hub := testHub()

	sender := testClient(hub, "unregistered", "") // Not registered with DID
	hub.Register(sender)

	// Create a valid message
	msg := messaging.Message{
		From:   "did:wba:example.com:agent:alice",
		To:     "did:wba:example.com:agent:bob",
		Method: "test.method",
	}
	req, _ := protocol.NewRequest("1", "test.method", msg)
	data, _ := protocol.Encode(req)

	sender.handleMessage(data)

	// Should get error response (not registered)
	select {
	case respData := <-sender.SendCh:
		var resp protocol.JSONRPCResponse
		json.Unmarshal(respData, &resp)
		if resp.Error == nil {
			t.Error("expected error response")
		}
		if resp.Error.Code != protocol.CodeSenderNotRegistered {
			t.Errorf("error code = %d, want %d", resp.Error.Code, protocol.CodeSenderNotRegistered)
		}
	default:
		t.Error("should have received error response")
	}
}

func TestHandleMessageSenderMismatch(t *testing.T) {
	hub := testHub()

	// Registered as alice but tries to send as bob
	sender := testClient(hub, "spoofing-attempt", "did:wba:example.com:agent:alice")
	hub.Register(sender)

	msg := messaging.Message{
		From:   "did:wba:example.com:agent:bob", // Spoofing!
		To:     "did:wba:example.com:agent:charlie",
		Method: "test.method",
	}
	req, _ := protocol.NewRequest("1", "test.method", msg)
	data, _ := protocol.Encode(req)

	sender.handleMessage(data)

	// Should get error response (sender mismatch)
	select {
	case respData := <-sender.SendCh:
		var resp protocol.JSONRPCResponse
		json.Unmarshal(respData, &resp)
		if resp.Error == nil {
			t.Error("expected error response for spoofing attempt")
		}
		if resp.Error.Code != protocol.CodeSenderMismatch {
			t.Errorf("error code = %d, want %d", resp.Error.Code, protocol.CodeSenderMismatch)
		}
	default:
		t.Error("should have received error response")
	}
}

func TestHandleMessageRoutingError(t *testing.T) {
	// Disable offline queue to test routing error
	cfg := DefaultRelayConfig()
	cfg.EnableOfflineQueue = false
	hub := NewRelayHubWithStore(cfg, registry.NewMemoryStore(), nil, testLogger())
	defer hub.Stop()

	sender := testClient(hub, "routing-error-test", "did:wba:example.com:agent:alice")
	hub.Register(sender)

	// Send to unknown recipient
	msg := messaging.Message{
		From:   "did:wba:example.com:agent:alice",
		To:     "did:wba:example.com:agent:unknown",
		Method: "test.method",
	}
	req, _ := protocol.NewRequest("1", "test.method", msg)
	data, _ := protocol.Encode(req)

	sender.handleMessage(data)

	// Should get routing error (since queue is disabled)
	select {
	case respData := <-sender.SendCh:
		var resp protocol.JSONRPCResponse
		json.Unmarshal(respData, &resp)
		if resp.Error == nil {
			t.Error("expected routing error")
		}
		if resp.Error.Code != protocol.CodeRoutingError {
			t.Errorf("error code = %d, want %d", resp.Error.Code, protocol.CodeRoutingError)
		}
	default:
		t.Error("should have received error response")
	}
}

func TestSendResult(t *testing.T) {
	hub := testHub()

	client := &Client{
		ID:     "result-test",
		SendCh: make(chan []byte, 256),
		hub:    hub,
	}

	result := map[string]string{"status": "ok"}
	client.sendResult("test-id", result)

	select {
	case data := <-client.SendCh:
		var resp protocol.JSONRPCResponse
		json.Unmarshal(data, &resp)
		if resp.Error != nil {
			t.Errorf("unexpected error: %v", resp.Error)
		}
		if resp.ID != "test-id" {
			t.Errorf("ID = %v, want 'test-id'", resp.ID)
		}
	default:
		t.Error("should have received response")
	}
}

func TestSendError(t *testing.T) {
	hub := testHub()

	client := &Client{
		ID:     "error-test",
		SendCh: make(chan []byte, 256),
		hub:    hub,
	}

	client.sendError("test-id", -32000, "test error")

	select {
	case data := <-client.SendCh:
		var resp protocol.JSONRPCResponse
		json.Unmarshal(data, &resp)
		if resp.Error == nil {
			t.Error("expected error response")
		}
		if resp.Error.Code != -32000 {
			t.Errorf("error code = %d, want -32000", resp.Error.Code)
		}
		if resp.Error.Message != "test error" {
			t.Errorf("error message = %q, want 'test error'", resp.Error.Message)
		}
	default:
		t.Error("should have received response")
	}
}

func TestSendResultBufferFull(t *testing.T) {
	hub := testHub()

	// Tiny buffer
	client := &Client{
		ID:     "full-buffer-result",
		SendCh: make(chan []byte, 1),
		hub:    hub,
	}

	// Fill buffer
	client.SendCh <- []byte("blocking")

	// Should not block or panic
	client.sendResult("id", map[string]string{})
}

func TestSendErrorBufferFull(t *testing.T) {
	hub := testHub()

	// Tiny buffer
	client := &Client{
		ID:     "full-buffer-error",
		SendCh: make(chan []byte, 1),
		hub:    hub,
	}

	// Fill buffer
	client.SendCh <- []byte("blocking")

	// Should not block or panic
	client.sendError("id", -32000, "error")
}

func TestConcurrentAccess(t *testing.T) {
	hub := testHub()

	// Concurrent registration and unregistration
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(id int) {
			client := &Client{
				ID:     "concurrent-" + string(rune('a'+id)),
				DID:    "did:wba:test.com:agent:" + string(rune('a'+id)),
				SendCh: make(chan []byte, 256),
				hub:    hub,
			}
			hub.Register(client)
			hub.Route(&messaging.Message{To: client.DID}, []byte("test"))
			hub.Broadcast("other", []byte("broadcast"))
			hub.Unregister(client)
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestBroadcastToEmptyHub(t *testing.T) {
	hub := testHub()

	// Should not panic with no clients
	hub.Broadcast("sender", []byte("test"))
}

func TestRouteToSelf(t *testing.T) {
	hub := testHub()

	client := &Client{
		ID:     "self-route",
		DID:    "did:wba:example.com:agent:self",
		SendCh: make(chan []byte, 256),
		hub:    hub,
	}
	hub.Register(client)

	msg := &messaging.Message{
		From: "did:wba:example.com:agent:self",
		To:   "did:wba:example.com:agent:self",
	}

	err := hub.Route(msg, []byte("self-message"))
	if err != nil {
		t.Fatalf("Route to self failed: %v", err)
	}

	select {
	case <-client.SendCh:
		// OK - received self-message
	default:
		t.Error("should receive message sent to self")
	}
}

// Test rate limiting

func TestMessageRateLimiting(t *testing.T) {
	// Create hub with very low rate limit for testing
	config := DefaultRelayConfig()
	config.MessageRateLimit = 1.0 // 1 msg/sec
	config.MessageBurstSize = 2.0 // burst of 2
	hub := NewRelayHub(config, testLogger())

	sender := testClient(hub, "rate-limited-sender", "did:wba:example.com:agent:alice")
	hub.Register(sender)

	recipient := testClient(hub, "recipient", "did:wba:example.com:agent:bob")
	hub.Register(recipient)

	// Create a valid message
	msg := messaging.Message{
		From:   "did:wba:example.com:agent:alice",
		To:     "did:wba:example.com:agent:bob",
		Method: "test.method",
	}
	req, _ := protocol.NewRequest("1", "test.method", msg)
	data, _ := protocol.Encode(req)

	// First 2 messages should succeed (burst)
	for i := 0; i < 2; i++ {
		sender.handleMessage(data)
	}

	// Drain recipient channel
	for len(recipient.SendCh) > 0 {
		<-recipient.SendCh
	}

	// Third message should be rate limited
	sender.handleMessage(data)

	// Check for rate limit error
	select {
	case respData := <-sender.SendCh:
		var resp protocol.JSONRPCResponse
		json.Unmarshal(respData, &resp)
		if resp.Error == nil {
			t.Error("expected rate limit error")
		}
		if resp.Error.Code != protocol.CodeRateLimited {
			t.Errorf("error code = %d, want %d", resp.Error.Code, protocol.CodeRateLimited)
		}
	default:
		t.Error("should have received rate limit error")
	}
}

func TestRegisterRateLimiting(t *testing.T) {
	// Create hub with very low rate limit for testing
	config := DefaultRelayConfig()
	config.RegisterRateLimit = 1.0 // 1 reg/sec
	hub := NewRelayHub(config, testLogger())

	client := testClient(hub, "register-limited", "")
	hub.Register(client)

	agent := registry.Agent{
		DID:         "did:wba:example.com:agent:test",
		DisplayName: "Test",
	}
	req, _ := protocol.NewRequest("1", "relay.register", agent)

	// First 2 registrations should succeed (burst of 2)
	for i := 0; i < 2; i++ {
		client.handleRegister(req)
		// Drain the response
		<-client.SendCh
	}

	// Third registration should be rate limited
	client.handleRegister(req)

	select {
	case respData := <-client.SendCh:
		var resp protocol.JSONRPCResponse
		json.Unmarshal(respData, &resp)
		if resp.Error == nil {
			t.Error("expected rate limit error")
		}
		if resp.Error.Code != protocol.CodeRateLimited {
			t.Errorf("error code = %d, want %d", resp.Error.Code, protocol.CodeRateLimited)
		}
	default:
		t.Error("should have received rate limit error")
	}
}

func TestDiscoverRateLimiting(t *testing.T) {
	// Create hub with very low rate limit for testing
	config := DefaultRelayConfig()
	config.DiscoverRateLimit = 1.0 // 1 discover/sec
	hub := NewRelayHub(config, testLogger())

	client := testClient(hub, "discover-limited", "")
	hub.Register(client)

	req, _ := protocol.NewRequest("1", "relay.discover", nil)

	// Exhaust burst (20 by default in testClient)
	for i := 0; i < 21; i++ {
		client.handleDiscover(req)
		// Drain the response
		select {
		case <-client.SendCh:
		default:
		}
	}

	// Next discover should be rate limited
	client.handleDiscover(req)

	select {
	case respData := <-client.SendCh:
		var resp protocol.JSONRPCResponse
		json.Unmarshal(respData, &resp)
		if resp.Error == nil {
			t.Error("expected rate limit error")
		}
		if resp.Error.Code != protocol.CodeRateLimited {
			t.Errorf("error code = %d, want %d", resp.Error.Code, protocol.CodeRateLimited)
		}
	default:
		t.Error("should have received rate limit error")
	}
}

func TestDefaultRelayConfig(t *testing.T) {
	config := DefaultRelayConfig()

	if config.MaxConnections != 1000 {
		t.Errorf("MaxConnections = %d, want 1000", config.MaxConnections)
	}
	if config.MessageRateLimit != 100.0 {
		t.Errorf("MessageRateLimit = %f, want 100.0", config.MessageRateLimit)
	}
	if config.ReadTimeout != 5*time.Minute {
		t.Errorf("ReadTimeout = %v, want 5m", config.ReadTimeout)
	}
	if config.WriteTimeout != 10*time.Second {
		t.Errorf("WriteTimeout = %v, want 10s", config.WriteTimeout)
	}
}

func TestConnectionLimit(t *testing.T) {
	// This test verifies the MaxConnections check exists
	// Full integration test would require real WebSocket connections
	config := DefaultRelayConfig()
	config.MaxConnections = 5
	hub := NewRelayHub(config, testLogger())

	if hub.config.MaxConnections != 5 {
		t.Errorf("MaxConnections = %d, want 5", hub.config.MaxConnections)
	}

	// Simulate connections
	hub.connections.Add(5)

	// Verify we're at limit
	if int(hub.connections.Load()) < hub.config.MaxConnections {
		t.Error("should be at connection limit")
	}
}

func TestMetricsFunctions(t *testing.T) {
	// Test that metric functions don't panic
	recordConnectionAccepted()
	recordConnectionClosed()
	recordConnectionRejected()
	recordMessageRouted()
	recordMessageDropped("test_reason")
	recordRateLimitHit("test_type")
	recordRegistration()
	recordDiscovery()
	recordError("test_error")
}

func TestNewRelayHubWithStore(t *testing.T) {
	logger := testLogger()
	config := DefaultRelayConfig()

	t.Run("with memory store", func(t *testing.T) {
		store := registry.NewMemoryStore()
		hub := NewRelayHubWithStore(config, store, nil, logger)
		defer hub.Stop()

		if hub == nil {
			t.Fatal("NewRelayHubWithStore returned nil")
		}
		if hub.store != store {
			t.Error("store should be the provided store")
		}
	})

	t.Run("with sqlite store", func(t *testing.T) {
		store, err := registry.NewSQLiteStore(registry.SQLiteConfig{Path: ":memory:"})
		if err != nil {
			t.Fatalf("failed to create SQLite store: %v", err)
		}
		defer store.Close()

		hub := NewRelayHubWithStore(config, store, nil, logger)
		defer hub.Stop()

		if hub == nil {
			t.Fatal("NewRelayHubWithStore returned nil")
		}

		// Test that operations work with SQLite store
		agent := registry.NewAgent("did:wba:test.com:agent:test", "Test Agent")
		if err := hub.store.Put(agent); err != nil {
			t.Fatalf("Put failed: %v", err)
		}

		retrieved, err := hub.store.GetByDID("did:wba:test.com:agent:test")
		if err != nil {
			t.Fatalf("GetByDID failed: %v", err)
		}
		if retrieved.DisplayName != "Test Agent" {
			t.Errorf("DisplayName = %q, want 'Test Agent'", retrieved.DisplayName)
		}
	})
}

func TestDeliveryAckOnSuccess(t *testing.T) {
	hub := testHub()
	defer hub.Stop()

	// Create and register sender
	sender := testClient(hub, "ack-sender", "did:wba:example.com:agent:alice")
	hub.Register(sender)

	// Create and register recipient
	recipient := testClient(hub, "ack-recipient", "did:wba:example.com:agent:bob")
	hub.Register(recipient)

	// Create message requesting ack
	msg := &messaging.Message{
		ID:         uuid.Must(uuid.NewV7()),
		From:       "did:wba:example.com:agent:alice",
		To:         "did:wba:example.com:agent:bob",
		Method:     "test.method",
		RequestAck: true,
	}

	data := []byte(`{"test": "data"}`)

	// Route the message
	err := hub.Route(msg, data)
	if err != nil {
		t.Fatalf("Route failed: %v", err)
	}

	// Recipient should receive the message
	select {
	case <-recipient.SendCh:
		// OK
	case <-time.After(100 * time.Millisecond):
		t.Error("recipient should have received message")
	}

	// Sender should receive ack notification
	select {
	case ackData := <-sender.SendCh:
		var notification protocol.JSONRPCRequest
		if err := json.Unmarshal(ackData, &notification); err != nil {
			t.Fatalf("failed to unmarshal ack: %v", err)
		}

		if notification.Method != "relay.ack" {
			t.Errorf("method = %q, want 'relay.ack'", notification.Method)
		}

		var ack struct {
			MessageID string `json:"message_id"`
			Delivered bool   `json:"delivered"`
			Status    string `json:"status"`
		}
		if err := json.Unmarshal(notification.Params, &ack); err != nil {
			t.Fatalf("failed to unmarshal ack params: %v", err)
		}

		if ack.MessageID != msg.ID.String() {
			t.Errorf("message_id = %q, want %q", ack.MessageID, msg.ID.String())
		}
		if !ack.Delivered {
			t.Error("delivered should be true")
		}
		if ack.Status != "delivered" {
			t.Errorf("status = %q, want 'delivered'", ack.Status)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("sender should have received ack")
	}
}

func TestDeliveryAckOnFailure(t *testing.T) {
	// Disable offline queue to test failure ack
	cfg := DefaultRelayConfig()
	cfg.EnableOfflineQueue = false
	hub := NewRelayHubWithStore(cfg, registry.NewMemoryStore(), nil, testLogger())
	defer hub.Stop()

	// Create and register sender
	sender := testClient(hub, "ack-sender", "did:wba:example.com:agent:alice")
	hub.Register(sender)

	// Create message requesting ack to non-existent recipient
	msg := &messaging.Message{
		ID:         uuid.Must(uuid.NewV7()),
		From:       "did:wba:example.com:agent:alice",
		To:         "did:wba:example.com:agent:unknown",
		Method:     "test.method",
		RequestAck: true,
	}

	data := []byte(`{"test": "data"}`)

	// Route should fail
	err := hub.Route(msg, data)
	if err == nil {
		t.Fatal("Route should have failed")
	}

	// Sender should receive negative ack notification
	select {
	case ackData := <-sender.SendCh:
		var notification protocol.JSONRPCRequest
		if err := json.Unmarshal(ackData, &notification); err != nil {
			t.Fatalf("failed to unmarshal ack: %v", err)
		}

		if notification.Method != "relay.ack" {
			t.Errorf("method = %q, want 'relay.ack'", notification.Method)
		}

		var ack struct {
			MessageID string `json:"message_id"`
			Delivered bool   `json:"delivered"`
			Status    string `json:"status"`
		}
		if err := json.Unmarshal(notification.Params, &ack); err != nil {
			t.Fatalf("failed to unmarshal ack params: %v", err)
		}

		if ack.MessageID != msg.ID.String() {
			t.Errorf("message_id = %q, want %q", ack.MessageID, msg.ID.String())
		}
		if ack.Delivered {
			t.Error("delivered should be false")
		}
		if ack.Status != "recipient not found" {
			t.Errorf("status = %q, want 'recipient not found'", ack.Status)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("sender should have received negative ack")
	}
}

func TestDeliveryAckOnQueued(t *testing.T) {
	hub := testHub()
	defer hub.Stop()

	// Create and register sender
	sender := testClient(hub, "ack-sender", "did:wba:example.com:agent:alice")
	hub.Register(sender)

	// Create message requesting ack to offline recipient
	msg := &messaging.Message{
		ID:         uuid.Must(uuid.NewV7()),
		From:       "did:wba:example.com:agent:alice",
		To:         "did:wba:example.com:agent:offline",
		Method:     "test.method",
		RequestAck: true,
	}

	data := []byte(`{"test": "data"}`)

	// Route should succeed (queued)
	err := hub.Route(msg, data)
	if err != nil {
		t.Fatalf("Route should succeed with queue: %v", err)
	}

	// Sender should receive queued ack notification
	select {
	case ackData := <-sender.SendCh:
		var notification protocol.JSONRPCRequest
		if err := json.Unmarshal(ackData, &notification); err != nil {
			t.Fatalf("failed to unmarshal ack: %v", err)
		}

		if notification.Method != "relay.ack" {
			t.Errorf("method = %q, want 'relay.ack'", notification.Method)
		}

		var ack struct {
			MessageID string `json:"message_id"`
			Delivered bool   `json:"delivered"`
			Status    string `json:"status"`
		}
		if err := json.Unmarshal(notification.Params, &ack); err != nil {
			t.Fatalf("failed to unmarshal ack params: %v", err)
		}

		if ack.MessageID != msg.ID.String() {
			t.Errorf("message_id = %q, want %q", ack.MessageID, msg.ID.String())
		}
		if !ack.Delivered {
			t.Error("delivered should be true (queued)")
		}
		if ack.Status != "queued" {
			t.Errorf("status = %q, want 'queued'", ack.Status)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("sender should have received queued ack")
	}
}

func TestNoAckWhenNotRequested(t *testing.T) {
	hub := testHub()
	defer hub.Stop()

	// Create and register sender
	sender := testClient(hub, "no-ack-sender", "did:wba:example.com:agent:alice")
	hub.Register(sender)

	// Create and register recipient
	recipient := testClient(hub, "no-ack-recipient", "did:wba:example.com:agent:bob")
	hub.Register(recipient)

	// Create message WITHOUT requesting ack
	msg := &messaging.Message{
		ID:         uuid.Must(uuid.NewV7()),
		From:       "did:wba:example.com:agent:alice",
		To:         "did:wba:example.com:agent:bob",
		Method:     "test.method",
		RequestAck: false, // No ack requested
	}

	data := []byte(`{"test": "data"}`)

	// Route the message
	err := hub.Route(msg, data)
	if err != nil {
		t.Fatalf("Route failed: %v", err)
	}

	// Recipient should receive the message
	select {
	case <-recipient.SendCh:
		// OK
	case <-time.After(100 * time.Millisecond):
		t.Error("recipient should have received message")
	}

	// Sender should NOT receive any ack
	select {
	case <-sender.SendCh:
		t.Error("sender should not receive ack when not requested")
	case <-time.After(50 * time.Millisecond):
		// OK - no ack received
	}
}
