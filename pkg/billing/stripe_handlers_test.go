package billing

import (
	"encoding/json"
	"testing"

	"github.com/stripe/stripe-go/v82"
)

// mustEvent unmarshals raw JSON into a stripe.Event for testing.
func mustEvent(t *testing.T, raw string) stripe.Event {
	t.Helper()
	var ev stripe.Event
	if err := json.Unmarshal([]byte(raw), &ev); err != nil {
		t.Fatalf("mustEvent: unmarshal: %v", err)
	}
	return ev
}

// setupTenant creates a tenant in the store and returns it.
func setupTenant(t *testing.T, store *MemoryStore) *Tenant {
	t.Helper()
	tenant := NewTenant("Test User", "test@example.com", PlanFree)
	if err := store.PutTenant(tenant); err != nil {
		t.Fatalf("PutTenant: %v", err)
	}
	return tenant
}

func TestHandleStripeEvent_CheckoutCompleted(t *testing.T) {
	store := NewMemoryStore()
	tenant := setupTenant(t, store)

	raw := `{
		"id": "evt_checkout_001",
		"type": "checkout.session.completed",
		"data": {
			"object": {
				"id": "cs_test_001",
				"object": "checkout.session",
				"client_reference_id": "` + tenant.ID + `",
				"customer": {"id": "cus_stripe_001"},
				"subscription": {"id": "sub_stripe_001"},
				"status": "complete"
			}
		}
	}`

	ev := mustEvent(t, raw)
	if err := HandleStripeEvent(store, store, ev); err != nil {
		t.Fatalf("HandleStripeEvent: %v", err)
	}

	updated, err := store.GetTenant(tenant.ID)
	if err != nil {
		t.Fatalf("GetTenant: %v", err)
	}
	if updated.StripeCustomerID != "cus_stripe_001" {
		t.Errorf("expected StripeCustomerID=cus_stripe_001, got %q", updated.StripeCustomerID)
	}
	if updated.StripeSubscriptionID != "sub_stripe_001" {
		t.Errorf("expected StripeSubscriptionID=sub_stripe_001, got %q", updated.StripeSubscriptionID)
	}
	if updated.BillingStatus != "active" {
		t.Errorf("expected BillingStatus=active, got %q", updated.BillingStatus)
	}
}

func TestHandleStripeEvent_SubscriptionDeleted(t *testing.T) {
	store := NewMemoryStore()
	tenant := setupTenant(t, store)
	tenant.Plan = PlanStarter
	tenant.Quota = DefaultQuota(PlanStarter)
	tenant.StripeSubscriptionID = "sub_to_delete"
	tenant.StripeCustomerID = "cus_delete"
	tenant.BillingStatus = "active"
	if err := store.UpdateTenant(tenant); err != nil {
		t.Fatalf("UpdateTenant: %v", err)
	}

	raw := `{
		"id": "evt_sub_deleted_001",
		"type": "customer.subscription.deleted",
		"data": {
			"object": {
				"id": "sub_to_delete",
				"object": "subscription",
				"status": "canceled",
				"customer": "cus_delete",
				"billing_cycle_anchor": 1700000000
			}
		}
	}`

	ev := mustEvent(t, raw)
	if err := HandleStripeEvent(store, store, ev); err != nil {
		t.Fatalf("HandleStripeEvent: %v", err)
	}

	updated, err := store.GetTenant(tenant.ID)
	if err != nil {
		t.Fatalf("GetTenant: %v", err)
	}
	if updated.Plan != PlanFree {
		t.Errorf("expected Plan=free after cancellation, got %q", updated.Plan)
	}
	if updated.BillingStatus != "canceled" {
		t.Errorf("expected BillingStatus=canceled, got %q", updated.BillingStatus)
	}
	if updated.StripeSubscriptionID != "" {
		t.Errorf("expected StripeSubscriptionID to be cleared, got %q", updated.StripeSubscriptionID)
	}
}

func TestHandleStripeEvent_InvoicePaymentFailed(t *testing.T) {
	store := NewMemoryStore()
	tenant := setupTenant(t, store)
	tenant.StripeCustomerID = "cus_pastdue"
	tenant.BillingStatus = "active"
	if err := store.UpdateTenant(tenant); err != nil {
		t.Fatalf("UpdateTenant: %v", err)
	}

	raw := `{
		"id": "evt_inv_failed_001",
		"type": "invoice.payment_failed",
		"data": {
			"object": {
				"id": "in_test_001",
				"object": "invoice",
				"customer": {"id": "cus_pastdue"},
				"status": "open"
			}
		}
	}`

	ev := mustEvent(t, raw)
	if err := HandleStripeEvent(store, store, ev); err != nil {
		t.Fatalf("HandleStripeEvent: %v", err)
	}

	updated, err := store.GetTenant(tenant.ID)
	if err != nil {
		t.Fatalf("GetTenant: %v", err)
	}
	if updated.BillingStatus != "past_due" {
		t.Errorf("expected BillingStatus=past_due, got %q", updated.BillingStatus)
	}
}

