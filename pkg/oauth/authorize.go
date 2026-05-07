package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

// IdentityProvider abstracts third-party login (Google OIDC) for the consent screen.
type IdentityProvider interface {
	// AuthURL returns the provider's authorization URL embedding the given state.
	AuthURL(state string) string
	// ExchangeCode exchanges a provider callback code for the authenticated user's email.
	ExchangeCode(ctx context.Context, code, state string) (email string, err error)
}

// TenantBrief is the minimal tenant information needed by the consent screen.
type TenantBrief struct {
	ID    string
	Name  string
	Email string
}

// TenantLookup resolves a tenant by email or ID for the consent screen.
// Implementations must be goroutine-safe.
type TenantLookup interface {
	GetTenantByEmail(email string) (*TenantBrief, error)
	GetTenantByID(id string) (*TenantBrief, error)
}

// AuthorizeServer groups the dependencies for all three authorize sub-handlers.
type AuthorizeServer struct {
	store    Store
	idp      IdentityProvider
	tenants  TenantLookup
	issuer   *JWTIssuer
	verifier *JWTVerifier
	baseURL  string // e.g. "https://msg2agent.example.com"
}

// NewAuthorizeServer creates an AuthorizeServer wiring all dependencies.
func NewAuthorizeServer(
	store Store,
	idp IdentityProvider,
	tenants TenantLookup,
	issuer *JWTIssuer,
	verifier *JWTVerifier,
	baseURL string,
) *AuthorizeServer {
	return &AuthorizeServer{
		store:    store,
		idp:      idp,
		tenants:  tenants,
		issuer:   issuer,
		verifier: verifier,
		baseURL:  strings.TrimRight(baseURL, "/"),
	}
}

// HandleAuthorize serves GET /oauth/authorize.
// If the request carries a valid session cookie the consent screen is shown;
// otherwise the user is redirected to the identity provider.
func (s *AuthorizeServer) HandleAuthorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("response_type") != "code" {
		oauthError(w, "unsupported_response_type", "only response_type=code is supported", http.StatusBadRequest)
		return
	}
	clientID := q.Get("client_id")
	redirectURI := q.Get("redirect_uri")
	codeChallenge := q.Get("code_challenge")
	codeChallengeMethod := q.Get("code_challenge_method")
	scope := q.Get("scope")
	state := q.Get("state")

	if clientID == "" || redirectURI == "" || codeChallenge == "" {
		oauthError(w, "invalid_request", "client_id, redirect_uri and code_challenge are required", http.StatusBadRequest)
		return
	}
	if codeChallengeMethod != "S256" {
		oauthError(w, "invalid_request", "code_challenge_method must be S256", http.StatusBadRequest)
		return
	}

	c, err := s.store.GetClient(clientID)
	if errors.Is(err, ErrClientNotFound) {
		oauthError(w, "invalid_client", "unknown client_id", http.StatusBadRequest)
		return
	}
	if err != nil {
		renderError(w, http.StatusInternalServerError, "Internal error", "Something went wrong. Please try again.")
		return
	}
	if err := validateRedirectURI(redirectURI); err != nil {
		oauthError(w, "invalid_redirect_uri", err.Error(), http.StatusBadRequest)
		return
	}
	if !slices.Contains(c.RedirectURIs, redirectURI) {
		oauthError(w, "invalid_redirect_uri", "redirect_uri not registered for this client", http.StatusBadRequest)
		return
	}

	// Check for an existing session cookie.
	tenantID, err := s.sessionFromCookie(r)
	if err != nil {
		// No valid session — start provider login. Encode OAuth params into the IDP state.
		idpState, err2 := s.encodeIDPState(clientID, redirectURI, codeChallenge, codeChallengeMethod, scope, state)
		if err2 != nil {
			renderError(w, http.StatusInternalServerError, "Internal error", "Something went wrong. Please try again.")
			return
		}
		http.Redirect(w, r, s.idp.AuthURL(idpState), http.StatusFound)
		return
	}

	// Session is valid — show consent screen.
	s.showConsent(w, r, tenantID, c, redirectURI, codeChallenge, codeChallengeMethod, scope, state)
}

// HandleGoogleCallback serves GET /oauth/google-callback.
func (s *AuthorizeServer) HandleGoogleCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	idpState := r.URL.Query().Get("state")
	if code == "" || idpState == "" {
		renderError(w, http.StatusBadRequest, "Sign-in failed", "The sign-in request was incomplete. Please start over.")
		return
	}

	email, err := s.idp.ExchangeCode(r.Context(), code, idpState)
	if err != nil {
		renderError(w, http.StatusBadGateway, "Sign-in failed", "Google rejected the sign-in. Please try again.")
		return
	}

	tenant, err := s.tenants.GetTenantByEmail(email)
	if err != nil {
		renderError(w, http.StatusForbidden, "No account found", "There's no msg2agent account for this email. Sign up at msg2agent.home.gianlucamazza.it/pricing.")
		return
	}

	sessionToken, err := s.issuer.IssueSessionCookie(tenant.ID)
	if err != nil {
		renderError(w, http.StatusInternalServerError, "Internal error", "Something went wrong. Please try again.")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "m2a_authz_session",
		Value:    sessionToken,
		Path:     "/oauth/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(SessionCookieTTL.Seconds()),
	})

	// Decode the original OAuth params from the IDP state and redirect back to authorize.
	orig, err := s.decodeIDPState(idpState)
	if err != nil {
		renderError(w, http.StatusBadRequest, "Sign-in expired", "Your sign-in attempt expired or the state was invalid. Please start over.")
		return
	}
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", orig.ClientID)
	q.Set("redirect_uri", orig.RedirectURI)
	q.Set("code_challenge", orig.CodeChallenge)
	q.Set("code_challenge_method", orig.CodeChallengeMethod)
	if orig.Scope != "" {
		q.Set("scope", orig.Scope)
	}
	if orig.State != "" {
		q.Set("state", orig.State)
	}
	http.Redirect(w, r, s.baseURL+"/oauth/authorize?"+q.Encode(), http.StatusFound)
}

