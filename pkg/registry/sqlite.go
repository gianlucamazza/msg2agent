package registry

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

// SQLiteStore is a SQLite-backed implementation of Store.
type SQLiteStore struct {
	db     *sql.DB
	logger *slog.Logger
	mu     sync.RWMutex // protects concurrent access
}

// SQLiteConfig holds configuration for SQLiteStore.
type SQLiteConfig struct {
	// Path is the database file path. Use ":memory:" for in-memory database.
	Path string

	// Logger is optional. If nil, a default logger is used.
	Logger *slog.Logger
}

// NewSQLiteStore creates a new SQLite-backed store.
func NewSQLiteStore(cfg SQLiteConfig) (*SQLiteStore, error) {
	if cfg.Path == "" {
		cfg.Path = ":memory:"
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	// For in-memory databases, use shared cache mode to allow multiple connections
	// to access the same database
	dbPath := cfg.Path
	if cfg.Path == ":memory:" {
		dbPath = "file::memory:?cache=shared"
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Limit connections for in-memory databases to avoid issues
	if cfg.Path == ":memory:" {
		db.SetMaxOpenConns(1)
	}

	// Enable WAL mode for better concurrent read performance (file-based only)
	if cfg.Path != ":memory:" {
		if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
			_ = db.Close() // Best effort cleanup
			return nil, fmt.Errorf("failed to enable WAL mode: %w", err)
		}
	}

	// Enable foreign keys
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		_ = db.Close() // Best effort cleanup
		return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	store := &SQLiteStore{
		db:     db,
		logger: logger,
	}

	// Run migrations
	if err := store.migrate(); err != nil {
		_ = db.Close() // Best effort cleanup
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return store, nil
}

// migrate runs database migrations.
func (s *SQLiteStore) migrate() error {
	migrations := []string{
		// Version 1: Create agents table
		`CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS agents (
			id TEXT PRIMARY KEY,
			did TEXT UNIQUE NOT NULL,
			display_name TEXT NOT NULL,
			public_keys TEXT NOT NULL DEFAULT '[]',
			endpoints TEXT NOT NULL DEFAULT '[]',
			capabilities TEXT NOT NULL DEFAULT '[]',
			acl TEXT,
			status TEXT NOT NULL DEFAULT 'offline',
			last_seen TIMESTAMP NOT NULL,
			metadata TEXT DEFAULT '{}',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_agents_did ON agents(did)`,
		`CREATE INDEX IF NOT EXISTS idx_agents_status ON agents(status)`,
		`CREATE INDEX IF NOT EXISTS idx_agents_last_seen ON agents(last_seen)`,
	}

	for i, migration := range migrations {
		// Check if migration already applied
		var applied bool
		err := s.db.QueryRow("SELECT 1 FROM schema_migrations WHERE version = ?", i).Scan(&applied)
		if err == nil && applied {
			continue
		}

		// Apply migration
		if _, err := s.db.Exec(migration); err != nil {
			return fmt.Errorf("migration %d failed: %w", i, err)
		}

		// Record migration (skip for schema_migrations table itself)
		if i > 0 {
			if _, err := s.db.Exec("INSERT OR IGNORE INTO schema_migrations (version) VALUES (?)", i); err != nil {
				return fmt.Errorf("failed to record migration %d: %w", i, err)
			}
		}
	}

	return nil
}

// Get retrieves an agent by ID.
func (s *SQLiteStore) Get(id uuid.UUID) (*Agent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.getByQuery("SELECT id, did, display_name, public_keys, endpoints, capabilities, acl, status, last_seen, metadata FROM agents WHERE id = ?", id.String())
}

// GetByDID retrieves an agent by DID.
func (s *SQLiteStore) GetByDID(did string) (*Agent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.getByQuery("SELECT id, did, display_name, public_keys, endpoints, capabilities, acl, status, last_seen, metadata FROM agents WHERE did = ?", did)
}

// getByQuery executes a query and scans the result into an Agent.
func (s *SQLiteStore) getByQuery(query string, args ...any) (*Agent, error) {
	var (
		idStr        string
		did          string
		displayName  string
		publicKeys   string
		endpoints    string
		capabilities string
		acl          sql.NullString
		status       string
		lastSeen     time.Time
		metadata     string
	)

	err := s.db.QueryRow(query, args...).Scan(
		&idStr, &did, &displayName, &publicKeys, &endpoints,
		&capabilities, &acl, &status, &lastSeen, &metadata,
	)
	if err == sql.ErrNoRows {
		return nil, ErrAgentNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}

	return s.scanAgent(idStr, did, displayName, publicKeys, endpoints, capabilities, acl, status, lastSeen, metadata)
}

// scanAgent converts database row values into an Agent.
func (s *SQLiteStore) scanAgent(idStr, did, displayName, publicKeys, endpoints, capabilities string, acl sql.NullString, status string, lastSeen time.Time, metadata string) (*Agent, error) {
	id, err := uuid.Parse(idStr)
	if err != nil {
		return nil, fmt.Errorf("invalid agent ID: %w", err)
	}

	agent := &Agent{
		ID:          id,
		DID:         did,
		DisplayName: displayName,
		Status:      AgentStatus(status),
		LastSeen:    lastSeen,
	}

	// Parse JSON fields
	if err := json.Unmarshal([]byte(publicKeys), &agent.PublicKeys); err != nil {
		return nil, fmt.Errorf("failed to parse public_keys: %w", err)
	}
	if err := json.Unmarshal([]byte(endpoints), &agent.Endpoints); err != nil {
		return nil, fmt.Errorf("failed to parse endpoints: %w", err)
	}
	if err := json.Unmarshal([]byte(capabilities), &agent.Capabilities); err != nil {
		return nil, fmt.Errorf("failed to parse capabilities: %w", err)
	}
	if err := json.Unmarshal([]byte(metadata), &agent.Metadata); err != nil {
		return nil, fmt.Errorf("failed to parse metadata: %w", err)
	}

	if acl.Valid && acl.String != "" {
		agent.ACL = &ACLPolicy{}
		if err := json.Unmarshal([]byte(acl.String), agent.ACL); err != nil {
			return nil, fmt.Errorf("failed to parse acl: %w", err)
		}
	}

	return agent, nil
}

// Put stores an agent (insert or update).
func (s *SQLiteStore) Put(agent *Agent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	publicKeys, err := json.Marshal(agent.PublicKeys)
	if err != nil {
		return fmt.Errorf("failed to marshal public_keys: %w", err)
	}
	endpoints, err := json.Marshal(agent.Endpoints)
	if err != nil {
		return fmt.Errorf("failed to marshal endpoints: %w", err)
	}
	capabilities, err := json.Marshal(agent.Capabilities)
	if err != nil {
		return fmt.Errorf("failed to marshal capabilities: %w", err)
	}
	metadata, err := json.Marshal(agent.Metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	var aclStr sql.NullString
	if agent.ACL != nil {
		aclBytes, err := json.Marshal(agent.ACL)
		if err != nil {
			return fmt.Errorf("failed to marshal acl: %w", err)
		}
		aclStr = sql.NullString{String: string(aclBytes), Valid: true}
	}

	_, err = s.db.Exec(`
		INSERT INTO agents (id, did, display_name, public_keys, endpoints, capabilities, acl, status, last_seen, metadata, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(id) DO UPDATE SET
			did = excluded.did,
			display_name = excluded.display_name,
			public_keys = excluded.public_keys,
			endpoints = excluded.endpoints,
			capabilities = excluded.capabilities,
			acl = excluded.acl,
			status = excluded.status,
			last_seen = excluded.last_seen,
			metadata = excluded.metadata,
			updated_at = CURRENT_TIMESTAMP
	`, agent.ID.String(), agent.DID, agent.DisplayName, string(publicKeys),
		string(endpoints), string(capabilities), aclStr,
		string(agent.Status), agent.LastSeen, string(metadata))

	if err != nil {
		return fmt.Errorf("failed to upsert agent: %w", err)
	}

	s.logger.Debug("agent stored", "id", agent.ID, "did", agent.DID)
	return nil
}

// Delete removes an agent by ID.
func (s *SQLiteStore) Delete(id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec("DELETE FROM agents WHERE id = ?", id.String())
	if err != nil {
		return fmt.Errorf("failed to delete agent: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return ErrAgentNotFound
	}

	s.logger.Debug("agent deleted", "id", id)
	return nil
}

// List returns all agents.
func (s *SQLiteStore) List() ([]*Agent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query("SELECT id, did, display_name, public_keys, endpoints, capabilities, acl, status, last_seen, metadata FROM agents ORDER BY last_seen DESC")
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var agents []*Agent
	for rows.Next() {
		var (
			idStr        string
			did          string
			displayName  string
			publicKeys   string
			endpoints    string
			capabilities string
			acl          sql.NullString
			status       string
			lastSeen     time.Time
			metadata     string
		)

		if err := rows.Scan(&idStr, &did, &displayName, &publicKeys, &endpoints, &capabilities, &acl, &status, &lastSeen, &metadata); err != nil {
			return nil, fmt.Errorf("scan failed: %w", err)
		}

		agent, err := s.scanAgent(idStr, did, displayName, publicKeys, endpoints, capabilities, acl, status, lastSeen, metadata)
		if err != nil {
			s.logger.Warn("failed to parse agent", "id", idStr, "error", err)
			continue
		}
		agents = append(agents, agent)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration failed: %w", err)
	}

	return agents, nil
}

// Search searches for agents by capability.
func (s *SQLiteStore) Search(capability string) ([]*Agent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Use JSON_EXTRACT to search within capabilities array
	// This searches for capability name in the JSON array
	rows, err := s.db.Query(`
		SELECT id, did, display_name, public_keys, endpoints, capabilities, acl, status, last_seen, metadata
		FROM agents
		WHERE capabilities LIKE ?
		ORDER BY last_seen DESC
	`, "%\"name\":\""+capability+"\"%")
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var agents []*Agent
	for rows.Next() {
		var (
			idStr        string
			did          string
			displayName  string
			publicKeys   string
			endpoints    string
			capabilities string
			acl          sql.NullString
			status       string
			lastSeen     time.Time
			metadata     string
		)

		if err := rows.Scan(&idStr, &did, &displayName, &publicKeys, &endpoints, &capabilities, &acl, &status, &lastSeen, &metadata); err != nil {
			return nil, fmt.Errorf("scan failed: %w", err)
		}

		agent, err := s.scanAgent(idStr, did, displayName, publicKeys, endpoints, capabilities, acl, status, lastSeen, metadata)
		if err != nil {
			s.logger.Warn("failed to parse agent", "id", idStr, "error", err)
			continue
		}

		// Double-check capability match (LIKE can have false positives)
		if agent.HasCapability(capability) {
			agents = append(agents, agent)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration failed: %w", err)
	}

	return agents, nil
}

// Close closes the database connection.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// Stats returns database statistics.
func (s *SQLiteStore) Stats() (map[string]any, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var totalAgents int
	var onlineAgents int
	var offlineAgents int

	if err := s.db.QueryRow("SELECT COUNT(*) FROM agents").Scan(&totalAgents); err != nil {
		return nil, err
	}
	if err := s.db.QueryRow("SELECT COUNT(*) FROM agents WHERE status = 'online'").Scan(&onlineAgents); err != nil {
		return nil, err
	}
	if err := s.db.QueryRow("SELECT COUNT(*) FROM agents WHERE status = 'offline'").Scan(&offlineAgents); err != nil {
		return nil, err
	}

	return map[string]any{
		"total_agents":   totalAgents,
		"online_agents":  onlineAgents,
		"offline_agents": offlineAgents,
	}, nil
}

// Cleanup removes stale agents that haven't been seen since the given time.
func (s *SQLiteStore) Cleanup(before time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec("DELETE FROM agents WHERE last_seen < ? AND status = 'offline'", before)
	if err != nil {
		return 0, fmt.Errorf("cleanup failed: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}

	if rows > 0 {
		s.logger.Info("cleaned up stale agents", "count", rows)
	}

	return rows, nil
}
