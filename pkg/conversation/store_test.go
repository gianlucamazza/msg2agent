package conversation

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// testStoreImpl runs the common store tests against a Store implementation.
func testStoreImpl(t *testing.T, store Store) {
	t.Run("Thread CRUD", func(t *testing.T) {
		thread := NewThread("did:wba:alice", "did:wba:bob")
		thread.Title = "Test Thread"

		// Create
		if err := store.CreateThread(thread); err != nil {
			t.Fatalf("CreateThread failed: %v", err)
		}

		// Get
		got, err := store.GetThread(thread.ID)
		if err != nil {
			t.Fatalf("GetThread failed: %v", err)
		}
		if got.Title != thread.Title {
			t.Errorf("expected title %q, got %q", thread.Title, got.Title)
		}
		if len(got.Participants) != 2 {
			t.Errorf("expected 2 participants, got %d", len(got.Participants))
		}

		// Update
		got.Title = "Updated Title"
		got.AddParticipant("did:wba:charlie")
		if err := store.UpdateThread(got); err != nil {
			t.Fatalf("UpdateThread failed: %v", err)
		}

		got2, _ := store.GetThread(thread.ID)
		if got2.Title != "Updated Title" {
			t.Errorf("expected updated title, got %q", got2.Title)
		}
		if len(got2.Participants) != 3 {
			t.Errorf("expected 3 participants, got %d", len(got2.Participants))
		}

		// Delete
		if err := store.DeleteThread(thread.ID); err != nil {
			t.Fatalf("DeleteThread failed: %v", err)
		}
		_, err = store.GetThread(thread.ID)
		if err != ErrThreadNotFound {
			t.Errorf("expected ErrThreadNotFound, got %v", err)
		}
	})

	t.Run("Thread not found", func(t *testing.T) {
		_, err := store.GetThread(uuid.Must(uuid.NewV7()))
		if err != ErrThreadNotFound {
			t.Errorf("expected ErrThreadNotFound, got %v", err)
		}
	})

	t.Run("Message CRUD", func(t *testing.T) {
		thread := NewThread("did:wba:alice", "did:wba:bob")
		_ = store.CreateThread(thread)

		msg := &StoredMessage{
			ID:          uuid.Must(uuid.NewV7()),
			ThreadID:    thread.ID,
			From:        "did:wba:alice",
			To:          "did:wba:bob",
			Type:        "chat",
			Body:        []byte(`{"text":"Hello"}`),
			Timestamp:   time.Now(),
			ThreadSeqNo: 1,
		}

		// Save
		if err := store.SaveMessage(msg); err != nil {
			t.Fatalf("SaveMessage failed: %v", err)
		}

		// Get
		got, err := store.GetMessage(msg.ID)
		if err != nil {
			t.Fatalf("GetMessage failed: %v", err)
		}
		if got.From != msg.From {
			t.Errorf("expected from %q, got %q", msg.From, got.From)
		}
		if string(got.Body) != string(msg.Body) {
			t.Errorf("expected body %q, got %q", msg.Body, got.Body)
		}

		// Thread message count updated
		thread2, _ := store.GetThread(thread.ID)
		if thread2.MessageCount != 1 {
			t.Errorf("expected message count 1, got %d", thread2.MessageCount)
		}

		// Delete
		if err := store.DeleteMessage(msg.ID); err != nil {
			t.Fatalf("DeleteMessage failed: %v", err)
		}
		_, err = store.GetMessage(msg.ID)
		if err != ErrMessageNotFound {
			t.Errorf("expected ErrMessageNotFound, got %v", err)
		}

		_ = store.DeleteThread(thread.ID)
	})

	t.Run("GetMessages filter", func(t *testing.T) {
		thread := NewThread("did:wba:alice", "did:wba:bob")
		_ = store.CreateThread(thread)

		// Add multiple messages
		for i := 1; i <= 5; i++ {
			msg := &StoredMessage{
				ID:          uuid.Must(uuid.NewV7()),
				ThreadID:    thread.ID,
				From:        "did:wba:alice",
				To:          "did:wba:bob",
				Type:        "chat",
				Body:        []byte(`{"text":"msg"}`),
				Timestamp:   time.Now().Add(time.Duration(i) * time.Second),
				ThreadSeqNo: i,
			}
			_ = store.SaveMessage(msg)
		}

		// Get all
		msgs, err := store.GetMessages(MessageFilter{ThreadID: thread.ID})
		if err != nil {
			t.Fatalf("GetMessages failed: %v", err)
		}
		if len(msgs) != 5 {
			t.Errorf("expected 5 messages, got %d", len(msgs))
		}

		// Verify ordering
		for i, msg := range msgs {
			if msg.ThreadSeqNo != i+1 {
				t.Errorf("expected seqno %d, got %d", i+1, msg.ThreadSeqNo)
			}
		}

		// Get with limit
		msgs, _ = store.GetMessages(MessageFilter{ThreadID: thread.ID, Limit: 2})
		if len(msgs) != 2 {
			t.Errorf("expected 2 messages with limit, got %d", len(msgs))
		}

		// Get with offset
		msgs, _ = store.GetMessages(MessageFilter{ThreadID: thread.ID, Offset: 3})
		if len(msgs) != 2 {
			t.Errorf("expected 2 messages with offset, got %d", len(msgs))
		}

		_ = store.DeleteThread(thread.ID)
	})

	t.Run("ListThreads filter", func(t *testing.T) {
		t1 := NewThread("did:wba:alice", "did:wba:bob")
		t2 := NewThread("did:wba:bob", "did:wba:charlie")
		t3 := NewThread("did:wba:alice", "did:wba:charlie")
		_ = store.CreateThread(t1)
		time.Sleep(10 * time.Millisecond)
		_ = store.CreateThread(t2)
		time.Sleep(10 * time.Millisecond)
		_ = store.CreateThread(t3)

		// Filter by participant
		threads, err := store.ListThreads(ThreadFilter{ParticipantDID: "did:wba:alice"})
		if err != nil {
			t.Fatalf("ListThreads failed: %v", err)
		}
		if len(threads) != 2 {
			t.Errorf("expected 2 threads for alice, got %d", len(threads))
		}

		threads, _ = store.ListThreads(ThreadFilter{ParticipantDID: "did:wba:bob"})
		if len(threads) != 2 {
			t.Errorf("expected 2 threads for bob, got %d", len(threads))
		}

		// With limit
		threads, _ = store.ListThreads(ThreadFilter{Limit: 1})
		if len(threads) != 1 {
			t.Errorf("expected 1 thread with limit, got %d", len(threads))
		}

		_ = store.DeleteThread(t1.ID)
		_ = store.DeleteThread(t2.ID)
		_ = store.DeleteThread(t3.ID)
	})

	t.Run("Receipts", func(t *testing.T) {
		thread := NewThread("did:wba:alice", "did:wba:bob")
		_ = store.CreateThread(thread)

		msg := &StoredMessage{
			ID:          uuid.Must(uuid.NewV7()),
			ThreadID:    thread.ID,
			From:        "did:wba:alice",
			To:          "did:wba:bob",
			Type:        "chat",
			Body:        []byte(`{"text":"Hello"}`),
			Timestamp:   time.Now(),
			ThreadSeqNo: 1,
		}
		_ = store.SaveMessage(msg)

		// Mark delivered
		if err := store.MarkDelivered(msg.ID, "did:wba:bob"); err != nil {
			t.Fatalf("MarkDelivered failed: %v", err)
		}

		receipt, err := store.GetReceipt(msg.ID, "did:wba:bob")
		if err != nil {
			t.Fatalf("GetReceipt failed: %v", err)
		}
		if !receipt.IsDelivered() {
			t.Error("expected delivered")
		}
		if receipt.IsRead() {
			t.Error("expected not read yet")
		}

		// Mark read
		if err := store.MarkRead(msg.ID, "did:wba:bob"); err != nil {
			t.Fatalf("MarkRead failed: %v", err)
		}

		receipt, _ = store.GetReceipt(msg.ID, "did:wba:bob")
		if !receipt.IsRead() {
			t.Error("expected read")
		}

		// Get all receipts
		receipts, err := store.GetReceipts(msg.ID)
		if err != nil {
			t.Fatalf("GetReceipts failed: %v", err)
		}
		if len(receipts) != 1 {
			t.Errorf("expected 1 receipt, got %d", len(receipts))
		}

		_ = store.DeleteThread(thread.ID)
	})

	t.Run("Receipt not found", func(t *testing.T) {
		_, err := store.GetReceipt(uuid.Must(uuid.NewV7()), "did:wba:someone")
		if err != ErrReceiptNotFound && err != ErrMessageNotFound {
			t.Errorf("expected ErrReceiptNotFound or ErrMessageNotFound, got %v", err)
		}
	})
}

