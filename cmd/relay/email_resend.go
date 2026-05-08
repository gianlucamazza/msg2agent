package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gianlucamazza/msg2agent/pkg/billing"
	"github.com/gianlucamazza/msg2agent/pkg/email"
)

// resendLimiter rate-limits POST /api/email/resend to 1 request per email per minute.
// Single-instance in-process; sufficient for home-lab deployment.
type resendLimiter struct {
	mu     sync.Mutex
	last   map[string]time.Time
	window time.Duration
}

var globalResendLimiter = &resendLimiter{
	last:   make(map[string]time.Time),
	window: time.Minute,
}

func (l *resendLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if t, ok := l.last[key]; ok && time.Since(t) < l.window {
		return false
	}
	l.last[key] = time.Now()
	// GC: prune entries older than window to avoid unbounded growth.
	for k, t := range l.last {
		if time.Since(t) > l.window {
			delete(l.last, k)
		}
	}
	return true
}

func resendVerificationHandler(store billing.Store, sender email.Sender, baseURL string, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			Email string `json:"email"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Email == "" {
			http.Error(w, `{"error":"email required"}`, http.StatusBadRequest)
			return
		}

		ip := signupRealIP(r)
		if !globalResendLimiter.allow(req.Email) || !globalResendLimiter.allow(ip) {
			w.Header().Set("Retry-After", "60")
			http.Error(w, `{"error":"too many requests; try again in a minute"}`, http.StatusTooManyRequests)
			return
		}

		tenant, err := store.GetTenantByEmail(req.Email)
		if err != nil || tenant == nil {
			// Return 200 even when email not found to avoid enumeration.
			w.WriteHeader(http.StatusOK)
			return
		}
		if tenant.EmailVerifiedAt != nil {
			w.WriteHeader(http.StatusOK)
			return
		}

		go sendVerificationEmail(store, sender, tenant.ID, tenant.Email, baseURL, logger)

		w.WriteHeader(http.StatusOK)
	}
}
