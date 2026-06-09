package billing

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	_ "modernc.org/sqlite" // SQLite driver

	"github.com/gianlucamazza/msg2agent/pkg/telemetry"
)

// Store persists tenants and API keys.
type Store interface {
	// Tenant operations
	PutTenant(t *Tenant) error
	GetTenant(id string) (*Tenant, error)
	GetTenantByEmail(email string) (*Tenant, error)
	ListTenants() ([]*Tenant, error)
	UpdateTenant(t *Tenant) error
	SuspendTenant(id string) error

	// APIKey operations
	PutAPIKey(k *APIKey) error
	GetAPIKeyByHash(hash string) (*APIKey, error)
	ListAPIKeys(tenantID string) ([]*APIKey, error)
	ListAPIKeysActive(tenantID string) ([]*APIKey, error)
	RevokeAPIKey(id string) error

	// OAuth identity operations (maps OAuth provider+sub → tenant).
	PutOAuthIdentity(provider, sub, tenantID, email string) error
	GetOAuthIdentityTenant(provider, sub string) (string, error)

	// Email verification tokens (magic-link signup verification).
	PutEmailVerificationToken(tokenHash, tenantID, email string, expiresAt time.Time) error
	// ConsumeEmailVerificationToken validates the token, deletes it, and returns
	// the associated tenantID and email. Returns ErrTokenNotFound if the token
	// is unknown or expired.
	ConsumeEmailVerificationToken(tokenHash string) (tenantID, email string, err error)

	// MarkTenantEmailVerified sets the email_verified_at timestamp on a tenant.
	MarkTenantEmailVerified(tenantID string, at time.Time) error

	// MarkStripeEventProcessed records a Stripe webhook event ID for idempotency.
	// Returns true if the event was newly inserted, false if it was already present.
	MarkStripeEventProcessed(eventID string) (bool, error)

	// Ping checks whether the store is reachable/healthy.
	Ping() error

	Close() error
}

// AdminStore provides audit and maintenance operations.
// *SQLiteStore implements both Store and AdminStore.
type AdminStore interface {
	VerifyAuditChain(tenantID string) ([]AuditChainResult, error)
	QueryEvents(f EventFilter) ([]AuditEvent, error)
	Verify() (*VerifyReport, error)
	PurgeEvents(before time.Time) (int64, error)
	Backup(destPath string) error
}

// EventStore persists billing audit events and aggregated usage for recovery.
// It is separate from Store so self-hosted deployments can opt out.
type EventStore interface {
	// RecordEvent appends a single billable event to the audit log.
	RecordEvent(tenantID, event, toolName, requestID string) error

	// LoadAggregates returns the stored monthly usage totals (for hot-cache restore).
	LoadAggregates() ([]UsageSnapshot, error)

	// FlushAggregates upserts the given snapshots into usage_aggregates.
	FlushAggregates(snapshots []UsageSnapshot) error
}

type memEmailToken struct {
	tenantID  string
	email     string
	expiresAt time.Time
}

// MemoryStore is an in-memory Store for testing and local single-tenant use.
type MemoryStore struct {
	mu               sync.RWMutex
	tenants          map[string]*Tenant
	keys             map[string]*APIKey        // keyed by hash
	oauthIds         map[string]string         // keyed by "provider:sub" → tenantID
	stripeEventsSeen map[string]struct{}       // stripe event IDs already processed
	emailTokens      map[string]*memEmailToken // keyed by token hash
}

// NewMemoryStore creates an empty in-memory billing store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		tenants:          make(map[string]*Tenant),
		keys:             make(map[string]*APIKey),
		oauthIds:         make(map[string]string),
		stripeEventsSeen: make(map[string]struct{}),
		emailTokens:      make(map[string]*memEmailToken),
	}
}

func (s *MemoryStore) PutTenant(t *Tenant) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tenants[t.ID] = t
	return nil
}

func (s *MemoryStore) GetTenant(id string) (*Tenant, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tenants[id]
	if !ok {
		return nil, ErrTenantNotFound
	}
	return t, nil
}

func (s *MemoryStore) ListTenants() ([]*Tenant, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Tenant, 0, len(s.tenants))
	for _, t := range s.tenants {
		out = append(out, t)
	}
	return out, nil
}

func (s *MemoryStore) UpdateTenant(t *Tenant) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tenants[t.ID]; !ok {
		return ErrTenantNotFound
	}
	t.UpdatedAt = time.Now().UTC()
	s.tenants[t.ID] = t
	return nil
}

func (s *MemoryStore) GetTenantByEmail(email string) (*Tenant, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, t := range s.tenants {
		if t.Email == email && t.Status != TenantStatusDeleted {
			return t, nil
		}
	}
	return nil, ErrTenantNotFound
}

