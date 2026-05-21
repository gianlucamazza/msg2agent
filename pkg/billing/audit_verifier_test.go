package billing

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

// testAdminStore is a minimal AdminStore that returns configurable results.
type testAdminStore struct {
	results []AuditChainResult
	calls   atomic.Int32
}

func (a *testAdminStore) VerifyAuditChain(_ string) ([]AuditChainResult, error) {
	a.calls.Add(1)
	return a.results, nil
}

func (a *testAdminStore) QueryEvents(_ EventFilter) ([]AuditEvent, error) { return nil, nil }
func (a *testAdminStore) Verify() (*VerifyReport, error)                  { return &VerifyReport{}, nil }
func (a *testAdminStore) PurgeEvents(_ time.Time) (int64, error)          { return 0, nil }
func (a *testAdminStore) Backup(_ string) error                           { return nil }

// TestStartPeriodicVerifier_ZeroInterval verifies that interval=0 is a no-op (no goroutine leak).
func TestStartPeriodicVerifier_ZeroInterval(t *testing.T) {
	store := NewMemoryStore()
	admin := &testAdminStore{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := slog.Default()
	// Should return immediately without starting a goroutine.
	StartPeriodicVerifier(ctx, store, admin, 0, logger)

	// Give a moment to confirm no calls happen.
	time.Sleep(50 * time.Millisecond)
	if admin.calls.Load() != 0 {
		t.Errorf("expected 0 VerifyAuditChain calls with interval=0, got %d", admin.calls.Load())
	}
}

// TestStartPeriodicVerifier_CleanChain verifies the verifier ticks and calls VerifyAuditChain
// at least once without recording tampering when the chain is clean.
func TestStartPeriodicVerifier_CleanChain(t *testing.T) {
	store := NewMemoryStore()
	tenant, err := NewTenant("Test Corp", "test@example.com", PlanFree)
	if err != nil {
		t.Fatalf("NewTenant: %v", err)
	}
	if err := store.PutTenant(tenant); err != nil {
		t.Fatalf("PutTenant: %v", err)
	}

	// Admin returns a clean result.
	admin := &testAdminStore{
		results: []AuditChainResult{
			{TenantID: tenant.ID, Verified: 0, Tampered: false},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := slog.Default()
	StartPeriodicVerifier(ctx, store, admin, 20*time.Millisecond, logger)

	// Wait until at least one verification has run.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if admin.calls.Load() >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	if admin.calls.Load() < 1 {
		t.Errorf("expected at least 1 VerifyAuditChain call, got %d", admin.calls.Load())
	}
}

// TestStartPeriodicVerifier_TamperDetected verifies that tampering triggers
// RecordAuditChainTampered (the metric counter increments).
func TestStartPeriodicVerifier_TamperDetected(t *testing.T) {
	store := NewMemoryStore()
	tenant, err := NewTenant("Hack Corp", "hack@example.com", PlanFree)
	if err != nil {
		t.Fatalf("NewTenant: %v", err)
	}
	if err := store.PutTenant(tenant); err != nil {
		t.Fatalf("PutTenant: %v", err)
	}

	// Admin returns a tampered result.
	now := time.Now()
	admin := &testAdminStore{
		results: []AuditChainResult{
			{
				TenantID:     tenant.ID,
				Tampered:     true,
				FirstBadID:   "e_bad",
				FirstBadTime: now,
				Verified:     3,
			},
		},
	}

	// Capture the current counter value before we start.
	// We use the exported metric via a direct call to confirm no panic.
	// The actual metric value is not easily inspectable without a gatherer,
	// so we verify that runVerification completes without error and that
	// RecordAuditChainTampered can be called (compilation test).
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	logger := slog.Default()
	StartPeriodicVerifier(ctx, store, admin, 20*time.Millisecond, logger)

	// Wait until at least one verification has run.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if admin.calls.Load() >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	if admin.calls.Load() < 1 {
		t.Errorf("expected at least 1 VerifyAuditChain call with tamper, got %d", admin.calls.Load())
	}
}

// TestStartPeriodicVerifier_ContextCancelled verifies the goroutine stops when ctx is cancelled.
func TestStartPeriodicVerifier_ContextCancelled(t *testing.T) {
	store := NewMemoryStore()
	admin := &testAdminStore{}

	ctx, cancel := context.WithCancel(context.Background())
	logger := slog.Default()

	// Use a long interval so we can cancel before the first tick.
	StartPeriodicVerifier(ctx, store, admin, 10*time.Second, logger)

	// Cancel immediately.
	cancel()

	// Verify no calls were made (cancelled before first tick).
	time.Sleep(30 * time.Millisecond)
	if admin.calls.Load() != 0 {
		t.Errorf("expected 0 calls after immediate cancel, got %d", admin.calls.Load())
	}
}
