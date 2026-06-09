package billing

import (
	"context"
	"net/http"
	"strings"

	"go.opentelemetry.io/otel/attribute"

	"github.com/gianlucamazza/msg2agent/pkg/telemetry"
)

type contextKey string

const tenantContextKey contextKey = "billing_tenant"

// TenantContextKey returns the context key used to store the authenticated Tenant.
// Use this when setting tenant in context outside the billing middleware (e.g. A2A handler).
// The concrete key type is unexported; callers only pass the returned value to
// context.WithValue / Context.Value, so an opaque any is sufficient.
func TenantContextKey() any { return tenantContextKey }

// TenantFromContext extracts the authenticated Tenant from the request context.
// Returns nil if no tenant is set (e.g. unauthenticated or auth disabled).
func TenantFromContext(ctx context.Context) *Tenant {
	t, _ := ctx.Value(tenantContextKey).(*Tenant)
	return t
}

// AccessTokenValidator validates OAuth 2.1 JWT access tokens minted by our own AS.
// *oauth.JWTVerifier satisfies this interface; defined here to avoid an import cycle.
type AccessTokenValidator interface {
	// ValidateClaims verifies the JWT signature, issuer, audience, and expiry,
	// returning the tenant ID on success.
	ValidateClaims(token string) (tenantID string, err error)
}

// BearerMiddleware authenticates requests via either a JWT access token or an API key.
// JWT tokens start with "eyJ"; API keys carry an sk_live_, sk_test_, or msg2a_ prefix.
// Both credential types are permanent first-class auth methods.
func BearerMiddleware(store Store, jwtVal AccessTokenValidator, allowAnon bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/health") || strings.HasPrefix(r.URL.Path, "/.well-known/") || strings.HasPrefix(r.URL.Path, "/oauth/") {
				next.ServeHTTP(w, r)
				return
			}

			rctx, span := telemetry.StartSpan(r.Context(), "billing", "billing.BearerMiddleware")
			defer span.End()

			ip := realIP(r)
			if !globalAuthLimiter.Allow(ip) {
				billingRateLimited.WithLabelValues("__auth_failed__").Inc()
				http.Error(w, "too many failed authentication attempts", http.StatusTooManyRequests)
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
				globalAuthLimiter.Consume(ip)
				http.Error(w, "invalid Authorization format; expected: Bearer <token>", http.StatusUnauthorized)
				return
			}
			tok := parts[1]

			var tenant *Tenant
			var authMethod string

			switch {
			case strings.HasPrefix(tok, "eyJ") && jwtVal != nil:
				tenantID, err := jwtVal.ValidateClaims(tok)
				if err != nil {
					globalAuthLimiter.Consume(ip)
					http.Error(w, "invalid or expired access token", http.StatusUnauthorized)
					return
				}
				t, err := store.GetTenant(tenantID)
				if err != nil {
					globalAuthLimiter.Consume(ip)
					http.Error(w, "tenant not found", http.StatusUnauthorized)
					return
				}
				tenant = t
				authMethod = "jwt"

			case IsAPIKeyToken(tok):
				hash, err := HashAPIKey(tok)
				if err != nil {
					globalAuthLimiter.Consume(ip)
					http.Error(w, ErrInvalidAPIKey.Error(), http.StatusUnauthorized)
					return
				}
				key, err := store.GetAPIKeyByHash(hash)
				if err != nil {
					globalAuthLimiter.Consume(ip)
					http.Error(w, "invalid or unknown API key", http.StatusUnauthorized)
					return
				}
				if !key.IsValid() {
					globalAuthLimiter.Consume(ip)
					http.Error(w, ErrAPIKeyRevoked.Error(), http.StatusUnauthorized)
					return
				}
				t, err := store.GetTenant(key.TenantID)
				if err != nil {
					http.Error(w, "tenant not found", http.StatusUnauthorized)
					return
				}
				tenant = t
				authMethod = "api_key"

			default:
				globalAuthLimiter.Consume(ip)
				http.Error(w, "unrecognized token format", http.StatusUnauthorized)
				return
			}

			if tenant.BillingStatus == "incomplete" || tenant.BillingStatus == "past_due" {
				http.Error(w, "payment required: update your payment method at https://msg2agent.xyz/app/", http.StatusPaymentRequired)
				return
			}
			if !tenant.IsActive() {
				http.Error(w, ErrTenantSuspended.Error(), http.StatusForbidden)
				return
			}

			span.SetAttributes(
				attribute.String("billing.tenant_id", tenant.ID),
				attribute.String("billing.auth.method", authMethod),
			)
			ctx := context.WithValue(rctx, tenantContextKey, tenant)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// APIKeyMiddleware authenticates requests via a Bearer API key (sk_live_, sk_test_, or msg2a_ legacy prefix).
// It resolves the tenant from the billing Store and stores it in the context.
// Requests with no Authorization header are rejected with 401 unless allowAnon is true.
// Metering is handled at the MCP tool level via MCPToolMeterMiddleware, not here.
func APIKeyMiddleware(store Store, allowAnon bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip auth for health and public discovery endpoints.
			if strings.HasPrefix(r.URL.Path, "/health") || strings.HasPrefix(r.URL.Path, "/.well-known/") {
				next.ServeHTTP(w, r)
				return
			}

			rctx, span := telemetry.StartSpan(r.Context(), "billing", "billing.APIKeyMiddleware")
			defer span.End()

			// Rate-limit IPs that repeatedly fail authentication.
			ip := realIP(r)
			if !globalAuthLimiter.Allow(ip) {
				billingRateLimited.WithLabelValues("__auth_failed__").Inc()
				http.Error(w, "too many failed authentication attempts", http.StatusTooManyRequests)
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
				globalAuthLimiter.Consume(ip)
				http.Error(w, "invalid Authorization format; expected: Bearer sk_live_...", http.StatusUnauthorized)
				return
			}

			plaintext := parts[1]
			hash, err := HashAPIKey(plaintext)
			if err != nil {
				globalAuthLimiter.Consume(ip)
				http.Error(w, ErrInvalidAPIKey.Error(), http.StatusUnauthorized)
				return
			}

			key, err := store.GetAPIKeyByHash(hash)
			if err != nil {
				globalAuthLimiter.Consume(ip)
				http.Error(w, "invalid or unknown API key", http.StatusUnauthorized)
				return
			}
			if !key.IsValid() {
				globalAuthLimiter.Consume(ip)
				http.Error(w, ErrAPIKeyRevoked.Error(), http.StatusUnauthorized)
				return
			}

			tenant, err := store.GetTenant(key.TenantID)
			if err != nil {
				http.Error(w, "tenant not found", http.StatusUnauthorized)
				return
			}
			if tenant.BillingStatus == "incomplete" || tenant.BillingStatus == "past_due" {
				http.Error(w, "payment required: update your payment method at https://msg2agent.xyz/app/", http.StatusPaymentRequired)
				return
			}
			if !tenant.IsActive() {
				http.Error(w, ErrTenantSuspended.Error(), http.StatusForbidden)
				return
			}

			span.SetAttributes(
				attribute.String("billing.tenant_id", tenant.ID),
				attribute.String("billing.auth.method", "api_key"),
			)
			ctx := context.WithValue(rctx, tenantContextKey, tenant)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