func (s *MemoryStore) SuspendTenant(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tenants[id]
	if !ok {
		return ErrTenantNotFound
	}
	t.Status = TenantStatusSuspended
	t.UpdatedAt = time.Now().UTC()
	return nil
}

func (s *MemoryStore) PutAPIKey(k *APIKey) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keys[k.KeyHash] = k
	return nil
}

func (s *MemoryStore) GetAPIKeyByHash(hash string) (*APIKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	k, ok := s.keys[hash]
	if !ok {
		return nil, ErrAPIKeyNotFound
	}
	return k, nil
}

func (s *MemoryStore) ListAPIKeys(tenantID string) ([]*APIKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*APIKey
	for _, k := range s.keys {
		if k.TenantID == tenantID {
			out = append(out, k)
		}
	}
	return out, nil
}

func (s *MemoryStore) ListAPIKeysActive(tenantID string) ([]*APIKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*APIKey
	for _, k := range s.keys {
		if k.TenantID == tenantID && k.IsValid() {
			out = append(out, k)
		}
	}
	return out, nil
}

func (s *MemoryStore) RevokeAPIKey(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, k := range s.keys {
		if k.ID == id {
			now := time.Now().UTC()
			k.RevokedAt = &now
			return nil
		}
	}
	return ErrAPIKeyNotFound
}

func (s *MemoryStore) PutEmailVerificationToken(tokenHash, tenantID, email string, expiresAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.emailTokens[tokenHash] = &memEmailToken{tenantID: tenantID, email: email, expiresAt: expiresAt}
	return nil
}

func (s *MemoryStore) ConsumeEmailVerificationToken(tokenHash string) (string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tok, ok := s.emailTokens[tokenHash]
	if !ok || time.Now().After(tok.expiresAt) {
		delete(s.emailTokens, tokenHash)
		return "", "", ErrTokenNotFound
	}
	delete(s.emailTokens, tokenHash)
	return tok.tenantID, tok.email, nil
}

func (s *MemoryStore) MarkTenantEmailVerified(tenantID string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tenants[tenantID]
	if !ok {
		return ErrTenantNotFound
	}
	t.EmailVerifiedAt = &at
	t.UpdatedAt = time.Now().UTC()
	return nil
}

func (s *MemoryStore) PutOAuthIdentity(provider, sub, tenantID, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.oauthIds[provider+":"+sub] = tenantID
	return nil
}

func (s *MemoryStore) GetOAuthIdentityTenant(provider, sub string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.oauthIds[provider+":"+sub]
	if !ok {
		return "", ErrOAuthIdentityNotFound
	}
	return id, nil
}

func (s *MemoryStore) MarkStripeEventProcessed(eventID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.stripeEventsSeen[eventID]; exists {
		return false, nil
	}
	s.stripeEventsSeen[eventID] = struct{}{}
	return true, nil
}

// RecordEvent implements EventStore for MemoryStore (no-op persistence, used in tests).
func (s *MemoryStore) RecordEvent(tenantID, event, toolName, requestID string) error {
	return nil
}

// LoadAggregates implements EventStore for MemoryStore.
func (s *MemoryStore) LoadAggregates() ([]UsageSnapshot, error) {
	return nil, nil
}

// FlushAggregates implements EventStore for MemoryStore.
func (s *MemoryStore) FlushAggregates(snapshots []UsageSnapshot) error {
	return nil
}

func (s *MemoryStore) Ping() error  { return nil }
func (s *MemoryStore) Close() error { return nil }

