package billing

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/stripe/stripe-go/v82"
)

// findTenantByStripeCustomerID scans all tenants for one with the given Stripe customer ID.
// O(n) over tenant count — acceptable for Phase 1 (< 10k tenants).
func findTenantByStripeCustomerID(store Store, customerID string) (*Tenant, error) {
	tenants, err := store.ListTenants()
	if err != nil {
		return nil, fmt.Errorf("billing: list tenants: %w", err)
	}
	for _, t := range tenants {
		if t.StripeCustomerID == customerID {
			return t, nil
		}
	}
	return nil, ErrTenantNotFound
}

// findTenantByStripeSubscriptionID scans all tenants for one with the given subscription ID.
// O(n) over tenant count — acceptable for Phase 1 (< 10k tenants).
func findTenantByStripeSubscriptionID(store Store, subscriptionID string) (*Tenant, error) {
	tenants, err := store.ListTenants()
	if err != nil {
		return nil, fmt.Errorf("billing: list tenants: %w", err)
	}
	for _, t := range tenants {
		if t.StripeSubscriptionID == subscriptionID {
			return t, nil
		}
	}
	return nil, ErrTenantNotFound
}

// HandleStripeEvent processes a verified Stripe webhook event, updating the store.
// eventStore may be nil; if non-nil, each processed event is appended to the audit log.
// Returns nil on success; returns error if a store update fails.
func HandleStripeEvent(store Store, eventStore EventStore, event stripe.Event) error {
	return HandleStripeEventWithConfig(store, eventStore, nil, event)
}

// HandleStripeEventWithConfig is like HandleStripeEvent but also accepts a StripeConfig
// so that customer.subscription.updated events can resolve the new plan from the price ID.
// cfg may be nil; in that case plan changes are not applied (only BillingStatus is updated).
func HandleStripeEventWithConfig(store Store, eventStore EventStore, cfg *StripeConfig, event stripe.Event) error {
	switch event.Type {
	case "checkout.session.completed":
		return handleCheckoutCompleted(store, eventStore, event)
	case "customer.subscription.updated":
		return handleSubscriptionUpdated(store, eventStore, cfg, event)
	case "customer.subscription.deleted":
		return handleSubscriptionDeleted(store, eventStore, event)
	case "invoice.payment_failed":
		return handleInvoicePaymentFailed(store, eventStore, event)
	}
	return nil
}

func handleCheckoutCompleted(store Store, eventStore EventStore, event stripe.Event) error {
	var sess stripe.CheckoutSession
	if err := json.Unmarshal(event.Data.Raw, &sess); err != nil {
		return fmt.Errorf("billing: unmarshal checkout.session.completed: %w", err)
	}

	tenantID := sess.ClientReferenceID
	if tenantID == "" {
		return errors.New("billing: checkout.session.completed: missing client_reference_id")
	}

	tenant, err := store.GetTenant(tenantID)
	if err != nil {
		return fmt.Errorf("billing: checkout.session.completed: get tenant %s: %w", tenantID, err)
	}

	if sess.Customer != nil && sess.Customer.ID != "" {
		tenant.StripeCustomerID = sess.Customer.ID
	}
	if sess.Subscription != nil && sess.Subscription.ID != "" {
		tenant.StripeSubscriptionID = sess.Subscription.ID
	}
	tenant.BillingStatus = "active"

	if err := store.UpdateTenant(tenant); err != nil {
		return fmt.Errorf("billing: checkout.session.completed: update tenant %s: %w", tenantID, err)
	}

	if eventStore != nil {
		_ = eventStore.RecordEvent(tenantID, string(EventStripeWebhook), string(event.Type), event.ID)
	}
	return nil
}

