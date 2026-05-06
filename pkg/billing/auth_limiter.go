package billing

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// authLimiter enforces a per-IP token bucket on failed authentication attempts.
// Capacity 10 tokens, refill rate 1/sec. Idle entries are GC'd after 1 hour.
type authLimiter struct {
	mu      sync.Mutex
	buckets map[string]*authBucket
}

type authBucket struct {
	tokens     float64
	lastUpdate time.Time
	lastSeen   time.Time
}

const (
	authBucketCapacity = 10.0
	authBucketRate     = 1.0 // tokens per second
	authBucketGCIdle   = time.Hour
)

func newAuthLimiter() *authLimiter {
	al := &authLimiter{buckets: make(map[string]*authBucket)}
	go al.gcLoop()
	return al
}

// Allow returns true if the IP has tokens remaining (does NOT consume a token on success).
// Call Consume to debit a token on a failed authentication.
func (al *authLimiter) Allow(ip string) bool {
	al.mu.Lock()
	defer al.mu.Unlock()
	b := al.getOrCreate(ip)
	al.refill(b)
	return b.tokens >= 1
}

// Consume debits one token from the IP bucket (call on auth failure).
func (al *authLimiter) Consume(ip string) {
	al.mu.Lock()
	defer al.mu.Unlock()
	b := al.getOrCreate(ip)
	al.refill(b)
	if b.tokens >= 1 {
		b.tokens--
	}
}

func (al *authLimiter) getOrCreate(ip string) *authBucket {
	b, ok := al.buckets[ip]
	if !ok {
		b = &authBucket{tokens: authBucketCapacity, lastUpdate: time.Now(), lastSeen: time.Now()}
		al.buckets[ip] = b
	}
	b.lastSeen = time.Now()
	return b
}

func (al *authLimiter) refill(b *authBucket) {
	now := time.Now()
	elapsed := now.Sub(b.lastUpdate).Seconds()
	b.lastUpdate = now
	b.tokens += elapsed * authBucketRate
	if b.tokens > authBucketCapacity {
		b.tokens = authBucketCapacity
	}
}

func (al *authLimiter) gcLoop() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		al.mu.Lock()
		cutoff := time.Now().Add(-authBucketGCIdle)
		for ip, b := range al.buckets {
			if b.lastSeen.Before(cutoff) {
				delete(al.buckets, ip)
			}
		}
		al.mu.Unlock()
	}
}

// realIP extracts the client IP from the request, preferring X-Real-IP / X-Forwarded-For
// when set by a trusted reverse proxy.
func realIP(r *http.Request) string {
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		// May be comma-separated list; take the first.
		parts := splitHeader(ip)
		if len(parts) > 0 {
			return parts[0]
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func splitHeader(v string) []string {
	var out []string
	start := 0
	for i := 0; i < len(v); i++ {
		if v[i] == ',' {
			if s := trim(v[start:i]); s != "" {
				out = append(out, s)
			}
			start = i + 1
		}
	}
	if s := trim(v[start:]); s != "" {
		out = append(out, s)
	}
	return out
}

func trim(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}

// package-level limiter shared across all middleware instances.
var globalAuthLimiter = newAuthLimiter()