// SQLiteStore persists billing data to a SQLite database.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore opens (or creates) a billing SQLite database at path.
func NewSQLiteStore(path string) (*SQLiteStore, error) {
	dsn := path + "?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("billing: open db: %w", err)
	}
	db.SetMaxOpenConns(1) // SQLite is single-writer
	s := &SQLiteStore{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

type migration struct {
	version int
	sql     string
}

// migrations is the ordered list of schema changes. Each entry is applied exactly
// once and recorded in schema_migrations. Never edit existing entries — add new ones.
var migrations = []migration{
	{1, `
		CREATE TABLE IF NOT EXISTS tenants (
			id          TEXT PRIMARY KEY,
			name        TEXT NOT NULL,
			email       TEXT NOT NULL,
			plan        TEXT NOT NULL,
			status      TEXT NOT NULL,
			quota_json  TEXT NOT NULL,
			created_at  TEXT NOT NULL,
			updated_at  TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS api_keys (
			id          TEXT PRIMARY KEY,
			tenant_id   TEXT NOT NULL REFERENCES tenants(id),
			name        TEXT NOT NULL,
			key_hash    TEXT NOT NULL UNIQUE,
			prefix      TEXT NOT NULL,
			created_at  TEXT NOT NULL,
			expires_at  TEXT,
			revoked_at  TEXT
		);
		CREATE INDEX IF NOT EXISTS api_keys_tenant ON api_keys(tenant_id);
		CREATE INDEX IF NOT EXISTS api_keys_hash   ON api_keys(key_hash);
		CREATE TABLE IF NOT EXISTS usage_events (
			id          TEXT PRIMARY KEY,
			tenant_id   TEXT NOT NULL,
			event       TEXT NOT NULL,
			tool_name   TEXT NOT NULL DEFAULT '',
			request_id  TEXT NOT NULL DEFAULT '',
			ts          TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS usage_events_tenant_ts ON usage_events(tenant_id, ts);
		CREATE TABLE IF NOT EXISTS usage_aggregates (
			tenant_id   TEXT NOT NULL,
			period      TEXT NOT NULL,
			event       TEXT NOT NULL,
			count       INTEGER NOT NULL DEFAULT 0,
			updated_at  TEXT NOT NULL,
			PRIMARY KEY (tenant_id, period, event)
		);
	`},
	// V2: add prev_hash column to usage_events for tamper-evidence hash chain.
	{2, `ALTER TABLE usage_events ADD COLUMN prev_hash TEXT NOT NULL DEFAULT ''`},
	// V3: OAuth identity → tenant mapping for OIDC login.
	{3, `
		CREATE TABLE IF NOT EXISTS oauth_identities (
			provider   TEXT NOT NULL,
			sub        TEXT NOT NULL,
			tenant_id  TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
			email      TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			PRIMARY KEY (provider, sub)
		);
		CREATE INDEX IF NOT EXISTS oauth_identities_tenant ON oauth_identities(tenant_id);
	`},
	// V4: Stripe billing state on tenants + idempotent webhook event log.
	// Each ALTER TABLE is run individually; duplicate-column errors are ignored
	// because SQLite does not support ALTER TABLE ... ADD COLUMN IF NOT EXISTS.
	{4, `_stripe_v4`},
	// V5: per-tenant DID seed for deterministic identity derivation (gateway pattern).
	// Existing tenants have did_seed=NULL and fall back to the shared gateway DID.
	{5, `ALTER TABLE tenants ADD COLUMN did_seed BLOB`},
	// V6: OAuth 2.1 AS tables for DCR, PKCE authorization codes, and refresh tokens.
	{6, `
		CREATE TABLE IF NOT EXISTS oauth_clients (
			client_id                    TEXT PRIMARY KEY,
			client_secret_hash           TEXT,
			client_name                  TEXT NOT NULL,
			redirect_uris                TEXT NOT NULL,
			grant_types                  TEXT NOT NULL,
			scope                        TEXT,
			token_endpoint_auth_method   TEXT NOT NULL DEFAULT 'none',
			created_at                   TEXT NOT NULL,
			created_ip                   TEXT
		);
		CREATE TABLE IF NOT EXISTS oauth_codes (
			code_hash              TEXT PRIMARY KEY,
			client_id              TEXT NOT NULL,
			tenant_id              TEXT NOT NULL,
			redirect_uri           TEXT NOT NULL,
			code_challenge         TEXT NOT NULL,
			code_challenge_method  TEXT NOT NULL,
			scope                  TEXT,
			expires_at             TEXT NOT NULL,
			used                   INTEGER NOT NULL DEFAULT 0
		);
		CREATE INDEX IF NOT EXISTS idx_oauth_codes_expires ON oauth_codes(expires_at);
		CREATE TABLE IF NOT EXISTS oauth_refresh_tokens (
			token_hash   TEXT PRIMARY KEY,
			client_id    TEXT NOT NULL,
			tenant_id    TEXT NOT NULL,
			scope        TEXT,
			expires_at   TEXT NOT NULL,
			revoked      INTEGER NOT NULL DEFAULT 0
		);
		CREATE INDEX IF NOT EXISTS idx_oauth_refresh_expires ON oauth_refresh_tokens(expires_at);
	`},
	// V7: email verification. Uses sentinel to run ALTER TABLE idempotently.
	{7, `_email_verify_v7`},
}

// emailVerifyV7Stmts are the individual SQL statements for the V7 migration.
var emailVerifyV7Stmts = []string{
	`ALTER TABLE tenants ADD COLUMN email_verified_at TEXT`,
	`CREATE TABLE IF NOT EXISTS email_verification_tokens (
		token_hash  TEXT PRIMARY KEY,
		tenant_id   TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
		email       TEXT NOT NULL,
		expires_at  TEXT NOT NULL,
		created_at  TEXT NOT NULL
	)`,
	`CREATE INDEX IF NOT EXISTS idx_email_tokens_expires ON email_verification_tokens(expires_at)`,
}

// stripeV4Stmts are the individual SQL statements for the V4 migration.
// They are run one-by-one so that ALTER TABLE "duplicate column" errors can be
// suppressed (SQLite does not support ALTER TABLE … ADD COLUMN IF NOT EXISTS).
var stripeV4Stmts = []string{
	`ALTER TABLE tenants ADD COLUMN stripe_customer_id TEXT`,
	`ALTER TABLE tenants ADD COLUMN stripe_subscription_id TEXT`,
	`ALTER TABLE tenants ADD COLUMN current_period_end TEXT`,
	`ALTER TABLE tenants ADD COLUMN billing_status TEXT NOT NULL DEFAULT 'active'`,
	`CREATE UNIQUE INDEX IF NOT EXISTS tenants_stripe_customer ON tenants(stripe_customer_id) WHERE stripe_customer_id IS NOT NULL`,
	`CREATE TABLE IF NOT EXISTS stripe_events_processed (
		event_id     TEXT PRIMARY KEY,
		processed_at TEXT NOT NULL
	)`,
}

func (s *SQLiteStore) migrate() error {
	// Bootstrap the migrations tracker table.
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("billing: create schema_migrations: %w", err)
	}

	// Determine the current schema version.
	var current int
	row := s.db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`)
	if err := row.Scan(&current); err != nil {
		return fmt.Errorf("billing: read schema version: %w", err)
	}

	// Apply each migration that hasn't been applied yet.
	for _, m := range migrations {
		if m.version <= current {
			continue
		}

		// Sentinel-based migrations run individual statements for idempotent ALTER TABLE.
		switch m.sql {
		case "_stripe_v4":
			if err := s.migrateStripeV4(); err != nil {
				return err
			}
		case "_email_verify_v7":
			if err := s.migrateEmailVerifyV7(); err != nil {
				return err
			}
		default:
			tx, err := s.db.Begin()
			if err != nil {
				return fmt.Errorf("billing: begin migration v%d: %w", m.version, err)
			}
			if _, err := tx.Exec(m.sql); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("billing: migrate v%d: %w", m.version, err)
			}
			if _, err := tx.Exec(
				`INSERT INTO schema_migrations(version, applied_at) VALUES(?, ?)`,
				m.version, time.Now().UTC().Format(time.RFC3339),
			); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("billing: record migration v%d: %w", m.version, err)
			}
			if err := tx.Commit(); err != nil {
				return fmt.Errorf("billing: commit migration v%d: %w", m.version, err)
			}
			continue
		}

		// Record V4 (and any future sentinel-based migrations) as applied.
		if _, err := s.db.Exec(
			`INSERT INTO schema_migrations(version, applied_at) VALUES(?, ?)`,
			m.version, time.Now().UTC().Format(time.RFC3339),
		); err != nil {
			return fmt.Errorf("billing: record migration v%d: %w", m.version, err)
		}
	}
	return nil
}

// migrateStripeV4 applies the V4 Stripe schema changes one statement at a time,
// ignoring "duplicate column name" errors from ALTER TABLE so the migration is
// idempotent even if it was previously partially applied.
func (s *SQLiteStore) migrateStripeV4() error {
	for _, stmt := range stripeV4Stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			if strings.Contains(err.Error(), "duplicate column name") {
				continue // column already exists — safe to skip
			}
			return fmt.Errorf("billing: migrate v4: %w", err)
		}
	}
	return nil
}

// migrateEmailVerifyV7 applies the V7 email verification schema changes one statement at
// a time, ignoring "duplicate column name" errors so the migration is idempotent.
func (s *SQLiteStore) migrateEmailVerifyV7() error {
	for _, stmt := range emailVerifyV7Stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			if strings.Contains(err.Error(), "duplicate column name") ||
				strings.Contains(err.Error(), "already exists") {
				continue
			}
			return fmt.Errorf("billing: migrate v7: %w", err)
		}
	}
	return nil
}

func (s *SQLiteStore) PutEmailVerificationToken(tokenHash, tenantID, email string, expiresAt time.Time) error {
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO email_verification_tokens(token_hash, tenant_id, email, expires_at, created_at)
		 VALUES(?, ?, ?, ?, ?)`,
		tokenHash, tenantID, email,
		expiresAt.UTC().Format(time.RFC3339),
		time.Now().UTC().Format(time.RFC3339),
	)
	return err
}

