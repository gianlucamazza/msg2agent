package billing

import (
	"context"
	"net/http"
	"strings"
)

type contextKey string

const tenantContextKey contextKey = "billing_tenant"

// TenantContextKey returns the context key used to store the authenticated Tenant.
// Use this when setting tenant in context outside the billing middleware (e.g. A2A handler).
func TenantContextKey() contextKey { return tenantContextKey }

// TenantFromContext extracts the authenticated Tenant from the request context.
// Returns nil if no tenant is set (e.g. unauthenticated or auth disabled).
func TenantFromContext(ctx context.Context) *Tenant {
	t, _ := ctx.Value(tenantContextKey).(*Tenant)
	return t
}

// APIKeyMiddleware authenticates requests via a Bearer API key (msg2a_ prefix).
// It resolves the tenant from the billing Store and stores it in the context.
// Requests with no Authorization header are rejected with 401 unless allowAnon is true.
// Metering is handled at the MCP tool level via MCPToolMeterMiddleware, not here.
func APIKeyMiddleware(store Store, allowAnon bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip auth for health endpoints only.
			// /.well-known/agent.json is A2A-specific; the MCP adapter does not serve it.
			if strings.HasPrefix(r.URL.Path, "/health") {
				next.ServeHTTP(w, r)
				return
			}

			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				if allowAnon {
					next.ServeHTTP(w, r)
					return
				}
				http.Error(w, "missing Authorization header", http.StatusUnauthorized)
				return
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
				http.Error(w, "invalid Authorization format; expected: Bearer msg2a_...", http.StatusUnauthorized)
				return
			}

			plaintext := parts[1]
			hash, err := HashAPIKey(plaintext)
			if err != nil {
				http.Error(w, ErrInvalidAPIKey.Error(), http.StatusUnauthorized)
				return
			}

			key, err := store.GetAPIKeyByHash(hash)
			if err != nil {
				http.Error(w, "invalid or unknown API key", http.StatusUnauthorized)
				return
			}
			if !key.IsValid() {
				http.Error(w, ErrAPIKeyRevoked.Error(), http.StatusUnauthorized)
				return
			}

			tenant, err := store.GetTenant(key.TenantID)
			if err != nil {
				http.Error(w, "tenant not found", http.StatusUnauthorized)
				return
			}
			if !tenant.IsActive() {
				http.Error(w, ErrTenantSuspended.Error(), http.StatusForbidden)
				return
			}

			ctx := context.WithValue(r.Context(), tenantContextKey, tenant)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
