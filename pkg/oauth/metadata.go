package oauth

import (
	"encoding/json"
	"net/http"
)

// ASMetadata is the RFC 8414 Authorization Server metadata document.
type ASMetadata struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	RegistrationEndpoint              string   `json:"registration_endpoint"`
	JwksURI                           string   `json:"jwks_uri"`
	ResponseTypesSupported            []string `json:"response_types_supported"`
	GrantTypesSupported               []string `json:"grant_types_supported"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
	ScopesSupported                   []string `json:"scopes_supported"`
}

// NewASMetadata builds the AS metadata for the given base URL.
func NewASMetadata(baseURL string) *ASMetadata {
	return &ASMetadata{
		Issuer:                            baseURL,
		AuthorizationEndpoint:             baseURL + "/oauth/authorize",
		TokenEndpoint:                     baseURL + "/oauth/token",
		RegistrationEndpoint:              baseURL + "/oauth/register",
		JwksURI:                           baseURL + "/.well-known/jwks.json",
		ResponseTypesSupported:            []string{"code"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		CodeChallengeMethodsSupported:     []string{"S256"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_post", "none"},
		ScopesSupported:                   []string{"mcp:tools:read", "mcp:tools:write", "mcp:tools:destructive"},
	}
}

// ASMetadataHandler returns an http.Handler serving RFC 8414 metadata as JSON.
func ASMetadataHandler(meta *ASMetadata) http.Handler {
	body, _ := json.Marshal(meta)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Write(body)
	})
}
