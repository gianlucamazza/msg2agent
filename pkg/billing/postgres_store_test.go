//go:build integration_pg

package billing

import (
	"fmt"
	"os"
	"sync"
	"testing"
	"time"
)

// pgDSN returns the Postgres DSN from environment, or skips the test.
func pgDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("BILLING_PG_DSN")
	if dsn == "" {
		t.Skip("BILLING_PG_DSN not set; skipping Postgres integration test")
	}
	return dsn
}

// newPGStore opens a PostgresStore for testing and registers cleanup.
func newPGStore(t *testing.T) *PostgresStore {
	t.Helper()
	s, err := NewPostgresStore(pgDSN(t))
	if err != nil {
		t.Fatalf("NewPostgresStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// pgFactories returns the dual-backend factory list including Postgres.
func pgFactories(t *testing.T) []struct {
	name    string
	factory storeFactory
} {
	t.Helper()
	base := factories(t)
	return append(base, struct {
		name    string
		factory storeFactory
	}{
		"postgres", func(t *testing.T) Store {
			return newPGStore(t)
		},
	})
}

func TestPostgresStore_TenantCRUD(t *testing.T) {
	s := newPGStore(t)
	tenant, err := NewTenant("PG Corp", "pg@example.com", PlanStarter)
	if err != nil {
		t.Fatalf("NewTenant: %v", err)
	}
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
		t.Errorf("Plan = %q, want starter", got.Plan)
	}
}

func TestPostgresStore_APIKeyCRUD(t *testing.T) {
	s := newPGStore(t)
	tenant, err := NewTenant("PG Key Corp", "pgkey@example.com", PlanFree)
	if err != nil {
		t.Fatalf("NewTenant: %v", err)
	}
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
		t.Errorf("ID = %q, want %q", got.ID, key.ID)
	}
}

func TestPostgresStore_OAuthIdentity(t *testing.T) {
	s := newPGStore(t)
	tenant, err := NewTenant("PG OAuth", "pgoauth@example.com", PlanFree)
	if err != nil {
		t.Fatalf("NewTenant: %v", err)
	}
	_ = s.PutTenant(tenant)

	if err := s.PutOAuthIdentity("google", "sub-pg-1", tenant.ID, "pgoauth@example.com"); err != nil {
		t.Fatalf("PutOAuthIdentity: %v", err)
	}
	got, err := s.GetOAuthIdentityTenant("google", "sub-pg-1")
	if err != nil {
		t.Fatalf("GetOAuthIdentityTenant: %v", err)
	}
	if got != tenant.ID {
		t.Errorf("tenant ID = %q, want %q", got, tenant.ID)
	}
	if _, err := s.GetOAuthIdentityTenant("google", "unknown-sub"); err != ErrOAuthIdentityNotFound {
		t.Errorf("unknown sub: want ErrOAuthIdentityNotFound, got %v", err)
	}
}

// TestPostgresHashChainConcurrent verifies that 50 concurrent RecordEvent calls
// for the same tenant produce a valid, untampered audit chain.
func TestPostgresHashChainConcurrent(t *testing.T) {
	s := newPGStore(t)
	tenant, err := NewTenant("Hash Chain Corp", "hashchain@example.com", PlanTeam)
	if err != nil {
		t.Fatalf("NewTenant: %v", err)
	}
	if err := s.PutTenant(tenant); err != nil {
		t.Fatalf("PutTenant: %v", err)
	}

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	errs := make([]error, goroutines)
	for i := range goroutines {
		go func(i int) {
			defer wg.Done()
			errs[i] = s.RecordEvent(tenant.ID, string(EventMessage), "send_message",
				fmt.Sprintf("req-%d", i))
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: RecordEvent: %v", i, err)
		}
	}

	results, err := s.VerifyAuditChain(tenant.ID)
	if err != nil {
		t.Fatalf("VerifyAuditChain: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("VerifyAuditChain returned no results")
	}
	r := results[0]
	if r.Tampered {
		t.Errorf("chain tampered: first bad ID = %q at %v", r.FirstBadID, r.FirstBadTime)
	}
	if r.Verified != goroutines {
		t.Errorf("verified = %d, want %d", r.Verified, goroutines)
	}
}

func TestPostgresStore_EventStore(t *testing.T) {
	s := newPGStore(t)
	tenant, err := NewTenant("PG Events Corp", "pgevents@example.com", PlanFree)
	if err != nil {
		t.Fatalf("NewTenant: %v", err)
	}
	_ = s.PutTenant(tenant)

	period := periodKey(time.Now().UTC())
	if err := s.RecordEvent(tenant.ID, string(EventMessage), "send_message", "req-pg-1"); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}

	snaps := []UsageSnapshot{{TenantID: tenant.ID, Period: period, Event: EventMessage, Count: 9}}
	if err := s.FlushAggregates(snaps); err != nil {
		t.Fatalf("FlushAggregates: %v", err)
	}

	loaded, err := s.LoadAggregates()
	if err != nil {
		t.Fatalf("LoadAggregates: %v", err)
	}
	var found bool
	for _, snap := range loaded {
		if snap.TenantID == tenant.ID && snap.Period == period {
			found = true
			if snap.Count != 9 {
				t.Errorf("aggregate count = %d, want 9", snap.Count)
			}
		}
	}
	if !found {
		t.Error("aggregate not found after FlushAggregates")
	}
}
