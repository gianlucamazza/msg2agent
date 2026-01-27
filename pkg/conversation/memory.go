package conversation

import (
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
)

// MemoryStore is an in-memory implementation of Store for testing and ephemeral use.
type MemoryStore struct {
	threads  map[uuid.UUID]*Thread
	messages map[uuid.UUID]*StoredMessage
	receipts map[string]*MessageReceipt // key: "messageID:recipientDID"

	// Indexes for efficient lookups
	messagesByThread map[uuid.UUID][]uuid.UUID // threadID -> messageIDs
	threadsByDID     map[string][]uuid.UUID    // participantDID -> threadIDs

	config StoreConfig
	mu     sync.RWMutex
	closed bool
}

// NewMemoryStore creates a new in-memory conversation store.
func NewMemoryStore(config StoreConfig) *MemoryStore {
	return &MemoryStore{
		threads:          make(map[uuid.UUID]*Thread),
		messages:         make(map[uuid.UUID]*StoredMessage),
		receipts:         make(map[string]*MessageReceipt),
		messagesByThread: make(map[uuid.UUID][]uuid.UUID),
		threadsByDID:     make(map[string][]uuid.UUID),
		config:           config,
	}
}

// CreateThread creates a new thread.
func (s *MemoryStore) CreateThread(thread *Thread) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrStoreClosed
	}

	// Clone thread to avoid external modifications
	t := *thread
	s.threads[t.ID] = &t

	// Index by participants
	for _, did := range t.Participants {
		s.threadsByDID[did] = append(s.threadsByDID[did], t.ID)
	}

	return nil
}

// GetThread retrieves a thread by ID.
func (s *MemoryStore) GetThread(id uuid.UUID) (*Thread, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrStoreClosed
	}

	thread, ok := s.threads[id]
	if !ok {
		return nil, ErrThreadNotFound
	}

	// Return a copy
	t := *thread
	t.Participants = slices.Clone(thread.Participants)
	return &t, nil
}

// UpdateThread updates an existing thread.
func (s *MemoryStore) UpdateThread(thread *Thread) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrStoreClosed
	}

	if _, ok := s.threads[thread.ID]; !ok {
		return ErrThreadNotFound
	}

	// Update index if participants changed
	old := s.threads[thread.ID]
	for _, did := range old.Participants {
		if !thread.HasParticipant(did) {
			s.removeThreadFromDID(did, thread.ID)
		}
	}
	for _, did := range thread.Participants {
		if !old.HasParticipant(did) {
			s.threadsByDID[did] = append(s.threadsByDID[did], thread.ID)
		}
	}

	t := *thread
	s.threads[thread.ID] = &t
	return nil
}

// DeleteThread removes a thread and all its messages.
func (s *MemoryStore) DeleteThread(id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrStoreClosed
	}

	thread, ok := s.threads[id]
	if !ok {
		return ErrThreadNotFound
	}

	// Remove from participant index
	for _, did := range thread.Participants {
		s.removeThreadFromDID(did, id)
	}

	// Delete all messages in thread
	for _, msgID := range s.messagesByThread[id] {
		delete(s.messages, msgID)
		// Delete receipts for this message
		for key := range s.receipts {
			if len(key) > 36 && key[:36] == msgID.String() {
				delete(s.receipts, key)
			}
		}
	}
	delete(s.messagesByThread, id)

	delete(s.threads, id)
	return nil
}

// ListThreads lists threads matching the filter.
func (s *MemoryStore) ListThreads(filter ThreadFilter) ([]*Thread, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrStoreClosed
	}

	var result []*Thread

	// If filtering by participant, use index
	if filter.ParticipantDID != "" {
		threadIDs := s.threadsByDID[filter.ParticipantDID]
		for _, id := range threadIDs {
			if thread, ok := s.threads[id]; ok {
				if s.threadMatchesFilter(thread, filter) {
					t := *thread
					t.Participants = slices.Clone(thread.Participants)
					result = append(result, &t)
				}
			}
		}
	} else {
		for _, thread := range s.threads {
			if s.threadMatchesFilter(thread, filter) {
				t := *thread
				t.Participants = slices.Clone(thread.Participants)
				result = append(result, &t)
			}
		}
	}

	// Sort by UpdatedAt descending
	slices.SortFunc(result, func(a, b *Thread) int {
		if a.UpdatedAt.After(b.UpdatedAt) {
			return -1
		}
		if a.UpdatedAt.Before(b.UpdatedAt) {
			return 1
		}
		return 0
	})

	// Apply pagination
	if filter.Offset > 0 {
		if filter.Offset >= len(result) {
			return []*Thread{}, nil
		}
		result = result[filter.Offset:]
	}
	if filter.Limit > 0 && len(result) > filter.Limit {
		result = result[:filter.Limit]
	}

	return result, nil
}

// SaveMessage saves a message to a thread.
func (s *MemoryStore) SaveMessage(msg *StoredMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrStoreClosed
	}

	// Verify thread exists
	thread, ok := s.threads[msg.ThreadID]
	if !ok {
		return ErrThreadNotFound
	}

	// Clone and store message
	m := *msg
	s.messages[m.ID] = &m

	// Update index
	s.messagesByThread[msg.ThreadID] = append(s.messagesByThread[msg.ThreadID], msg.ID)

	// Update thread stats
	thread.MessageCount++
	now := msg.Timestamp
	thread.LastMessage = &now
	thread.UpdatedAt = time.Now()

	return nil
}

// GetMessage retrieves a message by ID.
func (s *MemoryStore) GetMessage(id uuid.UUID) (*StoredMessage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrStoreClosed
	}

	msg, ok := s.messages[id]
	if !ok {
		return nil, ErrMessageNotFound
	}

	m := *msg
	return &m, nil
}

