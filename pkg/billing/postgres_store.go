package billing

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/gianlucamazza/msg2agent/pkg/telemetry"
	_ "github.com/jackc/pgx/v5/stdlib" // PostgreSQL driver registered as "pgx"
)

// PostgresStore persists billing data to a PostgreSQL database.
type PostgresStore struct {
	db *sql.DB
}

// pgMigration is a single versioned schema change for Postgres.
type pgMigration struct {
	version int
	sql     string
}

// pgMigrations is the ordered list of Postgres schema changes. Never edit existing
// entries — add new ones. V1 includes prev_hash from the start (unlike SQLite V2).
var pgMigrations = []pgMigration{
	{1, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    INTEGER PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		CREATE TABLE IF NOT EXISTS tenants (
			id         TEXT PRIMARY KEY,
			name       TEXT NOT NULL,
			email      TEXT NOT NULL,
			plan       TEXT NOT NULL DEFAULT 'free',
			status     TEXT NOT NULL DEFAULT 'active',
			quota      JSONB NOT NULL DEFAULT '{}',
			created_at TIMESTAMPTZ NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL
		);
		CREATE TABLE IF NOT EXISTS api_keys (
			id         TEXT PRIMARY KEY,
			tenant_id  TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
			name       TEXT NOT NULL,
			key_hash   TEXT NOT NULL UNIQUE,
			prefix     TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL,
			expires_at TIMESTAMPTZ,
			revoked_at TIMESTAMPTZ
		);
		CREATE INDEX IF NOT EXISTS api_keys_tenant ON api_keys(tenant_id);
		CREATE TABLE IF NOT EXISTS usage_events (
			id          TEXT NOT NULL,
			tenant_id   TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
			event       TEXT NOT NULL,
			tool_name   TEXT NOT NULL,
			request_id  TEXT NOT NULL,
			recorded_at TIMESTAMPTZ NOT NULL,
			hash        TEXT NOT NULL,
			prev_hash   TEXT NOT NULL DEFAULT ''
		);
		CREATE INDEX IF NOT EXISTS usage_events_tenant ON usage_events(tenant_id, recorded_at);
		CREATE TABLE IF NOT EXISTS usage_aggregates (
			tenant_id TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
			period    TEXT NOT NULL,
			event     TEXT NOT NULL,
			count     BIGINT NOT NULL DEFAULT 0,
			PRIMARY KEY (tenant_id, period, event)
		);
	`},
	// V2: nothing (prev_hash already included in V1 for Postgres)
	{2, `SELECT 1`},
	// V3: OAuth identity → tenant mapping for OIDC login.
	{3, `
		CREATE TABLE IF NOT EXISTS oauth_identities (
			provider   TEXT NOT NULL,
			sub        TEXT NOT NULL,
			tenant_id  TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
			email      TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (provider, sub)
		);
		CREATE INDEX IF NOT EXISTS oauth_identities_tenant ON oauth_identities(tenant_id);
	`},
	// V4: Stripe billing state on tenants + idempotent webhook event log.
	{4, `
		ALTER TABLE tenants ADD COLUMN IF NOT EXISTS stripe_customer_id TEXT;
		ALTER TABLE tenants ADD COLUMN IF NOT EXISTS stripe_subscription_id TEXT;
		ALTER TABLE tenants ADD COLUMN IF NOT EXISTS current_period_end TIMESTAMPTZ;
		ALTER TABLE tenants ADD COLUMN IF NOT EXISTS billing_status TEXT NOT NULL DEFAULT 'active';
		CREATE UNIQUE INDEX IF NOT EXISTS tenants_stripe_customer ON tenants(stripe_customer_id) WHERE stripe_customer_id IS NOT NULL;
		CREATE TABLE IF NOT EXISTS stripe_events_processed (
			event_id     TEXT PRIMARY KEY,
			processed_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
	`},
	// V5: per-tenant DID seed for deterministic identity derivation (gateway pattern).
	{5, `ALTER TABLE tenants ADD COLUMN IF NOT EXISTS did_seed BYTEA`},
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
			created_at                   TIMESTAMPTZ NOT NULL,
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
			expires_at             TIMESTAMPTZ NOT NULL,
			used                   INTEGER NOT NULL DEFAULT 0
		);
		CREATE INDEX IF NOT EXISTS idx_oauth_codes_expires ON oauth_codes(expires_at);
		CREATE TABLE IF NOT EXISTS oauth_refresh_tokens (
			token_hash   TEXT PRIMARY KEY,
			client_id    TEXT NOT NULL,
			tenant_id    TEXT NOT NULL,
			scope        TEXT,
			expires_at   TIMESTAMPTZ NOT NULL,
			revoked      INTEGER NOT NULL DEFAULT 0
		);
		CREATE INDEX IF NOT EXISTS idx_oauth_refresh_expires ON oauth_refresh_tokens(expires_at);
	`},
	// V7: email verification tokens + EmailVerifiedAt on tenants.
	{7, `
		ALTER TABLE tenants ADD COLUMN IF NOT EXISTS email_verified_at TIMESTAMPTZ;
		CREATE TABLE IF NOT EXISTS email_verification_tokens (
			token_hash  TEXT PRIMARY KEY,
			tenant_id   TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
			email       TEXT NOT NULL,
			expires_at  TIMESTAMPTZ NOT NULL,
			created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_email_tokens_expires ON email_verification_tokens(expires_at);
	`},
}

// NewPostgresStore opens a billing Postgres database at dsn and runs migrations.
func NewPostgresStore(dsn string) (*PostgresStore, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("billing: open postgres db: %w", err)
	}
	s := &PostgresStore{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *PostgresStore) migrate() error {
	// Bootstrap the migrations tracker table (must exist before we query it).
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`); err != nil {
		return fmt.Errorf("billing: pg create schema_migrations: %w", err)
	}

	var current int
	row := s.db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`)
	if err := row.Scan(&current); err != nil {
		return fmt.Errorf("billing: pg read schema version: %w", err)
	}

	for _, m := range pgMigrations {
		if m.version <= current {
			continue
		}
		tx, err := s.db.Begin()
		if err != nil {
			return fmt.Errorf("billing: pg begin migration v%d: %w", m.version, err)
		}
		if _, err := tx.Exec(m.sql); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("billing: pg migrate v%d: %w", m.version, err)
		}
		if _, err := tx.Exec(
			`INSERT INTO schema_migrations(version, applied_at) VALUES($1, NOW())
			 ON CONFLICT DO NOTHING`,
			m.version,
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("billing: pg record migration v%d: %w", m.version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("billing: pg commit migration v%d: %w", m.version, err)
		}
	}
	return nil
}

