// Package conversation provides conversation threading and persistence for chat messages.
package conversation

import (
	"slices"
	"time"

	"github.com/google/uuid"
)

// ThreadState represents the state of a conversation thread.
type ThreadState string

const (
	// ThreadStateActive indicates the thread is active with ongoing conversation.
	ThreadStateActive ThreadState = "active"
	// ThreadStateAwaitingInput indicates the thread is waiting for user input.
	ThreadStateAwaitingInput ThreadState = "awaiting-input"
	// ThreadStateCompleted indicates the thread has completed successfully.
	ThreadStateCompleted ThreadState = "completed"
	// ThreadStateFailed indicates the thread encountered an error.
	ThreadStateFailed ThreadState = "failed"
)

// Thread represents a conversation thread containing multiple messages.
type Thread struct {
	ID           uuid.UUID      `json:"id"`
	Title        string         `json:"title,omitempty"` // Optional thread title
	Participants []string       `json:"participants"`    // DIDs of participants
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	LastMessage  *time.Time     `json:"last_message,omitempty"` // Timestamp of last message
	MessageCount int            `json:"message_count"`
	Metadata     map[string]any `json:"metadata,omitempty"`

	// A2A integration fields
	State      ThreadState `json:"state,omitempty"`       // Thread state for A2A mapping
	TaskIDs    []string    `json:"task_ids,omitempty"`    // Associated A2A task IDs
	SessionIDs []string    `json:"session_ids,omitempty"` // Associated A2A session IDs
}

// NewThread creates a new thread with the given participants.
func NewThread(participants ...string) *Thread {
	now := time.Now()
	return &Thread{
		ID:           uuid.Must(uuid.NewV7()),
		Participants: participants,
		CreatedAt:    now,
		UpdatedAt:    now,
		MessageCount: 0,
		Metadata:     make(map[string]any),
	}
}

// HasParticipant checks if a DID is a participant in the thread.
func (t *Thread) HasParticipant(did string) bool {
	return slices.Contains(t.Participants, did)
}

// AddParticipant adds a DID to the thread participants if not already present.
func (t *Thread) AddParticipant(did string) bool {
	if t.HasParticipant(did) {
		return false
	}
	t.Participants = append(t.Participants, did)
	t.UpdatedAt = time.Now()
	return true
}

// StoredMessage represents a message stored in the conversation history.
// It embeds the core message fields needed for persistence.
type StoredMessage struct {
	ID          uuid.UUID      `json:"id"`
	ThreadID    uuid.UUID      `json:"thread_id"`
	ParentID    *uuid.UUID     `json:"parent_id,omitempty"`
	From        string         `json:"from"`
	To          string         `json:"to"`
	Type        string         `json:"type"`
	Body        []byte         `json:"body"`
	Timestamp   time.Time      `json:"timestamp"`
	ThreadSeqNo int            `json:"thread_seq_no"`
	Encrypted   bool           `json:"encrypted"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// MessageReceipt tracks delivery and read status of a message.
type MessageReceipt struct {
	MessageID    uuid.UUID  `json:"message_id"`
	RecipientDID string     `json:"recipient_did"`
	DeliveredAt  *time.Time `json:"delivered_at,omitempty"`
	ReadAt       *time.Time `json:"read_at,omitempty"`
}

// IsDelivered returns true if the message has been delivered.
func (r *MessageReceipt) IsDelivered() bool {
	return r.DeliveredAt != nil
}

// IsRead returns true if the message has been read.
func (r *MessageReceipt) IsRead() bool {
	return r.ReadAt != nil
}

// ThreadFilter provides options for filtering thread queries.
type ThreadFilter struct {
	ParticipantDID string     // Filter by participant
	Since          *time.Time // Threads updated since
	Limit          int        // Maximum results (0 = no limit)
	Offset         int        // Pagination offset
}

// MessageFilter provides options for filtering message queries.
type MessageFilter struct {
	ThreadID uuid.UUID  // Required: thread to query
	Since    *time.Time // Messages since timestamp
	Before   *time.Time // Messages before timestamp
	Types    []string   // Filter by message types
	Limit    int        // Maximum results (0 = no limit)
	Offset   int        // Pagination offset
}
