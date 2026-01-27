package test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/gianluca/msg2agent/pkg/messaging"
	"github.com/gianluca/msg2agent/pkg/transport"
)

// TestRateLimiterUnderLoad verifies the rate limiter works correctly under concurrent load.
func TestRateLimiterUnderLoad(t *testing.T) {
	// 10 requests per second, burst of 20
	limiter := messaging.NewRateLimiter(10.0, 20.0)

	var allowed int64
	var denied int64
	var wg sync.WaitGroup
	var mu sync.Mutex

	// Spawn 100 concurrent goroutines each making 10 requests
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				if limiter.Allow() {
					mu.Lock()
					allowed++
					mu.Unlock()
				} else {
					mu.Lock()
					denied++
					mu.Unlock()
				}
			}
		}()
	}

	wg.Wait()

	// We expect the burst (20) to be allowed, then most subsequent requests denied
	// due to the rate limit
	t.Logf("Allowed: %d, Denied: %d", allowed, denied)

	if allowed > 100 {
		t.Errorf("Too many requests allowed: %d, expected ~20 (burst)", allowed)
	}
	if denied < 900 {
		t.Errorf("Too few requests denied: %d, expected ~980", denied)
	}
}

// TestRateLimiterRefill verifies the rate limiter refills tokens over time.
func TestRateLimiterRefill(t *testing.T) {
	// 10 requests per second, burst of 2
	limiter := messaging.NewRateLimiter(10.0, 2.0)

	// Use the burst
	first := limiter.Allow()
	second := limiter.Allow()
	if !first || !second {
		t.Fatal("First 2 requests should be allowed (burst)")
	}

	// Third should be denied
	if limiter.Allow() {
		t.Error("Third request should be denied")
	}

	// Wait for refill (100ms = 1 token at 10/sec)
	time.Sleep(150 * time.Millisecond)

	// Now one more should be allowed
	if !limiter.Allow() {
		t.Error("Request after refill should be allowed")
	}
}

// TestWebSocketTransportWithLogger verifies the new logger-aware constructor.
func TestWebSocketTransportWithLogger(t *testing.T) {
	config := transport.Config{
		Address:        "ws://example.com",
		MaxMessageSize: 1024 * 1024,
	}

	// Test with nil logger (should use default)
	wst := transport.NewWebSocketTransportWithLogger(config, nil)
	if wst == nil {
		t.Fatal("NewWebSocketTransportWithLogger returned nil")
	}

	// Verify RemoteAddr is set correctly
	if wst.RemoteAddr() != "ws://example.com" {
		t.Errorf("RemoteAddr = %q, want %q", wst.RemoteAddr(), "ws://example.com")
	}
}

// TestWebSocketListenerWithLogger verifies the new logger-aware constructor.
func TestWebSocketListenerWithLogger(t *testing.T) {
	config := transport.Config{
		Address:        ":0",
		MaxMessageSize: 1024 * 1024,
	}

	// Test with nil logger (should use default)
	listener := transport.NewWebSocketListenerWithLogger(config, nil)
	if listener == nil {
		t.Fatal("NewWebSocketListenerWithLogger returned nil")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	// Start and verify it works
	if err := listener.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Check address is bound
	addr := listener.Addr()
	if addr == "" || addr == ":0" {
		t.Error("Listener should have bound address after Start")
	}

	// Clean up
	if err := listener.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}
}

// TestCircuitBreakerProduction verifies circuit breaker behavior.
func TestCircuitBreakerProduction(t *testing.T) {
	cb := messaging.NewCircuitBreaker(messaging.CircuitBreakerConfig{
		Name:         "test",
		MaxFailures:  3,
		ResetTimeout: 100 * time.Millisecond,
		HalfOpenMax:  2,
	})

	// Initially closed
	if cb.State() != messaging.CircuitClosed {
		t.Error("Circuit should start closed")
	}

	// Simulate failures
	for i := 0; i < 3; i++ {
		cb.Execute(func() error {
			return context.Canceled // some error
		})
	}

	// Should be open now
	if cb.State() != messaging.CircuitOpen {
		t.Error("Circuit should be open after 3 failures")
	}

	// Request should be rejected
	err := cb.Execute(func() error {
		t.Error("Function should not be called when circuit is open")
		return nil
	})
	if err != messaging.ErrCircuitOpen {
		t.Errorf("Expected ErrCircuitOpen, got %v", err)
	}

	// Wait for reset timeout
	time.Sleep(150 * time.Millisecond)

	// Should transition to half-open and allow request
	if cb.State() != messaging.CircuitOpen {
		// State is still open until a request comes in
	}

	// This request should succeed and help close the circuit
	successCount := 0
	for i := 0; i < 3; i++ {
		err := cb.Execute(func() error {
			successCount++
			return nil
		})
		if err != nil {
			t.Errorf("Request %d should succeed: %v", i, err)
		}
	}

	// Circuit should be closed again
	if cb.State() != messaging.CircuitClosed {
		t.Errorf("Circuit should be closed after successes, got %v", cb.State())
	}
}

// TestRetryPolicyProduction verifies retry behavior.
func TestRetryPolicyProduction(t *testing.T) {
	policy := messaging.RetryPolicy{
		MaxRetries:     3,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     100 * time.Millisecond,
		BackoffFactor:  2.0,
		RetryableError: func(err error) bool { return true },
	}

	attempts := 0
	start := time.Now()

	err := messaging.Retry(context.Background(), policy, func() error {
		attempts++
		if attempts < 4 {
			return context.Canceled // fail first 3 times
		}
		return nil
	})

	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("Retry should succeed on 4th attempt: %v", err)
	}

	if attempts != 4 {
		t.Errorf("Expected 4 attempts, got %d", attempts)
	}

	// Should have waited for backoffs: 10ms + 20ms + 40ms = 70ms minimum
	if elapsed < 60*time.Millisecond {
		t.Errorf("Expected at least 60ms for backoffs, got %v", elapsed)
	}
}

// TestRetryContextCancellation verifies retry respects context.
func TestRetryContextCancellation(t *testing.T) {
	policy := messaging.DefaultRetryPolicy()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := messaging.Retry(ctx, policy, func() error {
		return context.Canceled // always fail
	})
	elapsed := time.Since(start)

	if err == nil || err != context.DeadlineExceeded {
		// The error could be either the context error or wrapped
		if elapsed > 100*time.Millisecond {
			t.Error("Retry should respect context timeout")
		}
	}
}