// -------------------------------------------------------------------------
// Store interface — Tenant operations
// -------------------------------------------------------------------------

func (s *PostgresStore) PutTenant(t *Tenant) error {
	quota, err := json.Marshal(t.Quota)
	if err != nil {
		return err
	}
	var currentPeriodEnd interface{}
	if t.CurrentPeriodEnd != nil {
		currentPeriodEnd = t.CurrentPeriodEnd.UTC()
	}
	var didSeed any
	if len(t.DIDSeed) == 32 {
		didSeed = t.DIDSeed
	}
	_, err = s.db.Exec(`
		INSERT INTO tenants(id,name,email,plan,status,quota,created_at,updated_at,
		                    stripe_customer_id,stripe_subscription_id,current_period_end,billing_status,did_seed)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		ON CONFLICT(id) DO UPDATE SET
			name=EXCLUDED.name, email=EXCLUDED.email, plan=EXCLUDED.plan,
			status=EXCLUDED.status, quota=EXCLUDED.quota,
			updated_at=EXCLUDED.updated_at,
			stripe_customer_id=EXCLUDED.stripe_customer_id,
			stripe_subscription_id=EXCLUDED.stripe_subscription_id,
			current_period_end=EXCLUDED.current_period_end,
			billing_status=EXCLUDED.billing_status,
			did_seed=EXCLUDED.did_seed`,
		t.ID, t.Name, t.Email, string(t.Plan), string(t.Status),
		string(quota), t.CreatedAt.UTC(), t.UpdatedAt.UTC(),
		pgNullStr(t.StripeCustomerID), pgNullStr(t.StripeSubscriptionID),
		currentPeriodEnd, t.BillingStatus, didSeed,
	)
	return err
}