// GetMessages retrieves messages matching the filter.
func (s *MemoryStore) GetMessages(filter MessageFilter) ([]*StoredMessage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrStoreClosed
	}

	msgIDs := s.messagesByThread[filter.ThreadID]
	var result []*StoredMessage

	for _, id := range msgIDs {
		msg, ok := s.messages[id]
		if !ok {
			continue
		}
		if s.messageMatchesFilter(msg, filter) {
			m := *msg
			result = append(result, &m)
		}
	}

	// Sort by ThreadSeqNo ascending
	slices.SortFunc(result, func(a, b *StoredMessage) int {
		return a.ThreadSeqNo - b.ThreadSeqNo
	})

	// Apply pagination
	if filter.Offset > 0 {
		if filter.Offset >= len(result) {
			return []*StoredMessage{}, nil
		}
		result = result[filter.Offset:]
	}
	if filter.Limit > 0 && len(result) > filter.Limit {
		result = result[:filter.Limit]
	}

	return result, nil
}

// DeleteMessage removes a message.
func (s *MemoryStore) DeleteMessage(id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrStoreClosed
	}

	msg, ok := s.messages[id]
	if !ok {
		return ErrMessageNotFound
	}

	// Remove from thread index
	msgIDs := s.messagesByThread[msg.ThreadID]
	for i, mid := range msgIDs {
		if mid == id {
			s.messagesByThread[msg.ThreadID] = slices.Delete(msgIDs, i, i+1)
			break
		}
	}

	// Update thread message count
	if thread, ok := s.threads[msg.ThreadID]; ok {
		thread.MessageCount--
	}

	// Delete receipts
	for key := range s.receipts {
		if len(key) > 36 && key[:36] == id.String() {
			delete(s.receipts, key)
		}
	}

	delete(s.messages, id)
	return nil
}

// MarkDelivered marks a message as delivered to a recipient.
func (s *MemoryStore) MarkDelivered(messageID uuid.UUID, recipientDID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrStoreClosed
	}

	if _, ok := s.messages[messageID]; !ok {
		return ErrMessageNotFound
	}

	key := receiptKey(messageID, recipientDID)
	receipt, ok := s.receipts[key]
	if !ok {
		now := time.Now()
		s.receipts[key] = &MessageReceipt{
			MessageID:    messageID,
			RecipientDID: recipientDID,
			DeliveredAt:  &now,
		}
	} else if receipt.DeliveredAt == nil {
		now := time.Now()
		receipt.DeliveredAt = &now
	}

	return nil
}

// MarkRead marks a message as read by a recipient.
func (s *MemoryStore) MarkRead(messageID uuid.UUID, recipientDID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrStoreClosed
	}

	if _, ok := s.messages[messageID]; !ok {
		return ErrMessageNotFound
	}

	key := receiptKey(messageID, recipientDID)
	receipt, ok := s.receipts[key]
	now := time.Now()
	if !ok {
		// Mark as both delivered and read
		s.receipts[key] = &MessageReceipt{
			MessageID:    messageID,
			RecipientDID: recipientDID,
			DeliveredAt:  &now,
			ReadAt:       &now,
		}
	} else {
		if receipt.DeliveredAt == nil {
			receipt.DeliveredAt = &now
		}
		receipt.ReadAt = &now
	}

	return nil
}

// GetReceipt retrieves a receipt for a specific message and recipient.
func (s *MemoryStore) GetReceipt(messageID uuid.UUID, recipientDID string) (*MessageReceipt, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrStoreClosed
	}

	key := receiptKey(messageID, recipientDID)
	receipt, ok := s.receipts[key]
	if !ok {
		return nil, ErrReceiptNotFound
	}

	r := *receipt
	return &r, nil
}

// GetReceipts retrieves all receipts for a message.
func (s *MemoryStore) GetReceipts(messageID uuid.UUID) ([]*MessageReceipt, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrStoreClosed
	}

	prefix := messageID.String() + ":"
	var result []*MessageReceipt
	for key, receipt := range s.receipts {
		if len(key) > 36 && key[:37] == prefix {
			r := *receipt
			result = append(result, &r)
		}
	}

	return result, nil
}

// Close closes the store.
func (s *MemoryStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.closed = true
	// Clear all data
	s.threads = nil
	s.messages = nil
	s.receipts = nil
	s.messagesByThread = nil
	s.threadsByDID = nil

	return nil
}

// Helper functions

func (s *MemoryStore) removeThreadFromDID(did string, threadID uuid.UUID) {
	threadIDs := s.threadsByDID[did]
	for i, id := range threadIDs {
		if id == threadID {
			s.threadsByDID[did] = slices.Delete(threadIDs, i, i+1)
			return
		}
	}
}

func (s *MemoryStore) threadMatchesFilter(thread *Thread, filter ThreadFilter) bool {
	if filter.Since != nil && thread.UpdatedAt.Before(*filter.Since) {
		return false
	}
	return true
}

func (s *MemoryStore) messageMatchesFilter(msg *StoredMessage, filter MessageFilter) bool {
	if filter.Since != nil && msg.Timestamp.Before(*filter.Since) {
		return false
	}
	if filter.Before != nil && msg.Timestamp.After(*filter.Before) {
		return false
	}
	if len(filter.Types) > 0 && !slices.Contains(filter.Types, msg.Type) {
		return false
	}
	return true
}

func receiptKey(messageID uuid.UUID, recipientDID string) string {
	return messageID.String() + ":" + recipientDID
}
