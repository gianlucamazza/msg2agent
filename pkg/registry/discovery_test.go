package registry

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// --- Discovery Tests ---

// TestNewDiscovery tests discovery creation.
func TestNewDiscovery(t *testing.T) {
	store := NewMemoryStore()
	agent := NewAgent("did:wba:example.com:agent:test", "TestAgent")

	discovery := NewDiscovery(store, agent)

	if discovery == nil {
		t.Fatal("NewDiscovery returned nil")
	}
	if discovery.store != store {
		t.Error("store not set correctly")
	}
	if discovery.localAgent != agent {
		t.Error("localAgent not set correctly")
	}
}

// TestDiscoveryAnnounce tests announce message creation.
func TestDiscoveryAnnounce(t *testing.T) {
	store := NewMemoryStore()
	agent := NewAgent("did:wba:example.com:agent:alice", "Alice")

	discovery := NewDiscovery(store, agent)
	msg, err := discovery.Announce()

	if err != nil {
		t.Fatalf("Announce failed: %v", err)
	}
	if msg.Type != DiscoveryAnnounce {
		t.Errorf("Type = %q, want %q", msg.Type, DiscoveryAnnounce)
	}
	if msg.AgentID != agent.ID {
		t.Errorf("AgentID = %v, want %v", msg.AgentID, agent.ID)
	}
	if msg.DID != agent.DID {
		t.Errorf("DID = %q, want %q", msg.DID, agent.DID)
	}
	if msg.Agent == nil {
		t.Error("Agent should not be nil in announce")
	}
	if msg.Timestamp.IsZero() {
		t.Error("Timestamp should be set")
	}
}

// TestDiscoveryLeave tests leave message creation.
func TestDiscoveryLeave(t *testing.T) {
	store := NewMemoryStore()
	agent := NewAgent("did:wba:example.com:agent:bob", "Bob")

	discovery := NewDiscovery(store, agent)
	msg, err := discovery.Leave()

	if err != nil {
		t.Fatalf("Leave failed: %v", err)
	}
	if msg.Type != DiscoveryLeave {
		t.Errorf("Type = %q, want %q", msg.Type, DiscoveryLeave)
	}
	if msg.AgentID != agent.ID {
		t.Errorf("AgentID = %v, want %v", msg.AgentID, agent.ID)
	}
	if msg.DID != agent.DID {
		t.Errorf("DID = %q, want %q", msg.DID, agent.DID)
	}
	if msg.Agent != nil {
		t.Error("Agent should be nil in leave message")
	}
}

// TestDiscoveryQuery tests query message creation.
func TestDiscoveryQuery(t *testing.T) {
	store := NewMemoryStore()
	agent := NewAgent("did:wba:example.com:agent:seeker", "Seeker")

	discovery := NewDiscovery(store, agent)
	msg, err := discovery.Query("chat")

	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if msg.Type != DiscoveryQuery {
		t.Errorf("Type = %q, want %q", msg.Type, DiscoveryQuery)
	}
	if msg.Capability != "chat" {
		t.Errorf("Capability = %q, want %q", msg.Capability, "chat")
	}
	if msg.AgentID != agent.ID {
		t.Errorf("AgentID = %v, want %v", msg.AgentID, agent.ID)
	}
}

// TestDiscoveryHandleAnnounce tests handling announce messages.
func TestDiscoveryHandleAnnounce(t *testing.T) {
	store := NewMemoryStore()
	localAgent := NewAgent("did:wba:example.com:agent:local", "Local")

	discovery := NewDiscovery(store, localAgent)

	// Create remote agent's announce message
	remoteAgent := NewAgent("did:wba:example.com:agent:remote", "Remote")
	msg := &DiscoveryMessage{
		Type:      DiscoveryAnnounce,
		AgentID:   remoteAgent.ID,
		DID:       remoteAgent.DID,
		Agent:     remoteAgent,
		Timestamp: time.Now(),
	}

	data, _ := json.Marshal(msg)
	err := discovery.HandleMessage(data)

	if err != nil {
		t.Fatalf("HandleMessage failed: %v", err)
	}

	// Remote agent should be in store
	stored, err := store.Get(remoteAgent.ID)
	if err != nil {
		t.Fatalf("agent not stored: %v", err)
	}
	if stored.Status != StatusOnline {
		t.Errorf("agent status = %v, want StatusOnline", stored.Status)
	}
}