func (s *PostgresStore) GetTenant(id string) (*Tenant, error) {
	row := s.db.QueryRow(
		`SELECT id,name,email,plan,status,quota,created_at,updated_at,
		        stripe_customer_id,stripe_subscription_id,current_period_end,billing_status,did_seed,email_verified_at
		 FROM tenants WHERE id=$1`, id,
	)
	return pgScanTenant(row)
}

func (s *PostgresStore) GetTenantByEmail(email string) (*Tenant, error) {
	row := s.db.QueryRow(
		`SELECT id,name,email,plan,status,quota,created_at,updated_at,
		        stripe_customer_id,stripe_subscription_id,current_period_end,billing_status,did_seed,email_verified_at
		 FROM tenants WHERE email=$1 AND status != 'deleted' ORDER BY created_at ASC LIMIT 1`,
		strings.ToLower(email),
	)
	t, err := pgScanTenant(row)
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (s *PostgresStore) ListTenants() ([]*Tenant, error) {
	rows, err := s.db.Query(
		`SELECT id,name,email,plan,status,quota,created_at,updated_at,
		        stripe_customer_id,stripe_subscription_id,current_period_end,billing_status,did_seed,email_verified_at
		 FROM tenants`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Tenant
	for rows.Next() {
		t, err := pgScanTenant(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *PostgresStore) UpdateTenant(t *Tenant) error {
	quota, err := json.Marshal(t.Quota)
	if err != nil {
		return err
	}
	t.UpdatedAt = time.Now().UTC()
	res, err := s.db.Exec(
		`UPDATE tenants SET name=$1,email=$2,plan=$3,status=$4,quota=$5,updated_at=$6 WHERE id=$7`,
		t.Name, t.Email, string(t.Plan), string(t.Status), string(quota),
		t.UpdatedAt.UTC(), t.ID,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrTenantNotFound
	}
	return nil
}

func (s *PostgresStore) SuspendTenant(id string) error {
	res, err := s.db.Exec(
		`UPDATE tenants SET status=$1,updated_at=NOW() WHERE id=$2`,
		string(TenantStatusSuspended), id,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrTenantNotFound
	}
	return nil
}

// -------------------------------------------------------------------------
// Store interface — APIKey operations
// -------------------------------------------------------------------------

func (s *PostgresStore) PutAPIKey(k *APIKey) error {
	var expiresAt, revokedAt interface{}
	if k.ExpiresAt != nil {
		expiresAt = k.ExpiresAt.UTC()
	}
	if k.RevokedAt != nil {
		revokedAt = k.RevokedAt.UTC()
	}
	_, err := s.db.Exec(`
		INSERT INTO api_keys(id,tenant_id,name,key_hash,prefix,created_at,expires_at,revoked_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT(id) DO UPDATE SET revoked_at=EXCLUDED.revoked_at`,
		k.ID, k.TenantID, k.Name, k.KeyHash, k.Prefix,
		k.CreatedAt.UTC(), expiresAt, revokedAt,
	)
	return err
}

func (s *PostgresStore) GetAPIKeyByHash(hash string) (*APIKey, error) {
	row := s.db.QueryRow(
		`SELECT id,tenant_id,name,key_hash,prefix,created_at,expires_at,revoked_at FROM api_keys WHERE key_hash=$1`, hash,
	)
	return pgScanAPIKey(row)
}

func (s *PostgresStore) ListAPIKeys(tenantID string) ([]*APIKey, error) {
	rows, err := s.db.Query(
		`SELECT id,tenant_id,name,key_hash,prefix,created_at,expires_at,revoked_at FROM api_keys WHERE tenant_id=$1`, tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*APIKey
	for rows.Next() {
		k, err := pgScanAPIKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (s *PostgresStore) ListAPIKeysActive(tenantID string) ([]*APIKey, error) {
	rows, err := s.db.Query(
		`SELECT id,tenant_id,name,key_hash,prefix,created_at,expires_at,revoked_at
		 FROM api_keys
		 WHERE tenant_id=$1 AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > NOW())`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*APIKey
	for rows.Next() {
		k, err := pgScanAPIKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (s *PostgresStore) RevokeAPIKey(id string) error {
	res, err := s.db.Exec(`UPDATE api_keys SET revoked_at=NOW() WHERE id=$1`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrAPIKeyNotFound
	}
	return nil
}

func (s *PostgresStore) RenameAPIKey(id, name string) error {
	res, err := s.db.Exec(`UPDATE api_keys SET name=$1 WHERE id=$2`, name, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrAPIKeyNotFound
	}
	return nil
}

// -------------------------------------------------------------------------
// Store interface — OAuth identity operations
// -------------------------------------------------------------------------

func (s *PostgresStore) PutOAuthIdentity(provider, sub, tenantID, email string) error {
	_, err := s.db.Exec(
		`INSERT INTO oauth_identities(provider,sub,tenant_id,email,created_at)
		 VALUES($1,$2,$3,$4,NOW())
		 ON CONFLICT(provider,sub) DO UPDATE SET tenant_id=EXCLUDED.tenant_id, email=EXCLUDED.email`,
		provider, sub, tenantID, email,
	)
	return err
}

func (s *PostgresStore) GetOAuthIdentityTenant(provider, sub string) (string, error) {
	var tenantID string
	err := s.db.QueryRow(
		`SELECT tenant_id FROM oauth_identities WHERE provider=$1 AND sub=$2`,
		provider, sub,
	).Scan(&tenantID)
	if err == sql.ErrNoRows {
		return "", ErrOAuthIdentityNotFound
	}
	return tenantID, err
}

// -------------------------------------------------------------------------
// Store interface — Ping / Close
// -------------------------------------------------------------------------

// MarkStripeEventProcessed records a Stripe webhook event ID for idempotency.
// Returns true if the event was newly inserted, false if it was already present.
func (s *PostgresStore) PutEmailVerificationToken(tokenHash, tenantID, email string, expiresAt time.Time) error {
	_, err := s.db.Exec(
		`INSERT INTO email_verification_tokens(token_hash, tenant_id, email, expires_at, created_at)
		 VALUES($1,$2,$3,$4,$5)
		 ON CONFLICT(token_hash) DO UPDATE SET expires_at=excluded.expires_at`,
		tokenHash, tenantID, email, expiresAt.UTC(), time.Now().UTC(),
	)
	return err
}

func (s *PostgresStore) ConsumeEmailVerificationToken(tokenHash string) (string, string, error) {
	var tenantID, email string
	var expiresAt time.Time
	err := s.db.QueryRow(
		`DELETE FROM email_verification_tokens WHERE token_hash=$1 RETURNING tenant_id, email, expires_at`,
		tokenHash,
	).Scan(&tenantID, &email, &expiresAt)
	if err != nil {
		return "", "", ErrTokenNotFound
	}
	if time.Now().After(expiresAt) {
		return "", "", ErrTokenNotFound
	}
	return tenantID, email, nil
}

func (s *PostgresStore) MarkTenantEmailVerified(tenantID string, at time.Time) error {
	res, err := s.db.Exec(
		`UPDATE tenants SET email_verified_at=$1, updated_at=NOW() WHERE id=$2`,
		at.UTC(), tenantID,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrTenantNotFound
	}
	return nil
}

func (s *PostgresStore) MarkStripeEventProcessed(eventID string) (bool, error) {
	res, err := s.db.Exec(
		`INSERT INTO stripe_events_processed(event_id, processed_at) VALUES($1, NOW())
		 ON CONFLICT(event_id) DO NOTHING`,
		eventID,
	)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *PostgresStore) Ping() error  { return s.db.Ping() }
func (s *PostgresStore) Close() error { return s.db.Close() }

// -------------------------------------------------------------------------
// EventStore interface
// -------------------------------------------------------------------------

// RecordEvent appends one audit event with a hash chain, serialized per tenant
// via pg_advisory_xact_lock to prevent concurrent hash-chain corruption.
func (s *PostgresStore) RecordEvent(tenantID, event, toolName, requestID string) error {
	_, span := telemetry.StartSpan(context.Background(), "billing", "billing.pg.RecordEvent")
	defer span.End()
	span.SetAttributes(
		attribute.String("billing.tenant_id", tenantID),
		attribute.String("billing.event", event),
	)

	id := newID("e")
	ts := time.Now().UTC()

	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("billing: pg record event begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Serialize intra-tenant inserts so hash-chain reads are consistent.
	if _, err := tx.ExecContext(context.Background(),
		`SELECT pg_advisory_xact_lock(hashtext($1))`, tenantID,
	); err != nil {
		return fmt.Errorf("billing: pg advisory lock: %w", err)
	}

	var prevHash string
	_ = tx.QueryRowContext(context.Background(),
		`SELECT COALESCE(hash,'') FROM usage_events WHERE tenant_id=$1 ORDER BY recorded_at DESC LIMIT 1`,
		tenantID,
	).Scan(&prevHash)

	hash := auditHash(prevHash, id, tenantID, event, toolName, requestID, ts.Format(time.RFC3339))

	if _, err := tx.ExecContext(context.Background(),
		`INSERT INTO usage_events(id,tenant_id,event,tool_name,request_id,recorded_at,hash,prev_hash)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8)`,
		id, tenantID, event, toolName, requestID, ts, hash, prevHash,
	); err != nil {
		span.RecordError(err)
		return err
	}
	return tx.Commit()
}

// LoadAggregates reads stored monthly totals for hot-cache restore.
func (s *PostgresStore) LoadAggregates() ([]UsageSnapshot, error) {
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

// FlushAggregates upserts monthly totals.
func (s *PostgresStore) FlushAggregates(snapshots []UsageSnapshot) error {
	if len(snapshots) == 0 {
		return nil
	}
	for _, snap := range snapshots {
		_, err := s.db.Exec(
			`INSERT INTO usage_aggregates(tenant_id,period,event,count) VALUES($1,$2,$3,$4)
			 ON CONFLICT(tenant_id,period,event) DO UPDATE SET count=EXCLUDED.count`,
			snap.TenantID, snap.Period, string(snap.Event), snap.Count,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

// -------------------------------------------------------------------------
// AdminStore interface
// -------------------------------------------------------------------------

// VerifyAuditChain walks usage_events for tenantID in chronological order and
// recomputes each hash, reporting the first divergence. Empty tenantID = all tenants.
func (s *PostgresStore) VerifyAuditChain(tenantID string) ([]AuditChainResult, error) {
	ctx, span := telemetry.StartSpan(context.Background(), "billing", "billing.pg.VerifyAuditChain")
	defer span.End()
	span.SetAttributes(attribute.String("billing.tenant_id", tenantID))

	var tenants []string
	if tenantID != "" {
		tenants = []string{tenantID}
	} else {
		rows, err := s.db.Query(`SELECT DISTINCT tenant_id FROM usage_events ORDER BY tenant_id`)
		if err != nil {
			return nil, fmt.Errorf("billing: pg verify audit: list tenants: %w", err)
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
			`SELECT id,tenant_id,event,tool_name,request_id,recorded_at,hash,prev_hash
			 FROM usage_events WHERE tenant_id=$1 ORDER BY recorded_at ASC`,
			tid,
		)
		if err != nil {
			return nil, fmt.Errorf("billing: pg verify audit: query tenant %s: %w", tid, err)
		}

		prevHash := ""
		for rows.Next() {
			var id, ten, ev, tool, reqID, storedHash, storedPrev string
			var recordedAt time.Time
			if err := rows.Scan(&id, &ten, &ev, &tool, &reqID, &recordedAt, &storedHash, &storedPrev); err != nil {
				rows.Close()
				return nil, err
			}
			ts := recordedAt.UTC().Format(time.RFC3339)
			expected := auditHash(prevHash, id, ten, ev, tool, reqID, ts)
			if expected != storedHash {
				res.Tampered = true
				res.FirstBadID = id
				res.FirstBadTime = recordedAt
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

// QueryEvents returns raw audit events matching filter, ordered by timestamp ASC.
func (s *PostgresStore) QueryEvents(f EventFilter) ([]AuditEvent, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 10000
	}

	argN := 1
	nextArg := func() string {
		p := fmt.Sprintf("$%d", argN)
		argN++
		return p
	}

	var sb strings.Builder
	sb.WriteString(`SELECT id,tenant_id,event,tool_name,request_id,recorded_at FROM usage_events WHERE tenant_id=`)
	sb.WriteString(nextArg())
	args := []any{f.TenantID}

	if f.Event != "" {
		sb.WriteString(` AND event=`)
		sb.WriteString(nextArg())
		args = append(args, f.Event)
	}
	if !f.From.IsZero() {
		sb.WriteString(` AND recorded_at >= `)
		sb.WriteString(nextArg())
		args = append(args, f.From.UTC())
	}
	if !f.To.IsZero() {
		sb.WriteString(` AND recorded_at <= `)
		sb.WriteString(nextArg())
		args = append(args, f.To.UTC())
	}
	sb.WriteString(` ORDER BY recorded_at ASC LIMIT `)
	sb.WriteString(nextArg())
	args = append(args, limit)

	rows, err := s.db.Query(sb.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("billing: pg query events: %w", err)
	}
	defer rows.Close()

	var out []AuditEvent
	for rows.Next() {
		var ev AuditEvent
		if err := rows.Scan(&ev.ID, &ev.TenantID, &ev.Event, &ev.ToolName, &ev.RequestID, &ev.Timestamp); err != nil {
			return nil, err
		}
		ev.Timestamp = ev.Timestamp.UTC()
		out = append(out, ev)
	}
	return out, rows.Err()
}

// Verify performs a lightweight health check (SELECT 1 FROM tenants LIMIT 1).
func (s *PostgresStore) ListAggregatesByTenantPeriod(tenantID, period string) ([]UsageSnapshot, error) {
	q := `SELECT tenant_id, period, event, count FROM usage_aggregates WHERE tenant_id=$1`
	args := []any{tenantID}
	if period != "" {
		q += ` AND period=$2`
		args = append(args, period)
	}
	q += ` ORDER BY period DESC, event ASC`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("billing: pg list aggregates: %w", err)
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

func (s *PostgresStore) QueryToolBreakdown(tenantID, period string) ([]ToolUsageRow, error) {
	argN := 1
	nextArg := func() string { p := fmt.Sprintf("$%d", argN); argN++; return p }

	q := `SELECT tool_name, COUNT(*) AS cnt FROM usage_events WHERE tenant_id=` + nextArg() +
		` AND event='tool_calls' AND tool_name != ''`
	args := []any{tenantID}

	if period != "" {
		var year, month int
		if _, err := fmt.Sscanf(period, "%d-%d", &year, &month); err == nil && month >= 1 && month <= 12 {
			from := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
			var to time.Time
			if month == 12 {
				to = time.Date(year+1, 1, 1, 0, 0, 0, 0, time.UTC)
			} else {
				to = time.Date(year, time.Month(month+1), 1, 0, 0, 0, 0, time.UTC)
			}
			q += ` AND recorded_at >= ` + nextArg() + ` AND recorded_at < ` + nextArg()
			args = append(args, from, to)
		}
	}
	q += ` GROUP BY tool_name ORDER BY cnt DESC`

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("billing: pg tool breakdown: %w", err)
	}
	defer rows.Close()
	var out []ToolUsageRow
	for rows.Next() {
		var r ToolUsageRow
		if err := rows.Scan(&r.ToolName, &r.Count); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *PostgresStore) Verify() (*VerifyReport, error) {
	r := &VerifyReport{}
	type kv struct {
		dest *int
		q    string
	}
	queries := []kv{
		{&r.SchemaVersion, `SELECT COALESCE(MAX(version),0) FROM schema_migrations`},
		{&r.TenantCount, `SELECT COUNT(*) FROM tenants`},
		{&r.KeyCount, `SELECT COUNT(*) FROM api_keys WHERE revoked_at IS NULL`},
		{&r.AggregateCount, `SELECT COUNT(*) FROM usage_aggregates`},
	}
	for _, q := range queries {
		if err := s.db.QueryRow(q.q).Scan(q.dest); err != nil {
			return nil, fmt.Errorf("billing: pg verify: %w", err)
		}
	}
	return r, nil
}

// PurgeEvents deletes audit events older than before from usage_events.
// usage_aggregates (the source of truth for invoicing) is left intact.
// Returns the number of rows deleted.
func (s *PostgresStore) PurgeEvents(before time.Time) (int64, error) {
	res, err := s.db.Exec(
		`DELETE FROM usage_events WHERE recorded_at < $1`, before.UTC(),
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// Backup attempts to run pg_dump to destPath. The pg_dump binary path can be
// overridden via the MSG2AGENT_PG_DUMP environment variable.
func (s *PostgresStore) Backup(destPath string) error {
	pgDump := os.Getenv("MSG2AGENT_PG_DUMP")
	if pgDump == "" {
		pgDump = "pg_dump"
	}
	// pg_dump reads the DSN from the standard PG* env vars or $DATABASE_URL.
	// Callers must set PGDATABASE, PGHOST, PGUSER, PGPASSWORD (or PGSERVICE) before calling.
	cmd := exec.Command(pgDump, "--file="+destPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("billing: pg backup: %w\n%s\n"+
			"Hint: set MSG2AGENT_PG_DUMP to the full path of pg_dump and ensure "+
			"PGHOST/PGDATABASE/PGUSER/PGPASSWORD are configured", err, out)
	}
	return nil
}

// -------------------------------------------------------------------------
// Internal scan helpers
// -------------------------------------------------------------------------

func pgScanTenant(row scanner) (*Tenant, error) {
	var t Tenant
	var quotaJSON string
	var createdAt, updatedAt time.Time
	var stripeCustomerID, stripeSubscriptionID sql.NullString
	var currentPeriodEnd, emailVerifiedAt sql.NullTime
	var didSeed []byte
	err := row.Scan(&t.ID, &t.Name, &t.Email, (*string)(&t.Plan), (*string)(&t.Status),
		&quotaJSON, &createdAt, &updatedAt,
		&stripeCustomerID, &stripeSubscriptionID, &currentPeriodEnd, &t.BillingStatus,
		&didSeed, &emailVerifiedAt)
	if err == sql.ErrNoRows {
		return nil, ErrTenantNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(quotaJSON), &t.Quota); err != nil {
		return nil, err
	}
	t.CreatedAt = createdAt.UTC()
	t.UpdatedAt = updatedAt.UTC()
	if stripeCustomerID.Valid {
		t.StripeCustomerID = stripeCustomerID.String
	}
	if stripeSubscriptionID.Valid {
		t.StripeSubscriptionID = stripeSubscriptionID.String
	}
	if currentPeriodEnd.Valid {
		ts := currentPeriodEnd.Time.UTC()
		t.CurrentPeriodEnd = &ts
	}
	if len(didSeed) == 32 {
		t.DIDSeed = didSeed
	}
	if emailVerifiedAt.Valid {
		ts := emailVerifiedAt.Time.UTC()
		t.EmailVerifiedAt = &ts
	}
	return &t, nil
}

// pgNullStr converts a possibly-empty string into a sql.NullString.
func pgNullStr(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

func pgScanAPIKey(row scanner) (*APIKey, error) {
	var k APIKey
	var createdAt time.Time
	var expiresAt, revokedAt sql.NullTime
	err := row.Scan(&k.ID, &k.TenantID, &k.Name, &k.KeyHash, &k.Prefix,
		&createdAt, &expiresAt, &revokedAt)
	if err == sql.ErrNoRows {
		return nil, ErrAPIKeyNotFound
	}
	if err != nil {
		return nil, err
	}
	k.CreatedAt = createdAt.UTC()
	if expiresAt.Valid {
		t := expiresAt.Time.UTC()
		k.ExpiresAt = &t
	}
	if revokedAt.Valid {
		t := revokedAt.Time.UTC()
		k.RevokedAt = &t
	}
	return &k, nil
}
