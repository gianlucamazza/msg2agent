package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gianlucamazza/msg2agent/pkg/billing"
)

var emailRe = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

type signupRequest struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Plan  string `json:"plan"` // "free", "starter", "team"
}

type signupResponse struct {
	TenantID string `json:"tenant_id"`
	APIKey   string `json:"api_key"`
}

var validPlans = map[string]billing.Plan{
	"free":    billing.PlanFree,
	"starter": billing.PlanStarter,
	"team":    billing.PlanTeam,
}

type ipRateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*ipBucket
}

type ipBucket struct {
	count int
	reset int64 // unix second at which count resets
}

func newIPRateLimiter() *ipRateLimiter {
	return &ipRateLimiter{buckets: make(map[string]*ipBucket)}
}

func (l *ipRateLimiter) allow(ip string, maxPerWindow int, windowSec int64) bool {
	now := time.Now().Unix()
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.buckets[ip]
	if !ok || now >= b.reset {
		b = &ipBucket{count: 0, reset: now + windowSec}
		l.buckets[ip] = b
	}
	b.count++
	return b.count <= maxPerWindow
}

// signupHandler returns an HTTP handler for POST /api/tenants.
// It creates a tenant and issues the first API key (printed once).
// Per-IP rate limit: 5 signups per 60 seconds.
func signupHandler(store billing.Store, logger *slog.Logger) http.HandlerFunc {
	limiter := newIPRateLimiter()

	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		ip := signupRealIP(r)
		if !limiter.allow(ip, 5, 60) {
			http.Error(w, "signup rate limit exceeded; try again in 60 seconds", http.StatusTooManyRequests)
			return
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, 4096))
		if err != nil {
			http.Error(w, "failed to read request body", http.StatusBadRequest)
			return
		}
		var req signupRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}

		req.Name = strings.TrimSpace(req.Name)
		req.Email = strings.TrimSpace(strings.ToLower(req.Email))
		if req.Plan == "" {
			req.Plan = "free"
		}
		req.Plan = strings.ToLower(strings.TrimSpace(req.Plan))

		if len(req.Name) < 2 || len(req.Name) > 100 {
			http.Error(w, "name must be 2-100 characters", http.StatusBadRequest)
			return
		}
		if !emailRe.MatchString(req.Email) {
			http.Error(w, "invalid email address", http.StatusBadRequest)
			return
		}
		plan, ok := validPlans[req.Plan]
		if !ok {
			http.Error(w, "plan must be one of: free, starter, team", http.StatusBadRequest)
			return
		}

		tenant := billing.NewTenant(req.Name, req.Email, plan)
		if err := store.PutTenant(tenant); err != nil {
			logger.Error("signup: PutTenant failed", "error", err)
			http.Error(w, "failed to create tenant", http.StatusInternalServerError)
			return
		}

		plaintext, key, err := billing.GenerateAPIKey(tenant.ID, "default")
		if err != nil {
			logger.Error("signup: GenerateAPIKey failed", "error", err)
			http.Error(w, "failed to generate API key", http.StatusInternalServerError)
			return
		}
		if err := store.PutAPIKey(key); err != nil {
			logger.Error("signup: PutAPIKey failed", "error", err)
			http.Error(w, "failed to store API key", http.StatusInternalServerError)
			return
		}

		logger.Info("signup: tenant created", "tenant_id", tenant.ID, "email", req.Email, "plan", plan)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(signupResponse{TenantID: tenant.ID, APIKey: plaintext})
	}
}

// signupRealIP extracts the client IP, honoring X-Real-IP / X-Forwarded-For.
func signupRealIP(r *http.Request) string {
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		if idx := strings.Index(ip, ","); idx >= 0 {
			return strings.TrimSpace(ip[:idx])
		}
		return strings.TrimSpace(ip)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
