// Package billing provides tenant management, API key issuance, and usage metering.
package billing

import (
	"errors"
	"time"
)

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
	MaxAgentDIDs         int     // max registered agent DIDs
	MaxMessagesPerMonth  int64   // relay messages / calendar month
	MaxToolCallsPerMonth int64   // MCP tool calls / calendar month
	RateLimitMsgPerSec   float64 // token-bucket rate (messages/sec)
	RateLimitBurstSize   float64 // token-bucket burst
}

// DefaultQuota returns the quota for a given plan.
func DefaultQuota(p Plan) QuotaConfig {
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
}

// Billing errors.
var (
	ErrTenantNotFound  = errors.New("tenant not found")
	ErrTenantSuspended = errors.New("tenant account is suspended")
	ErrQuotaExceeded   = errors.New("quota exceeded for current billing period")
	ErrAPIKeyNotFound  = errors.New("API key not found")
	ErrAPIKeyRevoked   = errors.New("API key has been revoked")
	ErrInvalidAPIKey   = errors.New("invalid API key format")
)

// NewTenant creates a new tenant with a generated ID.
func NewTenant(name, email string, plan Plan) *Tenant {
	now := time.Now().UTC()
	return &Tenant{
		ID:        newID("t"),
		Name:      name,
		Email:     email,
		Plan:      plan,
		Status:    TenantStatusActive,
		Quota:     DefaultQuota(plan),
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// IsActive returns true if the tenant can use the service.
func (t *Tenant) IsActive() bool {
	return t.Status == TenantStatusActive
}