func TestMemoryStore(t *testing.T) {
	store := NewMemoryStore(DefaultStoreConfig())
	defer func() { _ = store.Close() }()
	testStoreImpl(t, store)
}

func TestMemoryStoreClose(t *testing.T) {
	store := NewMemoryStore(DefaultStoreConfig())
	_ = store.Close()

	_, err := store.GetThread(uuid.Must(uuid.NewV7()))
	if err != ErrStoreClosed {
		t.Errorf("expected ErrStoreClosed after close, got %v", err)
	}
}

func TestSQLiteStore(t *testing.T) {
	store, err := NewSQLiteStore(SQLiteConfig{Path: ":memory:"})
	if err != nil {
		t.Fatalf("failed to create SQLite store: %v", err)
	}
	defer func() { _ = store.Close() }()
	testStoreImpl(t, store)
}

func TestThread(t *testing.T) {
	thread := NewThread("did:wba:alice")

	if !thread.HasParticipant("did:wba:alice") {
		t.Error("should have alice as participant")
	}
	if thread.HasParticipant("did:wba:bob") {
		t.Error("should not have bob as participant")
	}

	added := thread.AddParticipant("did:wba:bob")
	if !added {
		t.Error("should have added bob")
	}
	if !thread.HasParticipant("did:wba:bob") {
		t.Error("should have bob after add")
	}

	added = thread.AddParticipant("did:wba:alice")
	if added {
		t.Error("should not add duplicate alice")
	}
}

func TestMessageReceipt(t *testing.T) {
	receipt := &MessageReceipt{
		MessageID:    uuid.Must(uuid.NewV7()),
		RecipientDID: "did:wba:bob",
	}

	if receipt.IsDelivered() {
		t.Error("should not be delivered initially")
	}
	if receipt.IsRead() {
		t.Error("should not be read initially")
	}

	now := time.Now()
	receipt.DeliveredAt = &now
	if !receipt.IsDelivered() {
		t.Error("should be delivered after setting DeliveredAt")
	}

	receipt.ReadAt = &now
	if !receipt.IsRead() {
		t.Error("should be read after setting ReadAt")
	}
}
