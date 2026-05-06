package billing

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	_ "modernc.org/sqlite" // SQLite driver
)

// Store persists tenants and API keys.
type Store interface {
	// Tenant operations
	PutTenant(t *Tenant) error
	GetTenant(id string) (*Tenant, error)
	ListTenants() ([]*Tenant, error)
	UpdateTenant(t *Tenant) error
	SuspendTenant(id string) error

	// APIKey operations
	PutAPIKey(k *APIKey) error
	GetAPIKeyByHash(hash string) (*APIKey, error)
	ListAPIKeys(tenantID string) ([]*APIKey, error)
	ListAPIKeysActive(tenantID string) ([]*APIKey, error)
	RevokeAPIKey(id string) error

	Close() error
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

// MemoryStore is an in-memory Store for testing and local single-tenant use.
type MemoryStore struct {
	mu      sync.RWMutex
	tenants map[string]*Tenant
	keys    map[string]*APIKey // keyed by hash
}

// NewMemoryStore creates an empty in-memory billing store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		tenants: make(map[string]*Tenant),
		keys:    make(map[string]*APIKey),
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

func (s *MemoryStore) Close() error { return nil }

// SQLiteStore persists billing data to a SQLite database.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore opens (or creates) a billing SQLite database at path.
func NewSQLiteStore(path string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", path)
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

func (s *SQLiteStore) migrate() error {
	_, err := s.db.Exec(`
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
	`)
	return err
}

func (s *SQLiteStore) PutTenant(t *Tenant) error {
	quota, err := json.Marshal(t.Quota)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`
		INSERT INTO tenants(id,name,email,plan,status,quota_json,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			name=excluded.name, email=excluded.email, plan=excluded.plan,
			status=excluded.status, quota_json=excluded.quota_json,
			updated_at=excluded.updated_at`,
		t.ID, t.Name, t.Email, string(t.Plan), string(t.Status),
		string(quota), t.CreatedAt.Format(time.RFC3339), t.UpdatedAt.Format(time.RFC3339),
	)
	return err
}

func (s *SQLiteStore) GetTenant(id string) (*Tenant, error) {
	row := s.db.QueryRow(
		`SELECT id,name,email,plan,status,quota_json,created_at,updated_at FROM tenants WHERE id=?`, id,
	)
	return scanTenant(row)
}

func (s *SQLiteStore) ListTenants() ([]*Tenant, error) {
	rows, err := s.db.Query(`SELECT id,name,email,plan,status,quota_json,created_at,updated_at FROM tenants`)
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
	res, err := s.db.Exec(
		`UPDATE tenants SET name=?,email=?,plan=?,status=?,quota_json=?,updated_at=? WHERE id=?`,
		t.Name, t.Email, string(t.Plan), string(t.Status), string(quota),
		t.UpdatedAt.Format(time.RFC3339), t.ID,
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

// RecordEvent implements EventStore — appends one audit event.
func (s *SQLiteStore) RecordEvent(tenantID, event, toolName, requestID string) error {
	_, err := s.db.Exec(
		`INSERT INTO usage_events(id,tenant_id,event,tool_name,request_id,ts) VALUES(?,?,?,?,?,?)`,
		newID("e"), tenantID, event, toolName, requestID, time.Now().UTC().Format(time.RFC3339),
	)
	return err
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
	err := row.Scan(&t.ID, &t.Name, &t.Email, (*string)(&t.Plan), (*string)(&t.Status),
		&quotaJSON, &createdStr, &updatedStr)
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
	return &t, nil
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
