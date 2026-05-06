package billing

import (
	"testing"
	"time"
)

// Tests run against both MemoryStore and SQLiteStore via the storeFactory type.

type storeFactory func(t *testing.T) Store

func factories(t *testing.T) []struct {
	name    string
	factory storeFactory
} {
	t.Helper()
	return []struct {
		name    string
		factory storeFactory
	}{
		{"memory", func(t *testing.T) Store { return NewMemoryStore() }},
		{"sqlite", func(t *testing.T) Store {
			s, err := NewSQLiteStore(":memory:")
			if err != nil {
				t.Fatalf("NewSQLiteStore: %v", err)
			}
			t.Cleanup(func() { _ = s.Close() })
			return s
		}},
	}
}

func TestStore_TenantCRUD(t *testing.T) {
	for _, f := range factories(t) {
		t.Run(f.name, func(t *testing.T) {
			s := f.factory(t)

			tenant := NewTenant("Acme Corp", "acme@example.com", PlanStarter)
			if err := s.PutTenant(tenant); err != nil {
				t.Fatalf("PutTenant: %v", err)
			}

			got, err := s.GetTenant(tenant.ID)
			if err != nil {
				t.Fatalf("GetTenant: %v", err)
			}
			if got.Name != tenant.Name {
				t.Errorf("Name = %q, want %q", got.Name, tenant.Name)
			}
			if got.Plan != PlanStarter {
				t.Errorf("Plan = %q, want %q", got.Plan, PlanStarter)
			}
			if got.Status != TenantStatusActive {
				t.Errorf("Status = %q, want %q", got.Status, TenantStatusActive)
			}
		})
	}
}

func TestStore_SuspendTenant(t *testing.T) {
	for _, f := range factories(t) {
		t.Run(f.name, func(t *testing.T) {
			s := f.factory(t)

			tenant := NewTenant("Beta Corp", "beta@example.com", PlanFree)
			_ = s.PutTenant(tenant)

			if err := s.SuspendTenant(tenant.ID); err != nil {
				t.Fatalf("SuspendTenant: %v", err)
			}
			got, _ := s.GetTenant(tenant.ID)
			if got.Status != TenantStatusSuspended {
				t.Errorf("Status = %q, want suspended", got.Status)
			}
			if got.IsActive() {
				t.Error("suspended tenant should not be active")
			}
		})
	}
}

func TestStore_SuspendTenant_notFound(t *testing.T) {
	for _, f := range factories(t) {
		t.Run(f.name, func(t *testing.T) {
			s := f.factory(t)
			if err := s.SuspendTenant("nonexistent"); err == nil {
				t.Error("expected error for nonexistent tenant")
			}
		})
	}
}

func TestStore_UpdateTenant(t *testing.T) {
	for _, f := range factories(t) {
		t.Run(f.name, func(t *testing.T) {
			s := f.factory(t)

			tenant := NewTenant("Gamma Corp", "gamma@example.com", PlanFree)
			_ = s.PutTenant(tenant)

			tenant.Plan = PlanTeam
			tenant.Quota = DefaultQuota(PlanTeam)
			if err := s.UpdateTenant(tenant); err != nil {
				t.Fatalf("UpdateTenant: %v", err)
			}

			got, _ := s.GetTenant(tenant.ID)
			if got.Plan != PlanTeam {
				t.Errorf("Plan = %q, want team", got.Plan)
			}
		})
	}
}

func TestStore_APIKeyCRUD(t *testing.T) {
	for _, f := range factories(t) {
		t.Run(f.name, func(t *testing.T) {
			s := f.factory(t)

			tenant := NewTenant("Delta Corp", "delta@example.com", PlanStarter)
			_ = s.PutTenant(tenant)

			plaintext, key, err := GenerateAPIKey(tenant.ID, "ci")
			if err != nil {
				t.Fatalf("GenerateAPIKey: %v", err)
			}
			if err := s.PutAPIKey(key); err != nil {
				t.Fatalf("PutAPIKey: %v", err)
			}

			hash, _ := HashAPIKey(plaintext)
			got, err := s.GetAPIKeyByHash(hash)
			if err != nil {
				t.Fatalf("GetAPIKeyByHash: %v", err)
			}
			if got.ID != key.ID {
				t.Errorf("key ID = %q, want %q", got.ID, key.ID)
			}
			if !got.IsValid() {
				t.Error("fresh key should be valid")
			}
		})
	}
}

func TestStore_RevokeAPIKey(t *testing.T) {
	for _, f := range factories(t) {
		t.Run(f.name, func(t *testing.T) {
			s := f.factory(t)

			tenant := NewTenant("Epsilon Corp", "eps@example.com", PlanFree)
			_ = s.PutTenant(tenant)

			plaintext, key, _ := GenerateAPIKey(tenant.ID, "prod")
			_ = s.PutAPIKey(key)

			if err := s.RevokeAPIKey(key.ID); err != nil {
				t.Fatalf("RevokeAPIKey: %v", err)
			}

			hash, _ := HashAPIKey(plaintext)
			got, _ := s.GetAPIKeyByHash(hash)
			if got.IsValid() {
				t.Error("revoked key should be invalid")
			}
		})
	}
}

