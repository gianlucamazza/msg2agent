package billing

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

// silentLogger returns a no-op logger suitable for tests.
func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func testNotifyEvent() NotifyEvent {
	return NotifyEvent{
		TenantID:  "t_test",
		Plan:      "starter",
		Period:    "2026-05",
		Event:     EventMessage,
		EventType: "quota_warning",
		Current:   800,
		Limit:     1000,
		Ratio:     0.8,
		Timestamp: time.Now().UTC(),
	}
}

// TestWebhookNotifier_200Success verifies that a 2xx response is counted as
// success and no retry occurs.
func TestWebhookNotifier_200Success(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := &WebhookNotifier{URL: srv.URL, Client: srv.Client(), Logger: silentLogger()}
	if err := n.Notify(context.Background(), testNotifyEvent()); err != nil {
		t.Fatalf("Notify: unexpected error: %v", err)
	}
	if calls.Load() != 1 {
		t.Errorf("expected 1 HTTP call, got %d", calls.Load())
	}
}

// TestWebhookNotifier_400NoRetry verifies that a 4xx response stops retrying
// immediately and returns an error.
func TestWebhookNotifier_400NoRetry(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	n := &WebhookNotifier{URL: srv.URL, Client: srv.Client(), Logger: silentLogger()}
	err := n.Notify(context.Background(), testNotifyEvent())
	if err == nil {
		t.Fatal("expected error on 400, got nil")
	}
	if calls.Load() != 1 {
		t.Errorf("expected exactly 1 attempt on 4xx, got %d", calls.Load())
	}
}

// TestWebhookNotifier_5xxRetries verifies that a 5xx response triggers up to
// maxWebhookAttempts attempts then returns an error.
// We shorten back-off by using a custom client with a very short timeout.
func TestWebhookNotifier_5xxRetries(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	// Override sleep via the real notifier — back-off is built-in but tests
	// accept the short 1s+2s delay. Use a very short context timeout instead.
	// Actually, we just allow the test to run with real back-off but verify the
	// call count; the total wait is 1s+2s = 3s which is acceptable for a test.
	// If you want a faster test, set a deadline on the context.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	n := &WebhookNotifier{URL: srv.URL, Client: srv.Client(), Logger: silentLogger()}
	err := n.Notify(ctx, testNotifyEvent())
	if err == nil {
		t.Fatal("expected error after all retries, got nil")
	}
	if calls.Load() != int32(maxWebhookAttempts) {
		t.Errorf("expected %d attempts, got %d", maxWebhookAttempts, calls.Load())
	}
}

// TestNewWebhookNotifierFromEnv_NotSet verifies that when BILLING_WEBHOOK_URL is
// not set, NewWebhookNotifierFromEnv returns nil.
func TestNewWebhookNotifierFromEnv_NotSet(t *testing.T) {
	t.Setenv("BILLING_WEBHOOK_URL", "")
	n := NewWebhookNotifierFromEnv(silentLogger())
	if n != nil {
		t.Errorf("expected nil notifier when env var is unset, got %+v", n)
	}
}

// TestNewWebhookNotifierFromEnv_Set verifies that setting BILLING_WEBHOOK_URL
// causes NewWebhookNotifierFromEnv to return a non-nil notifier.
func TestNewWebhookNotifierFromEnv_Set(t *testing.T) {
	t.Setenv("BILLING_WEBHOOK_URL", "http://localhost:9999/hook")
	n := NewWebhookNotifierFromEnv(silentLogger())
	if n == nil {
		t.Fatal("expected non-nil notifier when BILLING_WEBHOOK_URL is set")
	}
	if n.URL != "http://localhost:9999/hook" {
		t.Errorf("URL = %q, want %q", n.URL, "http://localhost:9999/hook")
	}
}

// TestNotifierState_ShouldNotify verifies threshold detection.
func TestNotifierState_ShouldNotify(t *testing.T) {
	ns := newNotifierState()
	k := counterKey{tenantID: "t1", period: "2026-05", event: EventMessage}

	// 50 % — below warning threshold → no notification.
	if evt, should := ns.shouldNotify(k, 0.5); should {
		t.Errorf("shouldNotify(0.5) = true (%s), want false", evt)
	}

	// At or above warning threshold (0.8) → quota_warning.
	evt, should := ns.shouldNotify(k, 0.85)
	if !should {
		t.Error("shouldNotify(0.85) = false, want true")
	}
	if evt != "quota_warning" {
		t.Errorf("event = %q, want %q", evt, "quota_warning")
	}

	// Same ratio again → no duplicate notification.
	if _, should := ns.shouldNotify(k, 0.85); should {
		t.Error("shouldNotify(0.85) second call = true, want false (dedup)")
	}

	// Above 1.0 → quota_exceeded.
	evt2, should2 := ns.shouldNotify(k, 1.0)
	if !should2 {
		t.Error("shouldNotify(1.0) = false, want true")
	}
	if evt2 != "quota_exceeded" {
		t.Errorf("event = %q, want %q", evt2, "quota_exceeded")
	}
}
