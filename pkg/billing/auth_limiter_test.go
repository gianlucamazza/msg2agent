package billing

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestAuthLimiter_allowsUpToCapacity(t *testing.T) {
	al := &authLimiter{buckets: make(map[string]*authBucket)}
	ip := "1.2.3.4"
	// Consume all 10 tokens.
	b := al.getOrCreate(ip)
	b.tokens = authBucketCapacity
	for i := 0; i < int(authBucketCapacity); i++ {
		if !al.Allow(ip) {
			t.Fatalf("expected allow at attempt %d", i+1)
		}
		al.Consume(ip)
	}
	// 11th should be denied.
	if al.Allow(ip) {
		t.Error("expected denial after capacity exhausted")
	}
}

func TestAuthLimiter_refillsOverTime(t *testing.T) {
	al := &authLimiter{buckets: make(map[string]*authBucket)}
	ip := "2.3.4.5"
	b := al.getOrCreate(ip)
	b.tokens = 0
	// Simulate 2 seconds elapsed.
	b.lastUpdate = time.Now().Add(-2 * time.Second)
	// Should have 2 tokens refilled.
	if !al.Allow(ip) {
		t.Error("expected allow after refill")
	}
}

func TestRealIP(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	if got := realIP(req); got != "10.0.0.1" {
		t.Errorf("realIP = %q, want 10.0.0.1", got)
	}
	req.Header.Set("X-Real-IP", "203.0.113.5")
	if got := realIP(req); got != "203.0.113.5" {
		t.Errorf("realIP with X-Real-IP = %q, want 203.0.113.5", got)
	}
}