// TestDiscoveryHandleAnnounceIgnoresSelf tests that local agent isn't stored.
func TestDiscoveryHandleAnnounceIgnoresSelf(t *testing.T) {
	store := NewMemoryStore()
	localAgent := NewAgent("did:wba:example.com:agent:self", "Self")

	discovery := NewDiscovery(store, localAgent)

	// Create announce message from self
	msg := &DiscoveryMessage{
		Type:      DiscoveryAnnounce,
		AgentID:   localAgent.ID,
		DID:       localAgent.DID,
		Agent:     localAgent,
		Timestamp: time.Now(),
	}

	data, _ := json.Marshal(msg)
	discovery.HandleMessage(data)

	// Self should not be in store
	_, err := store.Get(localAgent.ID)
	if err != ErrAgentNotFound {
		t.Errorf("self should not be stored: %v", err)
	}
}

// TestDiscoveryHandleLeave tests handling leave messages.
func TestDiscoveryHandleLeave(t *testing.T) {
	store := NewMemoryStore()
	localAgent := NewAgent("did:wba:example.com:agent:local", "Local")

	discovery := NewDiscovery(store, localAgent)

	// Add a remote agent
	remoteAgent := NewAgent("did:wba:example.com:agent:leaver", "Leaver")
	remoteAgent.SetOnline()
	store.Put(remoteAgent)

	// Create leave message
	msg := &DiscoveryMessage{
		Type:      DiscoveryLeave,
		AgentID:   remoteAgent.ID,
		DID:       remoteAgent.DID,
		Timestamp: time.Now(),
	}

	data, _ := json.Marshal(msg)
	err := discovery.HandleMessage(data)

	if err != nil {
		t.Fatalf("HandleMessage failed: %v", err)
	}

	// Agent should be offline now
	stored, _ := store.Get(remoteAgent.ID)
	if stored.Status != StatusOffline {
		t.Errorf("agent status = %v, want StatusOffline", stored.Status)
	}
}

// TestDiscoveryHandleLeaveNonexistent tests leaving non-existent agent.
func TestDiscoveryHandleLeaveNonexistent(t *testing.T) {
	store := NewMemoryStore()
	localAgent := NewAgent("did:wba:example.com:agent:local", "Local")

	discovery := NewDiscovery(store, localAgent)

	msg := &DiscoveryMessage{
		Type:      DiscoveryLeave,
		AgentID:   uuid.New(),
		DID:       "did:wba:example.com:agent:ghost",
		Timestamp: time.Now(),
	}

	data, _ := json.Marshal(msg)
	err := discovery.HandleMessage(data)

	// Should not error for non-existent agent
	if err != nil {
		t.Errorf("HandleMessage returned error for non-existent agent: %v", err)
	}
}

// TestDiscoveryHandleQuery tests handling query messages.
func TestDiscoveryHandleQuery(t *testing.T) {
	store := NewMemoryStore()
	localAgent := NewAgent("did:wba:example.com:agent:responder", "Responder")
	localAgent.AddCapability("translate", "Translation service", nil)

	discovery := NewDiscovery(store, localAgent)

	// Track responses
	var responses []*DiscoveryMessage
	discovery.OnMessage(func(msg *DiscoveryMessage) error {
		responses = append(responses, msg)
		return nil
	})

	// Query for matching capability
	msg := &DiscoveryMessage{
		Type:       DiscoveryQuery,
		AgentID:    uuid.New(),
		Capability: "translate",
		Timestamp:  time.Now(),
	}

	data, _ := json.Marshal(msg)
	err := discovery.HandleMessage(data)

	if err != nil {
		t.Fatalf("HandleMessage failed: %v", err)
	}

	if len(responses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(responses))
	}
	if responses[0].Type != DiscoveryResponse {
		t.Errorf("response type = %q, want %q", responses[0].Type, DiscoveryResponse)
	}
	if responses[0].Agent == nil {
		t.Error("response should include agent")
	}
}

