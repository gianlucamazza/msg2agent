package registry

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNewSQLiteStore(t *testing.T) {
	t.Run("in-memory", func(t *testing.T) {
		store, err := NewSQLiteStore(SQLiteConfig{Path: ":memory:"})
		if err != nil {
			t.Fatalf("failed to create store: %v", err)
		}
		defer store.Close()

		// Verify store is functional
		agents, err := store.List()
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}
		if len(agents) != 0 {
			t.Errorf("expected empty store, got %d agents", len(agents))
		}
	})

	t.Run("file-based", func(t *testing.T) {
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "test.db")

		store, err := NewSQLiteStore(SQLiteConfig{Path: dbPath})
		if err != nil {
			t.Fatalf("failed to create store: %v", err)
		}
		defer store.Close()

		// Verify file was created
		if _, err := os.Stat(dbPath); os.IsNotExist(err) {
			t.Error("database file was not created")
		}
	})

	t.Run("invalid-path", func(t *testing.T) {
		_, err := NewSQLiteStore(SQLiteConfig{Path: "/nonexistent/path/test.db"})
		if err == nil {
			t.Error("expected error for invalid path")
		}
	})
}

func TestSQLiteStore_Put(t *testing.T) {
	store, err := NewSQLiteStore(SQLiteConfig{Path: ":memory:"})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	agent := NewAgent("did:example:123", "Test Agent")
	agent.AddCapability("test", "Test capability", []string{"ping", "pong"})
	agent.AddEndpoint(TransportWebSocket, "ws://localhost:8080", 1)

	if err := store.Put(agent); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Verify agent was stored
	retrieved, err := store.Get(agent.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if retrieved.DID != agent.DID {
		t.Errorf("DID mismatch: got %q, want %q", retrieved.DID, agent.DID)
	}
	if retrieved.DisplayName != agent.DisplayName {
		t.Errorf("DisplayName mismatch: got %q, want %q", retrieved.DisplayName, agent.DisplayName)
	}
	if len(retrieved.Capabilities) != 1 {
		t.Errorf("expected 1 capability, got %d", len(retrieved.Capabilities))
	}
	if len(retrieved.Endpoints) != 1 {
		t.Errorf("expected 1 endpoint, got %d", len(retrieved.Endpoints))
	}
}

func TestSQLiteStore_PutUpdate(t *testing.T) {
	store, err := NewSQLiteStore(SQLiteConfig{Path: ":memory:"})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	agent := NewAgent("did:example:123", "Test Agent")
	if err := store.Put(agent); err != nil {
		t.Fatalf("initial Put failed: %v", err)
	}

	// Update agent
	agent.DisplayName = "Updated Agent"
	agent.Status = StatusOnline
	if err := store.Put(agent); err != nil {
		t.Fatalf("update Put failed: %v", err)
	}

	// Verify update
	retrieved, err := store.Get(agent.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if retrieved.DisplayName != "Updated Agent" {
		t.Errorf("DisplayName not updated: got %q", retrieved.DisplayName)
	}
	if retrieved.Status != StatusOnline {
		t.Errorf("Status not updated: got %q", retrieved.Status)
	}
}

func TestSQLiteStore_Get(t *testing.T) {
	store, err := NewSQLiteStore(SQLiteConfig{Path: ":memory:"})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	t.Run("existing", func(t *testing.T) {
		agent := NewAgent("did:example:get", "Get Agent")
		store.Put(agent)

		retrieved, err := store.Get(agent.ID)
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if retrieved.ID != agent.ID {
			t.Errorf("ID mismatch")
		}
	})

	t.Run("not-found", func(t *testing.T) {
		_, err := store.Get(uuid.New())
		if err != ErrAgentNotFound {
			t.Errorf("expected ErrAgentNotFound, got %v", err)
		}
	})
}

func TestSQLiteStore_GetByDID(t *testing.T) {
	store, err := NewSQLiteStore(SQLiteConfig{Path: ":memory:"})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	t.Run("existing", func(t *testing.T) {
		agent := NewAgent("did:example:bydid", "ByDID Agent")
		store.Put(agent)

		retrieved, err := store.GetByDID("did:example:bydid")
		if err != nil {
			t.Fatalf("GetByDID failed: %v", err)
		}
		if retrieved.DID != agent.DID {
			t.Errorf("DID mismatch")
		}
	})

	t.Run("not-found", func(t *testing.T) {
		_, err := store.GetByDID("did:example:nonexistent")
		if err != ErrAgentNotFound {
			t.Errorf("expected ErrAgentNotFound, got %v", err)
		}
	})
}

func TestSQLiteStore_Delete(t *testing.T) {
	store, err := NewSQLiteStore(SQLiteConfig{Path: ":memory:"})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	t.Run("existing", func(t *testing.T) {
		agent := NewAgent("did:example:delete", "Delete Agent")
		store.Put(agent)

		if err := store.Delete(agent.ID); err != nil {
			t.Fatalf("Delete failed: %v", err)
		}

		// Verify deleted
		_, err := store.Get(agent.ID)
		if err != ErrAgentNotFound {
			t.Errorf("expected ErrAgentNotFound after delete, got %v", err)
		}
	})

	t.Run("not-found", func(t *testing.T) {
		err := store.Delete(uuid.New())
		if err != ErrAgentNotFound {
			t.Errorf("expected ErrAgentNotFound, got %v", err)
		}
	})
}

func TestSQLiteStore_List(t *testing.T) {
	store, err := NewSQLiteStore(SQLiteConfig{Path: ":memory:"})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Add multiple agents
	for i := 0; i < 5; i++ {
		agent := NewAgent("did:example:list"+string(rune('0'+i)), "List Agent")
		store.Put(agent)
	}

	agents, err := store.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(agents) != 5 {
		t.Errorf("expected 5 agents, got %d", len(agents))
	}
}

func TestSQLiteStore_Search(t *testing.T) {
	store, err := NewSQLiteStore(SQLiteConfig{Path: ":memory:"})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Add agents with different capabilities
	agent1 := NewAgent("did:example:search1", "Search Agent 1")
	agent1.AddCapability("chat", "Chat capability", []string{"send", "receive"})
	store.Put(agent1)

	agent2 := NewAgent("did:example:search2", "Search Agent 2")
	agent2.AddCapability("storage", "Storage capability", []string{"read", "write"})
	store.Put(agent2)

	agent3 := NewAgent("did:example:search3", "Search Agent 3")
	agent3.AddCapability("chat", "Chat capability", []string{"send"})
	agent3.AddCapability("storage", "Storage capability", []string{"read"})
	store.Put(agent3)

	t.Run("single-match", func(t *testing.T) {
		agents, err := store.Search("storage")
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(agents) != 2 {
			t.Errorf("expected 2 agents with storage capability, got %d", len(agents))
		}
	})

	t.Run("multiple-matches", func(t *testing.T) {
		agents, err := store.Search("chat")
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(agents) != 2 {
			t.Errorf("expected 2 agents with chat capability, got %d", len(agents))
		}
	})

	t.Run("no-match", func(t *testing.T) {
		agents, err := store.Search("nonexistent")
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(agents) != 0 {
			t.Errorf("expected 0 agents, got %d", len(agents))
		}
	})
}

func TestSQLiteStore_ACL(t *testing.T) {
	store, err := NewSQLiteStore(SQLiteConfig{Path: ":memory:"})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	agent := NewAgent("did:example:acl", "ACL Agent")
	agent.ACL = &ACLPolicy{
		DefaultAllow: true,
		Rules: []ACLRule{
			{Principal: "*", Actions: []string{"read"}, Effect: "allow"},
			{Principal: "did:example:blocked", Actions: []string{"*"}, Effect: "deny"},
		},
	}

	if err := store.Put(agent); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	retrieved, err := store.Get(agent.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if retrieved.ACL == nil {
		t.Fatal("ACL is nil")
	}
	if !retrieved.ACL.DefaultAllow {
		t.Error("DefaultAllow should be true")
	}
	if len(retrieved.ACL.Rules) != 2 {
		t.Errorf("expected 2 ACL rules, got %d", len(retrieved.ACL.Rules))
	}
}

func TestSQLiteStore_PublicKeys(t *testing.T) {
	store, err := NewSQLiteStore(SQLiteConfig{Path: ":memory:"})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	agent := NewAgent("did:example:keys", "Keys Agent")
	agent.AddPublicKey("key-1", KeyTypeEd25519, []byte("fake-signing-key"), "signing")
	agent.AddPublicKey("key-2", KeyTypeX25519, []byte("fake-encryption-key"), "encryption")

	if err := store.Put(agent); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	retrieved, err := store.Get(agent.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if len(retrieved.PublicKeys) != 2 {
		t.Fatalf("expected 2 public keys, got %d", len(retrieved.PublicKeys))
	}

	signingKey := retrieved.GetSigningKey()
	if signingKey == nil {
		t.Fatal("signing key not found")
	}
	if signingKey.Type != KeyTypeEd25519 {
		t.Errorf("expected Ed25519, got %s", signingKey.Type)
	}

	encKey := retrieved.GetEncryptionKey()
	if encKey == nil {
		t.Fatal("encryption key not found")
	}
	if encKey.Type != KeyTypeX25519 {
		t.Errorf("expected X25519, got %s", encKey.Type)
	}
}

func TestSQLiteStore_Stats(t *testing.T) {
	store, err := NewSQLiteStore(SQLiteConfig{Path: ":memory:"})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Add agents with different statuses
	for i := 0; i < 3; i++ {
		agent := NewAgent("did:example:online"+string(rune('0'+i)), "Online Agent")
		agent.SetOnline()
		store.Put(agent)
	}
	for i := 0; i < 2; i++ {
		agent := NewAgent("did:example:offline"+string(rune('0'+i)), "Offline Agent")
		store.Put(agent)
	}

	stats, err := store.Stats()
	if err != nil {
		t.Fatalf("Stats failed: %v", err)
	}

	if stats["total_agents"].(int) != 5 {
		t.Errorf("expected 5 total agents, got %v", stats["total_agents"])
	}
	if stats["online_agents"].(int) != 3 {
		t.Errorf("expected 3 online agents, got %v", stats["online_agents"])
	}
	if stats["offline_agents"].(int) != 2 {
		t.Errorf("expected 2 offline agents, got %v", stats["offline_agents"])
	}
}

func TestSQLiteStore_Cleanup(t *testing.T) {
	store, err := NewSQLiteStore(SQLiteConfig{Path: ":memory:"})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Add old offline agents
	oldTime := time.Now().Add(-48 * time.Hour)
	for i := 0; i < 3; i++ {
		agent := NewAgent("did:example:old"+string(rune('0'+i)), "Old Agent")
		agent.LastSeen = oldTime
		store.Put(agent)
	}

	// Add recent agents
	for i := 0; i < 2; i++ {
		agent := NewAgent("did:example:recent"+string(rune('0'+i)), "Recent Agent")
		store.Put(agent)
	}

	// Add old but online agents (should not be cleaned up)
	onlineAgent := NewAgent("did:example:online", "Online Agent")
	onlineAgent.LastSeen = oldTime
	onlineAgent.SetOnline()
	store.Put(onlineAgent)

	// Cleanup agents older than 24 hours
	cleaned, err := store.Cleanup(time.Now().Add(-24 * time.Hour))
	if err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}

	if cleaned != 3 {
		t.Errorf("expected 3 agents cleaned up, got %d", cleaned)
	}

	// Verify remaining agents
	agents, _ := store.List()
	if len(agents) != 3 {
		t.Errorf("expected 3 remaining agents, got %d", len(agents))
	}
}

func TestSQLiteStore_Persistence(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "persist.db")

	// Create store and add agent
	store1, err := NewSQLiteStore(SQLiteConfig{Path: dbPath})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	agent := NewAgent("did:example:persist", "Persist Agent")
	agent.AddCapability("test", "Test capability", []string{"test"})
	store1.Put(agent)
	store1.Close()

	// Reopen store and verify data persisted
	store2, err := NewSQLiteStore(SQLiteConfig{Path: dbPath})
	if err != nil {
		t.Fatalf("failed to reopen store: %v", err)
	}
	defer store2.Close()

	retrieved, err := store2.GetByDID("did:example:persist")
	if err != nil {
		t.Fatalf("GetByDID failed: %v", err)
	}

	if retrieved.ID != agent.ID {
		t.Errorf("ID mismatch after persistence")
	}
	if len(retrieved.Capabilities) != 1 {
		t.Errorf("capabilities not persisted correctly")
	}
}

func TestSQLiteStore_ConcurrentAccess(t *testing.T) {
	store, err := NewSQLiteStore(SQLiteConfig{Path: ":memory:"})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Seed with initial data
	agent := NewAgent("did:example:concurrent", "Concurrent Agent")
	store.Put(agent)

	// Run concurrent operations
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(n int) {
			for j := 0; j < 100; j++ {
				// Mix of reads and writes
				if j%2 == 0 {
					store.List()
				} else {
					a := NewAgent("did:example:concurrent"+string(rune('0'+n)), "Agent")
					store.Put(a)
				}
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify store is still functional
	agents, err := store.List()
	if err != nil {
		t.Fatalf("List failed after concurrent access: %v", err)
	}
	if len(agents) == 0 {
		t.Error("expected agents after concurrent writes")
	}
}

func TestSQLiteStore_Metadata(t *testing.T) {
	store, err := NewSQLiteStore(SQLiteConfig{Path: ":memory:"})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	agent := NewAgent("did:example:metadata", "Metadata Agent")
	agent.Metadata = map[string]any{
		"version":  "1.0.0",
		"features": []string{"chat", "voice"},
		"config": map[string]any{
			"timeout": 30,
			"retries": 3,
		},
	}

	if err := store.Put(agent); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	retrieved, err := store.Get(agent.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if retrieved.Metadata["version"] != "1.0.0" {
		t.Errorf("version mismatch: got %v", retrieved.Metadata["version"])
	}

	features, ok := retrieved.Metadata["features"].([]any)
	if !ok || len(features) != 2 {
		t.Errorf("features mismatch: got %v", retrieved.Metadata["features"])
	}
}
