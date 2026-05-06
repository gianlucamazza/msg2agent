package oauth

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// dcrRateLimiter enforces ≤10 DCR requests per IP per hour via a sliding window.
type dcrRateLimiter struct {
	mu      sync.Mutex
	windows map[string][]time.Time
}

func newDCRLimiter() *dcrRateLimiter { return &dcrRateLimiter{windows: make(map[string][]time.Time)} }

func (l *dcrRateLimiter) Allow(ip string) bool {
	const (
		limit  = 10
		window = time.Hour
	)
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := time.Now().Add(-window)
	var fresh []time.Time
	for _, t := range l.windows[ip] {
		if t.After(cutoff) {
			fresh = append(fresh, t)
		}
	}
	if len(fresh) >= limit {
		l.windows[ip] = fresh
		return false
	}
	l.windows[ip] = append(fresh, time.Now())
	return true
}

var globalDCRLimiter = newDCRLimiter()

// DCRRequest is the RFC 7591 Dynamic Client Registration request body.
type DCRRequest struct {
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	Scope                   string   `json:"scope,omitempty"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
}

// DCRResponse is the RFC 7591 Dynamic Client Registration response body.
type DCRResponse struct {
	ClientID                string   `json:"client_id"`
	ClientSecret            string   `json:"client_secret,omitempty"`
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	Scope                   string   `json:"scope,omitempty"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	ClientIDIssuedAt        int64    `json:"client_id_issued_at"`
	ClientSecretExpiresAt   int64    `json:"client_secret_expires_at"`
}

// DCRHandler returns an http.Handler for POST /oauth/register (RFC 7591).
func DCRHandler(store Store) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		ip := dcrClientIP(r)
		if !globalDCRLimiter.Allow(ip) {
			oauthError(w, "too_many_requests", "registration rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		var req DCRRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			oauthError(w, "invalid_client_metadata", "invalid JSON body", http.StatusBadRequest)
			return
		}

		if req.ClientName == "" {
			oauthError(w, "invalid_client_metadata", "client_name is required", http.StatusBadRequest)
			return
		}
		if len(req.RedirectURIs) == 0 {
			oauthError(w, "invalid_client_metadata", "redirect_uris is required", http.StatusBadRequest)
			return
		}
		for _, ru := range req.RedirectURIs {
			if err := validateRedirectURI(ru); err != nil {
				oauthError(w, "invalid_redirect_uri", err.Error(), http.StatusBadRequest)
				return
			}
		}

		if len(req.GrantTypes) == 0 {
			req.GrantTypes = []string{"authorization_code"}
		}
		authMethod := req.TokenEndpointAuthMethod
		if authMethod == "" {
			authMethod = "none"
		}
		if authMethod != "none" && authMethod != "client_secret_post" {
			oauthError(w, "invalid_client_metadata", "unsupported token_endpoint_auth_method", http.StatusBadRequest)
			return
		}

		clientID := "cli_" + randomHex(16)
		now := time.Now().Unix()

		c := &Client{
			ClientID:                clientID,
			ClientName:              req.ClientName,
			RedirectURIs:            req.RedirectURIs,
			GrantTypes:              req.GrantTypes,
			Scope:                   req.Scope,
			TokenEndpointAuthMethod: authMethod,
			ClientIDIssuedAt:        now,
			ClientSecretExpiresAt:   0,
			CreatedIP:               ip,
		}

		var plainSecret string
		if authMethod == "client_secret_post" {
			var err error
			plainSecret, _, err = GenerateToken(32)
			if err != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			c.ClientSecretHash = HashToken(plainSecret)
		}

		if err := store.PutClient(c); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		resp := DCRResponse{
			ClientID:                c.ClientID,
			ClientName:              c.ClientName,
			RedirectURIs:            c.RedirectURIs,
			GrantTypes:              c.GrantTypes,
			Scope:                   c.Scope,
			TokenEndpointAuthMethod: c.TokenEndpointAuthMethod,
			ClientIDIssuedAt:        now,
			ClientSecretExpiresAt:   0,
		}
		if plainSecret != "" {
			resp.ClientSecret = plainSecret
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(resp)
	})
}

func validateRedirectURI(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("redirect_uri %q is not a valid URL", raw)
	}
	if u.Fragment != "" {
		return fmt.Errorf("redirect_uri must not contain a fragment")
	}
	host := u.Hostname()
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if host == "localhost" || host == "127.0.0.1" || host == "::1" {
			return nil
		}
		return fmt.Errorf("redirect_uri http scheme is only allowed for localhost")
	default:
		return fmt.Errorf("redirect_uri scheme %q is not allowed", u.Scheme)
	}
}

func dcrClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if parts := strings.Split(xff, ","); len(parts) > 0 {
			if ip := strings.TrimSpace(parts[0]); ip != "" {
				return ip
			}
		}
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	return host
}

func oauthError(w http.ResponseWriter, errCode, desc string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{
		"error":             errCode,
		"error_description": desc,
	})
}

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}
