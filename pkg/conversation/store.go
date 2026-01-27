package conversation

import (
	"errors"

	"github.com/google/uuid"
)

// Common errors for conversation store operations.
var (
	ErrThreadNotFound  = errors.New("thread not found")
	ErrMessageNotFound = errors.New("message not found")
	ErrReceiptNotFound = errors.New("receipt not found")
	ErrStoreClosed     = errors.New("store is closed")
)

// Store defines the interface for conversation persistence.
// Each agent maintains its own conversation history.
type Store interface {
	// Thread operations
	CreateThread(thread *Thread) error
	GetThread(id uuid.UUID) (*Thread, error)
	UpdateThread(thread *Thread) error
	DeleteThread(id uuid.UUID) error
	ListThreads(filter ThreadFilter) ([]*Thread, error)

	// Message operations
	SaveMessage(msg *StoredMessage) error
	GetMessage(id uuid.UUID) (*StoredMessage, error)
	GetMessages(filter MessageFilter) ([]*StoredMessage, error)
	DeleteMessage(id uuid.UUID) error

	// Receipt operations
	MarkDelivered(messageID uuid.UUID, recipientDID string) error
	MarkRead(messageID uuid.UUID, recipientDID string) error
	GetReceipt(messageID uuid.UUID, recipientDID string) (*MessageReceipt, error)
	GetReceipts(messageID uuid.UUID) ([]*MessageReceipt, error)

	// Lifecycle
	Close() error
}

// StoreConfig provides common configuration for store implementations.
type StoreConfig struct {
	// MaxThreads limits the number of threads to store (0 = unlimited).
	MaxThreads int

	// MaxMessagesPerThread limits messages per thread (0 = unlimited).
	MaxMessagesPerThread int

	// RetentionDays sets how long to keep messages (0 = forever).
	RetentionDays int
}

// DefaultStoreConfig returns sensible defaults for a conversation store.
func DefaultStoreConfig() StoreConfig {
	return StoreConfig{
		MaxThreads:           0, // unlimited
		MaxMessagesPerThread: 0, // unlimited
		RetentionDays:        0, // forever
	}
}
