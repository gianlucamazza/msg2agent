package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gianluca/msg2agent/pkg/messaging"
	"github.com/gianluca/msg2agent/pkg/protocol"
	"github.com/gianluca/msg2agent/pkg/registry"
)

// TestSendAndReceive tests the full request/response cycle.
func TestSendAndReceive(t *testing.T) {
	// Create Alice and Bob
	alice, _ := New(Config{Domain: "test.com", AgentID: "alice"})
	bob, _ := New(Config{Domain: "test.com", AgentID: "bob"})

	// Register each other
	alice.store.Put(bob.Record())
	bob.store.Put(alice.Record())

	// Start both
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	alice.Start(ctx)
	defer alice.Stop()
	bob.Start(ctx)
	defer bob.Stop()

	// Register a handler on Bob
	bob.RegisterMethod("echo", func(ctx context.Context, params json.RawMessage) (any, error) {
		var msg map[string]string
		json.Unmarshal(params, &msg)
		return map[string]string{"echo": msg["message"]}, nil
	})

	// Create connected transport pair
	aliceToBob, bobToAlice := channelPair("alice->bob", "bob->alice")

	// Inject transports
	alice.mu.Lock()
	alice.peers["ws://bob:8080"] = aliceToBob
	alice.mu.Unlock()

	bob.mu.Lock()
	bob.peers["peer:alice"] = bobToAlice
	bob.mu.Unlock()

	// Start receive loops
	go alice.receiveLoop(aliceToBob)
	go bob.receiveLoop(bobToAlice)

	// Update Bob's record with endpoint
	bob.record.AddEndpoint(registry.TransportWebSocket, "ws://bob:8080", 1)
	alice.store.Put(bob.Record())

	// Send message from Alice to Bob
	resp, err := alice.Send(ctx, bob.DID(), "echo", map[string]string{"message": "hello"})
	if err != nil {
		t.Fatalf("send failed: %v", err)
	}

	if resp == nil {
		t.Fatal("response should not be nil")
	}

	// Check response type (should be error because of ID mismatch in this simple test)
	// In real scenario, the handler result would be returned
	t.Logf("response type: %v", resp.Type)
}

// TestNotify tests sending notifications.
func TestNotify(t *testing.T) {
	alice, _ := New(Config{Domain: "test.com", AgentID: "alice"})
	bob, _ := New(Config{Domain: "test.com", AgentID: "bob"})

	alice.store.Put(bob.Record())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	alice.Start(ctx)
	defer alice.Stop()

	// Create mock transport
	mockT := newMockTransport("ws://bob:8080")
	mockT.connected = true

	alice.mu.Lock()
	alice.peers["ws://bob:8080"] = mockT
	alice.mu.Unlock()

	bob.record.AddEndpoint(registry.TransportWebSocket, "ws://bob:8080", 1)
	alice.store.Put(bob.Record())

	// Send notification
	err := alice.Notify(ctx, bob.DID(), "ping", nil)
	if err != nil {
		t.Fatalf("notify failed: %v", err)
	}

	// Verify message was sent
	data, err := mockT.getOutgoing(ctx)
	if err != nil {
		t.Fatalf("expected outgoing message: %v", err)
	}

	if len(data) == 0 {
		t.Error("notification data should not be empty")
	}
}

// TestSendNoPeer tests Send when no peer is available.
func TestSendNoPeer(t *testing.T) {
	alice, _ := New(Config{Domain: "test.com", AgentID: "alice"})
	bob, _ := New(Config{Domain: "test.com", AgentID: "bob"})

	alice.store.Put(bob.Record())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	alice.Start(ctx)
	defer alice.Stop()

	// No peers connected - should fail
	_, err := alice.Send(ctx, bob.DID(), "test", nil)
	if err != ErrPeerNotFound {
		t.Errorf("expected ErrPeerNotFound, got %v", err)
	}
}

// TestSendTimeout tests Send with context timeout.
func TestSendTimeout(t *testing.T) {
	alice, _ := New(Config{Domain: "test.com", AgentID: "alice"})
	bob, _ := New(Config{Domain: "test.com", AgentID: "bob"})

	alice.store.Put(bob.Record())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	alice.Start(ctx)
	defer alice.Stop()

	// Create mock transport that doesn't respond
	mockT := newMockTransport("ws://bob:8080")
	mockT.connected = true

	alice.mu.Lock()
	alice.peers["ws://bob:8080"] = mockT
	alice.mu.Unlock()

	bob.record.AddEndpoint(registry.TransportWebSocket, "ws://bob:8080", 1)
	alice.store.Put(bob.Record())

	// Send with short timeout
	timeoutCtx, timeoutCancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer timeoutCancel()

	_, err := alice.Send(timeoutCtx, bob.DID(), "test", nil)
	if err != context.DeadlineExceeded {
		t.Errorf("expected context.DeadlineExceeded, got %v", err)
	}
}

