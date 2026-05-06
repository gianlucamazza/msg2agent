package oauth

import (
	"encoding/json"
	"net/http"

	"github.com/lestrrat-go/jwx/v2/jwk"
)

// JWKSHandler returns an http.Handler serving the JWKS set as JSON.
func JWKSHandler(set jwk.Set) http.Handler {
	body, _ := json.Marshal(set)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Write(body)
	})
}
