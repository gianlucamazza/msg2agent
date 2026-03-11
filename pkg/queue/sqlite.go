package queue

import (
	"database/sql"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite" // SQLite driver
)

// SQLiteStore is a SQLite-backed implementation of Store.
type SQLiteStore struct {
	db     *sql.DB
	config Config
	logger *slog.Logger
	mu     sync.RWMutex
}

// SQLiteConfig holds configuration for SQLiteStore.
type SQLiteConfig struct {
	Path        string
	Logger      *slog.Logger
	QueueConfig Config
}

// NewSQLiteStore creates a new SQLite-backed queue store.
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

	if cfg.Path != ":memory:" {
		if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("failed to enable WAL mode: %w", err)
		}
	}

	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	store := &SQLiteStore{
		db:     db,
		config: cfg.QueueConfig,
		logger: logger,
	}

	if err := store.migrate(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return store, nil
}

func (s *SQLiteStore) migrate() error {
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS queue_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS message_queue (
			id TEXT PRIMARY KEY,
			recipient_did TEXT NOT NULL,
			sender_did TEXT,
			data BLOB NOT NULL,
			queued_at TIMESTAMP NOT NULL,
			expires_at TIMESTAMP NOT NULL,
			delivery_attempts INTEGER NOT NULL DEFAULT 0,
			last_attempt TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_queue_recipient ON message_queue(recipient_did)`,
		`CREATE INDEX IF NOT EXISTS idx_queue_expires ON message_queue(expires_at)`,
		`CREATE TABLE IF NOT EXISTS dead_letter_queue (
			id TEXT PRIMARY KEY,
			recipient_did TEXT NOT NULL,
			sender_did TEXT,
			data BLOB NOT NULL,
			queued_at TIMESTAMP NOT NULL,
			expires_at TIMESTAMP NOT NULL,
			delivery_attempts INTEGER NOT NULL,
			last_attempt TIMESTAMP,
			fail_reason TEXT,
			moved_at TIMESTAMP NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_dlq_recipient ON dead_letter_queue(recipient_did)`,
	}

	for i, migration := range migrations {
		var applied bool
		err := s.db.QueryRow("SELECT 1 FROM queue_migrations WHERE version = ?", i).Scan(&applied)
		if err == nil && applied {
			continue
		}

		if _, err := s.db.Exec(migration); err != nil {
			return fmt.Errorf("migration %d failed: %w", i, err)
		}

		if i > 0 {
			if _, err := s.db.Exec("INSERT OR IGNORE INTO queue_migrations (version) VALUES (?)", i); err != nil {
				return fmt.Errorf("failed to record migration %d: %w", i, err)
			}
		}
	}

	return nil
}

// Enqueue adds a message to the queue.
func (s *SQLiteStore) Enqueue(msg *QueuedMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check queue size limit
	if s.config.MaxQueueSize > 0 {
		var count int
		err := s.db.QueryRow("SELECT COUNT(*) FROM message_queue WHERE recipient_did = ?", msg.RecipientDID).Scan(&count)
		if err != nil {
			return fmt.Errorf("failed to check queue size: %w", err)
		}
		if count >= s.config.MaxQueueSize {
			return ErrQueueFull
		}
	}

	var lastAttempt sql.NullTime
	if msg.LastAttempt != nil {
		lastAttempt = sql.NullTime{Time: *msg.LastAttempt, Valid: true}
	}

	_, err := s.db.Exec(`
		INSERT INTO message_queue (id, recipient_did, sender_did, data, queued_at, expires_at, delivery_attempts, last_attempt)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, msg.ID.String(), msg.RecipientDID, msg.SenderDID, msg.Data, msg.QueuedAt, msg.ExpiresAt, msg.DeliveryAttempts, lastAttempt)

	if err != nil {
		return fmt.Errorf("failed to enqueue message: %w", err)
	}

	return nil
}

// Dequeue retrieves and removes pending messages for a recipient.
func (s *SQLiteStore) Dequeue(recipientDID string, limit int) ([]*QueuedMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Get messages
	query := `
		SELECT id, recipient_did, sender_did, data, queued_at, expires_at, delivery_attempts, last_attempt
		FROM message_queue
		WHERE recipient_did = ? AND expires_at > ?
		ORDER BY queued_at ASC
	`
	args := []any{recipientDID, time.Now()}
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var messages []*QueuedMessage
	var ids []string
	for rows.Next() {
		msg, err := s.scanMessage(rows)
		if err != nil {
			s.logger.Warn("failed to scan message", "error", err)
			continue
		}
		messages = append(messages, msg)
		ids = append(ids, msg.ID.String())
	}

	// Delete retrieved messages
	for _, id := range ids {
		if _, err := s.db.Exec("DELETE FROM message_queue WHERE id = ?", id); err != nil {
			s.logger.Error("failed to delete dequeued message", "id", id, "error", err)
			return messages, fmt.Errorf("failed to delete dequeued message %s: %w", id, err)
		}
	}

	return messages, rows.Err()
}

// Peek retrieves pending messages without removing them.
func (s *SQLiteStore) Peek(recipientDID string, limit int) ([]*QueuedMessage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
		SELECT id, recipient_did, sender_did, data, queued_at, expires_at, delivery_attempts, last_attempt
		FROM message_queue
		WHERE recipient_did = ? AND expires_at > ?
		ORDER BY queued_at ASC
	`
	args := []any{recipientDID, time.Now()}
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var messages []*QueuedMessage
	for rows.Next() {
		msg, err := s.scanMessage(rows)
		if err != nil {
			s.logger.Warn("failed to scan message", "error", err)
			continue
		}
		messages = append(messages, msg)
	}

	return messages, rows.Err()
}

func (s *SQLiteStore) scanMessage(rows *sql.Rows) (*QueuedMessage, error) {
	var (
		idStr            string
		recipientDID     string
		senderDID        sql.NullString
		data             []byte
		queuedAt         time.Time
		expiresAt        time.Time
		deliveryAttempts int
		lastAttempt      sql.NullTime
	)

	if err := rows.Scan(&idStr, &recipientDID, &senderDID, &data, &queuedAt, &expiresAt, &deliveryAttempts, &lastAttempt); err != nil {
		return nil, err
	}

	id, err := uuid.Parse(idStr)
	if err != nil {
		return nil, fmt.Errorf("invalid message ID: %w", err)
	}

	msg := &QueuedMessage{
		ID:               id,
		RecipientDID:     recipientDID,
		Data:             data,
		QueuedAt:         queuedAt,
		ExpiresAt:        expiresAt,
		DeliveryAttempts: deliveryAttempts,
	}

	if senderDID.Valid {
		msg.SenderDID = senderDID.String
	}
	if lastAttempt.Valid {
		msg.LastAttempt = &lastAttempt.Time
	}

	return msg, nil
}

// Ack removes a successfully delivered message.
func (s *SQLiteStore) Ack(messageID uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec("DELETE FROM message_queue WHERE id = ?", messageID.String())
	if err != nil {
		return fmt.Errorf("failed to ack message: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}

	return nil
}

// Nack returns a message to the queue for retry.
func (s *SQLiteStore) Nack(messageID uuid.UUID, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Get current message
	var (
		idStr            string
		recipientDID     string
		senderDID        sql.NullString
		data             []byte
		queuedAt         time.Time
		expiresAt        time.Time
		deliveryAttempts int
	)

	err := s.db.QueryRow(`
		SELECT id, recipient_did, sender_did, data, queued_at, expires_at, delivery_attempts
		FROM message_queue WHERE id = ?
	`, messageID.String()).Scan(&idStr, &recipientDID, &senderDID, &data, &queuedAt, &expiresAt, &deliveryAttempts)

	if err == sql.ErrNoRows {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("failed to get message: %w", err)
	}

	deliveryAttempts++
	now := time.Now()

	// Move to DLQ if max attempts exceeded
	if s.config.MaxDeliveryAttempts > 0 && deliveryAttempts >= s.config.MaxDeliveryAttempts {
		if s.config.EnableDLQ {
			_, err = s.db.Exec(`
				INSERT INTO dead_letter_queue (id, recipient_did, sender_did, data, queued_at, expires_at, delivery_attempts, last_attempt, fail_reason, moved_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			`, idStr, recipientDID, senderDID, data, queuedAt, expiresAt, deliveryAttempts, now, reason, now)
			if err != nil {
				return fmt.Errorf("failed to move to DLQ: %w", err)
			}
		}
		_, err = s.db.Exec("DELETE FROM message_queue WHERE id = ?", idStr)
		if err != nil {
			return fmt.Errorf("failed to delete from queue: %w", err)
		}
	} else {
		// Update delivery attempts
		_, err = s.db.Exec("UPDATE message_queue SET delivery_attempts = ?, last_attempt = ? WHERE id = ?", deliveryAttempts, now, idStr)
		if err != nil {
			return fmt.Errorf("failed to update message: %w", err)
		}
	}

	return nil
}

// GetQueueSize returns the number of pending messages for a recipient.
func (s *SQLiteStore) GetQueueSize(recipientDID string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM message_queue WHERE recipient_did = ? AND expires_at > ?", recipientDID, time.Now()).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get queue size: %w", err)
	}

	return count, nil
}

// GetDLQ retrieves messages from the dead letter queue.
func (s *SQLiteStore) GetDLQ(recipientDID string, limit int) ([]*DeadLetterMessage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
		SELECT id, recipient_did, sender_did, data, queued_at, expires_at, delivery_attempts, last_attempt, fail_reason, moved_at
		FROM dead_letter_queue
		WHERE recipient_did = ?
		ORDER BY moved_at DESC
	`
	args := []any{recipientDID}
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var messages []*DeadLetterMessage
	for rows.Next() {
		var (
			idStr            string
			recipientDID     string
			senderDID        sql.NullString
			data             []byte
			queuedAt         time.Time
			expiresAt        time.Time
			deliveryAttempts int
			lastAttempt      sql.NullTime
			failReason       sql.NullString
			movedAt          time.Time
		)

		if err := rows.Scan(&idStr, &recipientDID, &senderDID, &data, &queuedAt, &expiresAt, &deliveryAttempts, &lastAttempt, &failReason, &movedAt); err != nil {
			s.logger.Warn("failed to scan DLQ message", "error", err)
			continue
		}

		id, err := uuid.Parse(idStr)
		if err != nil {
			continue
		}

		msg := &DeadLetterMessage{
			QueuedMessage: QueuedMessage{
				ID:               id,
				RecipientDID:     recipientDID,
				Data:             data,
				QueuedAt:         queuedAt,
				ExpiresAt:        expiresAt,
				DeliveryAttempts: deliveryAttempts,
			},
			MovedAt: movedAt,
		}
		if senderDID.Valid {
			msg.SenderDID = senderDID.String
		}
		if lastAttempt.Valid {
			msg.LastAttempt = &lastAttempt.Time
		}
		if failReason.Valid {
			msg.FailReason = failReason.String
		}
		messages = append(messages, msg)
	}

	return messages, rows.Err()
}

// Cleanup removes expired messages.
func (s *SQLiteStore) Cleanup() (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec("DELETE FROM message_queue WHERE expires_at <= ?", time.Now())
	if err != nil {
		return 0, fmt.Errorf("cleanup failed: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows > 0 {
		s.logger.Info("cleaned up expired messages", "count", rows)
	}

	return rows, nil
}

// Close closes the database connection.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}