func (s *SQLiteStore) ConsumeEmailVerificationToken(tokenHash string) (string, string, error) {
	var tenantID, email, expiresAtStr string
	err := s.db.QueryRow(
		`SELECT tenant_id, email, expires_at FROM email_verification_tokens WHERE token_hash=?`,
		tokenHash,
	).Scan(&tenantID, &email, &expiresAtStr)
	if err != nil {
		return "", "", ErrTokenNotFound
	}
	expiresAt, err := time.Parse(time.RFC3339, expiresAtStr)
	if err != nil || time.Now().After(expiresAt) {
		_, _ = s.db.Exec(`DELETE FROM email_verification_tokens WHERE token_hash=?`, tokenHash)
		return "", "", ErrTokenNotFound
	}
	_, _ = s.db.Exec(`DELETE FROM email_verification_tokens WHERE token_hash=?`, tokenHash)
	return tenantID, email, nil
}

func (s *SQLiteStore) PutTenant(t *Tenant) error {
	quota, err := json.Marshal(t.Quota)
	if err != nil {
		return err
	}
	var currentPeriodEnd sql.NullString
	if t.CurrentPeriodEnd != nil {
		currentPeriodEnd = sql.NullString{String: t.CurrentPeriodEnd.UTC().Format(time.RFC3339), Valid: true}
	}
	_, err = s.db.Exec(`
		INSERT INTO tenants(id,name,email,plan,status,quota_json,created_at,updated_at,
		                    stripe_customer_id,stripe_subscription_id,current_period_end,billing_status,
		                    did_seed)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			name=excluded.name, email=excluded.email, plan=excluded.plan,
			status=excluded.status, quota_json=excluded.quota_json,
			updated_at=excluded.updated_at,
			stripe_customer_id=excluded.stripe_customer_id,
			stripe_subscription_id=excluded.stripe_subscription_id,
			current_period_end=excluded.current_period_end,
			billing_status=excluded.billing_status`,
		t.ID, t.Name, t.Email, string(t.Plan), string(t.Status),
		string(quota), t.CreatedAt.Format(time.RFC3339), t.UpdatedAt.Format(time.RFC3339),
		sqlNullStr(t.StripeCustomerID), sqlNullStr(t.StripeSubscriptionID),
		currentPeriodEnd, t.BillingStatus,
		sqlNullBytes(t.DIDSeed),
	)
	return err
}

