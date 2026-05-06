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
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, 4096))
		if err != nil {
			http.Error(w, "failed to read request body", http.StatusBadRequest)
			return
		}
		var req checkoutRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}

		plan, ok := checkoutValidPlans[req.Plan]
		if !ok {
			http.Error(w, "plan must be one of: starter, team", http.StatusBadRequest)
			return
		}
		if req.SuccessURL == "" {
			http.Error(w, "success_url is required", http.StatusBadRequest)
			return
		}
		if req.CancelURL == "" {
			http.Error(w, "cancel_url is required", http.StatusBadRequest)
			return
		}

		tenant := billing.TenantFromContext(r.Context())
		if tenant == nil {
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}

		// Lazily create a Stripe customer if one doesn't exist yet.
		if tenant.StripeCustomerID == "" {
			cust, err := stripeClient.CreateCustomer(tenant.ID, tenant.Email, tenant.Name)
			if err != nil {
				logger.Error("checkout: CreateCustomer failed", "tenant_id", tenant.ID, "error", err)
				http.Error(w, "failed to create Stripe customer", http.StatusInternalServerError)
				return
			}
			tenant.StripeCustomerID = cust.ID
			if err := store.UpdateTenant(tenant); err != nil {
				logger.Error("checkout: UpdateTenant (customer) failed", "tenant_id", tenant.ID, "error", err)
				http.Error(w, "failed to update tenant", http.StatusInternalServerError)
				return
			}
		}

		sess, err := stripeClient.CreateCheckoutSession(tenant.ID, plan, req.SuccessURL, req.CancelURL)
		if err != nil {
			logger.Error("checkout: CreateCheckoutSession failed", "tenant_id", tenant.ID, "plan", plan, "error", err)
			http.Error(w, "failed to create checkout session", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(checkoutResponse{URL: sess.URL})
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
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, 4096))
		if err != nil {
			http.Error(w, "failed to read request body", http.StatusBadRequest)
			return
		}
		var req portalRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.ReturnURL == "" {
			http.Error(w, "return_url is required", http.StatusBadRequest)
			return
		}

		tenant := billing.TenantFromContext(r.Context())
		if tenant == nil {
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		if tenant.StripeCustomerID == "" {
			http.Error(w, "no Stripe customer associated with this account", http.StatusBadRequest)
			return
		}

		sess, err := stripeClient.CreateBillingPortalSession(tenant.StripeCustomerID, req.ReturnURL)
		if err != nil {
			logger.Error("portal: CreateBillingPortalSession failed", "tenant_id", tenant.ID, "error", err)
			http.Error(w, "failed to create billing portal session", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(portalResponse{URL: sess.URL})
	}
}