func handleSubscriptionUpdated(store Store, eventStore EventStore, cfg *StripeConfig, event stripe.Event) error {
	var sub stripe.Subscription
	if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
		return fmt.Errorf("billing: unmarshal customer.subscription.updated: %w", err)
	}

	tenant, err := findTenantByStripeSubscriptionID(store, sub.ID)
	if err != nil {
		// Tenant not found: may be a subscription created before this system; skip silently.
		return nil
	}

	// In Stripe v82, CurrentPeriodEnd is on SubscriptionItem, not Subscription.
	if sub.Items != nil && len(sub.Items.Data) > 0 {
		item := sub.Items.Data[0]
		periodEnd := item.CurrentPeriodEnd
		if periodEnd > 0 {
			t := time.Unix(periodEnd, 0).UTC()
			tenant.CurrentPeriodEnd = &t
		}

		// If a StripeConfig is provided, resolve the new plan from the price ID and
		// update the tenant's Plan and Quota so quota enforcement reflects the change.
		if cfg != nil && item.Price != nil && item.Price.ID != "" {
			if newPlan, ok := cfg.PlanFromPriceID(item.Price.ID); ok && newPlan != tenant.Plan {
				tenant.Plan = newPlan
				tenant.Quota = DefaultQuota(newPlan)
			}
		}
	}

	switch sub.Status {
	case stripe.SubscriptionStatusActive:
		tenant.BillingStatus = "active"
	case stripe.SubscriptionStatusPastDue:
		tenant.BillingStatus = "past_due"
	case stripe.SubscriptionStatusCanceled:
		tenant.BillingStatus = "canceled"
	case stripe.SubscriptionStatusIncomplete:
		tenant.BillingStatus = "incomplete"
	default:
		tenant.BillingStatus = string(sub.Status)
	}

	if err := store.UpdateTenant(tenant); err != nil {
		return fmt.Errorf("billing: subscription.updated: update tenant %s: %w", tenant.ID, err)
	}

	if eventStore != nil {
		_ = eventStore.RecordEvent(tenant.ID, string(EventStripeWebhook), string(event.Type), event.ID)
	}
	return nil
}

func handleSubscriptionDeleted(store Store, eventStore EventStore, event stripe.Event) error {
	var sub stripe.Subscription
	if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
		return fmt.Errorf("billing: unmarshal customer.subscription.deleted: %w", err)
	}

	tenant, err := findTenantByStripeSubscriptionID(store, sub.ID)
	if err != nil {
		return nil // Not found: skip silently.
	}

	tenant.Plan = PlanFree
	tenant.Quota = DefaultQuota(PlanFree)
	tenant.BillingStatus = "canceled"
	tenant.StripeSubscriptionID = ""

	if err := store.UpdateTenant(tenant); err != nil {
		return fmt.Errorf("billing: subscription.deleted: update tenant %s: %w", tenant.ID, err)
	}

	if eventStore != nil {
		_ = eventStore.RecordEvent(tenant.ID, string(EventStripeWebhook), string(event.Type), event.ID)
	}
	return nil
}

func handleInvoicePaymentFailed(store Store, eventStore EventStore, event stripe.Event) error {
	var inv stripe.Invoice
	if err := json.Unmarshal(event.Data.Raw, &inv); err != nil {
		return fmt.Errorf("billing: unmarshal invoice.payment_failed: %w", err)
	}

	customerID := ""
	if inv.Customer != nil {
		customerID = inv.Customer.ID
	}
	if customerID == "" {
		return nil // No customer ID, nothing to do.
	}

	tenant, err := findTenantByStripeCustomerID(store, customerID)
	if err != nil {
		return nil // Not found: skip silently.
	}

	tenant.BillingStatus = "past_due"

	if err := store.UpdateTenant(tenant); err != nil {
		return fmt.Errorf("billing: invoice.payment_failed: update tenant %s: %w", tenant.ID, err)
	}

	if eventStore != nil {
		_ = eventStore.RecordEvent(tenant.ID, string(EventStripeWebhook), string(event.Type), event.ID)
	}
	return nil
}
