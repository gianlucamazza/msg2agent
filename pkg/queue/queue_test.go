package queue

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func testStoreImpl(t *testing.T, store Store) {
	t.Run("Enqueue and Dequeue", func(t *testing.T) {
		msg := &QueuedMessage{
			ID:           uuid.Must(uuid.NewV7()),
			RecipientDID: "did:wba:bob",
			SenderDID:    "did:wba:alice",
			Data:         []byte(`{"test":"data"}`),
			QueuedAt:     time.Now(),
			ExpiresAt:    time.Now().Add(time.Hour),
		}

		if err := store.Enqueue(msg); err != nil {
			t.Fatalf("Enqueue failed: %v", err)
		}

		// Peek should show message
		peeked, err := store.Peek("did:wba:bob", 10)
		if err != nil {
			t.Fatalf("Peek failed: %v", err)
		}
		if len(peeked) != 1 {
			t.Errorf("expected 1 message, got %d", len(peeked))
		}

		// Dequeue should return and remove message
		dequeued, err := store.Dequeue("did:wba:bob", 10)
		if err != nil {
			t.Fatalf("Dequeue failed: %v", err)
		}
		if len(dequeued) != 1 {
			t.Errorf("expected 1 message, got %d", len(dequeued))
		}
		if string(dequeued[0].Data) != string(msg.Data) {
			t.Error("data mismatch")
		}

		// Should be empty now
		peeked, _ = store.Peek("did:wba:bob", 10)
		if len(peeked) != 0 {
			t.Errorf("expected 0 messages after dequeue, got %d", len(peeked))
		}
	})

	t.Run("Ack", func(t *testing.T) {
		msg := &QueuedMessage{
			ID:           uuid.Must(uuid.NewV7()),
			RecipientDID: "did:wba:bob",
			Data:         []byte(`test`),
			QueuedAt:     time.Now(),
			ExpiresAt:    time.Now().Add(time.Hour),
		}
		_ = store.Enqueue(msg)

		// Peek to get it (without removing)
		peeked, _ := store.Peek("did:wba:bob", 10)
		if len(peeked) != 1 {
			t.Fatalf("expected 1 message")
		}

		// Ack should remove it
		if err := store.Ack(msg.ID); err != nil {
			t.Fatalf("Ack failed: %v", err)
		}

		peeked, _ = store.Peek("did:wba:bob", 10)
		if len(peeked) != 0 {
			t.Errorf("expected 0 messages after ack, got %d", len(peeked))
		}

		// Ack non-existent should fail
		err := store.Ack(uuid.Must(uuid.NewV7()))
		if err != ErrNotFound {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("Nack with DLQ", func(t *testing.T) {
		msg := &QueuedMessage{
			ID:               uuid.Must(uuid.NewV7()),
			RecipientDID:     "did:wba:charlie",
			Data:             []byte(`test`),
			QueuedAt:         time.Now(),
			ExpiresAt:        time.Now().Add(time.Hour),
			DeliveryAttempts: 2, // Will hit limit on next nack
		}
		_ = store.Enqueue(msg)

		// Nack should move to DLQ after max attempts
		if err := store.Nack(msg.ID, "test failure"); err != nil {
			t.Fatalf("Nack failed: %v", err)
		}

		// Should be removed from main queue
		peeked, _ := store.Peek("did:wba:charlie", 10)
		if len(peeked) != 0 {
			t.Errorf("expected 0 messages after nack, got %d", len(peeked))
		}

		// Should be in DLQ
		dlq, err := store.GetDLQ("did:wba:charlie", 10)
		if err != nil {
			t.Fatalf("GetDLQ failed: %v", err)
		}
		if len(dlq) != 1 {
			t.Errorf("expected 1 DLQ message, got %d", len(dlq))
		}
		if dlq[0].FailReason != "test failure" {
			t.Errorf("expected fail reason 'test failure', got %q", dlq[0].FailReason)
		}
	})

	t.Run("GetQueueSize", func(t *testing.T) {
		for i := 0; i < 3; i++ {
			_ = store.Enqueue(&QueuedMessage{
				ID:           uuid.Must(uuid.NewV7()),
				RecipientDID: "did:wba:dave",
				Data:         []byte(`test`),
				QueuedAt:     time.Now(),
				ExpiresAt:    time.Now().Add(time.Hour),
			})
		}

		size, err := store.GetQueueSize("did:wba:dave")
		if err != nil {
			t.Fatalf("GetQueueSize failed: %v", err)
		}
		if size != 3 {
			t.Errorf("expected size 3, got %d", size)
		}

		// Cleanup
		_, _ = store.Dequeue("did:wba:dave", 10)
	})

	t.Run("Expired messages", func(t *testing.T) {
		msg := &QueuedMessage{
			ID:           uuid.Must(uuid.NewV7()),
			RecipientDID: "did:wba:eve",
			Data:         []byte(`test`),
			QueuedAt:     time.Now().Add(-2 * time.Hour),
			ExpiresAt:    time.Now().Add(-time.Hour), // Already expired
		}
		_ = store.Enqueue(msg)

		// Should not return expired messages
		dequeued, _ := store.Dequeue("did:wba:eve", 10)
		if len(dequeued) != 0 {
			t.Errorf("expected 0 messages (expired), got %d", len(dequeued))
		}

		// Cleanup should remove them
		removed, err := store.Cleanup()
		if err != nil {
			t.Fatalf("Cleanup failed: %v", err)
		}
		if removed < 1 {
			t.Errorf("expected at least 1 removed, got %d", removed)
		}
	})

	t.Run("FIFO ordering", func(t *testing.T) {
		// Add messages with different queued times
		for i := 0; i < 3; i++ {
			_ = store.Enqueue(&QueuedMessage{
				ID:           uuid.Must(uuid.NewV7()),
				RecipientDID: "did:wba:frank",
				Data:         []byte{byte(i)},
				QueuedAt:     time.Now().Add(time.Duration(i) * time.Second),
				ExpiresAt:    time.Now().Add(time.Hour),
			})
		}

		// Dequeue should return oldest first
		dequeued, _ := store.Dequeue("did:wba:frank", 10)
		if len(dequeued) != 3 {
			t.Fatalf("expected 3 messages, got %d", len(dequeued))
		}

		for i, msg := range dequeued {
			if msg.Data[0] != byte(i) {
				t.Errorf("expected data %d at position %d, got %d", i, i, msg.Data[0])
			}
		}
	})
}

func TestMemoryStore(t *testing.T) {
	config := DefaultConfig()
	config.MaxDeliveryAttempts = 3
	config.EnableDLQ = true
	store := NewMemoryStore(config)
	defer func() { _ = store.Close() }()
	testStoreImpl(t, store)
}

func TestMemoryStoreQueueFull(t *testing.T) {
	config := DefaultConfig()
	config.MaxQueueSize = 2
	store := NewMemoryStore(config)
	defer func() { _ = store.Close() }()

	for i := 0; i < 2; i++ {
		err := store.Enqueue(&QueuedMessage{
			ID:           uuid.Must(uuid.NewV7()),
			RecipientDID: "did:wba:test",
			Data:         []byte(`test`),
			QueuedAt:     time.Now(),
			ExpiresAt:    time.Now().Add(time.Hour),
		})
		if err != nil {
			t.Fatalf("Enqueue %d failed: %v", i, err)
		}
	}

	// Third should fail
	err := store.Enqueue(&QueuedMessage{
		ID:           uuid.Must(uuid.NewV7()),
		RecipientDID: "did:wba:test",
		Data:         []byte(`test`),
		QueuedAt:     time.Now(),
		ExpiresAt:    time.Now().Add(time.Hour),
	})
	if err != ErrQueueFull {
		t.Errorf("expected ErrQueueFull, got %v", err)
	}
}

func TestSQLiteStore(t *testing.T) {
	config := DefaultConfig()
	config.MaxDeliveryAttempts = 3
	config.EnableDLQ = true
	store, err := NewSQLiteStore(SQLiteConfig{
		Path:        ":memory:",
		QueueConfig: config,
	})
	if err != nil {
		t.Fatalf("failed to create SQLite store: %v", err)
	}
	defer func() { _ = store.Close() }()
	testStoreImpl(t, store)
}

func TestQueuedMessageExpired(t *testing.T) {
	msg := &QueuedMessage{
		ID:        uuid.Must(uuid.NewV7()),
		ExpiresAt: time.Now().Add(-time.Hour),
	}
	if !msg.IsExpired() {
		t.Error("message should be expired")
	}

	msg.ExpiresAt = time.Now().Add(time.Hour)
	if msg.IsExpired() {
		t.Error("message should not be expired")
	}
}
