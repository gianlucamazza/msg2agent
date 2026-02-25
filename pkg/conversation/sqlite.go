package conversation

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite" // SQLite driver
)

// SQLiteStore is a SQLite-backed implementation of Store.
type SQLiteStore struct {
	db     *sql.DB
	logger *slog.Logger
	config StoreConfig
	mu     sync.RWMutex
}

// SQLiteConfig holds configuration for SQLiteStore.
type SQLiteConfig struct {
	// Path is the database file path. Use ":memory:" for in-memory database.
	Path string

	// Logger is optional. If nil, a default logger is used.
	Logger *slog.Logger

	// StoreConfig provides additional store options.
	StoreConfig StoreConfig
}

// NewSQLiteStore creates a new SQLite-backed conversation store.
func NewSQLiteStore(cfg SQLiteConfig) (*SQLiteStore, error) {
	if cfg.Path == "" {
		cfg.Path = ":memory:"
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	dbPath := cfg.Path
	if cfg.Path == ":memory:" {
		dbPath = "file::memory:?cache=shared"
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if cfg.Path == ":memory:" {
		db.SetMaxOpenConns(1)
	}

	// Enable WAL mode for file-based databases
	if cfg.Path != ":memory:" {
		if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("failed to enable WAL mode: %w", err)
		}
	}

	// Enable foreign keys
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	store := &SQLiteStore{
		db:     db,
		logger: logger,
		config: cfg.StoreConfig,
	}

	if err := store.migrate(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return store, nil
}

func (s *SQLiteStore) migrate() error {
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS conversation_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS threads (
			id TEXT PRIMARY KEY,
			title TEXT,
			participants TEXT NOT NULL DEFAULT '[]',
			created_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL,
			last_message TIMESTAMP,
			message_count INTEGER NOT NULL DEFAULT 0,
			metadata TEXT DEFAULT '{}'
		)`,
		`CREATE INDEX IF NOT EXISTS idx_threads_updated_at ON threads(updated_at)`,
		`CREATE TABLE IF NOT EXISTS messages (
			id TEXT PRIMARY KEY,
			thread_id TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
			parent_id TEXT,
			sender TEXT NOT NULL,
			recipient TEXT NOT NULL,
			type TEXT NOT NULL,
			body BLOB,
			timestamp TIMESTAMP NOT NULL,
			thread_seq_no INTEGER NOT NULL,
			encrypted INTEGER NOT NULL DEFAULT 0,
			metadata TEXT DEFAULT '{}'
		)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_thread_id ON messages(thread_id)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_timestamp ON messages(timestamp)`,
		`CREATE TABLE IF NOT EXISTS receipts (
			message_id TEXT NOT NULL,
			recipient_did TEXT NOT NULL,
			delivered_at TIMESTAMP,
			read_at TIMESTAMP,
			PRIMARY KEY (message_id, recipient_did),
			FOREIGN KEY (message_id) REFERENCES messages(id) ON DELETE CASCADE
		)`,
	}

	for i, migration := range migrations {
		var applied bool
		err := s.db.QueryRow("SELECT 1 FROM conversation_migrations WHERE version = ?", i).Scan(&applied)
		if err == nil && applied {
			continue
		}

		if _, err := s.db.Exec(migration); err != nil {
			return fmt.Errorf("migration %d failed: %w", i, err)
		}

		if i > 0 {
			if _, err := s.db.Exec("INSERT OR IGNORE INTO conversation_migrations (version) VALUES (?)", i); err != nil {
				return fmt.Errorf("failed to record migration %d: %w", i, err)
			}
		}
	}

	return nil
}

// CreateThread creates a new thread.
func (s *SQLiteStore) CreateThread(thread *Thread) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	participants, err := json.Marshal(thread.Participants)
	if err != nil {
		return fmt.Errorf("failed to marshal participants: %w", err)
	}
	metadata, err := json.Marshal(thread.Metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	var lastMessage sql.NullTime
	if thread.LastMessage != nil {
		lastMessage = sql.NullTime{Time: *thread.LastMessage, Valid: true}
	}

	_, err = s.db.Exec(`
		INSERT INTO threads (id, title, participants, created_at, updated_at, last_message, message_count, metadata)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, thread.ID.String(), thread.Title, string(participants), thread.CreatedAt, thread.UpdatedAt, lastMessage, thread.MessageCount, string(metadata))

	if err != nil {
		return fmt.Errorf("failed to insert thread: %w", err)
	}

	s.logger.Debug("thread created", "id", thread.ID)
	return nil
}

// GetThread retrieves a thread by ID.
func (s *SQLiteStore) GetThread(id uuid.UUID) (*Thread, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var (
		idStr        string
		title        sql.NullString
		participants string
		createdAt    time.Time
		updatedAt    time.Time
		lastMessage  sql.NullTime
		messageCount int
		metadata     string
	)

	err := s.db.QueryRow(`
		SELECT id, title, participants, created_at, updated_at, last_message, message_count, metadata
		FROM threads WHERE id = ?
	`, id.String()).Scan(&idStr, &title, &participants, &createdAt, &updatedAt, &lastMessage, &messageCount, &metadata)

	if err == sql.ErrNoRows {
		return nil, ErrThreadNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}

	return s.scanThread(idStr, title, participants, createdAt, updatedAt, lastMessage, messageCount, metadata)
}

func (s *SQLiteStore) scanThread(idStr string, title sql.NullString, participants string, createdAt, updatedAt time.Time, lastMessage sql.NullTime, messageCount int, metadata string) (*Thread, error) {
	id, err := uuid.Parse(idStr)
	if err != nil {
		return nil, fmt.Errorf("invalid thread ID: %w", err)
	}

	thread := &Thread{
		ID:           id,
		CreatedAt:    createdAt,
		UpdatedAt:    updatedAt,
		MessageCount: messageCount,
	}

	if title.Valid {
		thread.Title = title.String
	}
	if lastMessage.Valid {
		thread.LastMessage = &lastMessage.Time
	}

	if err := json.Unmarshal([]byte(participants), &thread.Participants); err != nil {
		return nil, fmt.Errorf("failed to parse participants: %w", err)
	}
	if err := json.Unmarshal([]byte(metadata), &thread.Metadata); err != nil {
		return nil, fmt.Errorf("failed to parse metadata: %w", err)
	}

	return thread, nil
}

// UpdateThread updates an existing thread.
func (s *SQLiteStore) UpdateThread(thread *Thread) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	participants, err := json.Marshal(thread.Participants)
	if err != nil {
		return fmt.Errorf("failed to marshal participants: %w", err)
	}
	metadata, err := json.Marshal(thread.Metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	var lastMessage sql.NullTime
	if thread.LastMessage != nil {
		lastMessage = sql.NullTime{Time: *thread.LastMessage, Valid: true}
	}

	result, err := s.db.Exec(`
		UPDATE threads SET title = ?, participants = ?, updated_at = ?, last_message = ?, message_count = ?, metadata = ?
		WHERE id = ?
	`, thread.Title, string(participants), thread.UpdatedAt, lastMessage, thread.MessageCount, string(metadata), thread.ID.String())

	if err != nil {
		return fmt.Errorf("failed to update thread: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrThreadNotFound
	}

	return nil
}

// DeleteThread removes a thread and all its messages.
func (s *SQLiteStore) DeleteThread(id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec("DELETE FROM threads WHERE id = ?", id.String())
	if err != nil {
		return fmt.Errorf("failed to delete thread: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrThreadNotFound
	}

	s.logger.Debug("thread deleted", "id", id)
	return nil
}

// ListThreads lists threads matching the filter.
func (s *SQLiteStore) ListThreads(filter ThreadFilter) ([]*Thread, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := "SELECT id, title, participants, created_at, updated_at, last_message, message_count, metadata FROM threads"
	var conditions []string
	var args []any

	if filter.ParticipantDID != "" {
		// Use JSON search for participant
		conditions = append(conditions, "participants LIKE ?")
		args = append(args, "%\""+filter.ParticipantDID+"\"%")
	}
	if filter.Since != nil {
		conditions = append(conditions, "updated_at >= ?")
		args = append(args, *filter.Since)
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY updated_at DESC"

	// SQLite requires LIMIT when using OFFSET
	if filter.Limit > 0 || filter.Offset > 0 {
		limit := filter.Limit
		if limit == 0 {
			limit = -1 // SQLite: -1 means no limit
		}
		query += fmt.Sprintf(" LIMIT %d", limit)
	}
	if filter.Offset > 0 {
		query += fmt.Sprintf(" OFFSET %d", filter.Offset) //nolint:gosec // integer formatting, safe
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var threads []*Thread
	for rows.Next() {
		var (
			idStr        string
			title        sql.NullString
			participants string
			createdAt    time.Time
			updatedAt    time.Time
			lastMessage  sql.NullTime
			messageCount int
			metadata     string
		)

		if err := rows.Scan(&idStr, &title, &participants, &createdAt, &updatedAt, &lastMessage, &messageCount, &metadata); err != nil {
			return nil, fmt.Errorf("scan failed: %w", err)
		}

		thread, err := s.scanThread(idStr, title, participants, createdAt, updatedAt, lastMessage, messageCount, metadata)
		if err != nil {
			s.logger.Warn("failed to parse thread", "id", idStr, "error", err)
			continue
		}

		// Double-check participant filter
		if filter.ParticipantDID != "" && !thread.HasParticipant(filter.ParticipantDID) {
			continue
		}

		threads = append(threads, thread)
	}

	return threads, rows.Err()
}

// SaveMessage saves a message to a thread.
func (s *SQLiteStore) SaveMessage(msg *StoredMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	metadata, err := json.Marshal(msg.Metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	var parentID sql.NullString
	if msg.ParentID != nil {
		parentID = sql.NullString{String: msg.ParentID.String(), Valid: true}
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Insert message
	_, err = tx.Exec(`
		INSERT INTO messages (id, thread_id, parent_id, sender, recipient, type, body, timestamp, thread_seq_no, encrypted, metadata)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, msg.ID.String(), msg.ThreadID.String(), parentID, msg.From, msg.To, msg.Type, msg.Body, msg.Timestamp, msg.ThreadSeqNo, msg.Encrypted, string(metadata))

	if err != nil {
		return fmt.Errorf("failed to insert message: %w", err)
	}

	// Update thread stats
	_, err = tx.Exec(`
		UPDATE threads SET message_count = message_count + 1, last_message = ?, updated_at = ?
		WHERE id = ?
	`, msg.Timestamp, time.Now(), msg.ThreadID.String())

	if err != nil {
		return fmt.Errorf("failed to update thread: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit: %w", err)
	}

	return nil
}

// GetMessage retrieves a message by ID.
func (s *SQLiteStore) GetMessage(id uuid.UUID) (*StoredMessage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var (
		idStr       string
		threadID    string
		parentID    sql.NullString
		sender      string
		recipient   string
		msgType     string
		body        []byte
		timestamp   time.Time
		threadSeqNo int
		encrypted   bool
		metadata    string
	)

	err := s.db.QueryRow(`
		SELECT id, thread_id, parent_id, sender, recipient, type, body, timestamp, thread_seq_no, encrypted, metadata
		FROM messages WHERE id = ?
	`, id.String()).Scan(&idStr, &threadID, &parentID, &sender, &recipient, &msgType, &body, &timestamp, &threadSeqNo, &encrypted, &metadata)

	if err == sql.ErrNoRows {
		return nil, ErrMessageNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}

	return s.scanMessage(idStr, threadID, parentID, sender, recipient, msgType, body, timestamp, threadSeqNo, encrypted, metadata)
}

func (s *SQLiteStore) scanMessage(idStr, threadIDStr string, parentID sql.NullString, sender, recipient, msgType string, body []byte, timestamp time.Time, threadSeqNo int, encrypted bool, metadata string) (*StoredMessage, error) {
	id, err := uuid.Parse(idStr)
	if err != nil {
		return nil, fmt.Errorf("invalid message ID: %w", err)
	}
	threadID, err := uuid.Parse(threadIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid thread ID: %w", err)
	}

	msg := &StoredMessage{
		ID:          id,
		ThreadID:    threadID,
		From:        sender,
		To:          recipient,
		Type:        msgType,
		Body:        body,
		Timestamp:   timestamp,
		ThreadSeqNo: threadSeqNo,
		Encrypted:   encrypted,
	}

	if parentID.Valid {
		pid, err := uuid.Parse(parentID.String)
		if err != nil {
			return nil, fmt.Errorf("invalid parent ID: %w", err)
		}
		msg.ParentID = &pid
	}

	if err := json.Unmarshal([]byte(metadata), &msg.Metadata); err != nil {
		return nil, fmt.Errorf("failed to parse metadata: %w", err)
	}

	return msg, nil
}

// GetMessages retrieves messages matching the filter.
func (s *SQLiteStore) GetMessages(filter MessageFilter) ([]*StoredMessage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := "SELECT id, thread_id, parent_id, sender, recipient, type, body, timestamp, thread_seq_no, encrypted, metadata FROM messages"
	conditions := []string{"thread_id = ?"}
	args := []any{filter.ThreadID.String()}

	if filter.Since != nil {
		conditions = append(conditions, "timestamp >= ?")
		args = append(args, *filter.Since)
	}
	if filter.Before != nil {
		conditions = append(conditions, "timestamp <= ?")
		args = append(args, *filter.Before)
	}
	if len(filter.Types) > 0 {
		placeholders := make([]string, len(filter.Types))
		for i, t := range filter.Types {
			placeholders[i] = "?"
			args = append(args, t)
		}
		conditions = append(conditions, "type IN ("+strings.Join(placeholders, ", ")+")")
	}

	query += " WHERE " + strings.Join(conditions, " AND ")
	query += " ORDER BY thread_seq_no ASC"

	// SQLite requires LIMIT when using OFFSET
	if filter.Limit > 0 || filter.Offset > 0 {
		limit := filter.Limit
		if limit == 0 {
			limit = -1 // SQLite: -1 means no limit
		}
		query += fmt.Sprintf(" LIMIT %d", limit)
	}
	if filter.Offset > 0 {
		query += fmt.Sprintf(" OFFSET %d", filter.Offset) //nolint:gosec // integer formatting, safe
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var messages []*StoredMessage
	for rows.Next() {
		var (
			idStr       string
			threadID    string
			parentID    sql.NullString
			sender      string
			recipient   string
			msgType     string
			body        []byte
			timestamp   time.Time
			threadSeqNo int
			encrypted   bool
			metadata    string
		)

		if err := rows.Scan(&idStr, &threadID, &parentID, &sender, &recipient, &msgType, &body, &timestamp, &threadSeqNo, &encrypted, &metadata); err != nil {
			return nil, fmt.Errorf("scan failed: %w", err)
		}

		msg, err := s.scanMessage(idStr, threadID, parentID, sender, recipient, msgType, body, timestamp, threadSeqNo, encrypted, metadata)
		if err != nil {
			s.logger.Warn("failed to parse message", "id", idStr, "error", err)
			continue
		}
		messages = append(messages, msg)
	}

	return messages, rows.Err()
}

// DeleteMessage removes a message.
func (s *SQLiteStore) DeleteMessage(id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Get thread ID first for updating count
	var threadID string
	err := s.db.QueryRow("SELECT thread_id FROM messages WHERE id = ?", id.String()).Scan(&threadID)
	if err == sql.ErrNoRows {
		return ErrMessageNotFound
	}
	if err != nil {
		return fmt.Errorf("query failed: %w", err)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.Exec("DELETE FROM messages WHERE id = ?", id.String())
	if err != nil {
		return fmt.Errorf("failed to delete message: %w", err)
	}

	_, err = tx.Exec("UPDATE threads SET message_count = message_count - 1 WHERE id = ?", threadID)
	if err != nil {
		return fmt.Errorf("failed to update thread: %w", err)
	}

	return tx.Commit()
}

// MarkDelivered marks a message as delivered to a recipient.
func (s *SQLiteStore) MarkDelivered(messageID uuid.UUID, recipientDID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Verify message exists
	var exists bool
	err := s.db.QueryRow("SELECT 1 FROM messages WHERE id = ?", messageID.String()).Scan(&exists)
	if err == sql.ErrNoRows {
		return ErrMessageNotFound
	}
	if err != nil {
		return fmt.Errorf("query failed: %w", err)
	}

	now := time.Now()
	_, err = s.db.Exec(`
		INSERT INTO receipts (message_id, recipient_did, delivered_at)
		VALUES (?, ?, ?)
		ON CONFLICT(message_id, recipient_did) DO UPDATE SET delivered_at = COALESCE(receipts.delivered_at, excluded.delivered_at)
	`, messageID.String(), recipientDID, now)

	if err != nil {
		return fmt.Errorf("failed to mark delivered: %w", err)
	}

	return nil
}

// MarkRead marks a message as read by a recipient.
func (s *SQLiteStore) MarkRead(messageID uuid.UUID, recipientDID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var exists bool
	err := s.db.QueryRow("SELECT 1 FROM messages WHERE id = ?", messageID.String()).Scan(&exists)
	if err == sql.ErrNoRows {
		return ErrMessageNotFound
	}
	if err != nil {
		return fmt.Errorf("query failed: %w", err)
	}

	now := time.Now()
	_, err = s.db.Exec(`
		INSERT INTO receipts (message_id, recipient_did, delivered_at, read_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(message_id, recipient_did) DO UPDATE SET
			delivered_at = COALESCE(receipts.delivered_at, excluded.delivered_at),
			read_at = excluded.read_at
	`, messageID.String(), recipientDID, now, now)

	if err != nil {
		return fmt.Errorf("failed to mark read: %w", err)
	}

	return nil
}

// GetReceipt retrieves a receipt for a specific message and recipient.
func (s *SQLiteStore) GetReceipt(messageID uuid.UUID, recipientDID string) (*MessageReceipt, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var (
		deliveredAt sql.NullTime
		readAt      sql.NullTime
	)

	err := s.db.QueryRow(`
		SELECT delivered_at, read_at FROM receipts WHERE message_id = ? AND recipient_did = ?
	`, messageID.String(), recipientDID).Scan(&deliveredAt, &readAt)

	if err == sql.ErrNoRows {
		return nil, ErrReceiptNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}

	receipt := &MessageReceipt{
		MessageID:    messageID,
		RecipientDID: recipientDID,
	}
	if deliveredAt.Valid {
		receipt.DeliveredAt = &deliveredAt.Time
	}
	if readAt.Valid {
		receipt.ReadAt = &readAt.Time
	}

	return receipt, nil
}

// GetReceipts retrieves all receipts for a message.
func (s *SQLiteStore) GetReceipts(messageID uuid.UUID) ([]*MessageReceipt, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`
		SELECT recipient_did, delivered_at, read_at FROM receipts WHERE message_id = ?
	`, messageID.String())
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var receipts []*MessageReceipt
	for rows.Next() {
		var (
			recipientDID string
			deliveredAt  sql.NullTime
			readAt       sql.NullTime
		)

		if err := rows.Scan(&recipientDID, &deliveredAt, &readAt); err != nil {
			return nil, fmt.Errorf("scan failed: %w", err)
		}

		receipt := &MessageReceipt{
			MessageID:    messageID,
			RecipientDID: recipientDID,
		}
		if deliveredAt.Valid {
			receipt.DeliveredAt = &deliveredAt.Time
		}
		if readAt.Valid {
			receipt.ReadAt = &readAt.Time
		}
		receipts = append(receipts, receipt)
	}

	return receipts, rows.Err()
}

// Close closes the database connection.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}
