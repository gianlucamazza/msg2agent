package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/gianluca/msg2agent/pkg/messaging"
	"github.com/gianluca/msg2agent/pkg/registry"
)

func TestNew(t *testing.T) {
	cfg := Config{
		Domain:      "test.example.com",
		AgentID:     "test-agent",
		DisplayName: "Test Agent",
	}

	agent, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	if agent.DID() == "" {
		t.Error("agent DID should not be empty")
	}

	if agent.Record() == nil {
		t.Error("agent record should not be nil")
	}
}

func TestVerifyMessageSignature(t *testing.T) {
	// Create Alice (sender)
	alice, err := New(Config{
		Domain:  "test.com",
		AgentID: "alice",
		Logger:  slog.Default(),
	})
	if err != nil {
		t.Fatalf("failed to create Alice: %v", err)
	}

	// Create Bob (receiver)
	bob, err := New(Config{
		Domain:  "test.com",
		AgentID: "bob",
		Logger:  slog.Default(),
	})
	if err != nil {
		t.Fatalf("failed to create Bob: %v", err)
	}

	// Register Alice in Bob's store so signature can be verified
	bob.store.Put(alice.Record())

	// Create a message from Alice to Bob
	msg, err := messaging.NewRequest(alice.DID(), bob.DID(), "ping", nil)
	if err != nil {
		t.Fatalf("failed to create message: %v", err)
	}

	// Sign the message with Alice's key
	msgBytes, _ := json.Marshal(msg)
	msg.Signature = alice.identity.Sign(msgBytes)

	// Bob verifies the signature
	if err := bob.verifyMessageSignature(msg); err != nil {
		t.Errorf("signature verification should succeed: %v", err)
	}
}

func TestVerifyMessageSignature_InvalidSignature(t *testing.T) {
	alice, _ := New(Config{Domain: "test.com", AgentID: "alice"})
	bob, _ := New(Config{Domain: "test.com", AgentID: "bob"})

	bob.store.Put(alice.Record())

	msg, _ := messaging.NewRequest(alice.DID(), bob.DID(), "ping", nil)

	// Sign with Alice's key
	msgBytes, _ := json.Marshal(msg)
	msg.Signature = alice.identity.Sign(msgBytes)

	// Tamper with the message
	msg.Method = "tampered"

	// Verification should fail
	if err := bob.verifyMessageSignature(msg); err == nil {
		t.Error("tampered message should fail verification")
	}
}

func TestVerifyMessageSignature_NoSignature(t *testing.T) {
	alice, _ := New(Config{Domain: "test.com", AgentID: "alice"})
	bob, _ := New(Config{Domain: "test.com", AgentID: "bob"})

	bob.store.Put(alice.Record())

	msg, _ := messaging.NewRequest(alice.DID(), bob.DID(), "ping", nil)
	// No signature

	if err := bob.verifyMessageSignature(msg); err != ErrSignatureInvalid {
		t.Errorf("expected ErrSignatureInvalid, got %v", err)
	}
}

func TestVerifyMessageSignature_UnknownSender(t *testing.T) {
	alice, _ := New(Config{Domain: "test.com", AgentID: "alice"})
	bob, _ := New(Config{Domain: "test.com", AgentID: "bob"})

	// Alice not registered in Bob's store

	msg, _ := messaging.NewRequest(alice.DID(), bob.DID(), "ping", nil)
	msgBytes, _ := json.Marshal(msg)
	msg.Signature = alice.identity.Sign(msgBytes)

	if err := bob.verifyMessageSignature(msg); err != ErrSenderNotFound {
		t.Errorf("expected ErrSenderNotFound, got %v", err)
	}
}

func TestEncryptDecryptMessageBody(t *testing.T) {
	// Create Alice and Bob
	alice, _ := New(Config{Domain: "test.com", AgentID: "alice"})
	bob, _ := New(Config{Domain: "test.com", AgentID: "bob"})

	// Register each other
	alice.store.Put(bob.Record())
	bob.store.Put(alice.Record())

	// Create a message with a body
	body := map[string]string{"greeting": "hello"}
	msg, err := messaging.NewRequest(alice.DID(), bob.DID(), "greet", body)
	if err != nil {
		t.Fatalf("failed to create message: %v", err)
	}

	originalBody := make([]byte, len(msg.Body))
	copy(originalBody, msg.Body)

	// Alice encrypts the message body
	if err := alice.encryptMessageBody(msg); err != nil {
		t.Fatalf("encryption failed: %v", err)
	}

	if !msg.Encrypted {
		t.Error("message should be marked as encrypted")
	}

	if bytes.Equal(msg.Body, originalBody) {
		t.Error("encrypted body should differ from original")
	}

	// Bob decrypts the message body
	if err := bob.decryptMessageBody(msg); err != nil {
		t.Fatalf("decryption failed: %v", err)
	}

	if msg.Encrypted {
		t.Error("message should not be marked as encrypted after decryption")
	}

	if !bytes.Equal(msg.Body, originalBody) {
		t.Errorf("decrypted body should match original: got %s, want %s", msg.Body, originalBody)
	}
}

