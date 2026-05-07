package oauth

import (
	"net/http"
)

// RevokeHandler returns an http.Handler for POST /oauth/revoke (RFC 7009).
// Only refresh tokens are revoked; access tokens are short-lived JWTs and
// are accepted but silently ignored (no introspection list maintained).
func RevokeHandler(store Store) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := r.ParseForm(); err != nil {
			oauthError(w, "invalid_request", "cannot parse form body", http.StatusBadRequest)
			return
		}
		token := r.FormValue("token")
		if token == "" {
			oauthError(w, "invalid_request", "token is required", http.StatusBadRequest)
			return
		}
		// Public client: only verify client_id exists (no auth).
		clientID := r.FormValue("client_id")
		if clientID != "" {
			if _, err := store.GetClient(clientID); err != nil {
				// Unknown client — RFC 7009 §2.2 says respond 200 anyway
				w.WriteHeader(http.StatusOK)
				return
			}
		}

		hint := r.FormValue("token_type_hint")
		// For access tokens we don't maintain a revocation list; respond 200 per RFC 7009.
		if hint == "access_token" {
			w.WriteHeader(http.StatusOK)
			return
		}
		// Default: treat as refresh_token.
		hash := HashToken(token)
		if err := store.RevokeRefreshToken(hash); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
}
