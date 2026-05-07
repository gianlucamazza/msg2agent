package main

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gianlucamazza/msg2agent/pkg/billing"
	"github.com/gianlucamazza/msg2agent/pkg/oauth"
)

// verifyEmailHandler handles GET /oauth/verify?token=<plain>.
// It consumes the one-time token, marks the tenant's email as verified, and
// redirects to the dashboard. dashBaseURL should be the dashboard origin
// (e.g. "https://app.msg2agent.xyz").
func verifyEmailHandler(store billing.Store, relayBaseURL string, logger *slog.Logger) http.HandlerFunc {
	// Derive the dashboard URL from the relay base URL by inserting "app." subdomain.
	dashURL := relayBaseURL
	if idx := strings.Index(dashURL, "://"); idx >= 0 {
		dashURL = dashURL[:idx+3] + "app." + dashURL[idx+3:]
	}

	return func(w http.ResponseWriter, r *http.Request) {
		plain := r.URL.Query().Get("token")
		if plain == "" {
			http.Error(w, "missing token", http.StatusBadRequest)
			return
		}

		hash := oauth.HashToken(plain)
		tenantID, _, err := store.ConsumeEmailVerificationToken(hash)
		if err != nil {
			http.Error(w, "Verification link expired or already used. Sign in and request a new link.", http.StatusBadRequest)
			return
		}

		if err := store.MarkTenantEmailVerified(tenantID, time.Now().UTC()); err != nil {
			logger.Warn("verify: MarkTenantEmailVerified failed", "tenant", tenantID, "error", err)
			http.Error(w, "Internal error. Please try again.", http.StatusInternalServerError)
			return
		}

		logger.Info("email verified", "tenant", tenantID)
		http.Redirect(w, r, dashURL+"/app/?verified=1", http.StatusFound)
	}
}