func TestEncryptDecryptMessageBody_EmptyBody(t *testing.T) {
	alice, _ := New(Config{Domain: "test.com", AgentID: "alice"})
	bob, _ := New(Config{Domain: "test.com", AgentID: "bob"})

	alice.store.Put(bob.Record())
	bob.store.Put(alice.Record())

	msg, _ := messaging.NewRequest(alice.DID(), bob.DID(), "ping", nil)

	// Should succeed with empty body
	if err := alice.encryptMessageBody(msg); err != nil {
		t.Errorf("encryption of empty body should succeed: %v", err)
	}

	if msg.Encrypted {
		t.Error("empty body should not be marked as encrypted")
	}
}

func TestEncryptMessageBody_RecipientNotFound(t *testing.T) {
	alice, _ := New(Config{Domain: "test.com", AgentID: "alice"})

	body := map[string]string{"data": "test"}
	msg, _ := messaging.NewRequest(alice.DID(), "did:wba:unknown.com:agent:unknown", "method", body)

	err := alice.encryptMessageBody(msg)
	if err != ErrRecipientNotFound {
		t.Errorf("expected ErrRecipientNotFound, got %v", err)
	}
}

func TestDecryptMessageBody_SenderNotFound(t *testing.T) {
	bob, _ := New(Config{Domain: "test.com", AgentID: "bob"})

	msg := &messaging.Message{
		From:      "did:wba:unknown.com:agent:alice",
		To:        bob.DID(),
		Body:      []byte("encrypted-data"),
		Encrypted: true,
	}

	err := bob.decryptMessageBody(msg)
	if err != ErrSenderNotFound {
		t.Errorf("expected ErrSenderNotFound, got %v", err)
	}
}

func TestDecryptMessageBody_WrongKey(t *testing.T) {
	alice, _ := New(Config{Domain: "test.com", AgentID: "alice"})
	bob, _ := New(Config{Domain: "test.com", AgentID: "bob"})
	eve, _ := New(Config{Domain: "test.com", AgentID: "eve"})

	// Register Alice in Bob's store
	bob.store.Put(alice.Record())

	// Create and encrypt message from Alice to Eve (not Bob)
	alice.store.Put(eve.Record())
	body := map[string]string{"secret": "data"}
	msg, _ := messaging.NewRequest(alice.DID(), eve.DID(), "secret", body)
	alice.encryptMessageBody(msg)

	// Change recipient to Bob (to simulate interception)
	msg.To = bob.DID()

	// Bob tries to decrypt - should fail because it was encrypted for Eve
	err := bob.decryptMessageBody(msg)
	if err != ErrDecryptionFailed {
		t.Errorf("expected ErrDecryptionFailed, got %v", err)
	}
}

func TestRegisterMethod(t *testing.T) {
	agent, _ := New(Config{Domain: "test.com", AgentID: "test"})

	called := false
	agent.RegisterMethod("test.method", func(ctx context.Context, params json.RawMessage) (any, error) {
		called = true
		return "ok", nil
	})

	agent.mu.RLock()
	handler, exists := agent.handlers["test.method"]
	agent.mu.RUnlock()

	if !exists {
		t.Error("handler should be registered")
	}

	result, err := handler(context.Background(), nil)
	if err != nil {
		t.Errorf("handler should not return error: %v", err)
	}

	if !called {
		t.Error("handler should have been called")
	}

	if result != "ok" {
		t.Errorf("expected 'ok', got %v", result)
	}
}

func TestAddCapability(t *testing.T) {
	agent, _ := New(Config{Domain: "test.com", AgentID: "test"})

	agent.AddCapability("math", "Mathematical operations", []string{"add", "subtract"})

	if !agent.Record().HasCapability("math") {
		t.Error("agent should have math capability")
	}
}

