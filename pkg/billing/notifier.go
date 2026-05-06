package billing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

// NotifyEvent is the payload POSTed to the webhook URL on quota transitions.
type NotifyEvent struct {
	TenantID  string     `json:"tenant_id"`
	Plan      string     `json:"plan"`
	Period    string     `json:"period"`
	Event     UsageEvent `json:"event"`
	EventType string     `json:"event_type"` // "quota_warning" | "quota_exceeded"
	Current   int64      `json:"current"`
	Limit     int64      `json:"limit"`
	Ratio     float64    `json:"ratio"`
	Timestamp time.Time  `json:"timestamp"`
}

// Notifier is called when a tenant's quota crosses a threshold.
type Notifier interface {
	Notify(ctx context.Context, ev NotifyEvent) error
}

// WebhookNotifier POSTs NotifyEvent JSON to a configurable URL (best-effort).
type WebhookNotifier struct {
	URL    string
	Client *http.Client
	Logger *slog.Logger
}

// NewWebhookNotifierFromEnv reads BILLING_WEBHOOK_URL and returns a notifier,
// or nil if the env var is not set.
func NewWebhookNotifierFromEnv(logger *slog.Logger) *WebhookNotifier {
	url := os.Getenv("BILLING_WEBHOOK_URL")
	if url == "" {
		return nil
	}
	return &WebhookNotifier{
		URL:    url,
		Client: &http.Client{Timeout: 5 * time.Second},
		Logger: logger,
	}
}

// maxWebhookAttempts is the total number of attempts (1 initial + 2 retries).
const maxWebhookAttempts = 3

// webhookPerAttemptTimeout is the HTTP client timeout for each attempt.
const webhookPerAttemptTimeout = 3 * time.Second

// Notify sends ev to the webhook URL. On 5xx responses it retries up to
// maxWebhookAttempts times total with exponential backoff (1s, 2s between).
// On 4xx responses it stops immediately (client error, no retry).
// On 2xx the delivery is counted as a success.
func (n *WebhookNotifier) Notify(ctx context.Context, ev NotifyEvent) error {
	body, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("billing: notify marshal: %w", err)
	}

	client := &http.Client{Timeout: webhookPerAttemptTimeout}

	backoff := []time.Duration{1 * time.Second, 2 * time.Second}
	var lastErr error

	for attempt := range maxWebhookAttempts {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.URL, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("billing: notify request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			// Transport-level failure — retry.
			lastErr = err
			if n.Logger != nil {
				n.Logger.Warn("billing: webhook transport error", "attempt", attempt+1, "error", err)
			}
		} else {
			statusCode := resp.StatusCode
			_ = resp.Body.Close()

			switch {
			case statusCode >= 200 && statusCode < 300:
				RecordWebhookDelivery("success")
				return nil
			case statusCode >= 400 && statusCode < 500:
				// Client error — do not retry.
				err := fmt.Errorf("billing: webhook 4xx response: %d", statusCode)
				RecordWebhookDelivery("failure")
				return err
			default:
				// 5xx or unexpected — retry.
				lastErr = fmt.Errorf("billing: webhook %d response", statusCode)
				if n.Logger != nil {
					n.Logger.Warn("billing: webhook server error", "attempt", attempt+1, "status", statusCode)
				}
			}
		}

		// Sleep before next retry (skip sleep after last attempt).
		if attempt < maxWebhookAttempts-1 {
			time.Sleep(backoff[attempt])
		}
	}

	RecordWebhookDelivery("failure")
	RecordWebhookDropped()
	return fmt.Errorf("billing: notify after retries: %w", lastErr)
}

// notifierState tracks the last ratio at which a notification was sent per counter key,
// preventing spam when multiple requests arrive simultaneously near a threshold.
type notifierState struct {
	mu            sync.Mutex
	lastNotified  map[counterKey]float64
	warnThreshold float64
}

func newNotifierState() *notifierState {
	threshold := 0.8
	if v := os.Getenv("BILLING_QUOTA_WARN_RATIO"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 && f < 1 {
			threshold = f
		}
	}
	return &notifierState{
		lastNotified:  make(map[counterKey]float64),
		warnThreshold: threshold,
	}
}

// shouldNotify returns (eventType, true) if ratio crosses a threshold that hasn't
// been notified yet for this counter key.
func (ns *notifierState) shouldNotify(k counterKey, ratio float64) (string, bool) {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	last := ns.lastNotified[k]
	switch {
	case ratio >= 1.0 && last < 1.0:
		ns.lastNotified[k] = ratio
		return "quota_exceeded", true
	case ratio >= ns.warnThreshold && ratio < 1.0 && last < ns.warnThreshold:
		ns.lastNotified[k] = ratio
		return "quota_warning", true
	}
	return "", false
}
