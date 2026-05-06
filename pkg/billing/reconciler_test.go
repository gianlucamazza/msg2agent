package billing

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

// TestStartStripeReconciler_NoOp verifies that passing interval=0 or a nil client
// does not panic and returns immediately without starting a goroutine.
func TestStartStripeReconciler_NoOp(t *testing.T) {
	store := NewMemoryStore()
	logger := slog.Default()

	// interval == 0: should be a no-op
	StartStripeReconciler(context.Background(), store, nil, 0, false, logger)

	// stripeClient == nil: should be a no-op
	StartStripeReconciler(context.Background(), store, nil, time.Hour, false, logger)

	// Both conditions met: no panic
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediately cancelled
	StartStripeReconciler(ctx, store, nil, 0, true, logger)
}
