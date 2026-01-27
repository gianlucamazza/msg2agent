package messaging

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// --- Retry Tests ---

// TestRetrySuccess tests successful execution without retries.
func TestRetrySuccess(t *testing.T) {
	policy := DefaultRetryPolicy()
	ctx := context.Background()

	callCount := 0
	err := Retry(ctx, policy, func() error {
		callCount++
		return nil
	})

	if err != nil {
		t.Errorf("Retry returned error: %v", err)
	}
	if callCount != 1 {
		t.Errorf("callCount = %d, want 1", callCount)
	}
}

// TestRetryEventualSuccess tests success after some failures.
func TestRetryEventualSuccess(t *testing.T) {
	policy := RetryPolicy{
		MaxRetries:     3,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     100 * time.Millisecond,
		BackoffFactor:  2.0,
		RetryableError: func(err error) bool { return true },
	}
	ctx := context.Background()

	callCount := 0
	err := Retry(ctx, policy, func() error {
		callCount++
		if callCount < 3 {
			return errors.New("temporary error")
		}
		return nil
	})

	if err != nil {
		t.Errorf("Retry returned error: %v", err)
	}
	if callCount != 3 {
		t.Errorf("callCount = %d, want 3", callCount)
	}
}

// TestRetryMaxRetriesExceeded tests max retries exhaustion.
func TestRetryMaxRetriesExceeded(t *testing.T) {
	policy := RetryPolicy{
		MaxRetries:     2,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     100 * time.Millisecond,
		BackoffFactor:  2.0,
		RetryableError: func(err error) bool { return true },
	}
	ctx := context.Background()

	permErr := errors.New("permanent error")
	callCount := 0
	err := Retry(ctx, policy, func() error {
		callCount++
		return permErr
	})

	if err == nil {
		t.Fatal("Retry should return error when max retries exceeded")
	}
	if !errors.Is(err, ErrMaxRetriesExceeded) {
		t.Errorf("error should wrap ErrMaxRetriesExceeded: %v", err)
	}
	if !errors.Is(err, permErr) {
		t.Errorf("error should wrap original error: %v", err)
	}
	if callCount != 3 { // initial + 2 retries
		t.Errorf("callCount = %d, want 3", callCount)
	}
}

// TestRetryNonRetryableError tests immediate failure on non-retryable error.
func TestRetryNonRetryableError(t *testing.T) {
	policy := RetryPolicy{
		MaxRetries:     5,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     100 * time.Millisecond,
		BackoffFactor:  2.0,
		RetryableError: func(err error) bool {
			return err.Error() != "non-retryable"
		},
	}
	ctx := context.Background()

	nonRetryable := errors.New("non-retryable")
	callCount := 0
	err := Retry(ctx, policy, func() error {
		callCount++
		return nonRetryable
	})

	if err != nonRetryable {
		t.Errorf("Retry returned %v, want %v", err, nonRetryable)
	}
	if callCount != 1 {
		t.Errorf("callCount = %d, want 1 (no retries for non-retryable)", callCount)
	}
}

// TestRetryContextCancellation tests cancellation during retry.
func TestRetryContextCancellation(t *testing.T) {
	policy := RetryPolicy{
		MaxRetries:     10,
		InitialBackoff: 100 * time.Millisecond,
		MaxBackoff:     1 * time.Second,
		BackoffFactor:  2.0,
		RetryableError: func(err error) bool { return true },
	}

	ctx, cancel := context.WithCancel(context.Background())

	callCount := 0
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := Retry(ctx, policy, func() error {
		callCount++
		return errors.New("always fail")
	})

	if !errors.Is(err, context.Canceled) {
		t.Errorf("Retry should return context.Canceled, got %v", err)
	}
}

// TestRetryContextDeadline tests deadline during retry wait.
func TestRetryContextDeadline(t *testing.T) {
	policy := RetryPolicy{
		MaxRetries:     10,
		InitialBackoff: 500 * time.Millisecond,
		MaxBackoff:     1 * time.Second,
		BackoffFactor:  2.0,
		RetryableError: func(err error) bool { return true },
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := Retry(ctx, policy, func() error {
		return errors.New("always fail")
	})

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Retry should return DeadlineExceeded, got %v", err)
	}
}

