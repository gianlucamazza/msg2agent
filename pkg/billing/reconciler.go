package billing

import (
	"context"
	"log/slog"
	"time"
)

// StripeSubscriptionState is the subset of Stripe subscription data used for reconciliation.
type StripeSubscriptionState struct {
	Status           string
	CurrentPeriodEnd *time.Time
}

// StripeReconcilerClient is a minimal interface for fetching subscription state from Stripe.
// *StripeClient satisfies this interface.
type StripeReconcilerClient interface {
	GetSubscription(subscriptionID string) (*StripeSubscriptionState, error)
}

// StartStripeReconciler launches a goroutine that periodically fetches subscription
// state from Stripe and reconciles it against the local tenant store.
// interval == 0 disables the reconciler.
// stripeClient may be nil; if nil, this is a no-op.
func StartStripeReconciler(ctx context.Context, store Store, stripeClient StripeReconcilerClient, interval time.Duration, autoFix bool, logger *slog.Logger) {
	if interval == 0 || stripeClient == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				runReconciliation(ctx, store, stripeClient, autoFix, logger)
			}
		}
	}()
}

// runReconciliation checks all tenants with a Stripe subscription and reconciles local state.
func runReconciliation(_ context.Context, store Store, stripeClient StripeReconcilerClient, autoFix bool, logger *slog.Logger) {
	tenants, err := store.ListTenants()
	if err != nil {
		logger.Error("stripe reconciler: list tenants failed", "error", err)
		return
	}

	for _, t := range tenants {
		if t.StripeSubscriptionID == "" {
			continue
		}
		state, err := stripeClient.GetSubscription(t.StripeSubscriptionID)
		if err != nil {
			logger.Warn("stripe reconciler: fetch subscription failed",
				"tenant_id", t.ID,
				"subscription_id", t.StripeSubscriptionID,
				"error", err)
			continue
		}

		diverged := false
		if t.BillingStatus != state.Status {
			logger.Warn("stripe reconciler: billing_status divergence",
				"tenant_id", t.ID,
				"local", t.BillingStatus,
				"stripe", state.Status)
			diverged = true
		}

		if autoFix && diverged {
			t.BillingStatus = state.Status
			t.CurrentPeriodEnd = state.CurrentPeriodEnd
			if err := store.UpdateTenant(t); err != nil {
				logger.Error("stripe reconciler: update tenant failed",
					"tenant_id", t.ID,
					"error", err)
			} else {
				logger.Info("stripe reconciler: tenant reconciled",
					"tenant_id", t.ID,
					"billing_status", state.Status)
			}
		}
	}
}