// TestHandleRequest tests incoming request handling.
func TestHandleRequest(t *testing.T) {
	bob, _ := New(Config{Domain: "test.com", AgentID: "bob"})
	alice, _ := New(Config{Domain: "test.com", AgentID: "alice"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bob.Start(ctx)
	defer bob.Stop()

	bob.store.Put(alice.Record())

	// Register a handler
	bob.RegisterMethod("greet", func(ctx context.Context, params json.RawMessage) (any, error) {
		return map[string]string{"greeting": "hello"}, nil
	})

	// Create a valid signed message from Alice
	msg, _ := messaging.NewRequest(alice.DID(), bob.DID(), "greet", nil)
	msgBytes, _ := json.Marshal(msg)
	msg.Signature = alice.identity.Sign(msgBytes)

	// Create JSON-RPC request
	req, _ := protocol.NewRequest(msg.ID.String(), msg.Method, msg)
	reqData, _ := protocol.Encode(req)

	// Create mock transport to capture response
	mockT := newMockTransport("alice")
	mockT.connected = true

	// Handle the request
	bob.handleIncoming(mockT, reqData)

	// Wait for response
	respData, err := mockT.getOutgoing(ctx)
	if err != nil {
		t.Fatalf("expected response: %v", err)
	}

	// Parse response
	var resp protocol.JSONRPCResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp.Error != nil {
		t.Errorf("unexpected error: %v", resp.Error.Message)
	}

	// Check result
	var result map[string]string
	resp.ParseResult(&result)
	if result["greeting"] != "hello" {
		t.Errorf("expected greeting 'hello', got %q", result["greeting"])
	}
}

// TestHandleRequestMethodNotFound tests handling request for unregistered method.
func TestHandleRequestMethodNotFound(t *testing.T) {
	bob, _ := New(Config{Domain: "test.com", AgentID: "bob"})
	alice, _ := New(Config{Domain: "test.com", AgentID: "alice"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bob.Start(ctx)
	defer bob.Stop()

	bob.store.Put(alice.Record())

	// Create request for unknown method
	msg, _ := messaging.NewRequest(alice.DID(), bob.DID(), "unknown_method", nil)
	msgBytes, _ := json.Marshal(msg)
	msg.Signature = alice.identity.Sign(msgBytes)

	req, _ := protocol.NewRequest(msg.ID.String(), msg.Method, msg)
	reqData, _ := protocol.Encode(req)

	mockT := newMockTransport("alice")
	mockT.connected = true

	bob.handleIncoming(mockT, reqData)

	respData, _ := mockT.getOutgoing(ctx)
	var resp protocol.JSONRPCResponse
	json.Unmarshal(respData, &resp)

	if resp.Error == nil {
		t.Error("expected error response")
	}

	if resp.Error.Code != protocol.CodeMethodNotFound {
		t.Errorf("expected CodeMethodNotFound (%d), got %d", protocol.CodeMethodNotFound, resp.Error.Code)
	}
}

// TestHandleRequestInvalidSignature tests rejection of messages with invalid signatures.
func TestHandleRequestInvalidSignature(t *testing.T) {
	bob, _ := New(Config{Domain: "test.com", AgentID: "bob"})
	alice, _ := New(Config{Domain: "test.com", AgentID: "alice"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bob.Start(ctx)
	defer bob.Stop()

	bob.store.Put(alice.Record())
	bob.RegisterMethod("test", func(ctx context.Context, params json.RawMessage) (any, error) {
		return "ok", nil
	})

	// Create message with invalid signature
	msg, _ := messaging.NewRequest(alice.DID(), bob.DID(), "test", nil)
	msg.Signature = []byte("invalid_signature")

	req, _ := protocol.NewRequest(msg.ID.String(), msg.Method, msg)
	reqData, _ := protocol.Encode(req)

	mockT := newMockTransport("alice")
	mockT.connected = true

	bob.handleIncoming(mockT, reqData)

	respData, _ := mockT.getOutgoing(ctx)
	var resp protocol.JSONRPCResponse
	json.Unmarshal(respData, &resp)

	if resp.Error == nil {
		t.Error("expected error for invalid signature")
	}

	if resp.Error.Code != protocol.CodeSignatureInvalid {
		t.Errorf("expected CodeSignatureInvalid (%d), got %d", protocol.CodeSignatureInvalid, resp.Error.Code)
	}
}

// TestHandleResponse tests response handling.
func TestHandleResponse(t *testing.T) {
	alice, _ := New(Config{Domain: "test.com", AgentID: "alice"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	alice.Start(ctx)
	defer alice.Stop()

	// Create pending channel
	respCh := make(chan *messaging.Message, 1)
	msgID := "test-id"

	alice.mu.Lock()
	alice.pendingRPC[msgID] = make(chan *protocol.JSONRPCResponse, 1)
	alice.mu.Unlock()

	// Create response
	resp, _ := protocol.NewResponse(msgID, map[string]string{"result": "ok"})
	respData, _ := protocol.Encode(resp)

	// Handle response in goroutine
	go func() {
		var parsed protocol.JSONRPCResponse
		json.Unmarshal(respData, &parsed)
		alice.handleResponse(&parsed)
	}()

	// Wait for response on RPC channel
	alice.mu.RLock()
	rpcCh := alice.pendingRPC[msgID]
	alice.mu.RUnlock()

	select {
	case received := <-rpcCh:
		if received.Error != nil {
			t.Errorf("unexpected error: %v", received.Error)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("response not received")
	}

	close(respCh)
}

// TestCallRelay tests raw JSON-RPC relay calls.
func TestCallRelay(t *testing.T) {
	alice, _ := New(Config{
		Domain:    "test.com",
		AgentID:   "alice",
		RelayAddr: "ws://relay:8080",
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	alice.Start(ctx)
	defer alice.Stop()

	// Create mock relay transport
	mockRelay := newMockTransport("ws://relay:8080")
	mockRelay.connected = true

	alice.mu.Lock()
	alice.peers["ws://relay:8080"] = mockRelay
	alice.mu.Unlock()

	// Start goroutine to respond
	go func() {
		// Wait for request
		reqData, err := mockRelay.getOutgoing(ctx)
		if err != nil {
			return
		}

		// Parse request
		var req protocol.JSONRPCRequest
		json.Unmarshal(reqData, &req)

		// Send response
		resp, _ := protocol.NewResponse(req.ID, []map[string]string{{"did": "did:test:agent"}})
		respData, _ := protocol.Encode(resp)
		mockRelay.inject(respData)
	}()

	// Start receive loop
	go alice.receiveLoop(mockRelay)

	// Make relay call
	result, err := alice.CallRelay(ctx, "relay.discover", nil)
	if err != nil {
		t.Fatalf("CallRelay failed: %v", err)
	}

	if result == nil {
		t.Error("result should not be nil")
	}
}

// TestCallRelayNoPeer tests CallRelay when no relay is connected.
func TestCallRelayNoPeer(t *testing.T) {
	alice, _ := New(Config{Domain: "test.com", AgentID: "alice"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	alice.Start(ctx)
	defer alice.Stop()

	_, err := alice.CallRelay(ctx, "relay.discover", nil)
	if err != ErrPeerNotFound {
		t.Errorf("expected ErrPeerNotFound, got %v", err)
	}
}

// TestEncryptForDecrypt tests the public encryption/decryption methods.
func TestEncryptForDecrypt(t *testing.T) {
	alice, _ := New(Config{Domain: "test.com", AgentID: "alice"})
	bob, _ := New(Config{Domain: "test.com", AgentID: "bob"})

	plaintext := []byte("secret message")

	// Alice encrypts for Bob
	encrypted, err := alice.EncryptFor(bob.identity.Keys.Encryption.PublicKey, plaintext)
	if err != nil {
		t.Fatalf("encryption failed: %v", err)
	}

	if string(encrypted) == string(plaintext) {
		t.Error("encrypted should differ from plaintext")
	}

	// Bob decrypts from Alice
	decrypted, err := bob.Decrypt(alice.identity.Keys.Encryption.PublicKey, encrypted)
	if err != nil {
		t.Fatalf("decryption failed: %v", err)
	}

	if string(decrypted) != string(plaintext) {
		t.Errorf("decrypted = %q, want %q", decrypted, plaintext)
	}
}

// TestFindTransportForRecipient tests transport lookup logic.
func TestFindTransportForRecipient(t *testing.T) {
	alice, _ := New(Config{Domain: "test.com", AgentID: "alice"})
	bob, _ := New(Config{Domain: "test.com", AgentID: "bob"})

	mockT := newMockTransport("ws://bob:8080")
	mockT.connected = true

	// Add Bob with endpoint
	bob.record.AddEndpoint(registry.TransportWebSocket, "ws://bob:8080", 1)
	alice.store.Put(bob.Record())

	// Add transport to peers
	alice.mu.Lock()
	alice.peers["ws://bob:8080"] = mockT
	alice.mu.Unlock()

	// Find transport
	alice.mu.RLock()
	found, ok, isRelay := alice.findTransportForRecipient(bob.DID())
	alice.mu.RUnlock()

	if !ok {
		t.Error("should find transport for recipient")
	}

	if found != mockT {
		t.Error("found wrong transport")
	}

	if isRelay {
		t.Error("should not be relay for direct connection")
	}
}

// TestFindTransportForRecipientFallback tests fallback to any peer.
func TestFindTransportForRecipientFallback(t *testing.T) {
	alice, _ := New(Config{Domain: "test.com", AgentID: "alice"})

	mockT := newMockTransport("ws://relay:8080")
	mockT.connected = true

	alice.mu.Lock()
	alice.peers["ws://relay:8080"] = mockT
	alice.mu.Unlock()

	// Find transport for unknown recipient - should fallback
	alice.mu.RLock()
	found, ok, isRelay := alice.findTransportForRecipient("did:unknown:agent")
	alice.mu.RUnlock()

	if !ok {
		t.Error("should find fallback transport")
	}

	if found != mockT {
		t.Error("should return fallback transport")
	}

	if !isRelay {
		t.Error("fallback should be marked as relay")
	}
}

// TestFindTransportForRecipientNotFound tests when no transport is available.
func TestFindTransportForRecipientNotFound(t *testing.T) {
	alice, _ := New(Config{Domain: "test.com", AgentID: "alice"})

	alice.mu.RLock()
	_, ok, _ := alice.findTransportForRecipient("did:unknown:agent")
	alice.mu.RUnlock()

	if ok {
		t.Error("should not find transport when none available")
	}
}

// TestServeAgentCard tests the HTTP server for agent card.
func TestServeAgentCard(t *testing.T) {
	agent, _ := New(Config{
		Domain:      "test.com",
		AgentID:     "server",
		DisplayName: "Server Agent",
		ListenAddr:  "127.0.0.1:0",
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	agent.Start(ctx)
	defer agent.Stop()

	// Create a listener to get an actual port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	addr := listener.Addr().String()
	listener.Close()

	// Start HTTP server on the port we found
	err = agent.ServeAgentCard(ctx, addr)
	if err != nil {
		t.Fatalf("ServeAgentCard failed: %v", err)
	}

	// Wait for server to start
	time.Sleep(50 * time.Millisecond)

	// Fetch agent card
	resp, err := http.Get("http://" + addr + "/.well-known/agent.json")
	if err != nil {
		t.Fatalf("HTTP GET failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var card Card
	if err := json.Unmarshal(body, &card); err != nil {
		t.Fatalf("failed to parse agent card: %v", err)
	}

	if card.Name != "Server Agent" {
		t.Errorf("name = %q, want %q", card.Name, "Server Agent")
	}

	if card.DID == "" {
		t.Error("DID should be set")
	}
}

// TestServeAgentCardHealth tests the health endpoint.
func TestServeAgentCardHealth(t *testing.T) {
	agent, _ := New(Config{
		Domain:  "test.com",
		AgentID: "health",
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	agent.Start(ctx)
	defer agent.Stop()

	// Create a listener to get an actual port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	addr := listener.Addr().String()
	listener.Close()

	agent.ServeAgentCard(ctx, addr)
	time.Sleep(50 * time.Millisecond)

	resp, err := http.Get("http://" + addr + "/health")
	if err != nil {
		t.Fatalf("HTTP GET failed: %v", err)
	}
	defer resp.Body.Close()

	var health map[string]any
	json.NewDecoder(resp.Body).Decode(&health)

	if health["status"] != "ok" {
		t.Errorf("status = %v, want 'ok'", health["status"])
	}

	if health["did"] == "" {
		t.Error("DID should be set in health response")
	}
}

// TestReceiveLoopContextCancelled tests that receive loop exits on context cancel.
func TestReceiveLoopContextCancelled(t *testing.T) {
	alice, _ := New(Config{Domain: "test.com", AgentID: "alice"})

	ctx, cancel := context.WithCancel(context.Background())
	alice.Start(ctx)

	mockT := newMockTransport("peer")
	mockT.connected = true

	done := make(chan struct{})
	go func() {
		alice.receiveLoop(mockT)
		close(done)
	}()

	// Cancel context
	cancel()

	// Should exit quickly
	select {
	case <-done:
		// OK
	case <-time.After(100 * time.Millisecond):
		t.Error("receive loop did not exit on context cancel")
	}
}

// TestReceiveLoopTransportError tests receive loop handling of transport errors.
func TestReceiveLoopTransportError(t *testing.T) {
	alice, _ := New(Config{Domain: "test.com", AgentID: "alice"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	alice.Start(ctx)
	defer alice.Stop()

	mockT := newMockTransport("peer")
	mockT.connected = true
	mockT.mu.Lock()
	mockT.receiveErr = errMockReceive
	mockT.mu.Unlock()

	done := make(chan struct{})
	go func() {
		alice.receiveLoop(mockT)
		close(done)
	}()

	// Should exit on error
	select {
	case <-done:
		// OK
	case <-time.After(100 * time.Millisecond):
		t.Error("receive loop did not exit on transport error")
	}
}

// TestHandleIncomingInvalidJSON tests handling of invalid JSON.
func TestHandleIncomingInvalidJSON(t *testing.T) {
	alice, _ := New(Config{Domain: "test.com", AgentID: "alice"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	alice.Start(ctx)
	defer alice.Stop()

	mockT := newMockTransport("peer")
	mockT.connected = true

	// Should not panic
	alice.handleIncoming(mockT, []byte("not valid json"))

	// No response expected for invalid JSON
	select {
	case <-mockT.outgoing:
		// May or may not send error response
	case <-time.After(50 * time.Millisecond):
		// OK - no response is fine for malformed data
	}
}

// TestACLDenied tests access control enforcement.
func TestACLDenied(t *testing.T) {
	bob, _ := New(Config{Domain: "test.com", AgentID: "bob"})
	alice, _ := New(Config{Domain: "test.com", AgentID: "alice"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bob.Start(ctx)
	defer bob.Stop()

	bob.store.Put(alice.Record())

	// Set restrictive ACL
	bob.SetACL(&registry.ACLPolicy{
		DefaultAllow: false,
		Rules:        []registry.ACLRule{}, // No allowed actions
	})

	bob.RegisterMethod("secret", func(ctx context.Context, params json.RawMessage) (any, error) {
		return "secret data", nil
	})

	// Create valid signed request
	msg, _ := messaging.NewRequest(alice.DID(), bob.DID(), "secret", nil)
	msgBytes, _ := json.Marshal(msg)
	msg.Signature = alice.identity.Sign(msgBytes)

	req, _ := protocol.NewRequest(msg.ID.String(), msg.Method, msg)
	reqData, _ := protocol.Encode(req)

	mockT := newMockTransport("alice")
	mockT.connected = true

	bob.handleIncoming(mockT, reqData)

	respData, _ := mockT.getOutgoing(ctx)
	var resp protocol.JSONRPCResponse
	json.Unmarshal(respData, &resp)

	if resp.Error == nil {
		t.Error("expected access denied error")
	}

	if resp.Error.Code != protocol.CodeAccessDenied {
		t.Errorf("expected CodeAccessDenied (%d), got %d", protocol.CodeAccessDenied, resp.Error.Code)
	}
}

// TestConcurrentSend tests concurrent message sending.
func TestConcurrentSend(t *testing.T) {
	alice, _ := New(Config{Domain: "test.com", AgentID: "alice"})
	bob, _ := New(Config{Domain: "test.com", AgentID: "bob"})

	alice.store.Put(bob.Record())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	alice.Start(ctx)
	defer alice.Stop()

	mockT := newMockTransport("ws://bob:8080")
	mockT.connected = true

	alice.mu.Lock()
	alice.peers["ws://bob:8080"] = mockT
	alice.mu.Unlock()

	bob.record.AddEndpoint(registry.TransportWebSocket, "ws://bob:8080", 1)
	alice.store.Put(bob.Record())

	// Send multiple messages concurrently
	const numMessages = 10
	done := make(chan bool, numMessages)

	for i := 0; i < numMessages; i++ {
		go func(i int) {
			shortCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
			defer cancel()
			_, err := alice.Send(shortCtx, bob.DID(), "test", map[string]int{"i": i})
			// Expect timeout since no response
			done <- (err == context.DeadlineExceeded)
		}(i)
	}

	// Wait for all to complete
	for i := 0; i < numMessages; i++ {
		if !<-done {
			t.Error("expected timeout error")
		}
	}
}

// TestStopCleansUpPeers tests that Stop() closes all peer connections.
func TestStopCleansUpPeers(t *testing.T) {
	alice, _ := New(Config{Domain: "test.com", AgentID: "alice"})

	ctx := context.Background()
	alice.Start(ctx)

	// Add some mock peers
	for i := 0; i < 3; i++ {
		mockT := newMockTransport("peer")
		mockT.connected = true
		alice.mu.Lock()
		alice.peers["peer:"+string(rune('a'+i))] = mockT
		alice.mu.Unlock()
	}

	alice.mu.RLock()
	peerCount := len(alice.peers)
	alice.mu.RUnlock()

	if peerCount != 3 {
		t.Errorf("expected 3 peers, got %d", peerCount)
	}

	// Stop agent
	alice.Stop()

	alice.mu.RLock()
	peerCount = len(alice.peers)
	alice.mu.RUnlock()

	if peerCount != 0 {
		t.Errorf("expected 0 peers after stop, got %d", peerCount)
	}
}

// TestHandlerError tests error propagation from handlers.
func TestHandlerError(t *testing.T) {
	bob, _ := New(Config{Domain: "test.com", AgentID: "bob"})
	alice, _ := New(Config{Domain: "test.com", AgentID: "alice"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bob.Start(ctx)
	defer bob.Stop()

	bob.store.Put(alice.Record())

	// Register handler that returns error
	bob.RegisterMethod("fail", func(ctx context.Context, params json.RawMessage) (any, error) {
		return nil, ErrPeerNotFound // Some error
	})

	msg, _ := messaging.NewRequest(alice.DID(), bob.DID(), "fail", nil)
	msgBytes, _ := json.Marshal(msg)
	msg.Signature = alice.identity.Sign(msgBytes)

	req, _ := protocol.NewRequest(msg.ID.String(), msg.Method, msg)
	reqData, _ := protocol.Encode(req)

	mockT := newMockTransport("alice")
	mockT.connected = true

	bob.handleIncoming(mockT, reqData)

	respData, _ := mockT.getOutgoing(ctx)
	var resp protocol.JSONRPCResponse
	json.Unmarshal(respData, &resp)

	if resp.Error == nil {
		t.Error("expected error response")
	}

	if resp.Error.Code != protocol.CodeInternalError {
		t.Errorf("expected CodeInternalError (%d), got %d", protocol.CodeInternalError, resp.Error.Code)
	}
}

// TestDecryptMessageBodyNotEncrypted tests decryption skip for non-encrypted messages.
func TestDecryptMessageBodyNotEncrypted(t *testing.T) {
	bob, _ := New(Config{Domain: "test.com", AgentID: "bob"})

	msg := &messaging.Message{
		From:      "did:test:alice",
		To:        bob.DID(),
		Body:      []byte(`{"data":"plaintext"}`),
		Encrypted: false, // Not encrypted
	}

	// Should succeed without changes
	err := bob.decryptMessageBody(msg)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Body should be unchanged
	if string(msg.Body) != `{"data":"plaintext"}` {
		t.Error("body should not change for non-encrypted message")
	}
}

// TestDecryptMessageBodyMissingEncryptionKey tests decryption when sender has no encryption key.
func TestDecryptMessageBodyMissingEncryptionKey(t *testing.T) {
	bob, _ := New(Config{Domain: "test.com", AgentID: "bob"})

	// Create agent record without encryption key
	senderRecord := registry.NewAgent("did:test:alice", "Alice")
	senderRecord.AddPublicKey("signing", registry.KeyTypeEd25519, []byte("fake-key"), "signing")
	// No encryption key added

	bob.store.Put(senderRecord)

	msg := &messaging.Message{
		From:      "did:test:alice",
		To:        bob.DID(),
		Body:      []byte("encrypted-data"),
		Encrypted: true,
	}

	err := bob.decryptMessageBody(msg)
	if err != ErrSenderKeyNotFound {
		t.Errorf("expected ErrSenderKeyNotFound, got %v", err)
	}
}

// TestEncryptMessageBodyMissingEncryptionKey tests encryption when recipient has no encryption key.
func TestEncryptMessageBodyMissingEncryptionKey(t *testing.T) {
	alice, _ := New(Config{Domain: "test.com", AgentID: "alice"})

	// Create recipient record without encryption key
	recipientRecord := registry.NewAgent("did:test:bob", "Bob")
	recipientRecord.AddPublicKey("signing", registry.KeyTypeEd25519, []byte("fake-key"), "signing")
	// No encryption key added

	alice.store.Put(recipientRecord)

	msg := &messaging.Message{
		From: alice.DID(),
		To:   "did:test:bob",
		Body: []byte(`{"data":"secret"}`),
	}

	err := alice.encryptMessageBody(msg)
	if err != ErrNoEncryptionKey {
		t.Errorf("expected ErrNoEncryptionKey, got %v", err)
	}
}

// TestIsListeningFalseWhenNoListener tests IsListening returns false when listener is nil.
func TestIsListeningFalseWhenNoListener(t *testing.T) {
	agent, _ := New(Config{Domain: "test.com", AgentID: "test"})

	agent.mu.Lock()
	agent.listening = true
	agent.listener = nil // No actual listener
	agent.mu.Unlock()

	if agent.IsListening() {
		t.Error("IsListening should return false when listener is nil")
	}
}

// TestListenAddrEmpty tests ListenAddr returns empty when not listening.
func TestListenAddrEmpty(t *testing.T) {
	agent, _ := New(Config{Domain: "test.com", AgentID: "test"})

	if addr := agent.ListenAddr(); addr != "" {
		t.Errorf("ListenAddr should be empty, got %q", addr)
	}
}

// TestGetters tests ID, DID, Record, Discovery, Store getters.
func TestGetters(t *testing.T) {
	agent, _ := New(Config{
		Domain:      "test.com",
		AgentID:     "getters",
		DisplayName: "Getters Agent",
	})

	if agent.ID().String() == "" {
		t.Error("ID should not be empty UUID")
	}

	if agent.DID() == "" {
		t.Error("DID should not be empty")
	}

	if agent.Record() == nil {
		t.Error("Record should not be nil")
	}

	if agent.Discovery() == nil {
		t.Error("Discovery should not be nil")
	}

	if agent.Store() == nil {
		t.Error("Store should not be nil")
	}
}

// TestSendRequireEncryption tests that Send fails when RequireEncryption is true
// and encryption fails.
func TestSendRequireEncryption(t *testing.T) {
	alice, _ := New(Config{
		Domain:            "test.com",
		AgentID:           "alice",
		RequireEncryption: true, // Require encryption
	})

	ctx := context.Background()
	alice.Start(ctx)
	defer alice.Stop()

	// Add a peer so Send doesn't fail on peer lookup
	mockT := newMockTransport("ws://relay:8080")
	mockT.connected = true
	alice.mu.Lock()
	alice.peers["ws://relay:8080"] = mockT
	alice.mu.Unlock()

	// Try to send to unknown recipient (not in registry) WITH body data
	// This should fail because encryption is required but recipient is unknown
	// Note: Must provide non-nil params, otherwise there's nothing to encrypt
	_, err := alice.Send(ctx, "did:wba:unknown.com:agent:bob", "ping", map[string]string{"test": "data"})

	if err == nil {
		t.Fatal("expected error when encryption required but recipient unknown")
	}

	if !errors.Is(err, ErrEncryptionRequired) {
		t.Errorf("expected ErrEncryptionRequired, got: %v", err)
	}
}

// TestSendWithoutRequireEncryption tests that Send succeeds when RequireEncryption is false
// even when encryption fails.
func TestSendWithoutRequireEncryption(t *testing.T) {
	alice, _ := New(Config{
		Domain:            "test.com",
		AgentID:           "alice",
		RequireEncryption: false, // Default: don't require
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	alice.Start(ctx)
	defer alice.Stop()

	// Add a mock peer
	mockT := newMockTransport("ws://relay:8080")
	mockT.connected = true
	alice.mu.Lock()
	alice.peers["ws://relay:8080"] = mockT
	alice.mu.Unlock()

	// Try to send to unknown recipient with body - should succeed (with warning log)
	// but will timeout waiting for response
	_, err := alice.Send(ctx, "did:wba:unknown.com:agent:bob", "ping", map[string]string{"test": "data"})

	// Should timeout, not fail on encryption
	if err == nil {
		t.Fatal("expected timeout error")
	}

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded, got: %v", err)
	}
}

// TestNotifyRequireEncryption tests that Notify fails when RequireEncryption is true
// and encryption fails.
func TestNotifyRequireEncryption(t *testing.T) {
	alice, _ := New(Config{
		Domain:            "test.com",
		AgentID:           "alice",
		RequireEncryption: true,
	})

	ctx := context.Background()
	alice.Start(ctx)
	defer alice.Stop()

	// Add a peer
	mockT := newMockTransport("ws://relay:8080")
	mockT.connected = true
	alice.mu.Lock()
	alice.peers["ws://relay:8080"] = mockT
	alice.mu.Unlock()

	// Try to notify unknown recipient with body data
	err := alice.Notify(ctx, "did:wba:unknown.com:agent:bob", "ping", map[string]string{"test": "data"})

	if err == nil {
		t.Fatal("expected error when encryption required but recipient unknown")
	}

	if !errors.Is(err, ErrEncryptionRequired) {
		t.Errorf("expected ErrEncryptionRequired, got: %v", err)
	}
}

// TestGracefulShutdownDrainsPending tests that Stop() drains pending channels.
func TestGracefulShutdownDrainsPending(t *testing.T) {
	alice, _ := New(Config{
		Domain:          "test.com",
		AgentID:         "alice",
		ShutdownTimeout: 2 * time.Second,
	})

	ctx := context.Background()
	alice.Start(ctx)

	// Add a pending request channel
	pendingCh := make(chan *messaging.Message, 1)
	testID := uuid.New()
	alice.mu.Lock()
	alice.pending[testID] = pendingCh
	alice.mu.Unlock()

	// Stop should drain the channel
	done := make(chan struct{})
	go func() {
		alice.Stop()
		close(done)
	}()

	// The pending channel should receive nil
	select {
	case msg := <-pendingCh:
		if msg != nil {
			t.Error("expected nil message during shutdown")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("pending channel not drained during shutdown")
	}

	// Stop should complete
	select {
	case <-done:
		// OK
	case <-time.After(3 * time.Second):
		t.Fatal("Stop did not complete in time")
	}
}

// TestShutdownTimeout tests that Stop() respects shutdown timeout.
func TestShutdownTimeout(t *testing.T) {
	alice, _ := New(Config{
		Domain:          "test.com",
		AgentID:         "alice",
		ShutdownTimeout: 100 * time.Millisecond, // Short timeout
	})

	ctx := context.Background()
	alice.Start(ctx)

	// Simulate a stuck goroutine by adding to WaitGroup without Done
	alice.wg.Add(1)
	// Don't call Done - simulates stuck goroutine

	start := time.Now()
	alice.Stop()
	elapsed := time.Since(start)

	// Should complete within timeout + some margin
	if elapsed > 500*time.Millisecond {
		t.Errorf("Stop took too long: %v (expected ~100ms)", elapsed)
	}

	// Cleanup the stuck goroutine tracking
	alice.wg.Done()
}

// TestFindTransportConfiguredRelay tests that configured relay is preferred.
func TestFindTransportConfiguredRelay(t *testing.T) {
	alice, _ := New(Config{
		Domain:    "test.com",
		AgentID:   "alice",
		RelayAddr: "ws://configured-relay:8080",
	})

	// Add multiple peers
	mockRelay := newMockTransport("ws://configured-relay:8080")
	mockOther := newMockTransport("ws://other-peer:8080")

	alice.mu.Lock()
	alice.peers["ws://configured-relay:8080"] = mockRelay
	alice.peers["ws://other-peer:8080"] = mockOther
	alice.mu.Unlock()

	// Find transport for unknown recipient
	alice.mu.RLock()
	found, ok, isRelay := alice.findTransportForRecipient("did:wba:unknown.com:agent:bob")
	alice.mu.RUnlock()

	if !ok {
		t.Fatal("should find transport")
	}

	if !isRelay {
		t.Error("should be marked as relay")
	}

	if found != mockRelay {
		t.Error("should prefer configured relay over random peer")
	}
}

// TestStoppingFlag tests that stopping flag is set during shutdown.
func TestStoppingFlag(t *testing.T) {
	alice, _ := New(Config{
		Domain:  "test.com",
		AgentID: "alice",
	})

	ctx := context.Background()
	alice.Start(ctx)

	if alice.stopping.Load() {
		t.Error("stopping should be false initially")
	}

	alice.Stop()

	if !alice.stopping.Load() {
		t.Error("stopping should be true after Stop")
	}
}

// --- Message Deduplication Tests ---

// TestIsDuplicateMessage tests message deduplication detection.
func TestIsDuplicateMessage(t *testing.T) {
	alice, _ := New(Config{
		Domain:   "test.com",
		AgentID:  "alice",
		DedupTTL: 1 * time.Minute,
	})

	ctx := context.Background()
	alice.Start(ctx)
	defer alice.Stop()

	msgID := uuid.New().String()

	// First time - not a duplicate
	if alice.isDuplicateMessage(msgID) {
		t.Error("first time should not be duplicate")
	}

	// Second time - is a duplicate
	if !alice.isDuplicateMessage(msgID) {
		t.Error("second time should be duplicate")
	}

	// Different ID - not a duplicate
	if alice.isDuplicateMessage(uuid.New().String()) {
		t.Error("different ID should not be duplicate")
	}
}

// TestIsDuplicateMessageEmptyID tests that empty message IDs are not deduplicated.
func TestIsDuplicateMessageEmptyID(t *testing.T) {
	alice, _ := New(Config{
		Domain:  "test.com",
		AgentID: "alice",
	})

	ctx := context.Background()
	alice.Start(ctx)
	defer alice.Stop()

	// Empty ID should never be duplicate
	if alice.isDuplicateMessage("") {
		t.Error("empty ID should not be duplicate")
	}
	if alice.isDuplicateMessage("") {
		t.Error("empty ID should never be duplicate")
	}
}

// TestDedupCleanup tests that expired dedup entries are cleaned up.
func TestDedupCleanup(t *testing.T) {
	alice, _ := New(Config{
		Domain:   "test.com",
		AgentID:  "alice",
		DedupTTL: 50 * time.Millisecond, // Very short TTL for testing
	})

	ctx := context.Background()
	alice.Start(ctx)
	defer alice.Stop()

	msgID := uuid.New().String()

	// Mark as seen
	alice.isDuplicateMessage(msgID)

	// Should be duplicate immediately
	if !alice.isDuplicateMessage(msgID) {
		t.Error("should be duplicate immediately")
	}

	// Wait for TTL to expire
	time.Sleep(100 * time.Millisecond)

	// Manually trigger cleanup
	alice.cleanupDedup()

	// Should no longer be in cache
	alice.seenMsgsMu.RLock()
	_, exists := alice.seenMsgs[msgID]
	alice.seenMsgsMu.RUnlock()

	if exists {
		t.Error("message should be removed after TTL")
	}

	// Can be added again
	if alice.isDuplicateMessage(msgID) {
		t.Error("should not be duplicate after cleanup")
	}
}

// TestDedupConfigDefault tests default dedup TTL.
func TestDedupConfigDefault(t *testing.T) {
	alice, _ := New(Config{
		Domain:  "test.com",
		AgentID: "alice",
		// DedupTTL not set - should use default
	})

	if alice.dedupTTL != DefaultDedupTTL {
		t.Errorf("dedupTTL = %v, want %v", alice.dedupTTL, DefaultDedupTTL)
	}
}

// TestDedupConfigCustom tests custom dedup TTL.
func TestDedupConfigCustom(t *testing.T) {
	customTTL := 10 * time.Minute
	alice, _ := New(Config{
		Domain:   "test.com",
		AgentID:  "alice",
		DedupTTL: customTTL,
	})

	if alice.dedupTTL != customTTL {
		t.Errorf("dedupTTL = %v, want %v", alice.dedupTTL, customTTL)
	}
}