func TestSetACL(t *testing.T) {
	agent, _ := New(Config{Domain: "test.com", AgentID: "test"})

	policy := &registry.ACLPolicy{
		DefaultAllow: false,
		Rules: []registry.ACLRule{
			{Principal: "*", Actions: []string{"ping"}, Effect: "allow"},
		},
	}

	agent.SetACL(policy)

	if agent.Record().ACL == nil {
		t.Error("ACL should be set")
	}

	if agent.Record().ACL.DefaultAllow {
		t.Error("default should be deny")
	}
}

func TestStartStop(t *testing.T) {
	agent, _ := New(Config{Domain: "test.com", AgentID: "test"})

	// Start
	if err := agent.Start(context.Background()); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	if agent.Record().Status != registry.StatusOnline {
		t.Error("agent should be online")
	}

	// Double start should fail
	if err := agent.Start(context.Background()); err != ErrAlreadyStarted {
		t.Errorf("expected ErrAlreadyStarted, got %v", err)
	}

	// Stop
	if err := agent.Stop(); err != nil {
		t.Fatalf("stop failed: %v", err)
	}

	if agent.Record().Status != registry.StatusOffline {
		t.Error("agent should be offline")
	}

	// Double stop should fail
	if err := agent.Stop(); err != ErrNotStarted {
		t.Errorf("expected ErrNotStarted, got %v", err)
	}
}

func TestListen(t *testing.T) {
	agent, err := New(Config{
		Domain:      "test.com",
		AgentID:     "listener",
		DisplayName: "Listener Agent",
		ListenAddr:  "127.0.0.1:0",
	})
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := agent.Start(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer agent.Stop()

	// Start listening
	if err := agent.Listen(ctx); err != nil {
		t.Fatalf("listen failed: %v", err)
	}

	// Should be listening now
	if !agent.IsListening() {
		t.Error("agent should be listening")
	}

	// Actual address should be set
	addr := agent.ListenAddr()
	if addr == "" {
		t.Error("listen address should not be empty")
	}

	// Should have updated endpoint
	if len(agent.Record().Endpoints) == 0 {
		t.Error("endpoint should be set")
	}

	// Double listen should fail
	if err := agent.Listen(ctx); err != ErrAlreadyListening {
		t.Errorf("expected ErrAlreadyListening, got %v", err)
	}
}

func TestListenNoAddress(t *testing.T) {
	agent, _ := New(Config{
		Domain:  "test.com",
		AgentID: "noaddr",
		// No ListenAddr
	})

	ctx := context.Background()
	agent.Start(ctx)
	defer agent.Stop()

	// Should not error, just skip
	if err := agent.Listen(ctx); err != nil {
		t.Errorf("listen with no address should not error: %v", err)
	}

	if agent.IsListening() {
		t.Error("agent should not be listening without address")
	}
}

func TestPeerCount(t *testing.T) {
	agent, _ := New(Config{
		Domain:      "test.com",
		AgentID:     "counter",
		DisplayName: "Counter Agent",
	})

	if agent.PeerCount() != 0 {
		t.Error("initial peer count should be 0")
	}
}

func TestBuildAgentCard(t *testing.T) {
	agent, _ := New(Config{
		Domain:      "test.com",
		AgentID:     "card",
		DisplayName: "Card Agent",
		ListenAddr:  "127.0.0.1:8080",
	})

	// Add a capability
	agent.RegisterMethod("greet", func(ctx context.Context, params json.RawMessage) (any, error) {
		return "hello", nil
	})
	agent.AddCapability("greeting", "Greeting service", []string{"greet"})

	card := agent.buildAgentCard()

	if card.Name != "Card Agent" {
		t.Errorf("name = %q, want %q", card.Name, "Card Agent")
	}

	if card.DID == "" {
		t.Error("DID should be set")
	}

	if card.Version != "1.0.0" {
		t.Errorf("version = %q, want %q", card.Version, "1.0.0")
	}

	if !card.Capabilities.EndToEndEncryption {
		t.Error("EndToEndEncryption should be true")
	}

	if len(card.Skills) != 1 {
		t.Errorf("skills count = %d, want 1", len(card.Skills))
	}

	if len(card.PublicKeys) < 2 {
		t.Error("should have at least 2 public keys (signing + encryption)")
	}
}

func TestEncodeBase58Agent(t *testing.T) {
	// Test empty
	if got := encodeBase58(nil); got != "" {
		t.Errorf("encodeBase58(nil) = %q, want empty", got)
	}

	// Test known value
	if got := encodeBase58([]byte("hello")); got == "" {
		t.Error("encodeBase58(hello) should not be empty")
	}
}
