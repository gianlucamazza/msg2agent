package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jws"
	"github.com/lestrrat-go/jwx/v2/jwt"
)

const (
	googleAuthEndpoint  = "https://accounts.google.com/o/oauth2/v2/auth"
	googleTokenEndpoint = "https://oauth2.googleapis.com/token"
	googleJWKSURL       = "https://www.googleapis.com/oauth2/v3/certs"
	googleIssuer1       = "https://accounts.google.com"
	googleIssuer2       = "accounts.google.com"
)

// GoogleIDP implements IdentityProvider using Google OAuth2 / OIDC.
type GoogleIDP struct {
	clientID     string
	clientSecret string
	redirectURI  string

	jwksCache    jwk.Set
	jwksCachedAt time.Time
}

// NewGoogleIDP constructs a GoogleIDP. redirectURI must be the full callback URL
// (e.g. "https://msg2agent.example.com/oauth/google-callback").
func NewGoogleIDP(clientID, clientSecret, redirectURI string) *GoogleIDP {
	return &GoogleIDP{
		clientID:     clientID,
		clientSecret: clientSecret,
		redirectURI:  redirectURI,
	}
}

// AuthURL returns the Google authorization URL embedding the given state.
func (g *GoogleIDP) AuthURL(state string) string {
	q := url.Values{}
	q.Set("client_id", g.clientID)
	q.Set("redirect_uri", g.redirectURI)
	q.Set("response_type", "code")
	q.Set("scope", "openid email profile")
	q.Set("state", state)
	q.Set("access_type", "online")
	return googleAuthEndpoint + "?" + q.Encode()
}

// ExchangeCode exchanges the Google callback code for the user's email.
func (g *GoogleIDP) ExchangeCode(ctx context.Context, code, _ string) (string, error) {
	body := url.Values{}
	body.Set("code", code)
	body.Set("client_id", g.clientID)
	body.Set("client_secret", g.clientSecret)
	body.Set("redirect_uri", g.redirectURI)
	body.Set("grant_type", "authorization_code")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, googleTokenEndpoint, strings.NewReader(body.Encode()))
	if err != nil {
		return "", fmt.Errorf("google: build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("google: token exchange: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("google: token endpoint returned %d: %s", resp.StatusCode, raw)
	}

	var tok struct {
		IDToken string `json:"id_token"`
	}
	if err := json.Unmarshal(raw, &tok); err != nil || tok.IDToken == "" {
		return "", errors.New("google: no id_token in response")
	}

	return g.emailFromIDToken(ctx, tok.IDToken)
}

func (g *GoogleIDP) emailFromIDToken(ctx context.Context, idToken string) (string, error) {
	ks, err := g.getJWKS(ctx)
	if err != nil {
		return "", err
	}

	parsed, err := jwt.Parse([]byte(idToken),
		jwt.WithKeySet(ks, jws.WithInferAlgorithmFromKey(true)),
		jwt.WithValidate(true),
	)
	if err != nil {
		return "", fmt.Errorf("google: invalid id_token: %w", err)
	}

	issuer := parsed.Issuer()
	if issuer != googleIssuer1 && issuer != googleIssuer2 {
		return "", fmt.Errorf("google: unexpected issuer %q", issuer)
	}

	emailRaw, ok := parsed.Get("email")
	if !ok {
		return "", errors.New("google: id_token missing email claim")
	}
	email, ok := emailRaw.(string)
	if !ok || email == "" {
		return "", errors.New("google: id_token email claim is not a string")
	}
	return email, nil
}

func (g *GoogleIDP) getJWKS(ctx context.Context) (jwk.Set, error) {
	if g.jwksCache != nil && time.Since(g.jwksCachedAt) < time.Hour {
		return g.jwksCache, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, googleJWKSURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("google: fetch JWKS: %w", err)
	}
	defer resp.Body.Close()
	ks, err := jwk.ParseReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("google: parse JWKS: %w", err)
	}
	g.jwksCache = ks
	g.jwksCachedAt = time.Now()
	return ks, nil
}
