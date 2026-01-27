package queue

import (
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
)

// MemoryStore is an in-memory implementation of Store.
type MemoryStore struct {
	config     Config
	queues     map[string][]*QueuedMessage // recipientDID -> messages
	dlq        map[string][]*DeadLetterMessage
	totalCount int // Total messages across all queues
	mu         sync.RWMutex
	closed     bool
}

// NewMemoryStore creates a new in-memory queue store.
func NewMemoryStore(config Config) *MemoryStore {
	return &MemoryStore{
		config: config,
		queues: make(map[string][]*QueuedMessage),
		dlq:    make(map[string][]*DeadLetterMessage),
	}
}

// Enqueue adds a message to the queue.
func (s *MemoryStore) Enqueue(msg *QueuedMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrQueueClosed
	}

	// Check per-recipient queue size limit
	if s.config.MaxQueueSize > 0 && len(s.queues[msg.RecipientDID]) >= s.config.MaxQueueSize {
		return ErrQueueFull
	}

	// Check global limit and evict oldest if necessary
	if s.config.MaxTotalMessages > 0 && s.totalCount >= s.config.MaxTotalMessages {
		s.evictOldestLocked()
	}

	// Clone message to prevent external modification
	m := *msg
	s.queues[msg.RecipientDID] = append(s.queues[msg.RecipientDID], &m)
	s.totalCount++

	return nil
}

// evictOldestLocked removes the oldest message across all queues.
// Must be called with lock held.
func (s *MemoryStore) evictOldestLocked() {
	var oldestTime time.Time
	var oldestDID string
	var oldestIdx int
	first := true

	// Find the oldest message across all queues
	for did, queue := range s.queues {
		for i, msg := range queue {
			if first || msg.QueuedAt.Before(oldestTime) {
				oldestTime = msg.QueuedAt
				oldestDID = did
				oldestIdx = i
				first = false
			}
		}
	}

	// Remove the oldest message
	if !first && oldestDID != "" {
		s.queues[oldestDID] = slices.Delete(s.queues[oldestDID], oldestIdx, oldestIdx+1)
		s.totalCount--
		// Clean up empty queue
		if len(s.queues[oldestDID]) == 0 {
			delete(s.queues, oldestDID)
		}
	}
}

// Dequeue retrieves and removes pending messages for a recipient.
func (s *MemoryStore) Dequeue(recipientDID string, limit int) ([]*QueuedMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil, ErrQueueClosed
	}

	queue := s.queues[recipientDID]
	if len(queue) == 0 {
		return []*QueuedMessage{}, nil
	}

	// Sort by queued time
	slices.SortFunc(queue, func(a, b *QueuedMessage) int {
		return a.QueuedAt.Compare(b.QueuedAt)
	})

	// Filter expired and limit
	var result []*QueuedMessage
	var remaining []*QueuedMessage

	for _, msg := range queue {
		if msg.IsExpired() {
			remaining = append(remaining, msg) // Keep expired for Cleanup to handle
			continue
		}
		if limit > 0 && len(result) >= limit {
			remaining = append(remaining, msg)
		} else {
			result = append(result, msg)
		}
	}

	// Update total count
	s.totalCount -= len(result)
	s.queues[recipientDID] = remaining
	if len(remaining) == 0 {
		delete(s.queues, recipientDID)
	}

	return result, nil
}

// Peek retrieves pending messages without removing them.
func (s *MemoryStore) Peek(recipientDID string, limit int) ([]*QueuedMessage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrQueueClosed
	}

	queue := s.queues[recipientDID]
	if len(queue) == 0 {
		return []*QueuedMessage{}, nil
	}

	// Sort by queued time
	sorted := slices.Clone(queue)
	slices.SortFunc(sorted, func(a, b *QueuedMessage) int {
		return a.QueuedAt.Compare(b.QueuedAt)
	})

	// Filter expired and limit
	var result []*QueuedMessage
	for _, msg := range sorted {
		if msg.IsExpired() {
			continue
		}
		if limit > 0 && len(result) >= limit {
			break
		}
		// Return copy
		m := *msg
		result = append(result, &m)
	}

	return result, nil
}

// Ack removes a successfully delivered message.
func (s *MemoryStore) Ack(messageID uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrQueueClosed
	}

	for did, queue := range s.queues {
		for i, msg := range queue {
			if msg.ID == messageID {
				s.queues[did] = slices.Delete(queue, i, i+1)
				s.totalCount--
				if len(s.queues[did]) == 0 {
					delete(s.queues, did)
				}
				return nil
			}
		}
	}

	return ErrNotFound
}

// Nack returns a message to the queue for retry.
func (s *MemoryStore) Nack(messageID uuid.UUID, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrQueueClosed
	}

	for did, queue := range s.queues {
		for i, msg := range queue {
			if msg.ID == messageID {
				msg.DeliveryAttempts++
				now := time.Now()
				msg.LastAttempt = &now

				// Move to DLQ if max attempts exceeded
				if s.config.MaxDeliveryAttempts > 0 && msg.DeliveryAttempts >= s.config.MaxDeliveryAttempts {
					if s.config.EnableDLQ {
						dlqMsg := &DeadLetterMessage{
							QueuedMessage: *msg,
							FailReason:    reason,
							MovedAt:       now,
						}
						s.dlq[did] = append(s.dlq[did], dlqMsg)
					}
					s.queues[did] = slices.Delete(queue, i, i+1)
					s.totalCount--
					if len(s.queues[did]) == 0 {
						delete(s.queues, did)
					}
				}
				return nil
			}
		}
	}

	return ErrNotFound
}

// GetQueueSize returns the number of pending messages for a recipient.
func (s *MemoryStore) GetQueueSize(recipientDID string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return 0, ErrQueueClosed
	}

	count := 0
	for _, msg := range s.queues[recipientDID] {
		if !msg.IsExpired() {
			count++
		}
	}

	return count, nil
}

// GetDLQ retrieves messages from the dead letter queue.
func (s *MemoryStore) GetDLQ(recipientDID string, limit int) ([]*DeadLetterMessage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrQueueClosed
	}

	dlq := s.dlq[recipientDID]
	if len(dlq) == 0 {
		return []*DeadLetterMessage{}, nil
	}

	var result []*DeadLetterMessage
	for _, msg := range dlq {
		if limit > 0 && len(result) >= limit {
			break
		}
		m := *msg
		result = append(result, &m)
	}

	return result, nil
}

// Cleanup removes expired messages.
func (s *MemoryStore) Cleanup() (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return 0, ErrQueueClosed
	}

	var removed int64
	for did, queue := range s.queues {
		var remaining []*QueuedMessage
		for _, msg := range queue {
			if msg.IsExpired() {
				removed++
				s.totalCount--
			} else {
				remaining = append(remaining, msg)
			}
		}
		if len(remaining) == 0 {
			delete(s.queues, did)
		} else {
			s.queues[did] = remaining
		}
	}

	return removed, nil
}

// Close closes the store.
func (s *MemoryStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.closed = true
	s.queues = nil
	s.dlq = nil

	return nil
}