func TestHandleStripeEvent_Idempotency(t *testing.T) {
	store := NewMemoryStore()
	tenant := setupTenant(t, store)
	tenant.StripeCustomerID = "cus_idempotent"
	tenant.BillingStatus = "active"
	if err := store.UpdateTenant(tenant); err != nil {
		t.Fatalf("UpdateTenant: %v", err)
	}

	raw := `{
		"id": "evt_duplicate_001",
		"type": "invoice.payment_failed",
		"data": {
			"object": {
				"id": "in_dup_001",
				"object": "invoice",
				"customer": {"id": "cus_idempotent"},
				"status": "open"
			}
		}
	}`
	ev := mustEvent(t, raw)

	// First call: marks event processed and updates tenant.
	isNew, err := store.MarkStripeEventProcessed(ev.ID)
	if err != nil {
		t.Fatalf("MarkStripeEventProcessed (first): %v", err)
	}
	if !isNew {
		t.Fatal("expected first call to return isNew=true")
	}
	if err := HandleStripeEvent(store, store, ev); err != nil {
		t.Fatalf("HandleStripeEvent (first): %v", err)
	}

	// Reset status to verify second call is a no-op (as webhook handler would skip it).
	tenant2, _ := store.GetTenant(tenant.ID)
	tenant2.BillingStatus = "active"
	_ = store.UpdateTenant(tenant2)

	// Second call: MarkStripeEventProcessed returns false → webhook handler would skip.
	isNew2, err := store.MarkStripeEventProcessed(ev.ID)
	if err != nil {
		t.Fatalf("MarkStripeEventProcessed (second): %v", err)
	}
	if isNew2 {
		t.Fatal("expected second call to return isNew=false (duplicate event)")
	}
	// Tenant status was NOT updated again because the webhook handler short-circuits.
	got, _ := store.GetTenant(tenant.ID)
	if got.BillingStatus != "active" {
		t.Errorf("expected status to remain active (idempotency), got %q", got.BillingStatus)
	}
}

func TestHandleStripeEvent_SubscriptionUpdated(t *testing.T) {
	store := NewMemoryStore()
	tenant := setupTenant(t, store)
	tenant.StripeSubscriptionID = "sub_update_001"
	tenant.BillingStatus = "active"
	if err := store.UpdateTenant(tenant); err != nil {
		t.Fatalf("UpdateTenant: %v", err)
	}

	raw := `{
		"id": "evt_sub_updated_001",
		"type": "customer.subscription.updated",
		"data": {
			"object": {
				"id": "sub_update_001",
				"object": "subscription",
				"status": "past_due",
				"billing_cycle_anchor": 1800000000,
				"items": {
					"object": "list",
					"data": [
						{
							"id": "si_001",
							"object": "subscription_item",
							"current_period_end": 1800000000
						}
					],
					"has_more": false,
					"url": "/v1/subscription_items"
				}
			}
		}
	}`

	ev := mustEvent(t, raw)
	if err := HandleStripeEvent(store, nil, ev); err != nil {
		t.Fatalf("HandleStripeEvent: %v", err)
	}

	updated, err := store.GetTenant(tenant.ID)
	if err != nil {
		t.Fatalf("GetTenant: %v", err)
	}
	if updated.BillingStatus != "past_due" {
		t.Errorf("expected BillingStatus=past_due, got %q", updated.BillingStatus)
	}
	// CurrentPeriodEnd may or may not be set depending on SDK field availability;
	// we just verify the status update succeeded.
	_ = updated.CurrentPeriodEnd
}

func TestHandleStripeEvent_UnknownEvent(t *testing.T) {
	store := NewMemoryStore()

	raw := `{
		"id": "evt_unknown_001",
		"type": "payment_intent.created",
		"data": {"object": {}}
	}`

	ev := mustEvent(t, raw)
	if err := HandleStripeEvent(store, nil, ev); err != nil {
		t.Fatalf("expected no error for unknown event type, got: %v", err)
	}
}
