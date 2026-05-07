package main

import (
	"io"
	"log/slog"
	"net/http"

	"github.com/gianlucamazza/msg2agent/pkg/billing"
)

// stripeWebhookHandler handles POST /api/billing/webhook.
// No auth middleware — Stripe calls this directly; the signature is verified cryptographically.
func stripeWebhookHandler(store billing.Store, stripeClient *billing.StripeClient, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Limit body to 1 MB (Stripe events are typically much smaller).
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, "failed to read request body", http.StatusBadRequest)
			return
		}

		// Verify the Stripe webhook signature.
		event, err := stripeClient.VerifyWebhookSignature(body, r.Header.Get("Stripe-Signature"))
		if err != nil {
			logger.Warn("stripe webhook: signature verification failed", "error", err)
			http.Error(w, "invalid webhook signature", http.StatusBadRequest)
			return
		}

		// Idempotency check: skip already-processed events.
		isNew, err := store.MarkStripeEventProcessed(event.ID)
		if err != nil {
			logger.Error("stripe webhook: MarkStripeEventProcessed failed", "event_id", event.ID, "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if !isNew {
			// Already processed — return 200 OK (Stripe expects 2xx for success).
			w.WriteHeader(http.StatusOK)
			return
		}

		// Dispatch to business logic. EventStore is optional; SQLiteStore implements it
		// but MemoryStore also does (no-op). We always pass store as EventStore since
		// both store implementations satisfy EventStore in production.
		var eventStore billing.EventStore
		if es, ok := store.(billing.EventStore); ok {
			eventStore = es
		}

		if err := billing.HandleStripeEventWithConfig(store, eventStore, stripeClient.Config(), event); err != nil {
			logger.Error("stripe webhook: HandleStripeEvent failed",
				"event_id", event.ID, "event_type", event.Type, "error", err)
			// Return 200 to prevent Stripe from retrying events that fail due to our
			// internal errors (e.g. tenant not found). Stripe retries on 4xx/5xx.
			// For idempotency-safe errors we prefer to log and move on.
			w.WriteHeader(http.StatusOK)
			return
		}

		w.WriteHeader(http.StatusOK)
	}
}
