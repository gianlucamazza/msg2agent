package main

import (
	"fmt"

	"github.com/gianlucamazza/msg2agent/pkg/billing"
	"github.com/gianlucamazza/msg2agent/pkg/oauth"
)

// billingOAuthStore is the subset of billing.SQLiteStore needed for OAuth operations.
// We use a combined interface to avoid a hard dependency on *SQLiteStore in wiring code.
type billingOAuthStore interface {
	billing.Store
	oauth.Store
	GetTenantByEmail(email string) (*billing.Tenant, error)
}

// billingTenantLookup wraps a billingOAuthStore to satisfy oauth.TenantLookup.
type billingTenantLookup struct {
	store billingOAuthStore
}

func (l *billingTenantLookup) GetTenantByEmail(email string) (*oauth.TenantBrief, error) {
	t, err := l.store.GetTenantByEmail(email)
	if err != nil {
		return nil, err
	}
	return &oauth.TenantBrief{ID: t.ID, Name: t.Name, Email: t.Email}, nil
}

func (l *billingTenantLookup) GetTenantByID(id string) (*oauth.TenantBrief, error) {
	t, err := l.store.GetTenant(id)
	if err != nil {
		return nil, fmt.Errorf("billing: get tenant %s: %w", id, err)
	}
	return &oauth.TenantBrief{ID: t.ID, Name: t.Name, Email: t.Email}, nil
}
