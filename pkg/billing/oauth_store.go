package billing

// oauth_store.go — oauth.Store implementation on SQLiteStore.
// Keeps OAuth client/code/refresh-token CRUD inside the existing billing DB.

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gianlucamazza/msg2agent/pkg/oauth"
)

// ─── Client ──────────────────────────────────────────────────────────────────

func (s *SQLiteStore) PutClient(c *oauth.Client) error {
	ruJSON, _ := json.Marshal(c.RedirectURIs)
	gtJSON, _ := json.Marshal(c.GrantTypes)
	_, err := s.db.Exec(`
		INSERT INTO oauth_clients
			(client_id,client_secret_hash,client_name,redirect_uris,grant_types,scope,
			 token_endpoint_auth_method,created_at,created_ip)
		VALUES (?,?,?,?,?,?,?,?,?)
		ON CONFLICT(client_id) DO UPDATE SET
			client_secret_hash=EXCLUDED.client_secret_hash,
			client_name=EXCLUDED.client_name,
			redirect_uris=EXCLUDED.redirect_uris,
			grant_types=EXCLUDED.grant_types,
			scope=EXCLUDED.scope,
			token_endpoint_auth_method=EXCLUDED.token_endpoint_auth_method`,
		c.ClientID, sqlNullStr(c.ClientSecretHash), c.ClientName,
		string(ruJSON), string(gtJSON), sqlNullStr(c.Scope),
		c.TokenEndpointAuthMethod,
		time.Unix(c.ClientIDIssuedAt, 0).UTC().Format(time.RFC3339),
		sqlNullStr(c.CreatedIP),
	)
	return err
}

func (s *SQLiteStore) GetClient(clientID string) (*oauth.Client, error) {
	row := s.db.QueryRow(`
		SELECT client_id,client_secret_hash,client_name,redirect_uris,grant_types,scope,
		       token_endpoint_auth_method,created_at,created_ip
		FROM oauth_clients WHERE client_id=?`, clientID)

	var c oauth.Client
	var secretHash, scope, createdIP sql.NullString
	var ruJSON, gtJSON, createdAt string
	err := row.Scan(&c.ClientID, &secretHash, &c.ClientName,
		&ruJSON, &gtJSON, &scope, &c.TokenEndpointAuthMethod, &createdAt, &createdIP)
	if err == sql.ErrNoRows {
		return nil, oauth.ErrClientNotFound
	}
	if err != nil {
		return nil, err
	}
	if secretHash.Valid {
		c.ClientSecretHash = secretHash.String
	}
	if scope.Valid {
		c.Scope = scope.String
	}
	if createdIP.Valid {
		c.CreatedIP = createdIP.String
	}
	json.Unmarshal([]byte(ruJSON), &c.RedirectURIs)
	json.Unmarshal([]byte(gtJSON), &c.GrantTypes)
	if t, err2 := time.Parse(time.RFC3339, createdAt); err2 == nil {
		c.ClientIDIssuedAt = t.Unix()
	}
	return &c, nil
}

// ─── Authorization codes ──────────────────────────────────────────────────────

func (s *SQLiteStore) PutCode(code *oauth.Code) error {
	_, err := s.db.Exec(`
		INSERT INTO oauth_codes
			(code_hash,client_id,tenant_id,redirect_uri,code_challenge,code_challenge_method,scope,expires_at,used)
		VALUES (?,?,?,?,?,?,?,?,0)`,
		code.CodeHash, code.ClientID, code.TenantID, code.RedirectURI,
		code.CodeChallenge, code.CodeChallengeMethod, sqlNullStr(code.Scope),
		code.ExpiresAt.UTC().Format(time.RFC3339),
	)
	return err
}

