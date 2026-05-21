// Package billing provides tenant management, API key issuance, and usage metering.
package billing

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
)

var (
	quotaOverrideOnce  sync.Once
	quotaOverrides     map[string]QuotaConfig // plan name → override
	quotaOverrideError error
)

// loadQuotaOverrides reads BILLING_QUOTAS_FILE (JSON) once and returns plan overrides.
// The file format is {"free": {...}, "starter": {...}, ...}. Missing fields keep defaults.
func loadQuotaOverrides() (map[string]QuotaConfig, error) {
	quotaOverrideOnce.Do(func() {
		path := os.Getenv("BILLING_QUOTAS_FILE")
		if path == "" {
			return
		}
		data, err := os.ReadFile(path)
		if err != nil {
			quotaOverrideError = err
			return
		}
		quotaOverrideError = json.Unmarshal(data, &quotaOverrides)
	})
	return quotaOverrides, quotaOverrideError
}

// Plan identifies a subscription tier.
type Plan string

const (
	PlanFree       Plan = "free"       // self-hosted, no cloud relay
	PlanStarter    Plan = "starter"    // $19/mo: 5 DIDs, 10k msg/mo
	PlanTeam       Plan = "team"       // $99/mo: 50 DIDs, 200k msg/mo
	PlanEnterprise Plan = "enterprise" // custom
)

// QuotaConfig defines the limits for a plan.
type QuotaConfig struct {
	MaxAgentDIDs         int     `json:"max_agent_dids"`           // max registered agent DIDs
	MaxMessagesPerMonth  int64   `json:"max_messages_per_month"`   // relay messages / calendar month
	MaxToolCallsPerMonth int64   `json:"max_tool_calls_per_month"` // MCP tool calls / calendar month
	RateLimitMsgPerSec   float64 `json:"rate_limit_msg_per_sec"`   // token-bucket rate (messages/sec)
	RateLimitBurstSize   float64 `json:"rate_limit_burst_size"`    // token-bucket burst
}

// hardcodedQuota returns the built-in quota defaults per plan.
func hardcodedQuota(p Plan) QuotaConfig {
	switch p {
	case PlanStarter:
		return QuotaConfig{
			MaxAgentDIDs:         5,
			MaxMessagesPerMonth:  10_000,
			MaxToolCallsPerMonth: 50_000,
			RateLimitMsgPerSec:   10,
			RateLimitBurstSize:   50,
		}
	case PlanTeam:
		return QuotaConfig{
			MaxAgentDIDs:         50,
			MaxMessagesPerMonth:  200_000,
			MaxToolCallsPerMonth: 1_000_000,
			RateLimitMsgPerSec:   100,
			RateLimitBurstSize:   500,
		}
	case PlanEnterprise:
		return QuotaConfig{
			MaxAgentDIDs:         100_000,
			MaxMessagesPerMonth:  1_000_000_000,
			MaxToolCallsPerMonth: 1_000_000_000,
			RateLimitMsgPerSec:   10_000,
			RateLimitBurstSize:   50_000,
		}
	default: // PlanFree
		return QuotaConfig{
			MaxAgentDIDs:         3,
			MaxMessagesPerMonth:  1_000,
			MaxToolCallsPerMonth: 5_000,
			RateLimitMsgPerSec:   5,
			RateLimitBurstSize:   20,
		}
	}
}

// DefaultQuota returns the quota for a given plan, with optional overrides from
// BILLING_QUOTAS_FILE (JSON map of plan name → QuotaConfig partial overrides).
func DefaultQuota(p Plan) QuotaConfig {
	q := hardcodedQuota(p)
	overrides, _ := loadQuotaOverrides()
	if ov, ok := overrides[string(p)]; ok {
		// Apply non-zero fields from override.
		if ov.MaxAgentDIDs > 0 {
			q.MaxAgentDIDs = ov.MaxAgentDIDs
		}
		if ov.MaxMessagesPerMonth > 0 {
			q.MaxMessagesPerMonth = ov.MaxMessagesPerMonth
		}
		if ov.MaxToolCallsPerMonth > 0 {
			q.MaxToolCallsPerMonth = ov.MaxToolCallsPerMonth
		}
		if ov.RateLimitMsgPerSec > 0 {
			q.RateLimitMsgPerSec = ov.RateLimitMsgPerSec
		}
		if ov.RateLimitBurstSize > 0 {
			q.RateLimitBurstSize = ov.RateLimitBurstSize
		}
	}
	return q
}

// TenantStatus is the lifecycle state of a tenant account.
type TenantStatus string

const (
	TenantStatusActive    TenantStatus = "active"
	TenantStatusSuspended TenantStatus = "suspended"
	TenantStatusDeleted   TenantStatus = "deleted"
)

// Tenant is a billing account owning one or more agent DIDs.
type Tenant struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	Email     string       `json:"email"`
	Plan      Plan         `json:"plan"`
	Status    TenantStatus `json:"status"`
	Quota     QuotaConfig  `json:"quota"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`

	// Stripe billing state.
	StripeCustomerID     string     `json:"stripe_customer_id,omitempty" db:"stripe_customer_id"`
	StripeSubscriptionID string     `json:"stripe_subscription_id,omitempty" db:"stripe_subscription_id"`
	CurrentPeriodEnd     *time.Time `json:"current_period_end,omitempty" db:"current_period_end"`
	BillingStatus        string     `json:"billing_status,omitempty" db:"billing_status"` // active|past_due|canceled|incomplete

	// DIDSeed is a 32-byte random seed used to deterministically derive the
	// tenant's Ed25519 signing key and DID via billing.DeriveTenantIdentity.
	// Set once at signup; never changes. Nil for tenants created before V5 migration.
	DIDSeed []byte `json:"-" db:"did_seed"`

	// EmailVerifiedAt is set when the tenant clicks the verification magic-link.
	// Nil means the email has not yet been verified.
	EmailVerifiedAt *time.Time `json:"email_verified_at,omitempty" db:"email_verified_at"`
}

// Billing errors.
var (
	ErrTenantNotFound        = errors.New("tenant not found")
	ErrTenantSuspended       = errors.New("tenant account is suspended")
	ErrQuotaExceeded         = errors.New("quota exceeded for current billing period")
	ErrAPIKeyNotFound        = errors.New("API key not found")
	ErrAPIKeyRevoked         = errors.New("API key has been revoked")
	ErrInvalidAPIKey         = errors.New("invalid API key format")
	ErrOAuthIdentityNotFound = errors.New("OAuth identity not found")
	ErrTokenNotFound         = errors.New("verification token not found or expired")
)

// NewTenant creates a new tenant with a generated ID and a random DID seed.
func NewTenant(name, email string, plan Plan) (*Tenant, error) {
	now := time.Now().UTC()
	seed := make([]byte, 32)
	if _, err := rand.Read(seed); err != nil {
		return nil, fmt.Errorf("generate DID seed: %w", err)
	}
	return &Tenant{
		ID:            newID("t"),
		Name:          name,
		Email:         email,
		Plan:          plan,
		Status:        TenantStatusActive,
		Quota:         DefaultQuota(plan),
		CreatedAt:     now,
		UpdatedAt:     now,
		BillingStatus: "active",
		DIDSeed:       seed,
	}, nil
}

// IsActive returns true if the tenant can use the service.
func (t *Tenant) IsActive() bool {
	return t.Status == TenantStatusActive
}