func TestStore_ListAPIKeysActive(t *testing.T) {
	for _, f := range factories(t) {
		t.Run(f.name, func(t *testing.T) {
			s := f.factory(t)

			tenant := NewTenant("Zeta Corp", "zeta@example.com", PlanFree)
			_ = s.PutTenant(tenant)

			_, k1, _ := GenerateAPIKey(tenant.ID, "active-key")
			_, k2, _ := GenerateAPIKey(tenant.ID, "to-revoke")
			_ = s.PutAPIKey(k1)
			_ = s.PutAPIKey(k2)
			_ = s.RevokeAPIKey(k2.ID)

			active, err := s.ListAPIKeysActive(tenant.ID)
			if err != nil {
				t.Fatalf("ListAPIKeysActive: %v", err)
			}
			if len(active) != 1 {
				t.Errorf("active keys = %d, want 1", len(active))
			}
			if active[0].ID != k1.ID {
				t.Errorf("active key ID = %q, want %q", active[0].ID, k1.ID)
			}
		})
	}
}

func TestStore_GetTenant_notFound(t *testing.T) {
	for _, f := range factories(t) {
		t.Run(f.name, func(t *testing.T) {
			s := f.factory(t)
			_, err := s.GetTenant("nonexistent")
			if err == nil {
				t.Error("expected error for missing tenant")
			}
		})
	}
}

func TestSQLiteStore_EventStore(t *testing.T) {
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	tenant := NewTenant("Eta Corp", "eta@example.com", PlanFree)
	_ = s.PutTenant(tenant)

	period := periodKey(time.Now().UTC())
	if err := s.RecordEvent(tenant.ID, string(EventMessage), "send_message", "req1"); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}

	snaps := []UsageSnapshot{{TenantID: tenant.ID, Period: period, Event: EventMessage, Count: 7}}
	if err := s.FlushAggregates(snaps); err != nil {
		t.Fatalf("FlushAggregates: %v", err)
	}

	loaded, err := s.LoadAggregates()
	if err != nil {
		t.Fatalf("LoadAggregates: %v", err)
	}
	if len(loaded) == 0 {
		t.Fatal("LoadAggregates returned empty")
	}
	if loaded[0].Count != 7 {
		t.Errorf("aggregate count = %d, want 7", loaded[0].Count)
	}

	// Upsert same period should overwrite count.
	snaps[0].Count = 15
	_ = s.FlushAggregates(snaps)
	loaded2, _ := s.LoadAggregates()
	if loaded2[0].Count != 15 {
		t.Errorf("upsert aggregate count = %d, want 15", loaded2[0].Count)
	}
}

func TestSQLiteStore_WALAndFK(t *testing.T) {
	// WAL mode requires a file-based DB; :memory: always reports "memory".
	dir := t.TempDir()
	s, err := NewSQLiteStore(dir + "/billing_test.db")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	// WAL mode should be active.
	var mode string
	if err := s.db.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatalf("PRAGMA journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %q, want wal", mode)
	}

	// Foreign key enforcement: inserting a key for a non-existent tenant must fail.
	_, fkErr := s.db.Exec(
		`INSERT INTO api_keys(id,tenant_id,name,key_hash,prefix,created_at) VALUES('k1','nonexistent','default','hash','pre','2026-01-01T00:00:00Z')`,
	)
	if fkErr == nil {
		t.Error("expected FK constraint error for orphan api_key, got nil")
	}
}

func TestStore_OAuthIdentity(t *testing.T) {
	for _, f := range factories(t) {
		t.Run(f.name, func(t *testing.T) {
			store := f.factory(t)
			tenant := NewTenant("OAuth Corp", "oauth@example.com", PlanFree)
			if err := store.PutTenant(tenant); err != nil {
				t.Fatalf("PutTenant: %v", err)
			}

			// Insert identity and retrieve tenant.
			if err := store.PutOAuthIdentity("google", "sub-123", tenant.ID, "oauth@example.com"); err != nil {
				t.Fatalf("PutOAuthIdentity: %v", err)
			}
			got, err := store.GetOAuthIdentityTenant("google", "sub-123")
			if err != nil {
				t.Fatalf("GetOAuthIdentityTenant: %v", err)
			}
			if got != tenant.ID {
				t.Errorf("tenantID = %q, want %q", got, tenant.ID)
			}

			// Unknown identity returns ErrOAuthIdentityNotFound.
			if _, err := store.GetOAuthIdentityTenant("google", "unknown"); err != ErrOAuthIdentityNotFound {
				t.Errorf("unknown sub: want ErrOAuthIdentityNotFound, got %v", err)
			}

			// Upsert updates email.
			if err := store.PutOAuthIdentity("google", "sub-123", tenant.ID, "new@example.com"); err != nil {
				t.Fatalf("PutOAuthIdentity upsert: %v", err)
			}
		})
	}
}

func TestSQLiteStore_OAuthIdentity_FK(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSQLiteStore(dir + "/billing_fk_test.db")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	// Inserting with a non-existent tenant_id must fail (FK constraint).
	err = s.PutOAuthIdentity("google", "sub-nonexistent", "nonexistent_tenant", "x@x.com")
	if err == nil {
		t.Error("expected FK constraint violation, got nil")
	}
}

func TestSQLiteStore_SchemaVersioning(t *testing.T) {
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	var version int
	if err := s.db.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil {
		t.Fatalf("schema_migrations: %v", err)
	}
	if version != len(migrations) {
		t.Errorf("schema version = %d, want %d", version, len(migrations))
	}

	// Calling migrate again must be a no-op (idempotent).
	if err := s.migrate(); err != nil {
		t.Errorf("second migrate() returned error: %v", err)
	}
	var version2 int
	_ = s.db.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&version2)
	if version2 != version {
		t.Errorf("version changed after second migrate: %d → %d", version, version2)
	}
}