// TestRetryBackoffCapping tests that backoff is capped at MaxBackoff.
func TestRetryBackoffCapping(t *testing.T) {
	policy := RetryPolicy{
		MaxRetries:     5,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     20 * time.Millisecond, // Very low cap
		BackoffFactor:  10.0,                  // Aggressive growth
		RetryableError: func(err error) bool { return true },
	}
	ctx := context.Background()

	start := time.Now()
	Retry(ctx, policy, func() error {
		return errors.New("fail")
	})
	elapsed := time.Since(start)

	// With capped backoff: 10 + 20 + 20 + 20 + 20 = 90ms max wait
	// Without cap: 10 + 100 + 1000 + 10000 + ... = much more
	if elapsed > 500*time.Millisecond {
		t.Errorf("elapsed = %v, backoff should be capped", elapsed)
	}
}

// TestDefaultRetryPolicy tests default policy values.
func TestDefaultRetryPolicy(t *testing.T) {
	policy := DefaultRetryPolicy()

	if policy.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d, want 3", policy.MaxRetries)
	}
	if policy.InitialBackoff != 100*time.Millisecond {
		t.Errorf("InitialBackoff = %v, want 100ms", policy.InitialBackoff)
	}
	if policy.MaxBackoff != 5*time.Second {
		t.Errorf("MaxBackoff = %v, want 5s", policy.MaxBackoff)
	}
	if policy.BackoffFactor != 2.0 {
		t.Errorf("BackoffFactor = %v, want 2.0", policy.BackoffFactor)
	}
	if policy.RetryableError == nil {
		t.Error("RetryableError should not be nil")
	}
	// Default should retry all errors
	if !policy.RetryableError(errors.New("any error")) {
		t.Error("default RetryableError should return true for any error")
	}
}

// --- CircuitBreaker Tests ---

// TestCircuitBreakerInitialState tests initial state is closed.
func TestCircuitBreakerInitialState(t *testing.T) {
	config := DefaultCircuitBreakerConfig("test")
	cb := NewCircuitBreaker(config)

	if cb.State() != CircuitClosed {
		t.Errorf("initial state = %v, want CircuitClosed", cb.State())
	}
}

// TestCircuitBreakerExecuteSuccess tests successful execution.
func TestCircuitBreakerExecuteSuccess(t *testing.T) {
	config := DefaultCircuitBreakerConfig("test")
	cb := NewCircuitBreaker(config)

	err := cb.Execute(func() error {
		return nil
	})

	if err != nil {
		t.Errorf("Execute returned error: %v", err)
	}
	if cb.State() != CircuitClosed {
		t.Errorf("state after success = %v, want CircuitClosed", cb.State())
	}
}

// TestCircuitBreakerOpensAfterFailures tests opening after max failures.
func TestCircuitBreakerOpensAfterFailures(t *testing.T) {
	config := CircuitBreakerConfig{
		Name:         "test",
		MaxFailures:  3,
		ResetTimeout: 1 * time.Second,
		HalfOpenMax:  1,
	}
	cb := NewCircuitBreaker(config)

	testErr := errors.New("test error")
	for i := 0; i < 3; i++ {
		_ = cb.Execute(func() error {
			return testErr
		})
	}

	if cb.State() != CircuitOpen {
		t.Errorf("state after %d failures = %v, want CircuitOpen", config.MaxFailures, cb.State())
	}

	// Next request should be rejected
	err := cb.Execute(func() error {
		t.Error("function should not be called when circuit is open")
		return nil
	})

	if !errors.Is(err, ErrCircuitOpen) {
		t.Errorf("Execute when open returned %v, want ErrCircuitOpen", err)
	}
}