// TestDiscoveryHandleQueryNoMatch tests query with no matching capability.
func TestDiscoveryHandleQueryNoMatch(t *testing.T) {
	store := NewMemoryStore()
	localAgent := NewAgent("did:wba:example.com:agent:limited", "Limited")
	localAgent.AddCapability("chat", "Chat only", nil)

	discovery := NewDiscovery(store, localAgent)

	var responses []*DiscoveryMessage
	discovery.OnMessage(func(msg *DiscoveryMessage) error {
		responses = append(responses, msg)
		return nil
	})

	// Query for non-matching capability
	msg := &DiscoveryMessage{
		Type:       DiscoveryQuery,
		AgentID:    uuid.New(),
		Capability: "translate", // We don't have this
		Timestamp:  time.Now(),
	}

	data, _ := json.Marshal(msg)
	discovery.HandleMessage(data)

	if len(responses) != 0 {
		t.Errorf("expected 0 responses for non-matching query, got %d", len(responses))
	}
}

// TestDiscoveryHandleResponse tests handling response messages.
func TestDiscoveryHandleResponse(t *testing.T) {
	store := NewMemoryStore()
	localAgent := NewAgent("did:wba:example.com:agent:querier", "Querier")

	discovery := NewDiscovery(store, localAgent)

	// Simulate receiving a response
	responder := NewAgent("did:wba:example.com:agent:responder", "Responder")
	msg := &DiscoveryMessage{
		Type:      DiscoveryResponse,
		AgentID:   responder.ID,
		DID:       responder.DID,
		Agent:     responder,
		Timestamp: time.Now(),
	}

	data, _ := json.Marshal(msg)
	err := discovery.HandleMessage(data)

	if err != nil {
		t.Fatalf("HandleMessage failed: %v", err)
	}

	// Responder should be stored
	stored, err := store.Get(responder.ID)
	if err != nil {
		t.Fatalf("responder not stored: %v", err)
	}
	if stored.Status != StatusOnline {
		t.Errorf("responder status = %v, want StatusOnline", stored.Status)
	}
}

// TestDiscoveryHandleUnknownType tests handling unknown message types.
func TestDiscoveryHandleUnknownType(t *testing.T) {
	store := NewMemoryStore()
	localAgent := NewAgent("did:wba:example.com:agent:local", "Local")

	discovery := NewDiscovery(store, localAgent)

	msg := &DiscoveryMessage{
		Type:      "unknown_type",
		AgentID:   uuid.New(),
		Timestamp: time.Now(),
	}

	data, _ := json.Marshal(msg)
	err := discovery.HandleMessage(data)

	if err == nil {
		t.Error("HandleMessage should error on unknown type")
	}
}

// TestDiscoveryHandleInvalidJSON tests handling invalid JSON.
func TestDiscoveryHandleInvalidJSON(t *testing.T) {
	store := NewMemoryStore()
	localAgent := NewAgent("did:wba:example.com:agent:local", "Local")

	discovery := NewDiscovery(store, localAgent)

	err := discovery.HandleMessage([]byte("not valid json"))

	if err == nil {
		t.Error("HandleMessage should error on invalid JSON")
	}
}

