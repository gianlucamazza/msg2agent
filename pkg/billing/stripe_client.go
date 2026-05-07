package billing

import (
	"fmt"
	"os"
	"time"

	"github.com/stripe/stripe-go/v82"
	portalsession "github.com/stripe/stripe-go/v82/billingportal/session"
	checkoutsession "github.com/stripe/stripe-go/v82/checkout/session"
	"github.com/stripe/stripe-go/v82/customer"
	stripesubscription "github.com/stripe/stripe-go/v82/subscription"
	"github.com/stripe/stripe-go/v82/webhook"
)

// StripeConfig configures the Stripe client.
type StripeConfig struct {
	SecretKey     string
	WebhookSecret string
	PriceIDs      map[Plan]string // plan → Stripe price ID
}

// StripeConfigFromEnv builds StripeConfig from environment variables.
// Returns nil if STRIPE_SECRET_KEY is not set.
func StripeConfigFromEnv() *StripeConfig {
	key := os.Getenv("STRIPE_SECRET_KEY")
	if key == "" {
		return nil
	}
	return &StripeConfig{
		SecretKey:     key,
		WebhookSecret: os.Getenv("STRIPE_WEBHOOK_SECRET"),
		PriceIDs: map[Plan]string{
			PlanFree:       os.Getenv("STRIPE_PRICE_FREE"),
			PlanStarter:    os.Getenv("STRIPE_PRICE_STARTER"),
			PlanTeam:       os.Getenv("STRIPE_PRICE_TEAM"),
			PlanEnterprise: os.Getenv("STRIPE_PRICE_ENTERPRISE"),
		},
	}
}

// PlanFromPriceID returns the Plan for the given Stripe price ID, doing a reverse lookup
// in cfg.PriceIDs. Returns false if the price ID is not configured.
func (cfg *StripeConfig) PlanFromPriceID(priceID string) (Plan, bool) {
	for plan, id := range cfg.PriceIDs {
		if id != "" && id == priceID {
			return plan, true
		}
	}
	return "", false
}

// StripeClient wraps the Stripe SDK for billing operations.
type StripeClient struct {
	cfg StripeConfig
}

// Config returns the StripeConfig used by this client.
func (c *StripeClient) Config() *StripeConfig { return &c.cfg }

// NewStripeClient creates a new StripeClient and sets the Stripe API key.
func NewStripeClient(cfg StripeConfig) *StripeClient {
	stripe.Key = cfg.SecretKey
	return &StripeClient{cfg: cfg}
}

// CreateCustomer creates a Stripe customer for the given tenant.
func (c *StripeClient) CreateCustomer(tenantID, email, name string) (*stripe.Customer, error) {
	params := &stripe.CustomerParams{
		Email: stripe.String(email),
		Name:  stripe.String(name),
	}
	params.AddMetadata("tenant_id", tenantID)
	cust, err := customer.New(params)
	if err != nil {
		return nil, fmt.Errorf("stripe: create customer for tenant %s: %w", tenantID, err)
	}
	return cust, nil
}

// CreateCheckoutSession creates a hosted checkout session for plan upgrade.
// successURL and cancelURL are redirect targets after checkout.
func (c *StripeClient) CreateCheckoutSession(tenantID string, plan Plan, successURL, cancelURL string) (*stripe.CheckoutSession, error) {
	priceID, ok := c.cfg.PriceIDs[plan]
	if !ok || priceID == "" {
		return nil, fmt.Errorf("stripe: price ID not configured for plan %s", plan)
	}
	params := &stripe.CheckoutSessionParams{
		Mode:              stripe.String("subscription"),
		ClientReferenceID: stripe.String(tenantID),
		SuccessURL:        stripe.String(successURL),
		CancelURL:         stripe.String(cancelURL),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				Price:    stripe.String(priceID),
				Quantity: stripe.Int64(1),
			},
		},
	}
	sess, err := checkoutsession.New(params)
	if err != nil {
		return nil, fmt.Errorf("stripe: create checkout session for tenant %s plan %s: %w", tenantID, plan, err)
	}
	return sess, nil
}

// CreateBillingPortalSession creates a customer portal session for subscription management.
func (c *StripeClient) CreateBillingPortalSession(customerID, returnURL string) (*stripe.BillingPortalSession, error) {
	params := &stripe.BillingPortalSessionParams{
		Customer:  stripe.String(customerID),
		ReturnURL: stripe.String(returnURL),
	}
	sess, err := portalsession.New(params)
	if err != nil {
		return nil, fmt.Errorf("stripe: create billing portal session for customer %s: %w", customerID, err)
	}
	return sess, nil
}

// VerifyWebhookSignature validates a Stripe webhook payload and returns the event.
func (c *StripeClient) VerifyWebhookSignature(payload []byte, sigHeader string) (stripe.Event, error) {
	event, err := webhook.ConstructEvent(payload, sigHeader, c.cfg.WebhookSecret)
	if err != nil {
		return stripe.Event{}, fmt.Errorf("stripe: webhook signature verification failed: %w", err)
	}
	return event, nil
}

// GetSubscription fetches a Stripe subscription by ID and returns a StripeSubscriptionState.
// This implements StripeReconcilerClient.
func (c *StripeClient) GetSubscription(subscriptionID string) (*StripeSubscriptionState, error) {
	sub, err := stripesubscription.Get(subscriptionID, nil)
	if err != nil {
		return nil, fmt.Errorf("stripe: get subscription %s: %w", subscriptionID, err)
	}

	state := &StripeSubscriptionState{
		Status: string(sub.Status),
	}
	// In Stripe v82, CurrentPeriodEnd lives on SubscriptionItem, not Subscription.
	if sub.Items != nil && len(sub.Items.Data) > 0 {
		if pe := sub.Items.Data[0].CurrentPeriodEnd; pe > 0 {
			t := time.Unix(pe, 0).UTC()
			state.CurrentPeriodEnd = &t
		}
	}
	return state, nil
}