// TestCircuitBreakerTransitionToHalfOpen tests transition after reset timeout.
func TestCircuitBreakerTransitionToHalfOpen(t *testing.T) {
	config := CircuitBreakerConfig{
		Name:         "test",
		MaxFailures:  1,
		ResetTimeout: 50 * time.Millisecond,
		HalfOpenMax:  1,
	}
	cb := NewCircuitBreaker(config)

	// Open the circuit
	cb.Execute(func() error { return errors.New("fail") })
	if cb.State() != CircuitOpen {
		t.Fatal("circuit should be open")
	}

	// Wait for reset timeout
	time.Sleep(100 * time.Millisecond)

	// Next request should trigger half-open
	err := cb.Execute(func() error { return nil })
	if err != nil {
		t.Errorf("Execute in half-open returned error: %v", err)
	}

	if cb.State() != CircuitClosed {
		t.Errorf("state after success in half-open = %v, want CircuitClosed", cb.State())
	}
}

// TestCircuitBreakerHalfOpenFailure tests failure in half-open reopens circuit.
func TestCircuitBreakerHalfOpenFailure(t *testing.T) {
	config := CircuitBreakerConfig{
		Name:         "test",
		MaxFailures:  1,
		ResetTimeout: 50 * time.Millisecond,
		HalfOpenMax:  2,
	}
	cb := NewCircuitBreaker(config)

	// Open the circuit
	cb.Execute(func() error { return errors.New("fail") })
	time.Sleep(100 * time.Millisecond)

	// Fail in half-open
	cb.Execute(func() error { return errors.New("fail again") })

	if cb.State() != CircuitOpen {
		t.Errorf("state after half-open failure = %v, want CircuitOpen", cb.State())
	}
}

// TestCircuitBreakerHalfOpenRequiresMultipleSuccesses tests HalfOpenMax.
func TestCircuitBreakerHalfOpenRequiresMultipleSuccesses(t *testing.T) {
	config := CircuitBreakerConfig{
		Name:         "test",
		MaxFailures:  1,
		ResetTimeout: 50 * time.Millisecond,
		HalfOpenMax:  3,
	}
	cb := NewCircuitBreaker(config)

	// Open the circuit
	cb.Execute(func() error { return errors.New("fail") })
	time.Sleep(100 * time.Millisecond)

	// First success - still half-open
	cb.Execute(func() error { return nil })
	if cb.State() != CircuitHalfOpen {
		t.Errorf("state after 1 success = %v, want CircuitHalfOpen", cb.State())
	}

	// Second success - still half-open
	cb.Execute(func() error { return nil })
	if cb.State() != CircuitHalfOpen {
		t.Errorf("state after 2 successes = %v, want CircuitHalfOpen", cb.State())
	}

	// Third success - should close
	cb.Execute(func() error { return nil })
	if cb.State() != CircuitClosed {
		t.Errorf("state after 3 successes = %v, want CircuitClosed", cb.State())
	}
}

// TestCircuitBreakerReset tests manual reset.
func TestCircuitBreakerReset(t *testing.T) {
	config := CircuitBreakerConfig{
		Name:         "test",
		MaxFailures:  1,
		ResetTimeout: 10 * time.Minute, // Long timeout
		HalfOpenMax:  1,
	}
	cb := NewCircuitBreaker(config)

	// Open the circuit
	cb.Execute(func() error { return errors.New("fail") })
	if cb.State() != CircuitOpen {
		t.Fatal("circuit should be open")
	}

	// Manual reset
	cb.Reset()

	if cb.State() != CircuitClosed {
		t.Errorf("state after Reset = %v, want CircuitClosed", cb.State())
	}

	// Should accept requests again
	err := cb.Execute(func() error { return nil })
	if err != nil {
		t.Errorf("Execute after Reset returned error: %v", err)
	}
}

