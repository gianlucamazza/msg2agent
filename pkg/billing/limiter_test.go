package billing

import (
	"testing"
	"time"
)

func TestTenantBucket_Allow(t *testing.T) {
	// Burst of 5, rate=5/sec
	b := newTenantBucket(5, 5)

	// Should allow 5 tokens immediately.
	for i := range 5 {
		if !b.Allow() {
			t.Errorf("Allow() = false at token %d, want true", i)
		}
	}
	// 6th token should fail (bucket empty).
	if b.Allow() {
		t.Error("Allow() = true when bucket empty, want false")
	}
}

func TestTenantBucket_refill(t *testing.T) {
	b := newTenantBucket(100, 1) // 1 burst, 100/sec
	if !b.Allow() {
		t.Fatal("first Allow() failed")
	}
	if b.Allow() {
		t.Error("second Allow() should fail")
	}

	// Simulate time passing by manipulating lastUpdate.
	b.mu.Lock()
	b.lastUpdate = time.Now().Add(-100 * time.Millisecond) // ~10 tokens at 100/sec
	b.mu.Unlock()

	if !b.Allow() {
		t.Error("Allow() after refill should succeed")
	}
}

func TestTenantRateLimiterPool_Allow(t *testing.T) {
	store := NewMemoryStore()
	tenant := NewTenant("T1", "t1@example.com", PlanStarter)
	// PlanStarter: 10/sec, burst 50
	_ = store.PutTenant(tenant)

	pool := NewTenantRateLimiterPool(store)

	// Burst allows many rapid calls up to burst size.
	allowed := 0
	for range 60 {
		if pool.Allow(tenant.ID) {
			allowed++
		}
	}
	// Should allow up to burst (50) but not all 60.
	if allowed > 50 {
		t.Errorf("allowed %d > burst 50", allowed)
	}
	if allowed < 10 {
		t.Errorf("allowed %d < expected minimum", allowed)
	}
}

func TestTenantRateLimiterPool_Evict(t *testing.T) {
	store := NewMemoryStore()
	tenant := NewTenant("T2", "t2@example.com", PlanFree)
	_ = store.PutTenant(tenant)

	pool := NewTenantRateLimiterPool(store)
	pool.Allow(tenant.ID) // create bucket

	pool.mu.RLock()
	_, existed := pool.buckets[tenant.ID]
	pool.mu.RUnlock()
	if !existed {
		t.Fatal("bucket not created after Allow()")
	}

	pool.Evict(tenant.ID)

	pool.mu.RLock()
	_, still := pool.buckets[tenant.ID]
	pool.mu.RUnlock()
	if still {
		t.Error("bucket still present after Evict()")
	}
}

func TestTenantRateLimiterPool_unknownTenant_usesDefaults(t *testing.T) {
	store := NewMemoryStore()
	pool := NewTenantRateLimiterPool(store)

	// Unknown tenant should use default rate (5/sec, burst 20).
	// Should still allow calls without panic.
	allowed := 0
	for range 25 {
		if pool.Allow("unknown") {
			allowed++
		}
	}
	if allowed == 0 {
		t.Error("unknown tenant: no calls allowed (expected some from default burst)")
	}
}
