package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/gianlucamazza/msg2agent/pkg/billing"
)

type checkoutRequest struct {
	Plan       string `json:"plan"`
	SuccessURL string `json:"success_url"`
	CancelURL  string `json:"cancel_url"`
}

type checkoutResponse struct {
	URL string `json:"url"`
}

// checkoutValidPlans lists plans available for self-service checkout.
// Free and Enterprise are excluded: Free has no payment, Enterprise requires custom pricing.
var checkoutValidPlans = map[string]billing.Plan{
	"starter": billing.PlanStarter,
	"team":    billing.PlanTeam,
}

// checkoutHandler handles POST /api/billing/checkout.
// Requires an authenticated tenant (via APIKeyMiddleware).
// Body: {"plan": "starter|team", "success_url": "...", "cancel_url": "..."}
// Response: {"url": "https://checkout.stripe.com/..."}
func checkoutHandler(store billing.Store, stripeClient *billing.StripeClient, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, 4096))
		if err != nil {
			writeError(w, http.StatusBadRequest, "failed to read request body")
			return
		}
		var req checkoutRequest
		if err := json.Unmarshal(body, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}

		plan, ok := checkoutValidPlans[req.Plan]
		if !ok {
			writeError(w, http.StatusBadRequest, "plan must be one of: starter, team")
			return
		}
		if req.SuccessURL == "" {
			writeError(w, http.StatusBadRequest, "success_url is required")
			return
		}
		if req.CancelURL == "" {
			writeError(w, http.StatusBadRequest, "cancel_url is required")
			return
		}

		tenant := billing.TenantFromContext(r.Context())
		if tenant == nil {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}

		// Lazily create a Stripe customer if one doesn't exist yet.
		if tenant.StripeCustomerID == "" {
			cust, err := stripeClient.CreateCustomer(tenant.ID, tenant.Email, tenant.Name)
			if err != nil {
				logger.Error("checkout: CreateCustomer failed", "tenant_id", tenant.ID, "error", err)
				writeError(w, http.StatusInternalServerError, "failed to create Stripe customer")
				return
			}
			tenant.StripeCustomerID = cust.ID
			if err := store.UpdateTenant(tenant); err != nil {
				logger.Error("checkout: UpdateTenant (customer) failed", "tenant_id", tenant.ID, "error", err)
				writeError(w, http.StatusInternalServerError, "failed to update tenant")
				return
			}
		}

		sess, err := stripeClient.CreateCheckoutSession(tenant.ID, plan, req.SuccessURL, req.CancelURL)
		if err != nil {
			logger.Error("checkout: CreateCheckoutSession failed", "tenant_id", tenant.ID, "plan", plan, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to create checkout session")
			return
		}

		writeRelayJSON(w, http.StatusOK, checkoutResponse{URL: sess.URL})
	}
}

type portalRequest struct {
	ReturnURL string `json:"return_url"`
}

type portalResponse struct {
	URL string `json:"url"`
}

// portalHandler handles POST /api/billing/portal.
// Requires an authenticated tenant with an existing Stripe customer.
// Body: {"return_url": "..."}
// Response: {"url": "https://billing.stripe.com/..."}
func portalHandler(store billing.Store, stripeClient *billing.StripeClient, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, 4096))
		if err != nil {
			writeError(w, http.StatusBadRequest, "failed to read request body")
			return
		}
		var req portalRequest
		if err := json.Unmarshal(body, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		if req.ReturnURL == "" {
			writeError(w, http.StatusBadRequest, "return_url is required")
			return
		}

		tenant := billing.TenantFromContext(r.Context())
		if tenant == nil {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		if tenant.StripeCustomerID == "" {
			writeError(w, http.StatusBadRequest, "no Stripe customer associated with this account")
			return
		}

		sess, err := stripeClient.CreateBillingPortalSession(tenant.StripeCustomerID, req.ReturnURL)
		if err != nil {
			logger.Error("portal: CreateBillingPortalSession failed", "tenant_id", tenant.ID, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to create billing portal session")
			return
		}

		writeRelayJSON(w, http.StatusOK, portalResponse{URL: sess.URL})
	}
}
