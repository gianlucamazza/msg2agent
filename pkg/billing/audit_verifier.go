package billing

import (
	"context"
	"log/slog"
	"time"
)

// StartPeriodicVerifier launches a goroutine that verifies the audit hash chain
// for all active tenants at the given interval. When tampering is detected,
// it logs an error and increments the billing_audit_chain_tampered_total metric.
// Returns immediately; the goroutine is stopped when ctx is canceled.
// interval == 0 disables the verifier (no-op).
func StartPeriodicVerifier(ctx context.Context, store Store, admin AdminStore, interval time.Duration, logger *slog.Logger) {
	if interval == 0 {
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
				runVerification(ctx, store, admin, logger)
			}
		}
	}()
}

func runVerification(ctx context.Context, store Store, admin AdminStore, logger *slog.Logger) {
	tenants, err := store.ListTenants()
	if err != nil {
		logger.Error("audit verifier: failed to list tenants", "error", err)
		return
	}
	for _, t := range tenants {
		if ctx.Err() != nil {
			return
		}
		results, err := admin.VerifyAuditChain(t.ID)
		if err != nil {
			logger.Error("audit verifier: VerifyAuditChain failed", "tenant_id", t.ID, "error", err)
			continue
		}
		for _, r := range results {
			if r.Tampered {
				logger.Error("audit verifier: chain tampering detected",
					"tenant_id", t.ID,
					"first_bad_id", r.FirstBadID,
					"first_bad_time", r.FirstBadTime)
				RecordAuditChainTampered(t.ID)
			}
		}
	}
}
