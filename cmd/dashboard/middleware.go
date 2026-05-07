package main

import (
	"net/http"
	"strconv"
	"sync"
	"time"
)

// securityHeaders wraps h with standard browser security response headers.
func securityHeaders(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; object-src 'none'; base-uri 'self'; frame-ancestors 'none'; form-action 'self'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "geolocation=(), camera=(), microphone=()")
		h.ServeHTTP(w, r)
	})
}

// keyCreateLimiter is a per-tenant token bucket for POST /api/dashboard/keys.
// Capacity: 10 creates/hour per tenant.
type keyCreateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*kcBucket
}

type kcBucket struct {
	tokens     float64
	lastUpdate time.Time
	lastSeen   time.Time
}

const (
	kcCapacity = 10.0
	kcRate     = 10.0 / 3600.0 // 10 per hour in tokens/sec
	kcGCIdle   = 2 * time.Hour
)

var globalKeyCreateLimiter = &keyCreateLimiter{buckets: make(map[string]*kcBucket)}

func init() {
	go globalKeyCreateLimiter.gcLoop()
}

func (l *keyCreateLimiter) allow(tenantID string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.buckets[tenantID]
	if !ok {
		b = &kcBucket{tokens: kcCapacity, lastUpdate: time.Now()}
		l.buckets[tenantID] = b
	}
	b.lastSeen = time.Now()
	now := time.Now()
	elapsed := now.Sub(b.lastUpdate).Seconds()
	b.lastUpdate = now
	b.tokens += elapsed * kcRate
	if b.tokens > kcCapacity {
		b.tokens = kcCapacity
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func (l *keyCreateLimiter) gcLoop() {
	t := time.NewTicker(30 * time.Minute)
	defer t.Stop()
	for range t.C {
		l.mu.Lock()
		cutoff := time.Now().Add(-kcGCIdle)
		for id, b := range l.buckets {
			if b.lastSeen.Before(cutoff) {
				delete(l.buckets, id)
			}
		}
		l.mu.Unlock()
	}
}

// retryAfterSecs returns the number of seconds until a token is available.
func (l *keyCreateLimiter) retryAfterSecs(tenantID string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.buckets[tenantID]
	if !ok || b.tokens >= 1 {
		return 0
	}
	needed := 1 - b.tokens
	secs := needed / kcRate
	return int(secs) + 1
}

func writeRateLimitError(w http.ResponseWriter, retryAfter int) {
	w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusTooManyRequests)
	_, _ = w.Write([]byte(`{"error":"too many key creation requests; try again later"}`))
}
