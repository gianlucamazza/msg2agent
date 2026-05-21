package billing

import (
	"context"
	"net/http"
	"strings"
)

// OAuthClaims carries the identity fields extracted from a validated JWT.
// This mirrors a2a.Claims but is defined here to avoid an import cycle
// (adapters/a2a already imports pkg/billing).
type OAuthClaims struct {
	Subject string
	Issuer  string
	Email   string
}

// JWTValidator is the subset of a2a.OAuth2Validator used by OAuth2Middleware.
// *a2a.OAuth2Validator satisfies this interface via an adapter; see NewA2AOAuth2Validator.
type JWTValidator interface {
	ValidateTokenToBillingClaims(token string) (*OAuthClaims, error)
}

// OAuth2Middleware returns an HTTP middleware that validates Bearer JWTs and
// resolves claims to a billing Tenant via the store. It is designed to wrap
// APIKeyMiddleware: tokens with an API key prefix (sk_live_, sk_test_, msg2a_)
// are passed through untouched so APIKeyMiddleware can handle them.
//
// autoProvisionPlan: if non-empty, a new PlanFree (or specified plan) tenant is
// auto-created on first login. Controlled via MSG2AGENT_OAUTH_AUTO_PROVISION env.
func OAuth2Middleware(validator JWTValidator, store Store, autoProvisionPlan Plan) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				next.ServeHTTP(w, r)
				return
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
				next.ServeHTTP(w, r)
				return
			}

			token := parts[1]
			// API key tokens are handled by APIKeyMiddleware (inner); pass through.
			if strings.HasPrefix(token, apiKeyPrefixLive) ||
				strings.HasPrefix(token, apiKeyPrefixTest) ||
				strings.HasPrefix(token, apiKeyPrefixLegacy) {
				next.ServeHTTP(w, r)
				return
			}

			// Validate JWT.
			claims, err := validator.ValidateTokenToBillingClaims(token)
			if err != nil {
				http.Error(w, "invalid OAuth2 token: "+err.Error(), http.StatusUnauthorized)
				return
			}

			// Resolve (iss, sub) → tenant.
			tenantID, err := store.GetOAuthIdentityTenant(claims.Issuer, claims.Subject)
			if err == ErrOAuthIdentityNotFound && autoProvisionPlan != "" {
				tenant, err := NewTenant(claims.Email, claims.Email, autoProvisionPlan)
				if err != nil {
					http.Error(w, "failed to create tenant", http.StatusInternalServerError)
					return
				}
				if err := store.PutTenant(tenant); err != nil {
					http.Error(w, "failed to provision tenant", http.StatusInternalServerError)
					return
				}
				if err := store.PutOAuthIdentity(claims.Issuer, claims.Subject, tenant.ID, claims.Email); err != nil {
					http.Error(w, "failed to link OAuth identity", http.StatusInternalServerError)
					return
				}
				tenantID = tenant.ID
			} else if err != nil {
				http.Error(w, "OAuth identity not registered; contact support", http.StatusForbidden)
				return
			}

			tenant, err := store.GetTenant(tenantID)
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
