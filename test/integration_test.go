// Package test provides integration tests for msg2agent.
package test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/gianluca/msg2agent/pkg/agent"
	"github.com/gianluca/msg2agent/pkg/identity"
	"github.com/gianluca/msg2agent/pkg/messaging"
	"github.com/gianluca/msg2agent/pkg/registry"
	"github.com/gianluca/msg2agent/pkg/security"
)

func TestAgentCreation(t *testing.T) {
	cfg := agent.Config{
		Domain:      "test.local",
		AgentID:     "test-agent",
		DisplayName: "Test Agent",
	}

	a, err := agent.New(cfg)
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	if a.DID() == "" {
		t.Error("agent DID should not be empty")
	}

	if !identity.ValidateDID(a.DID()) {
		t.Errorf("agent DID should be valid: %s", a.DID())
	}
}

func TestAgentMethodRegistration(t *testing.T) {
	cfg := agent.Config{
		Domain:      "test.local",
		AgentID:     "test-agent",
		DisplayName: "Test Agent",
	}

	a, err := agent.New(cfg)
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	// Register method
	a.RegisterMethod("test.echo", func(ctx context.Context, params json.RawMessage) (any, error) {
		var input map[string]any
		json.Unmarshal(params, &input)
		return input, nil
	})

	// Add capability
	a.AddCapability("test", "Test capability", []string{"test.echo"})

	if !a.Record().HasCapability("test") {
		t.Error("agent should have test capability")
	}
}

func TestAgentLifecycle(t *testing.T) {
	cfg := agent.Config{
		Domain:      "test.local",
		AgentID:     "test-agent",
		DisplayName: "Test Agent",
	}

	a, err := agent.New(cfg)
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start
	if err := a.Start(ctx); err != nil {
		t.Fatalf("failed to start agent: %v", err)
	}

	if a.Record().Status != registry.StatusOnline {
		t.Error("agent should be online after start")
	}

	// Can't start twice
	if err := a.Start(ctx); err == nil {
		t.Error("starting twice should fail")
	}

	// Stop
	if err := a.Stop(); err != nil {
		t.Fatalf("failed to stop agent: %v", err)
	}

	if a.Record().Status != registry.StatusOffline {
		t.Error("agent should be offline after stop")
	}
}

func TestIdentityGeneration(t *testing.T) {
	ident, err := identity.NewIdentity("example.com", "agent123")
	if err != nil {
		t.Fatalf("failed to create identity: %v", err)
	}

	// Check DID format
	expectedPrefix := "did:wba:example.com:agent:agent123"
	if ident.String() != expectedPrefix {
		t.Errorf("unexpected DID: %s, expected %s", ident.String(), expectedPrefix)
	}

	// Check keys are generated
	if len(ident.SigningPublicKey()) == 0 {
		t.Error("signing public key should not be empty")
	}
	if len(ident.EncryptionPublicKey()) == 0 {
		t.Error("encryption public key should not be empty")
	}

	// Check signing works
	data := []byte("test data")
	sig := ident.Sign(data)
	if len(sig) == 0 {
		t.Error("signature should not be empty")
	}

	// Check DID document is generated
	if ident.Document == nil {
		t.Error("DID document should not be nil")
	}
}

func TestMessageFlow(t *testing.T) {
	// Create request
	req, err := messaging.NewRequest("did:wba:alice", "did:wba:bob", "chat.send", map[string]string{
		"text": "Hello Bob!",
	})
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	// Create response
	resp, err := messaging.NewResponse(req, map[string]string{
		"text": "Hi Alice!",
	})
	if err != nil {
		t.Fatalf("failed to create response: %v", err)
	}

	// Verify correlation
	if resp.CorrelationID == nil || *resp.CorrelationID != req.ID {
		t.Error("response should correlate to request")
	}

	// Verify direction swap
	if resp.From != req.To || resp.To != req.From {
		t.Error("response should swap from/to")
	}
}

func TestACLIntegration(t *testing.T) {
	enforcer := security.NewACLEnforcer()

	// Create agent with ACL
	bob := registry.NewAgent("did:wba:example.com:agent:bob", "Bob")
	bob.ACL = security.NewPolicyBuilder().
		SetDefaultAllow(false).
		Allow("did:wba:example.com:agent:alice", "chat.*").
		Deny("*", "admin.*").
		Build()

	// Alice can chat
	if err := enforcer.CheckAccess(bob, "did:wba:example.com:agent:alice", "chat.send"); err != nil {
		t.Errorf("Alice should be able to chat: %v", err)
	}

	// Alice can't admin
	if err := enforcer.CheckAccess(bob, "did:wba:example.com:agent:alice", "admin.delete"); err != security.ErrAccessDenied {
		t.Error("Alice should not be able to admin")
	}

	// Random agent can't chat
	if err := enforcer.CheckAccess(bob, "did:wba:random:agent", "chat.send"); err != security.ErrAccessDenied {
		t.Error("Random agent should not be able to chat")
	}
}

func TestDiscovery(t *testing.T) {
	store := registry.NewMemoryStore()

	// Register some agents
	alice := registry.NewAgent("did:wba:alice", "Alice")
	alice.AddCapability("chat", "Chat capability", nil)
	store.Put(alice)

	bob := registry.NewAgent("did:wba:bob", "Bob")
	bob.AddCapability("code", "Coding capability", nil)
	store.Put(bob)

	charlie := registry.NewAgent("did:wba:charlie", "Charlie")
	charlie.AddCapability("chat", "Chat capability", nil)
	charlie.AddCapability("code", "Coding capability", nil)
	store.Put(charlie)

	// Search by capability
	chatAgents, _ := store.Search("chat")
	if len(chatAgents) != 2 {
		t.Errorf("expected 2 chat agents, got %d", len(chatAgents))
	}

	codeAgents, _ := store.Search("code")
	if len(codeAgents) != 2 {
		t.Errorf("expected 2 code agents, got %d", len(codeAgents))
	}

	// Find by DID
	found, err := store.GetByDID("did:wba:bob")
	if err != nil {
		t.Fatalf("failed to find Bob: %v", err)
	}
	if found.DisplayName != "Bob" {
		t.Errorf("expected Bob, got %s", found.DisplayName)
	}
}