// TestCircuitBreakerSuccessResetsFailureCount tests success resets failures in closed state.
func TestCircuitBreakerSuccessResetsFailureCount(t *testing.T) {
	config := CircuitBreakerConfig{
		Name:         "test",
		MaxFailures:  3,
		ResetTimeout: 1 * time.Second,
		HalfOpenMax:  1,
	}
	cb := NewCircuitBreaker(config)

	// Two failures
	cb.Execute(func() error { return errors.New("fail 1") })
	cb.Execute(func() error { return errors.New("fail 2") })

	// Success should reset counter
	cb.Execute(func() error { return nil })

	// Two more failures should not open circuit
	cb.Execute(func() error { return errors.New("fail 3") })
	cb.Execute(func() error { return errors.New("fail 4") })

	if cb.State() != CircuitClosed {
		t.Errorf("state = %v, want CircuitClosed (failure count reset)", cb.State())
	}
}

// TestCircuitBreakerConcurrent tests concurrent access.
func TestCircuitBreakerConcurrent(t *testing.T) {
	config := CircuitBreakerConfig{
		Name:         "test",
		MaxFailures:  100,
		ResetTimeout: 1 * time.Second,
		HalfOpenMax:  10,
	}
	cb := NewCircuitBreaker(config)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				cb.Execute(func() error { return nil })
			} else {
				cb.Execute(func() error { return errors.New("fail") })
			}
		}(i)
	}

	wg.Wait()
	// Just ensure no panics or deadlocks
	_ = cb.State()
}

// TestDefaultCircuitBreakerConfig tests default config values.
func TestDefaultCircuitBreakerConfig(t *testing.T) {
	config := DefaultCircuitBreakerConfig("mybreaker")

	if config.Name != "mybreaker" {
		t.Errorf("Name = %q, want %q", config.Name, "mybreaker")
	}
	if config.MaxFailures != 5 {
		t.Errorf("MaxFailures = %d, want 5", config.MaxFailures)
	}
	if config.ResetTimeout != 30*time.Second {
		t.Errorf("ResetTimeout = %v, want 30s", config.ResetTimeout)
	}
	if config.HalfOpenMax != 3 {
		t.Errorf("HalfOpenMax = %d, want 3", config.HalfOpenMax)
	}
}

// --- RateLimiter Tests ---

// TestRateLimiterAllow tests basic allow functionality.
func TestRateLimiterAllow(t *testing.T) {
	// 10 tokens/sec, bucket of 5
	rl := NewRateLimiter(10, 5)

	// Should allow first 5 requests (bucket starts full)
	for i := 0; i < 5; i++ {
		if !rl.Allow() {
			t.Errorf("Allow() = false at request %d, want true", i+1)
		}
	}

	// 6th request should be denied
	if rl.Allow() {
		t.Error("Allow() = true after bucket exhausted, want false")
	}
}

// TestRateLimiterTokenRefill tests token refill over time.
func TestRateLimiterTokenRefill(t *testing.T) {
	// 100 tokens/sec, bucket of 2
	rl := NewRateLimiter(100, 2)

	// Exhaust tokens
	rl.Allow()
	rl.Allow()
	if rl.Allow() {
		t.Error("bucket should be empty")
	}

	// Wait for refill (100 tokens/sec = 1 token per 10ms)
	time.Sleep(20 * time.Millisecond)

	// Should have at least 1 token now
	if !rl.Allow() {
		t.Error("Allow() = false after refill, want true")
	}
}

// TestRateLimiterBucketCap tests that tokens don't exceed bucket size.
func TestRateLimiterBucketCap(t *testing.T) {
	// High rate, small bucket
	rl := NewRateLimiter(1000, 3)

	// Wait long enough to "over-fill" if not capped
	time.Sleep(50 * time.Millisecond)

	// Should only allow 3 requests
	allowed := 0
	for rl.Allow() {
		allowed++
		if allowed > 10 {
			t.Fatal("bucket not capped, allowing too many requests")
		}
	}

	if allowed != 3 {
		t.Errorf("allowed = %d, want 3 (bucket size)", allowed)
	}
}

