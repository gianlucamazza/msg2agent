package billing

import (
	"sync"
	"time"
)

// tenantBucket is a token-bucket rate limiter for a single tenant.
type tenantBucket struct {
	rate       float64 // tokens/sec
	bucketSize float64
	tokens     float64
	lastUpdate time.Time
	mu         sync.Mutex
}

func newTenantBucket(ratePerSec, burst float64) *tenantBucket {
	return &tenantBucket{
		rate:       ratePerSec,
		bucketSize: burst,
		tokens:     burst,
		lastUpdate: time.Now(),
	}
}

// Allow returns true if a token is available and consumes it.
func (b *tenantBucket) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(b.lastUpdate).Seconds()
	b.lastUpdate = now

	b.tokens += elapsed * b.rate
	if b.tokens > b.bucketSize {
		b.tokens = b.bucketSize
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// TenantRateLimiterPool maintains one token-bucket per tenant, created lazily
// from the tenant's QuotaConfig.
type TenantRateLimiterPool struct {
	mu      sync.RWMutex
	buckets map[string]*tenantBucket
	store   Store // for quota lookup
}

// NewTenantRateLimiterPool creates a pool backed by the given billing Store.
func NewTenantRateLimiterPool(store Store) *TenantRateLimiterPool {
	return &TenantRateLimiterPool{
		buckets: make(map[string]*tenantBucket),
		store:   store,
	}
}

// Allow checks the rate limit for a tenant. Returns false when throttled.
// On the first call for a tenant it fetches the quota from the Store.
func (p *TenantRateLimiterPool) Allow(tenantID string) bool {
	p.mu.RLock()
	b, ok := p.buckets[tenantID]
	p.mu.RUnlock()
	if ok {
		return b.Allow()
	}

	// Create bucket from tenant quota.
	rate := 5.0
	burst := 20.0
	if t, err := p.store.GetTenant(tenantID); err == nil {
		rate = t.Quota.RateLimitMsgPerSec
		burst = t.Quota.RateLimitBurstSize
	}

	p.mu.Lock()
	if b, ok = p.buckets[tenantID]; !ok {
		b = newTenantBucket(rate, burst)
		p.buckets[tenantID] = b
	}
	p.mu.Unlock()
	return b.Allow()
}

// Evict removes the bucket for a tenant (e.g. after a plan change).
func (p *TenantRateLimiterPool) Evict(tenantID string) {
	p.mu.Lock()
	delete(p.buckets, tenantID)
	p.mu.Unlock()
}