func (s *SQLiteStore) UseCode(codeHash string) (*oauth.Code, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var code oauth.Code
	var scope sql.NullString
	var expiresAt string
	var used int
	err = tx.QueryRow(`
		SELECT code_hash,client_id,tenant_id,redirect_uri,code_challenge,code_challenge_method,scope,expires_at,used
		FROM oauth_codes WHERE code_hash=?`, codeHash).Scan(
		&code.CodeHash, &code.ClientID, &code.TenantID, &code.RedirectURI,
		&code.CodeChallenge, &code.CodeChallengeMethod, &scope, &expiresAt, &used,
	)
	if err == sql.ErrNoRows {
		return nil, oauth.ErrCodeNotFound
	}
	if err != nil {
		return nil, err
	}
	if scope.Valid {
		code.Scope = scope.String
	}
	exp, _ := time.Parse(time.RFC3339, expiresAt)
	code.ExpiresAt = exp

	if used != 0 || time.Now().UTC().After(exp) {
		return nil, oauth.ErrCodeExpiredOrUsed
	}

	if _, err = tx.Exec(`UPDATE oauth_codes SET used=1 WHERE code_hash=?`, codeHash); err != nil {
		return nil, err
	}
	return &code, tx.Commit()
}

// ─── Refresh tokens ───────────────────────────────────────────────────────────

func (s *SQLiteStore) PutRefreshToken(rt *oauth.RefreshToken) error {
	_, err := s.db.Exec(`
		INSERT INTO oauth_refresh_tokens (token_hash,client_id,tenant_id,scope,expires_at,revoked)
		VALUES (?,?,?,?,?,0)`,
		rt.TokenHash, rt.ClientID, rt.TenantID, sqlNullStr(rt.Scope),
		rt.ExpiresAt.UTC().Format(time.RFC3339),
	)
	return err
}

func (s *SQLiteStore) RotateRefreshToken(oldHash string, newRT *oauth.RefreshToken) (*oauth.RefreshToken, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var revoked int
	var expiresAt string
	err = tx.QueryRow(`SELECT revoked,expires_at FROM oauth_refresh_tokens WHERE token_hash=?`, oldHash).
		Scan(&revoked, &expiresAt)
	if err == sql.ErrNoRows {
		return nil, oauth.ErrRefreshTokenNotFound
	}
	if err != nil {
		return nil, err
	}
	exp, _ := time.Parse(time.RFC3339, expiresAt)
	if revoked != 0 || time.Now().UTC().After(exp) {
		return nil, oauth.ErrRefreshTokenRevoked
	}

	if _, err = tx.Exec(`UPDATE oauth_refresh_tokens SET revoked=1 WHERE token_hash=?`, oldHash); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(`
		INSERT INTO oauth_refresh_tokens (token_hash,client_id,tenant_id,scope,expires_at,revoked)
		VALUES (?,?,?,?,?,0)`,
		newRT.TokenHash, newRT.ClientID, newRT.TenantID, sqlNullStr(newRT.Scope),
		newRT.ExpiresAt.UTC().Format(time.RFC3339),
	); err != nil {
		return nil, err
	}
	return newRT, tx.Commit()
}

// ─── Cleanup ──────────────────────────────────────────────────────────────────

func (s *SQLiteStore) CleanupOAuthExpired() error {
	now := time.Now().UTC().Format(time.RFC3339)
	stmts := []string{
		`DELETE FROM oauth_codes WHERE expires_at < ? OR used = 1`,
		`DELETE FROM oauth_refresh_tokens WHERE expires_at < ? OR revoked = 1`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt, now); err != nil {
			return fmt.Errorf("billing: cleanup oauth: %w", err)
		}
	}
	return nil
}

// GetTenantByEmail returns the first active tenant matching the given email address.
func (s *SQLiteStore) GetTenantByEmail(email string) (*Tenant, error) {
	row := s.db.QueryRow(`
		SELECT id,name,email,plan,status,quota_json,created_at,updated_at,
		       stripe_customer_id,stripe_subscription_id,current_period_end,billing_status,did_seed
		FROM tenants WHERE email=? AND status != 'deleted' ORDER BY created_at ASC LIMIT 1`,
		strings.ToLower(email),
	)
	return scanTenant(row)
}