// TestDiscoveryFindByCapability tests capability search.
func TestDiscoveryFindByCapability(t *testing.T) {
	store := NewMemoryStore()

	// Add agents with different capabilities
	agent1 := NewAgent("did:wba:example.com:agent:1", "Agent1")
	agent1.AddCapability("chat", "Chat", nil)
	store.Put(agent1)

	agent2 := NewAgent("did:wba:example.com:agent:2", "Agent2")
	agent2.AddCapability("translate", "Translate", nil)
	store.Put(agent2)

	agent3 := NewAgent("did:wba:example.com:agent:3", "Agent3")
	agent3.AddCapability("chat", "Chat", nil)
	store.Put(agent3)

	localAgent := NewAgent("did:wba:example.com:agent:local", "Local")
	discovery := NewDiscovery(store, localAgent)

	ctx := context.Background()
	results, err := discovery.FindByCapability(ctx, "chat")

	if err != nil {
		t.Fatalf("FindByCapability failed: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("found %d agents, want 2", len(results))
	}
}

// TestDiscoveryFindByDID tests DID search.
func TestDiscoveryFindByDID(t *testing.T) {
	store := NewMemoryStore()

	agent := NewAgent("did:wba:example.com:agent:findme", "FindMe")
	store.Put(agent)

	localAgent := NewAgent("did:wba:example.com:agent:local", "Local")
	discovery := NewDiscovery(store, localAgent)

	ctx := context.Background()
	found, err := discovery.FindByDID(ctx, "did:wba:example.com:agent:findme")

	if err != nil {
		t.Fatalf("FindByDID failed: %v", err)
	}
	if found.ID != agent.ID {
		t.Errorf("found wrong agent: %v", found.ID)
	}
}

// TestDiscoveryFindByDIDNotFound tests DID search for non-existent agent.
func TestDiscoveryFindByDIDNotFound(t *testing.T) {
	store := NewMemoryStore()
	localAgent := NewAgent("did:wba:example.com:agent:local", "Local")
	discovery := NewDiscovery(store, localAgent)

	ctx := context.Background()
	_, err := discovery.FindByDID(ctx, "did:wba:example.com:agent:ghost")

	if err != ErrAgentNotFound {
		t.Errorf("FindByDID error = %v, want ErrAgentNotFound", err)
	}
}

// TestDiscoveryGetOnlineAgents tests listing online agents.
func TestDiscoveryGetOnlineAgents(t *testing.T) {
	store := NewMemoryStore()

	online := NewAgent("did:wba:example.com:agent:online", "Online")
	online.SetOnline()
	store.Put(online)

	offline := NewAgent("did:wba:example.com:agent:offline", "Offline")
	offline.SetOffline()
	store.Put(offline)

	localAgent := NewAgent("did:wba:example.com:agent:local", "Local")
	discovery := NewDiscovery(store, localAgent)

	agents, err := discovery.GetOnlineAgents()

	if err != nil {
		t.Fatalf("GetOnlineAgents failed: %v", err)
	}
	if len(agents) != 1 {
		t.Errorf("found %d online agents, want 1", len(agents))
	}
	if agents[0].ID != online.ID {
		t.Error("wrong agent returned")
	}
}

// TestDiscoveryOnMessage tests message handler registration.
func TestDiscoveryOnMessage(t *testing.T) {
	store := NewMemoryStore()
	localAgent := NewAgent("did:wba:example.com:agent:local", "Local")
	localAgent.AddCapability("test", "Test", nil)

	discovery := NewDiscovery(store, localAgent)

	handlerCalls := 0
	discovery.OnMessage(func(msg *DiscoveryMessage) error {
		handlerCalls++
		return nil
	})

	// Second handler
	discovery.OnMessage(func(msg *DiscoveryMessage) error {
		handlerCalls++
		return nil
	})

	// Trigger query that matches
	msg := &DiscoveryMessage{
		Type:       DiscoveryQuery,
		AgentID:    uuid.New(),
		Capability: "test",
		Timestamp:  time.Now(),
	}

	data, _ := json.Marshal(msg)
	discovery.HandleMessage(data)

	if handlerCalls != 2 {
		t.Errorf("handler called %d times, want 2", handlerCalls)
	}
}

// TestDiscoveryConcurrent tests concurrent access.
func TestDiscoveryConcurrent(t *testing.T) {
	store := NewMemoryStore()
	localAgent := NewAgent("did:wba:example.com:agent:local", "Local")
	localAgent.AddCapability("test", "Test", nil)

	discovery := NewDiscovery(store, localAgent)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			// Random operations
			discovery.Announce()
			discovery.Leave()
			discovery.Query("test")
			discovery.GetOnlineAgents()
		}()
	}

	wg.Wait()
}

// --- FileStore Tests ---

// TestNewFileStore tests FileStore creation.
func TestNewFileStore(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "agents.json")

	store, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	if store == nil {
		t.Fatal("NewFileStore returned nil")
	}
	if store.path != path {
		t.Errorf("path = %q, want %q", store.path, path)
	}
}

// TestFileStorePersistence tests save and load.
func TestFileStorePersistence(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "agents.json")

	// Create store and add agent
	store1, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	agent := NewAgent("did:wba:example.com:agent:persist", "Persist")
	agent.AddCapability("test", "Test", nil)
	if err := store1.Put(agent); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("file should exist after Put")
	}

	// Create new store from same file
	store2, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore (load) failed: %v", err)
	}

	// Agent should be loaded
	loaded, err := store2.Get(agent.ID)
	if err != nil {
		t.Fatalf("Get after reload failed: %v", err)
	}
	if loaded.DisplayName != agent.DisplayName {
		t.Errorf("DisplayName = %q, want %q", loaded.DisplayName, agent.DisplayName)
	}
	if !loaded.HasCapability("test") {
		t.Error("capability not persisted")
	}
}