func (s *SQLiteStore) GetTenant(id string) (*Tenant, error) {
	row := s.db.QueryRow(
		`SELECT id,name,email,plan,status,quota_json,created_at,updated_at,
		        stripe_customer_id,stripe_subscription_id,current_period_end,billing_status,
		        did_seed,email_verified_at
		 FROM tenants WHERE id=?`, id,
	)
	return scanTenant(row)
}

func (s *SQLiteStore) ListTenants() ([]*Tenant, error) {
	rows, err := s.db.Query(
		`SELECT id,name,email,plan,status,quota_json,created_at,updated_at,
		        stripe_customer_id,stripe_subscription_id,current_period_end,billing_status,
		        did_seed,email_verified_at
		 FROM tenants`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Tenant
	for rows.Next() {
		t, err := scanTenant(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) UpdateTenant(t *Tenant) error {
	quota, err := json.Marshal(t.Quota)
	if err != nil {
		return err
	}
	t.UpdatedAt = time.Now().UTC()
	var currentPeriodEnd sql.NullString
	if t.CurrentPeriodEnd != nil {
		currentPeriodEnd = sql.NullString{String: t.CurrentPeriodEnd.UTC().Format(time.RFC3339), Valid: true}
	}
	res, err := s.db.Exec(
		`UPDATE tenants SET name=?,email=?,plan=?,status=?,quota_json=?,updated_at=?,
		 stripe_customer_id=?,stripe_subscription_id=?,current_period_end=?,billing_status=?
		 WHERE id=?`,
		t.Name, t.Email, string(t.Plan), string(t.Status), string(quota),
		t.UpdatedAt.Format(time.RFC3339),
		sqlNullStr(t.StripeCustomerID), sqlNullStr(t.StripeSubscriptionID),
		currentPeriodEnd, t.BillingStatus,
		t.ID,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrTenantNotFound
	}
	return nil
}

func (s *SQLiteStore) SuspendTenant(id string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.Exec(
		`UPDATE tenants SET status=?,updated_at=? WHERE id=?`,
		string(TenantStatusSuspended), now, id,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrTenantNotFound
	}
	return nil
}

func (s *SQLiteStore) MarkTenantEmailVerified(tenantID string, at time.Time) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.Exec(
		`UPDATE tenants SET email_verified_at=?,updated_at=? WHERE id=?`,
		at.UTC().Format(time.RFC3339), now, tenantID,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrTenantNotFound
	}
	return nil
}

func (s *SQLiteStore) ListAPIKeysActive(tenantID string) ([]*APIKey, error) {
	rows, err := s.db.Query(
		`SELECT id,tenant_id,name,key_hash,prefix,created_at,expires_at,revoked_at
		 FROM api_keys
		 WHERE tenant_id=? AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > ?)`,
		tenantID, time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*APIKey
	for rows.Next() {
		k, err := scanAPIKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// auditHash computes the chain hash for a new event: sha256(prev || canonical).
func auditHash(prevHash, id, tenantID, event, toolName, requestID, ts string) string {
	canonical := strings.Join([]string{id, tenantID, event, toolName, requestID, ts}, "|")
	sum := sha256.Sum256([]byte(prevHash + canonical))
	return hex.EncodeToString(sum[:])
}

// RecordEvent implements EventStore — appends one audit event with a hash chain.
// The single DB connection means the read-last/insert is implicitly serialized.
func (s *SQLiteStore) RecordEvent(tenantID, event, toolName, requestID string) error {
	_, span := telemetry.StartSpan(context.Background(), "billing", "billing.RecordEvent")
	defer span.End()
	span.SetAttributes(
		attribute.String("billing.tenant_id", tenantID),
		attribute.String("billing.event", event),
	)

	id := newID("e")
	ts := time.Now().UTC().Format(time.RFC3339)

	// Read the previous hash for this tenant's chain (genesis = "").
	var prevHash string
	_ = s.db.QueryRow(
		`SELECT COALESCE(prev_hash,'') FROM usage_events WHERE tenant_id=? ORDER BY ts DESC LIMIT 1`,
		tenantID,
	).Scan(&prevHash)

	hash := auditHash(prevHash, id, tenantID, event, toolName, requestID, ts)

	_, err := s.db.Exec(
		`INSERT INTO usage_events(id,tenant_id,event,tool_name,request_id,ts,prev_hash) VALUES(?,?,?,?,?,?,?)`,
		id, tenantID, event, toolName, requestID, ts, hash,
	)
	if err != nil {
		span.RecordError(err)
	}
	return err
}

// AuditChainResult holds the outcome of VerifyAuditChain.
type AuditChainResult struct {
	TenantID     string
	Verified     int64
	Tampered     bool
	FirstBadID   string
	FirstBadTime time.Time
}

// VerifyAuditChain walks usage_events for tenantID in chronological order and
// recomputes each hash, reporting the first divergence. Empty tenantID = all tenants.
func (s *SQLiteStore) VerifyAuditChain(tenantID string) ([]AuditChainResult, error) {
	ctx, span := telemetry.StartSpan(context.Background(), "billing", "billing.VerifyAuditChain")
	defer span.End()
	span.SetAttributes(attribute.String("billing.tenant_id", tenantID))
	// Collect tenant IDs to verify.
	var tenants []string
	if tenantID != "" {
		tenants = []string{tenantID}
	} else {
		rows, err := s.db.Query(`SELECT DISTINCT tenant_id FROM usage_events ORDER BY tenant_id`)
		if err != nil {
			return nil, fmt.Errorf("billing: verify audit: list tenants: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var tid string
			if err := rows.Scan(&tid); err != nil {
				return nil, err
			}
			tenants = append(tenants, tid)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}

	var results []AuditChainResult
	for _, tid := range tenants {
		res := AuditChainResult{TenantID: tid}
		rows, err := s.db.Query(
			`SELECT id,tenant_id,event,tool_name,request_id,ts,prev_hash FROM usage_events WHERE tenant_id=? ORDER BY ts ASC`,
			tid,
		)
		if err != nil {
			return nil, fmt.Errorf("billing: verify audit: query tenant %s: %w", tid, err)
		}

		prevHash := ""
		for rows.Next() {
			var id, ten, ev, tool, reqID, ts, storedHash string
			if err := rows.Scan(&id, &ten, &ev, &tool, &reqID, &ts, &storedHash); err != nil {
				rows.Close()
				return nil, err
			}
			expected := auditHash(prevHash, id, ten, ev, tool, reqID, ts)
			if expected != storedHash {
				res.Tampered = true
				res.FirstBadID = id
				res.FirstBadTime, _ = time.Parse(time.RFC3339, ts)
				telemetry.AddEvent(ctx, "billing.audit_tampered",
					attribute.String("billing.tenant_id", ten),
					attribute.String("billing.first_bad_id", id))
				RecordAuditChainTampered(ten)
				rows.Close()
				goto nextTenant
			}
			prevHash = storedHash
			res.Verified++
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	nextTenant:
		results = append(results, res)
	}
	return results, nil
}

// LoadAggregates implements EventStore — reads stored monthly totals for hot-cache restore.
func (s *SQLiteStore) LoadAggregates() ([]UsageSnapshot, error) {
	rows, err := s.db.Query(`SELECT tenant_id,period,event,count FROM usage_aggregates`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UsageSnapshot
	for rows.Next() {
		var snap UsageSnapshot
		if err := rows.Scan(&snap.TenantID, &snap.Period, (*string)(&snap.Event), &snap.Count); err != nil {
			return nil, err
		}
		out = append(out, snap)
	}
	return out, rows.Err()
}

// FlushAggregates implements EventStore — upserts monthly totals.
func (s *SQLiteStore) FlushAggregates(snapshots []UsageSnapshot) error {
	if len(snapshots) == 0 {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, snap := range snapshots {
		_, err := s.db.Exec(
			`INSERT INTO usage_aggregates(tenant_id,period,event,count,updated_at) VALUES(?,?,?,?,?)
			 ON CONFLICT(tenant_id,period,event) DO UPDATE SET count=excluded.count, updated_at=excluded.updated_at`,
			snap.TenantID, snap.Period, string(snap.Event), snap.Count, now,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

// VerifyReport summarizes the billing DB state.
type VerifyReport struct {
	SchemaVersion  int
	TenantCount    int
	KeyCount       int
	AggregateCount int
}

// Verify returns counts from key billing tables and the current schema version.
func (s *SQLiteStore) Verify() (*VerifyReport, error) {
	r := &VerifyReport{}
	queries := []struct {
		dest *int
		q    string
	}{
		{&r.SchemaVersion, `SELECT COALESCE(MAX(version),0) FROM schema_migrations`},
		{&r.TenantCount, `SELECT COUNT(*) FROM tenants`},
		{&r.KeyCount, `SELECT COUNT(*) FROM api_keys WHERE revoked_at IS NULL`},
		{&r.AggregateCount, `SELECT COUNT(*) FROM usage_aggregates`},
	}
	for _, q := range queries {
		if err := s.db.QueryRow(q.q).Scan(q.dest); err != nil {
			return nil, fmt.Errorf("billing: verify: %w", err)
		}
	}
	return r, nil
}

// Backup writes a consistent snapshot of the database to destPath using VACUUM INTO.
// Safe to call while the DB is live; does not block readers or writers.
func (s *SQLiteStore) Backup(destPath string) error {
	if _, err := s.db.Exec(`VACUUM INTO ?`, destPath); err != nil {
		return fmt.Errorf("billing: backup: %w", err)
	}
	return nil
}

// Ping checks whether the database connection is alive.
func (s *SQLiteStore) Ping() error {
	return s.db.Ping()
}

// EventFilter constrains a QueryEvents call.
type EventFilter struct {
	TenantID string    // required
	Event    string    // optional; empty = all event types
	From     time.Time // zero = open-ended
	To       time.Time // zero = open-ended
	Limit    int       // 0 → default 10000
}

// AuditEvent is a row from usage_events with a parsed timestamp.
type AuditEvent struct {
	ID        string
	TenantID  string
	Event     string
	ToolName  string
	RequestID string
	Timestamp time.Time
}

// QueryEvents returns raw audit events matching filter, ordered by timestamp ASC.
// It uses the usage_events_tenant_ts index. Maximum 10000 rows unless filter.Limit is set.
func (s *SQLiteStore) QueryEvents(f EventFilter) ([]AuditEvent, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 10000
	}

	q := `SELECT id,tenant_id,event,tool_name,request_id,ts FROM usage_events WHERE tenant_id=?`
	args := []any{f.TenantID}
	if f.Event != "" {
		q += ` AND event=?`
		args = append(args, f.Event)
	}
	if !f.From.IsZero() {
		q += ` AND ts >= ?`
		args = append(args, f.From.UTC().Format(time.RFC3339))
	}
	if !f.To.IsZero() {
		q += ` AND ts <= ?`
		args = append(args, f.To.UTC().Format(time.RFC3339))
	}
	q += ` ORDER BY ts ASC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("billing: query events: %w", err)
	}
	defer rows.Close()

	var out []AuditEvent
	for rows.Next() {
		var ev AuditEvent
		var tsStr string
		if err := rows.Scan(&ev.ID, &ev.TenantID, &ev.Event, &ev.ToolName, &ev.RequestID, &tsStr); err != nil {
			return nil, err
		}
		ev.Timestamp, _ = time.Parse(time.RFC3339, tsStr)
		out = append(out, ev)
	}
	return out, rows.Err()
}

// PurgeEvents deletes audit events older than before from usage_events.
// usage_aggregates (the source of truth for invoicing) is left intact.
// Returns the number of rows deleted.
func (s *SQLiteStore) PurgeEvents(before time.Time) (int64, error) {
	res, err := s.db.Exec(
		`DELETE FROM usage_events WHERE ts < ?`,
		before.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanTenant(row scanner) (*Tenant, error) {
	var t Tenant
	var quotaJSON, createdStr, updatedStr string
	var stripeCustomerID, stripeSubscriptionID, currentPeriodEndStr, emailVerifiedAtStr sql.NullString
	var didSeed []byte
	err := row.Scan(&t.ID, &t.Name, &t.Email, (*string)(&t.Plan), (*string)(&t.Status),
		&quotaJSON, &createdStr, &updatedStr,
		&stripeCustomerID, &stripeSubscriptionID, &currentPeriodEndStr, &t.BillingStatus,
		&didSeed, &emailVerifiedAtStr)
	if err == sql.ErrNoRows {
		return nil, ErrTenantNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(quotaJSON), &t.Quota); err != nil {
		return nil, err
	}
	t.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
	t.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)
	if stripeCustomerID.Valid {
		t.StripeCustomerID = stripeCustomerID.String
	}
	if stripeSubscriptionID.Valid {
		t.StripeSubscriptionID = stripeSubscriptionID.String
	}
	if currentPeriodEndStr.Valid && currentPeriodEndStr.String != "" {
		ts, _ := time.Parse(time.RFC3339, currentPeriodEndStr.String)
		t.CurrentPeriodEnd = &ts
	}
	if len(didSeed) == 32 {
		t.DIDSeed = didSeed
	}
	if emailVerifiedAtStr.Valid && emailVerifiedAtStr.String != "" {
		ts, _ := time.Parse(time.RFC3339, emailVerifiedAtStr.String)
		t.EmailVerifiedAt = &ts
	}
	return &t, nil
}

// sqlNullBytes converts a possibly-nil byte slice into a sql.RawBytes-compatible value.
func sqlNullBytes(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}

// sqlNullStr converts a possibly-empty string into a sql.NullString.
func sqlNullStr(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

func (s *SQLiteStore) PutAPIKey(k *APIKey) error {
	var expiresAt, revokedAt sql.NullString
	if k.ExpiresAt != nil {
		expiresAt = sql.NullString{String: k.ExpiresAt.Format(time.RFC3339), Valid: true}
	}
	if k.RevokedAt != nil {
		revokedAt = sql.NullString{String: k.RevokedAt.Format(time.RFC3339), Valid: true}
	}
	_, err := s.db.Exec(`
		INSERT INTO api_keys(id,tenant_id,name,key_hash,prefix,created_at,expires_at,revoked_at)
		VALUES(?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET revoked_at=excluded.revoked_at`,
		k.ID, k.TenantID, k.Name, k.KeyHash, k.Prefix,
		k.CreatedAt.Format(time.RFC3339), expiresAt, revokedAt,
	)
	return err
}

func (s *SQLiteStore) GetAPIKeyByHash(hash string) (*APIKey, error) {
	row := s.db.QueryRow(
		`SELECT id,tenant_id,name,key_hash,prefix,created_at,expires_at,revoked_at FROM api_keys WHERE key_hash=?`, hash,
	)
	return scanAPIKey(row)
}

func (s *SQLiteStore) ListAPIKeys(tenantID string) ([]*APIKey, error) {
	rows, err := s.db.Query(
		`SELECT id,tenant_id,name,key_hash,prefix,created_at,expires_at,revoked_at FROM api_keys WHERE tenant_id=?`, tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*APIKey
	for rows.Next() {
		k, err := scanAPIKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) RevokeAPIKey(id string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.Exec(`UPDATE api_keys SET revoked_at=? WHERE id=?`, now, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrAPIKeyNotFound
	}
	return nil
}

func (s *SQLiteStore) PutOAuthIdentity(provider, sub, tenantID, email string) error {
	_, err := s.db.Exec(
		`INSERT INTO oauth_identities(provider,sub,tenant_id,email,created_at)
		 VALUES(?,?,?,?,?)
		 ON CONFLICT(provider,sub) DO UPDATE SET tenant_id=excluded.tenant_id, email=excluded.email`,
		provider, sub, tenantID, email, time.Now().UTC().Format(time.RFC3339),
	)
	return err
}

func (s *SQLiteStore) GetOAuthIdentityTenant(provider, sub string) (string, error) {
	var tenantID string
	err := s.db.QueryRow(
		`SELECT tenant_id FROM oauth_identities WHERE provider=? AND sub=?`,
		provider, sub,
	).Scan(&tenantID)
	if err == sql.ErrNoRows {
		return "", ErrOAuthIdentityNotFound
	}
	return tenantID, err
}

// MarkStripeEventProcessed records a Stripe webhook event ID to ensure idempotency.
// Returns true if the event was newly inserted, false if it was already present.
func (s *SQLiteStore) MarkStripeEventProcessed(eventID string) (bool, error) {
	res, err := s.db.Exec(
		`INSERT INTO stripe_events_processed(event_id, processed_at) VALUES(?, ?)
		 ON CONFLICT(event_id) DO NOTHING`,
		eventID, time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *SQLiteStore) Close() error { return s.db.Close() }

func scanAPIKey(row scanner) (*APIKey, error) {
	var k APIKey
	var createdStr string
	var expiresStr, revokedStr sql.NullString
	err := row.Scan(&k.ID, &k.TenantID, &k.Name, &k.KeyHash, &k.Prefix,
		&createdStr, &expiresStr, &revokedStr)
	if err == sql.ErrNoRows {
		return nil, ErrAPIKeyNotFound
	}
	if err != nil {
		return nil, err
	}
	k.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
	if expiresStr.Valid {
		t, _ := time.Parse(time.RFC3339, expiresStr.String)
		k.ExpiresAt = &t
	}
	if revokedStr.Valid {
		t, _ := time.Parse(time.RFC3339, revokedStr.String)
		k.RevokedAt = &t
	}
	return &k, nil
}
