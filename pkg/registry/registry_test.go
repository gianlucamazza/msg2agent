package registry

import (
	"testing"

	"github.com/google/uuid"
)

func TestNewAgent(t *testing.T) {
	agent := NewAgent("did:wba:example.com:agent:test", "Test Agent")

	if agent.ID == uuid.Nil {
		t.Error("agent ID should not be nil")
	}
	if agent.DID != "did:wba:example.com:agent:test" {
		t.Errorf("unexpected DID: %s", agent.DID)
	}
	if agent.DisplayName != "Test Agent" {
		t.Errorf("unexpected display name: %s", agent.DisplayName)
	}
	if agent.Status != StatusOffline {
		t.Errorf("unexpected status: %s", agent.Status)
	}
}

func TestAgentCapabilities(t *testing.T) {
	agent := NewAgent("did:wba:example.com:agent:test", "Test")

	agent.AddCapability("chat", "Chat capability", []string{"chat.send", "chat.receive"})

	if !agent.HasCapability("chat") {
		t.Error("agent should have chat capability")
	}
	if agent.HasCapability("unknown") {
		t.Error("agent should not have unknown capability")
	}
}

func TestAgentEndpoints(t *testing.T) {
	agent := NewAgent("did:wba:example.com:agent:test", "Test")

	agent.AddEndpoint(TransportWebSocket, "ws://localhost:8080", 1)
	agent.AddEndpoint(TransportGRPC, "grpc://localhost:9090", 2)

	if len(agent.Endpoints) != 2 {
		t.Errorf("expected 2 endpoints, got %d", len(agent.Endpoints))
	}
	if agent.Endpoints[0].Transport != TransportWebSocket {
		t.Errorf("expected WebSocket transport, got %s", agent.Endpoints[0].Transport)
	}
}

func TestAgentStatus(t *testing.T) {
	agent := NewAgent("did:wba:example.com:agent:test", "Test")

	if agent.Status != StatusOffline {
		t.Error("agent should start offline")
	}

	agent.SetOnline()
	if agent.Status != StatusOnline {
		t.Error("agent should be online")
	}

	agent.SetOffline()
	if agent.Status != StatusOffline {
		t.Error("agent should be offline")
	}
}

func TestMemoryStore(t *testing.T) {
	store := NewMemoryStore()

	agent := NewAgent("did:wba:example.com:agent:test", "Test")

	// Put
	if err := store.Put(agent); err != nil {
		t.Fatalf("failed to put agent: %v", err)
	}

	// Get by ID
	retrieved, err := store.Get(agent.ID)
	if err != nil {
		t.Fatalf("failed to get agent by ID: %v", err)
	}
	if retrieved.DID != agent.DID {
		t.Errorf("expected DID %s, got %s", agent.DID, retrieved.DID)
	}

	// Get by DID
	retrieved, err = store.GetByDID(agent.DID)
	if err != nil {
		t.Fatalf("failed to get agent by DID: %v", err)
	}
	if retrieved.ID != agent.ID {
		t.Errorf("expected ID %s, got %s", agent.ID, retrieved.ID)
	}

	// List
	agents, err := store.List()
	if err != nil {
		t.Fatalf("failed to list agents: %v", err)
	}
	if len(agents) != 1 {
		t.Errorf("expected 1 agent, got %d", len(agents))
	}

	// Search by capability
	agent.AddCapability("test", "Test capability", nil)
	store.Put(agent)

	found, err := store.Search("test")
	if err != nil {
		t.Fatalf("failed to search agents: %v", err)
	}
	if len(found) != 1 {
		t.Errorf("expected 1 agent, got %d", len(found))
	}

	// Delete
	if err := store.Delete(agent.ID); err != nil {
		t.Fatalf("failed to delete agent: %v", err)
	}

	_, err = store.Get(agent.ID)
	if err != ErrAgentNotFound {
		t.Errorf("expected ErrAgentNotFound, got %v", err)
	}
}
