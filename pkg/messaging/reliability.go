package messaging

import (
	"context"
	"errors"
	"math"
	"sync"
	"time"
)

// Reliability errors.
var (
	ErrCircuitOpen        = errors.New("circuit breaker is open")
	ErrMaxRetriesExceeded = errors.New("max retries exceeded")
)

// RetryPolicy defines how retries should be performed.
type RetryPolicy struct {
	MaxRetries     int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	BackoffFactor  float64
	RetryableError func(error) bool
}

// DefaultRetryPolicy returns a sensible default retry policy.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxRetries:     3,
		InitialBackoff: 100 * time.Millisecond,
		MaxBackoff:     5 * time.Second,
		BackoffFactor:  2.0,
		RetryableError: func(_ error) bool { return true },
	}
}

// Retry executes a function with retries according to the policy.
func Retry(ctx context.Context, policy RetryPolicy, fn func() error) error {
	var lastErr error
	backoff := policy.InitialBackoff

	for attempt := 0; attempt <= policy.MaxRetries; attempt++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		lastErr = fn()
		if lastErr == nil {
			return nil
		}

		if !policy.RetryableError(lastErr) {
			return lastErr
		}

		if attempt < policy.MaxRetries {
			// Wait before next attempt
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}

			// Increase backoff
			backoff = time.Duration(float64(backoff) * policy.BackoffFactor)
			if backoff > policy.MaxBackoff {
				backoff = policy.MaxBackoff
			}
		}
	}

	return errors.Join(ErrMaxRetriesExceeded, lastErr)
}

// CircuitState represents the state of a circuit breaker.
type CircuitState int

// Circuit breaker states.
const (
	CircuitClosed CircuitState = iota
	CircuitOpen
	CircuitHalfOpen
)

// CircuitBreaker implements the circuit breaker pattern.
type CircuitBreaker struct {
	name         string
	maxFailures  int
	resetTimeout time.Duration
	halfOpenMax  int

	mu          sync.RWMutex
	state       CircuitState
	failures    int
	successes   int
	lastFailure time.Time
}

// CircuitBreakerConfig configures a circuit breaker.
type CircuitBreakerConfig struct {
	Name         string
	MaxFailures  int
	ResetTimeout time.Duration
	HalfOpenMax  int
}

// DefaultCircuitBreakerConfig returns sensible defaults.
func DefaultCircuitBreakerConfig(name string) CircuitBreakerConfig {
	return CircuitBreakerConfig{
		Name:         name,
		MaxFailures:  5,
		ResetTimeout: 30 * time.Second,
		HalfOpenMax:  3,
	}
}

// NewCircuitBreaker creates a new circuit breaker.
func NewCircuitBreaker(config CircuitBreakerConfig) *CircuitBreaker {
	return &CircuitBreaker{
		name:         config.Name,
		maxFailures:  config.MaxFailures,
		resetTimeout: config.ResetTimeout,
		halfOpenMax:  config.HalfOpenMax,
		state:        CircuitClosed,
	}
}

// Execute runs a function through the circuit breaker.
func (cb *CircuitBreaker) Execute(fn func() error) error {
	if !cb.allowRequest() {
		return ErrCircuitOpen
	}

	err := fn()

	if err != nil {
		cb.recordFailure()
	} else {
		cb.recordSuccess()
	}

	return err
}

// allowRequest checks if a request should be allowed.
func (cb *CircuitBreaker) allowRequest() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CircuitClosed:
		return true
	case CircuitOpen:
		// Check if we should transition to half-open
		if time.Since(cb.lastFailure) > cb.resetTimeout {
			cb.state = CircuitHalfOpen
			cb.successes = 0
			return true
		}
		return false
	case CircuitHalfOpen:
		return true
	}
	return false
}

// recordFailure records a failure.
func (cb *CircuitBreaker) recordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures++
	cb.lastFailure = time.Now()

	if cb.state == CircuitHalfOpen {
		cb.state = CircuitOpen
		return
	}

	if cb.failures >= cb.maxFailures {
		cb.state = CircuitOpen
	}
}

// recordSuccess records a success.
func (cb *CircuitBreaker) recordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == CircuitHalfOpen {
		cb.successes++
		if cb.successes >= cb.halfOpenMax {
			cb.state = CircuitClosed
			cb.failures = 0
		}
		return
	}

	cb.failures = 0
}

// State returns the current circuit state.
func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// Reset resets the circuit breaker to closed state.
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.state = CircuitClosed
	cb.failures = 0
	cb.successes = 0
}

// RateLimiter implements token bucket rate limiting.
type RateLimiter struct {
	rate       float64
	bucketSize float64
	tokens     float64
	lastUpdate time.Time
	mu         sync.Mutex
}

// NewRateLimiter creates a new rate limiter.
func NewRateLimiter(rate float64, bucketSize float64) *RateLimiter {
	return &RateLimiter{
		rate:       rate,
		bucketSize: bucketSize,
		tokens:     bucketSize,
		lastUpdate: time.Now(),
	}
}

// Allow checks if a request should be allowed.
func (rl *RateLimiter) Allow() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(rl.lastUpdate).Seconds()
	rl.lastUpdate = now

	// Refill tokens
	rl.tokens += elapsed * rl.rate
	rl.tokens = math.Min(rl.tokens, rl.bucketSize)

	if rl.tokens >= 1 {
		rl.tokens--
		return true
	}
	return false
}

// Wait waits until a token is available.
func (rl *RateLimiter) Wait(ctx context.Context) error {
	for {
		if rl.Allow() {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(1/rl.rate*1000) * time.Millisecond):
		}
	}
}