// TestFileStoreDelete tests deletion with persistence.
func TestFileStoreDelete(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "agents.json")

	store, _ := NewFileStore(path)

	agent := NewAgent("did:wba:example.com:agent:delete", "Delete")
	store.Put(agent)
	store.Delete(agent.ID)

	// Create new store from file
	store2, _ := NewFileStore(path)

	_, err := store2.Get(agent.ID)
	if err != ErrAgentNotFound {
		t.Errorf("deleted agent should not be loaded: %v", err)
	}
}

// TestFileStoreLoadCorruptedFile tests handling corrupted file.
func TestFileStoreLoadCorruptedFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "corrupted.json")

	// Write invalid JSON
	os.WriteFile(path, []byte("not valid json"), 0600)

	_, err := NewFileStore(path)
	if err == nil {
		t.Error("NewFileStore should fail on corrupted file")
	}
}

// TestFileStoreMultipleAgents tests persistence of multiple agents.
func TestFileStoreMultipleAgents(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "multi.json")

	store, _ := NewFileStore(path)

	for i := 0; i < 5; i++ {
		agent := NewAgent("did:wba:example.com:agent:"+string(rune('a'+i)), "Agent")
		store.Put(agent)
	}

	// Reload
	store2, _ := NewFileStore(path)
	agents, _ := store2.List()

	if len(agents) != 5 {
		t.Errorf("loaded %d agents, want 5", len(agents))
	}
}

// TestFileStoreSearch tests search with FileStore.
func TestFileStoreSearch(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "search.json")

	store, _ := NewFileStore(path)

	agent1 := NewAgent("did:wba:example.com:agent:1", "One")
	agent1.AddCapability("chat", "Chat", nil)
	store.Put(agent1)

	agent2 := NewAgent("did:wba:example.com:agent:2", "Two")
	agent2.AddCapability("translate", "Translate", nil)
	store.Put(agent2)

	results, _ := store.Search("chat")
	if len(results) != 1 {
		t.Errorf("search found %d, want 1", len(results))
	}
}

// --- Discovery Message Constants Tests ---

// TestDiscoveryMessageConstants tests message type constants.
func TestDiscoveryMessageConstants(t *testing.T) {
	if DiscoveryAnnounce != "announce" {
		t.Errorf("DiscoveryAnnounce = %q, want %q", DiscoveryAnnounce, "announce")
	}
	if DiscoveryQuery != "query" {
		t.Errorf("DiscoveryQuery = %q, want %q", DiscoveryQuery, "query")
	}
	if DiscoveryResponse != "response" {
		t.Errorf("DiscoveryResponse = %q, want %q", DiscoveryResponse, "response")
	}
	if DiscoveryLeave != "leave" {
		t.Errorf("DiscoveryLeave = %q, want %q", DiscoveryLeave, "leave")
	}
}

// TestDiscoveryMessageJSON tests JSON serialization.
func TestDiscoveryMessageJSON(t *testing.T) {
	agent := NewAgent("did:wba:example.com:agent:json", "JSON")
	msg := &DiscoveryMessage{
		Type:       DiscoveryAnnounce,
		AgentID:    agent.ID,
		DID:        agent.DID,
		Capability: "test",
		Agent:      agent,
		Timestamp:  time.Now(),
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded DiscoveryMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.Type != msg.Type {
		t.Errorf("Type = %q, want %q", decoded.Type, msg.Type)
	}
	if decoded.AgentID != msg.AgentID {
		t.Errorf("AgentID mismatch")
	}
	if decoded.DID != msg.DID {
		t.Errorf("DID mismatch")
	}
	if decoded.Capability != msg.Capability {
		t.Errorf("Capability mismatch")
	}
}

// --- Error Type Tests ---

// TestDiscoveryErrors tests error constants.
func TestDiscoveryErrors(t *testing.T) {
	if ErrDiscoveryTimeout == nil {
		t.Error("ErrDiscoveryTimeout should not be nil")
	}
	if ErrNoAgentsFound == nil {
		t.Error("ErrNoAgentsFound should not be nil")
	}
}
