// Package test provides integration tests for msg2agent.
package test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gianlucamazza/msg2agent/pkg/billing"
)

// TestBillingE2E exercises the full billing lifecycle in-process:
// create tenant → issue key → record events → restart (restore) → export CSV.
func TestBillingE2E(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "billing.db")

	// --- Phase 1: create tenant, issue key, record events ---
	store, err := billing.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}

	tenant := billing.NewTenant("E2E Corp", "e2e@example.com", billing.PlanStarter)
	if err := store.PutTenant(tenant); err != nil {
		t.Fatalf("PutTenant: %v", err)
	}

	plaintext, key, err := billing.GenerateAPIKey(tenant.ID, "test")
	if err != nil {
		t.Fatalf("GenerateAPIKey: %v", err)
	}
	if err := store.PutAPIKey(key); err != nil {
		t.Fatalf("PutAPIKey: %v", err)
	}

	// Verify the key can be looked up by hash.
	hash, err := billing.HashAPIKey(plaintext)
	if err != nil {
		t.Fatalf("HashAPIKey: %v", err)
	}
	found, err := store.GetAPIKeyByHash(hash)
	if err != nil {
		t.Fatalf("GetAPIKeyByHash: %v", err)
	}
	if found.TenantID != tenant.ID {
		t.Errorf("key tenant mismatch: got %s, want %s", found.TenantID, tenant.ID)
	}

	// Set up meter with async persistence.
	ctx, cancel := context.WithCancel(context.Background())
	meter := billing.NewUsageMeter()
	meter.WithStore(ctx, store, nil)

	// Record 5 messages and 2 tool calls.
	for i := range 5 {
		meter.RecordAudit(tenant.ID, billing.EventMessage, "send_message", "", 1)
		_ = i
	}
	for i := range 2 {
		meter.RecordAudit(tenant.ID, billing.EventToolCall, "list_agents", "", 1)
		_ = i
	}

	// Verify in-memory counts.
	if got := meter.Current(tenant.ID, billing.EventMessage); got != 5 {
		t.Errorf("EventMessage count = %d, want 5", got)
	}
	if got := meter.Current(tenant.ID, billing.EventToolCall); got != 2 {
		t.Errorf("EventToolCall count = %d, want 2", got)
	}

	// Quota not exceeded for starter plan (10k messages).
	if err := meter.CheckQuota(tenant.ID, billing.EventMessage, tenant.Quota.MaxMessagesPerMonth); err != nil {
		t.Errorf("CheckQuota unexpectedly exceeded: %v", err)
	}

	// Force-flush the in-memory snapshot to the DB so restart can restore it.
	if err := store.FlushAggregates(meter.Snapshot()); err != nil {
		t.Fatalf("FlushAggregates: %v", err)
	}
	cancel()
	store.Close()

	// --- Phase 2: restart — restore counters from aggregates ---
	store2, err := billing.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer store2.Close()

	meter2 := billing.NewUsageMeter()
	if err := meter2.RestoreFromAggregates(store2); err != nil {
		t.Fatalf("RestoreFromAggregates: %v", err)
	}

	// Counters must survive the restart.
	if got := meter2.Current(tenant.ID, billing.EventMessage); got != 5 {
		t.Errorf("post-restart EventMessage = %d, want 5", got)
	}
	if got := meter2.Current(tenant.ID, billing.EventToolCall); got != 2 {
		t.Errorf("post-restart EventToolCall = %d, want 2", got)
	}

	// --- Phase 3: CSV export ---
	var buf bytes.Buffer
	if err := billing.ExportCSV(&buf, "", store2); err != nil {
		t.Fatalf("ExportCSV: %v", err)
	}

	csv := buf.String()
	if !strings.Contains(csv, "tenant_id,period,event,count") {
		t.Errorf("CSV missing header; got:\n%s", csv)
	}
	if !strings.Contains(csv, tenant.ID) {
		t.Errorf("CSV missing tenant ID %s; got:\n%s", tenant.ID, csv)
	}
	if !strings.Contains(csv, "message") {
		t.Errorf("CSV missing 'message' event; got:\n%s", csv)
	}

	// Ensure the temp DB file is present (sanity).
	if _, err := os.Stat(dbPath); err != nil {
		t.Errorf("billing.db missing: %v", err)
	}
}

// TestBillingE2E_QuotaExceeded verifies that quota enforcement fires correctly
// after an in-process restart (counters restored from aggregates).
func TestBillingE2E_QuotaExceeded(t *testing.T) {
	dir := t.TempDir()
	store, err := billing.NewSQLiteStore(filepath.Join(dir, "billing.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	// Free plan: 1000 messages/month.
	tenant := billing.NewTenant("Quota Corp", "q@example.com", billing.PlanFree)
	if err := store.PutTenant(tenant); err != nil {
		t.Fatalf("PutTenant: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	meter := billing.NewUsageMeter()
	meter.WithStore(ctx, store, nil)

	limit := tenant.Quota.MaxMessagesPerMonth // 1000 for PlanFree

	// Record limit-1 messages.
	meter.Record(tenant.ID, billing.EventMessage, limit-1)

	// One more should pass.
	if err := meter.CheckQuota(tenant.ID, billing.EventMessage, limit); err != nil {
		t.Errorf("expected pass at limit-1, got: %v", err)
	}

	// Record one more to reach the limit.
	meter.Record(tenant.ID, billing.EventMessage, 1)

	// Now quota should be exceeded.
	if err := meter.CheckQuota(tenant.ID, billing.EventMessage, limit); err == nil {
		t.Error("expected quota exceeded error, got nil")
	}
}
