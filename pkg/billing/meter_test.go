package billing

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestUsageMeter_Record(t *testing.T) {
	m := NewUsageMeter()
	m.Record("t1", EventMessage, 1)
	m.Record("t1", EventMessage, 4)
	m.Record("t2", EventMessage, 1)

	if got := m.Current("t1", EventMessage); got != 5 {
		t.Errorf("t1 EventMessage = %d, want 5", got)
	}
	if got := m.Current("t2", EventMessage); got != 1 {
		t.Errorf("t2 EventMessage = %d, want 1", got)
	}
	if got := m.Current("t1", EventToolCall); got != 0 {
		t.Errorf("t1 EventToolCall = %d, want 0", got)
	}
}

func TestUsageMeter_concurrency(t *testing.T) {
	m := NewUsageMeter()
	const goroutines = 100
	const perGoroutine = 100

	var wg sync.WaitGroup
	for i := range goroutines {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for range perGoroutine {
				m.Record(fmt.Sprintf("t%d", i%5), EventMessage, 1)
			}
		}(i)
	}
	wg.Wait()

	var total int64
	for i := range 5 {
		total += m.Current(fmt.Sprintf("t%d", i), EventMessage)
	}
	if total != goroutines*perGoroutine {
		t.Errorf("total = %d, want %d", total, goroutines*perGoroutine)
	}
}

func TestUsageMeter_CheckQuota(t *testing.T) {
	m := NewUsageMeter()
	const limit = 10

	for range limit - 1 {
		m.Record("t1", EventMessage, 1)
	}
	if err := m.CheckQuota("t1", EventMessage, limit); err != nil {
		t.Errorf("under-limit: unexpected error: %v", err)
	}

	m.Record("t1", EventMessage, 1) // at limit
	if err := m.CheckQuota("t1", EventMessage, limit); err == nil {
		t.Error("at limit: expected ErrQuotaExceeded, got nil")
	}
}

func TestUsageMeter_CheckQuota_zero_skips(t *testing.T) {
	m := NewUsageMeter()
	m.Record("t1", EventMessage, 999)
	if err := m.CheckQuota("t1", EventMessage, 0); err != nil {
		t.Errorf("limit=0 should be no-op, got %v", err)
	}
}

func TestPeriodKey_UTC(t *testing.T) {
	ts := time.Date(2026, 5, 15, 23, 59, 59, 0, time.UTC)
	if got := periodKey(ts); got != "2026-05" {
		t.Errorf("periodKey = %q, want %q", got, "2026-05")
	}
	// Local time in a different timezone must not affect UTC period
	loc := time.FixedZone("UTC+14", 14*3600)
	ts2 := time.Date(2026, 5, 1, 0, 30, 0, 0, loc) // May 1st UTC+14 = April 30th UTC
	if got := periodKey(ts2.UTC()); got != "2026-04" {
		t.Errorf("UTC conversion: got %q, want %q", got, "2026-04")
	}
}

func TestUsageMeter_RestoreFromAggregates(t *testing.T) {
	store := newMemEventStore()
	store.aggregates = []UsageSnapshot{
		{TenantID: "t1", Period: periodKey(time.Now().UTC()), Event: EventMessage, Count: 42},
	}

	m := NewUsageMeter()
	if err := m.RestoreFromAggregates(store); err != nil {
		t.Fatalf("RestoreFromAggregates: %v", err)
	}
	if got := m.Current("t1", EventMessage); got != 42 {
		t.Errorf("restored count = %d, want 42", got)
	}
}

func TestUsageMeter_WithStore_flushes(t *testing.T) {
	store := newMemEventStore()
	m := NewUsageMeter()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.WithStore(ctx, store, nil)

	// Send 100 events to trigger a batch flush (batchSize = 100).
	for i := range 100 {
		m.RecordAudit("t1", EventMessage, "send_message", fmt.Sprintf("req%d", i), 1)
	}

	// Give the goroutine time to process the full batch.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		store.mu.Lock()
		n := len(store.events)
		store.mu.Unlock()
		if n >= 100 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	store.mu.Lock()
	n := len(store.events)
	store.mu.Unlock()
	if n < 100 {
		t.Errorf("expected 100 audit events persisted, got %d", n)
	}
}

func TestUsageMeter_ChannelFull_noBlock(t *testing.T) {
	// Fill the channel; the 1001st Record must not deadlock.
	m := NewUsageMeter()
	// Create a tiny channel to simulate full buffer.
	m.eventCh = make(chan auditEvent, 1)

	done := make(chan struct{})
	go func() {
		for range 5 {
			m.RecordAudit("t1", EventMessage, "send_message", "", 1)
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("RecordAudit blocked on full channel")
	}
}

// memEventStore is a minimal in-memory EventStore for testing.
type memEventStore struct {
	mu         sync.Mutex
	events     []auditEvent
	aggregates []UsageSnapshot
}

func newMemEventStore() *memEventStore { return &memEventStore{} }

func (s *memEventStore) RecordEvent(tenantID, event, toolName, requestID string) error {
	s.mu.Lock()
	s.events = append(s.events, auditEvent{tenantID: tenantID, event: event, toolName: toolName, requestID: requestID})
	s.mu.Unlock()
	return nil
}

func (s *memEventStore) LoadAggregates() ([]UsageSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.aggregates, nil
}

func (s *memEventStore) FlushAggregates(snaps []UsageSnapshot) error {
	s.mu.Lock()
	s.aggregates = snaps
	s.mu.Unlock()
	return nil
}
