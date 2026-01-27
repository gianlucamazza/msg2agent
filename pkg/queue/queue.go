// Package queue provides offline message queuing for store-and-forward delivery.
package queue

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Common errors for queue operations.
var (
	ErrQueueFull   = errors.New("queue is full")
	ErrMessageTTL  = errors.New("message TTL expired")
	ErrQueueClosed = errors.New("queue is closed")
	ErrNotFound    = errors.New("message not found")
)

// Config holds queue configuration.
type Config struct {
	// MessageTTL is how long messages are kept before expiring.
	MessageTTL time.Duration

	// MaxQueueSize is the maximum number of messages per recipient.
	MaxQueueSize int

	// MaxTotalMessages is the maximum total messages across all recipients.
	// When exceeded, oldest messages are evicted (LRU). 0 = unlimited.
	MaxTotalMessages int

	// MaxDeliveryAttempts is the maximum delivery retry attempts.
	MaxDeliveryAttempts int

	// AckTimeout is how long to wait for delivery acknowledgment.
	AckTimeout time.Duration

	// EnableDLQ enables dead letter queue for failed messages.
	EnableDLQ bool
}

// DefaultConfig returns sensible defaults for the queue.
func DefaultConfig() Config {
	return Config{
		MessageTTL:          7 * 24 * time.Hour, // 7 days
		MaxQueueSize:        10000,              // Per recipient
		MaxTotalMessages:    100000,             // Global limit
		MaxDeliveryAttempts: 3,
		AckTimeout:          30 * time.Second,
		EnableDLQ:           true,
	}
}

// QueuedMessage represents a message waiting for delivery.
type QueuedMessage struct {
	ID               uuid.UUID  `json:"id"`
	RecipientDID     string     `json:"recipient_did"`
	Data             []byte     `json:"data"` // Raw message bytes
	QueuedAt         time.Time  `json:"queued_at"`
	ExpiresAt        time.Time  `json:"expires_at"`
	DeliveryAttempts int        `json:"delivery_attempts"`
	LastAttempt      *time.Time `json:"last_attempt,omitempty"`
	SenderDID        string     `json:"sender_did,omitempty"` // For ack notification
}

// IsExpired returns true if the message has exceeded its TTL.
func (m *QueuedMessage) IsExpired() bool {
	return time.Now().After(m.ExpiresAt)
}

// DeadLetterMessage represents a message that failed delivery.
type DeadLetterMessage struct {
	QueuedMessage
	FailReason string    `json:"fail_reason"`
	MovedAt    time.Time `json:"moved_at"`
}

// Store defines the interface for queue persistence.
type Store interface {
	// Enqueue adds a message to the queue.
	Enqueue(msg *QueuedMessage) error

	// Dequeue retrieves and removes pending messages for a recipient.
	// Returns messages ordered by queued time (oldest first).
	Dequeue(recipientDID string, limit int) ([]*QueuedMessage, error)

	// Peek retrieves pending messages without removing them.
	Peek(recipientDID string, limit int) ([]*QueuedMessage, error)

	// Ack removes a successfully delivered message.
	Ack(messageID uuid.UUID) error

	// Nack returns a message to the queue for retry.
	// Increments delivery attempts. Moves to DLQ if max attempts exceeded.
	Nack(messageID uuid.UUID, reason string) error

	// GetQueueSize returns the number of pending messages for a recipient.
	GetQueueSize(recipientDID string) (int, error)

	// GetDLQ retrieves messages from the dead letter queue.
	GetDLQ(recipientDID string, limit int) ([]*DeadLetterMessage, error)

	// Cleanup removes expired messages. Returns count of removed messages.
	Cleanup() (int64, error)

	// Close closes the store.
	Close() error
}

// Stats holds queue statistics.
type Stats struct {
	TotalQueued      int64         `json:"total_queued"`
	TotalDelivered   int64         `json:"total_delivered"`
	TotalExpired     int64         `json:"total_expired"`
	TotalDLQ         int64         `json:"total_dlq"`
	PendingMessages  int64         `json:"pending_messages"`
	OldestMessageAge time.Duration `json:"oldest_message_age,omitempty"`
}