// TestRateLimiterWait tests blocking wait for token.
func TestRateLimiterWait(t *testing.T) {
	// 100 tokens/sec, bucket of 1
	rl := NewRateLimiter(100, 1)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// First request uses the token
	if err := rl.Wait(ctx); err != nil {
		t.Errorf("first Wait returned error: %v", err)
	}

	// Second request should wait
	start := time.Now()
	if err := rl.Wait(ctx); err != nil {
		t.Errorf("second Wait returned error: %v", err)
	}
	elapsed := time.Since(start)

	// Should have waited ~10ms for refill
	if elapsed < 5*time.Millisecond {
		t.Errorf("elapsed = %v, expected wait for token refill", elapsed)
	}
}

// TestRateLimiterWaitContextCancel tests cancellation during wait.
func TestRateLimiterWaitContextCancel(t *testing.T) {
	// Very slow rate
	rl := NewRateLimiter(0.1, 1) // 0.1 tokens/sec = 10 sec per token

	// Exhaust bucket
	rl.Allow()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := rl.Wait(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Wait returned %v, want DeadlineExceeded", err)
	}
}

// TestRateLimiterConcurrent tests concurrent access.
func TestRateLimiterConcurrent(t *testing.T) {
	// High rate to allow concurrent access
	rl := NewRateLimiter(10000, 100)

	var wg sync.WaitGroup
	var allowed atomic.Int64

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if rl.Allow() {
				allowed.Add(1)
			}
		}()
	}

	wg.Wait()

	// Should have allowed most requests given high rate and bucket
	if allowed.Load() < 40 {
		t.Errorf("allowed = %d, expected most of 50 requests to be allowed", allowed.Load())
	}
}

// TestRateLimiterZeroRate tests behavior with zero rate.
func TestRateLimiterZeroRate(t *testing.T) {
	// Zero rate means no refill
	rl := NewRateLimiter(0, 2)

	// Should allow 2 requests (initial bucket)
	if !rl.Allow() {
		t.Error("first Allow() = false, want true")
	}
	if !rl.Allow() {
		t.Error("second Allow() = false, want true")
	}

	// No more
	if rl.Allow() {
		t.Error("third Allow() = true, want false (no refill)")
	}
}

// TestRateLimiterHighThroughput tests high throughput scenario.
func TestRateLimiterHighThroughput(t *testing.T) {
	// Very high rate
	rl := NewRateLimiter(100000, 1000)

	start := time.Now()
	allowed := 0
	for i := 0; i < 1000; i++ {
		if rl.Allow() {
			allowed++
		}
	}
	elapsed := time.Since(start)

	// Should complete quickly
	if elapsed > 100*time.Millisecond {
		t.Errorf("high throughput test took %v, expected < 100ms", elapsed)
	}
	if allowed < 900 {
		t.Errorf("allowed = %d, want >= 900", allowed)
	}
}

// --- Error Type Tests ---

// TestErrorCircuitOpen tests ErrCircuitOpen.
func TestErrorCircuitOpen(t *testing.T) {
	if ErrCircuitOpen == nil {
		t.Fatal("ErrCircuitOpen should not be nil")
	}
	if ErrCircuitOpen.Error() == "" {
		t.Error("ErrCircuitOpen.Error() should not be empty")
	}
}

// TestErrorMaxRetriesExceeded tests ErrMaxRetriesExceeded.
func TestErrorMaxRetriesExceeded(t *testing.T) {
	if ErrMaxRetriesExceeded == nil {
		t.Fatal("ErrMaxRetriesExceeded should not be nil")
	}
	if ErrMaxRetriesExceeded.Error() == "" {
		t.Error("ErrMaxRetriesExceeded.Error() should not be empty")
	}
}

// --- CircuitState Tests ---

// TestCircuitStateValues tests state constant values.
func TestCircuitStateValues(t *testing.T) {
	if CircuitClosed != 0 {
		t.Errorf("CircuitClosed = %d, want 0", CircuitClosed)
	}
	if CircuitOpen != 1 {
		t.Errorf("CircuitOpen = %d, want 1", CircuitOpen)
	}
	if CircuitHalfOpen != 2 {
		t.Errorf("CircuitHalfOpen = %d, want 2", CircuitHalfOpen)
	}
}
