package main

import (
	"crypto/ed25519"

	"github.com/gianlucamazza/msg2agent/pkg/billing"
	"github.com/gianlucamazza/msg2agent/pkg/oauth"
)

// oauthLoadKey loads or generates an Ed25519 signing key from path.
func oauthLoadKey(path string) (ed25519.PrivateKey, error) {
	return oauth.LoadOrGenerateEd25519(path)
}

// oauthBuildKID builds the JWK set and returns the key ID.
func oauthBuildKID(priv ed25519.PrivateKey) (interface{}, string, error) {
	set, kid, err := oauth.BuildJWK(priv)
	return set, kid, err
}

// oauthNewVerifier creates a JWTVerifier that satisfies billing.AccessTokenValidator.
func oauthNewVerifier(priv ed25519.PrivateKey, kid, asBase string) billing.AccessTokenValidator {
	return oauth.NewJWTVerifier(priv, asBase, asBase+"/mcp")
}
