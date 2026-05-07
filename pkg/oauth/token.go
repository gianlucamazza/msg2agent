package oauth

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// TokenHandler returns an http.Handler for POST /oauth/token (RFC 6749 §4.1 + §6).
// tenants is optional; when non-nil, email and name claims are embedded in issued tokens.
func TokenHandler(store Store, issuer *JWTIssuer, audience string, tenants TenantLookup) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := r.ParseForm(); err != nil {
			oauthError(w, "invalid_request", "cannot parse form body", http.StatusBadRequest)
			return
		}
		switch r.FormValue("grant_type") {
		case "authorization_code":
			handleAuthCode(w, r, store, issuer, audience, tenants)
		case "refresh_token":
			handleRefresh(w, r, store, issuer, audience, tenants)
		default:
			oauthError(w, "unsupported_grant_type", "supported: authorization_code, refresh_token", http.StatusBadRequest)
		}
	})
}

func handleAuthCode(w http.ResponseWriter, r *http.Request, store Store, issuer *JWTIssuer, audience string, tenants TenantLookup) {
	clientID := r.FormValue("client_id")
	redirectURI := r.FormValue("redirect_uri")
	codeVerifier := r.FormValue("code_verifier")
	plainCode := r.FormValue("code")

	if clientID == "" || redirectURI == "" || codeVerifier == "" || plainCode == "" {
		oauthError(w, "invalid_request", "client_id, redirect_uri, code and code_verifier are required", http.StatusBadRequest)
		return
	}

	c, err := store.GetClient(clientID)
	if errors.Is(err, ErrClientNotFound) {
		oauthError(w, "invalid_client", "unknown client_id", http.StatusUnauthorized)
		return
	}
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if c.TokenEndpointAuthMethod == "client_secret_post" {
		if HashToken(r.FormValue("client_secret")) != c.ClientSecretHash {
			oauthError(w, "invalid_client", "client authentication failed", http.StatusUnauthorized)
			return
		}
	}

	code, err := store.UseCode(HashToken(plainCode))
	if errors.Is(err, ErrCodeNotFound) || errors.Is(err, ErrCodeExpiredOrUsed) {
		oauthError(w, "invalid_grant", "authorization code is invalid or expired", http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if code.ClientID != clientID {
		oauthError(w, "invalid_grant", "code was issued to a different client", http.StatusBadRequest)
		return
	}
	if code.RedirectURI != redirectURI {
		oauthError(w, "invalid_grant", "redirect_uri mismatch", http.StatusBadRequest)
		return
	}
	if err := pkceVerify(codeVerifier, code.CodeChallenge); err != nil {
		oauthError(w, "invalid_grant", "code_verifier does not match code_challenge", http.StatusBadRequest)
		return
	}

	plainRefresh, refreshHash, err := GenerateToken(32)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	refreshExpiry := time.Now().UTC().Add(RefreshTokenTTL)
	if err := store.PutRefreshToken(&RefreshToken{
		TokenHash: refreshHash,
		ClientID:  clientID,
		TenantID:  code.TenantID,
		Scope:     code.Scope,
		ExpiresAt: refreshExpiry,
	}); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	writeTokenJSON(w, issuer, audience, code.TenantID, clientID, code.Scope, plainRefresh, refreshExpiry, tenants)
}

func handleRefresh(w http.ResponseWriter, r *http.Request, store Store, issuer *JWTIssuer, audience string, tenants TenantLookup) {
	plainToken := r.FormValue("refresh_token")
	clientID := r.FormValue("client_id")
	if plainToken == "" || clientID == "" {
		oauthError(w, "invalid_request", "refresh_token and client_id are required", http.StatusBadRequest)
		return
	}

	plainNew, newHash, err := GenerateToken(32)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	newExpiry := time.Now().UTC().Add(RefreshTokenTTL)
	rt, err := store.RotateRefreshToken(HashToken(plainToken), &RefreshToken{
		TokenHash: newHash,
		ClientID:  clientID,
		ExpiresAt: newExpiry,
	})
	if errors.Is(err, ErrRefreshTokenNotFound) || errors.Is(err, ErrRefreshTokenRevoked) {
		oauthError(w, "invalid_grant", "refresh token is invalid or revoked", http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	writeTokenJSON(w, issuer, audience, rt.TenantID, clientID, rt.Scope, plainNew, newExpiry, tenants)
}

func writeTokenJSON(w http.ResponseWriter, issuer *JWTIssuer, audience, tenantID, clientID, scope, plainRefresh string, refreshExpiry time.Time, tenants TenantLookup) {
	var email, name string
	if tenants != nil {
		if t, err := tenants.GetTenantByID(tenantID); err == nil {
			email = t.Email
			name = t.Name
		}
	}
	accessToken, err := issuer.IssueAccessToken(tenantID, clientID, scope, audience, uuid.New().String(), email, name)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	resp := map[string]any{
		"access_token":             accessToken,
		"token_type":               "Bearer",
		"expires_in":               int(AccessTokenTTL.Seconds()),
		"refresh_token":            plainRefresh,
		"refresh_token_expires_in": int(time.Until(refreshExpiry).Seconds()),
	}
	if scope != "" {
		resp["scope"] = scope
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(resp)
}
