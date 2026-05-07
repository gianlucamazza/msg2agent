package main

import (
	"encoding/json"
	"fmt"
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
	TenantID    string `json:"tenant_id"`
	APIKey      string `json:"api_key,omitempty"`
	Status      string `json:"status"`
	CheckoutURL string `json:"checkout_url,omitempty"`
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
//
// Free plan: creates tenant + API key immediately (key active on creation).
// Paid plans (starter, team): creates tenant with BillingStatus="incomplete",
// issues an inactive API key, and returns a Stripe Checkout URL. The key becomes
// active automatically when the checkout.session.completed webhook fires.
// Requires stripeClient != nil for paid plans; returns 503 otherwise.
//
// Per-IP rate limit: 5 signups per 60 seconds.
func signupHandler(store billing.Store, stripeClient *billing.StripeClient, logger *slog.Logger) http.HandlerFunc {
	limiter := newIPRateLimiter()

	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		ip := signupRealIP(r)
		if !limiter.allow(ip, 5, 60) {
			writeError(w, http.StatusTooManyRequests, "signup rate limit exceeded; try again in 60 seconds")
			return
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, 4096))
		if err != nil {
			writeError(w, http.StatusBadRequest, "failed to read request body")
			return
		}
		var req signupRequest
		if err := json.Unmarshal(body, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}

		req.Name = strings.TrimSpace(req.Name)
		req.Email = strings.TrimSpace(strings.ToLower(req.Email))
		if req.Plan == "" {
			req.Plan = "free"
		}
		req.Plan = strings.ToLower(strings.TrimSpace(req.Plan))

		if len(req.Name) < 2 || len(req.Name) > 100 {
			writeError(w, http.StatusBadRequest, "name must be 2-100 characters")
			return
		}
		if !emailRe.MatchString(req.Email) {
			writeError(w, http.StatusBadRequest, "invalid email address")
			return
		}
		plan, ok := validPlans[req.Plan]
		if !ok {
			writeError(w, http.StatusBadRequest, "plan must be one of: free, starter, team")
			return
		}

		isPaid := plan != billing.PlanFree
		if isPaid && stripeClient == nil {
			writeError(w, http.StatusServiceUnavailable, "paid plans are not available: billing not configured")
			return
		}

		tenant := billing.NewTenant(req.Name, req.Email, plan)

		var checkoutURL string
		if isPaid {
			// Mark as incomplete until Stripe checkout.session.completed fires.
			tenant.BillingStatus = "incomplete"

			origin := fmt.Sprintf("https://%s", r.Host)
			if r.Header.Get("X-Forwarded-Proto") == "" {
				origin = fmt.Sprintf("http://%s", r.Host)
			}
			sess, err := stripeClient.CreateCheckoutSession(
				tenant.ID, plan,
				origin+"/app/?checkout=success&tenant="+tenant.ID,
				origin+"/pricing?checkout=canceled",
			)
			if err != nil {
				logger.Error("signup: CreateCheckoutSession failed", "error", err)
				writeError(w, http.StatusInternalServerError, "failed to create checkout session")
				return
			}
			checkoutURL = sess.URL
		}

		if err := store.PutTenant(tenant); err != nil {
			logger.Error("signup: PutTenant failed", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to create tenant")
			return
		}

		plaintext, key, err := billing.GenerateAPIKey(tenant.ID, "default")
		if err != nil {
			logger.Error("signup: GenerateAPIKey failed", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to generate API key")
			return
		}
		if err := store.PutAPIKey(key); err != nil {
			logger.Error("signup: PutAPIKey failed", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to store API key")
			return
		}

		logger.Info("signup: tenant created",
			"tenant_id", tenant.ID,
			"email", req.Email,
			"plan", plan,
			"paid", isPaid,
		)

		status := "active"
		if isPaid {
			status = "incomplete"
		}

		resp := signupResponse{
			TenantID:    tenant.ID,
			Status:      status,
			CheckoutURL: checkoutURL,
		}
		// Return the API key for free tenants. For paid tenants the key is issued
		// but inactive (BillingStatus=incomplete); we return it so the user can
		// save it and use it after checkout without another API call.
		resp.APIKey = plaintext

		writeRelayJSON(w, http.StatusCreated, resp)
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