// HandleConfirm serves POST /oauth/authorize/confirm.
func (s *AuthorizeServer) HandleConfirm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		renderError(w, http.StatusMethodNotAllowed, "Method not allowed", "This endpoint only accepts POST requests.")
		return
	}
	if err := r.ParseForm(); err != nil {
		renderError(w, http.StatusBadRequest, "Bad request", "Could not parse the form data. Please try again.")
		return
	}

	sessionToken := r.FormValue("session")
	tenantID, err := s.verifier.ValidateSessionCookie(sessionToken)
	if err != nil {
		renderError(w, http.StatusUnauthorized, "Session expired", "Your consent session expired. Please start the sign-in again.")
		return
	}

	redirectURI := r.FormValue("redirect_uri")
	state := r.FormValue("state")

	if r.FormValue("action") != "allow" {
		redirectWithError(w, r, redirectURI, "access_denied", "user denied the request", state)
		return
	}

	clientID := r.FormValue("client_id")
	codeChallenge := r.FormValue("code_challenge")
	codeChallengeMethod := r.FormValue("code_challenge_method")
	scope := r.FormValue("scope")

	plainCode, codeHash, err := GenerateToken(32)
	if err != nil {
		renderError(w, http.StatusInternalServerError, "Internal error", "Something went wrong. Please try again.")
		return
	}

	code := &Code{
		CodeHash:            codeHash,
		ClientID:            clientID,
		TenantID:            tenantID,
		RedirectURI:         redirectURI,
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: codeChallengeMethod,
		Scope:               scope,
		ExpiresAt:           time.Now().UTC().Add(60 * time.Second),
	}
	if err := s.store.PutCode(code); err != nil {
		renderError(w, http.StatusInternalServerError, "Internal error", "Something went wrong. Please try again.")
		return
	}

	q := url.Values{}
	q.Set("code", plainCode)
	if state != "" {
		q.Set("state", state)
	}
	http.Redirect(w, r, redirectURI+"?"+q.Encode(), http.StatusFound)
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func (s *AuthorizeServer) sessionFromCookie(r *http.Request) (tenantID string, err error) {
	c, err := r.Cookie("m2a_authz_session")
	if err != nil {
		return "", err
	}
	return s.verifier.ValidateSessionCookie(c.Value)
}

func (s *AuthorizeServer) showConsent(w http.ResponseWriter, _ *http.Request,
	tenantID string, c *Client,
	redirectURI, codeChallenge, codeChallengeMethod, scope, state string,
) {
	sessionToken, err := s.issuer.IssueSessionCookie(tenantID)
	if err != nil {
		renderError(w, http.StatusInternalServerError, "Internal error", "Something went wrong. Please try again.")
		return
	}

	// Resolve tenant name for display.
	tenantName := tenantID
	if t, err2 := s.tenants.GetTenantByID(tenantID); err2 == nil && t != nil {
		tenantName = t.Name
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	consentTmpl.Execute(w, consentData{
		ClientName:          c.ClientName,
		TenantName:          tenantName,
		Scopes:              scopeList(scope),
		SessionToken:        sessionToken,
		ClientID:            c.ClientID,
		RedirectURI:         redirectURI,
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: codeChallengeMethod,
		Scope:               scope,
		State:               state,
	})
}

type idpStatePayload struct {
	ClientID            string `json:"c"`
	RedirectURI         string `json:"r"`
	CodeChallenge       string `json:"cc"`
	CodeChallengeMethod string `json:"cm"`
	Scope               string `json:"s,omitempty"`
	State               string `json:"st,omitempty"`
	IssuedAt            int64  `json:"iat"`
}

func (s *AuthorizeServer) encodeIDPState(clientID, redirectURI, codeChallenge, codeChallengeMethod, scope, state string) (string, error) {
	payload := idpStatePayload{
		ClientID:            clientID,
		RedirectURI:         redirectURI,
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: codeChallengeMethod,
		Scope:               scope,
		State:               state,
		IssuedAt:            time.Now().Unix(),
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("oauth: encode idp state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (s *AuthorizeServer) decodeIDPState(encoded string) (*idpStatePayload, error) {
	b, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("oauth: decode idp state: %w", err)
	}
	var p idpStatePayload
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("oauth: unmarshal idp state: %w", err)
	}
	if time.Since(time.Unix(p.IssuedAt, 0)) > 10*time.Minute {
		return nil, fmt.Errorf("oauth: idp state expired")
	}
	return &p, nil
}

func redirectWithError(w http.ResponseWriter, r *http.Request, redirectURI, errCode, desc, state string) {
	q := url.Values{}
	q.Set("error", errCode)
	q.Set("error_description", desc)
	if state != "" {
		q.Set("state", state)
	}
	http.Redirect(w, r, redirectURI+"?"+q.Encode(), http.StatusFound)
}

func scopeList(scope string) []string {
	if scope == "" {
		return nil
	}
	return strings.Fields(scope)
}

// pkceVerify returns nil if verifier matches challenge per RFC 7636 §4.6.
func pkceVerify(verifier, challenge string) error {
	h := sha256.Sum256([]byte(verifier))
	got := base64.RawURLEncoding.EncodeToString(h[:])
	if got != challenge {
		return fmt.Errorf("oauth: PKCE code_verifier does not match code_challenge")
	}
	return nil
}
