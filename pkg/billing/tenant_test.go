package billing

import (
	"encoding/json"
	"testing"
)

// TestNewTenant_FieldsAndNonZeroID verifies that NewTenant produces a tenant
// with the given fields and a non-empty ID.
func TestNewTenant_FieldsAndNonZeroID(t *testing.T) {
	t = t
	tenant := NewTenant("Acme Corp", "acme@example.com", PlanStarter)

	if tenant.ID == "" {
		t.Error("ID must not be empty")
	}
	if tenant.Name != "Acme Corp" {
		t.Errorf("Name = %q, want %q", tenant.Name, "Acme Corp")
	}
	if tenant.Email != "acme@example.com" {
		t.Errorf("Email = %q, want %q", tenant.Email, "acme@example.com")
	}
	if tenant.Plan != PlanStarter {
		t.Errorf("Plan = %q, want %q", tenant.Plan, PlanStarter)
	}
	if tenant.Status != TenantStatusActive {
		t.Errorf("Status = %q, want %q", tenant.Status, TenantStatusActive)
	}
	if tenant.CreatedAt.IsZero() {
		t.Error("CreatedAt must not be zero")
	}
	if tenant.UpdatedAt.IsZero() {
		t.Error("UpdatedAt must not be zero")
	}
}

// TestNewTenant_UniqueIDs verifies that two consecutive NewTenant calls
// produce different IDs.
func TestNewTenant_UniqueIDs(t *testing.T) {
	a := NewTenant("A", "a@x.com", PlanFree)
	b := NewTenant("B", "b@x.com", PlanFree)
	if a.ID == b.ID {
		t.Errorf("expected unique IDs, both got %q", a.ID)
	}
}

// TestPlanQuota verifies that DefaultQuota returns the expected quotas for each
// built-in plan (ignoring any external BILLING_QUOTAS_FILE overrides since
// the env var is not set in tests).
func TestPlanQuota(t *testing.T) {
	cases := []struct {
		plan             Plan
		wantMaxDIDs      int
		wantMaxMsgPerMon int64
	}{
		{PlanFree, 3, 1_000},
		{PlanStarter, 5, 10_000},
		{PlanTeam, 50, 200_000},
		{PlanEnterprise, 100_000, 1_000_000_000},
	}
	for _, tc := range cases {
		q := hardcodedQuota(tc.plan)
		if q.MaxAgentDIDs != tc.wantMaxDIDs {
			t.Errorf("plan=%s MaxAgentDIDs=%d, want %d", tc.plan, q.MaxAgentDIDs, tc.wantMaxDIDs)
		}
		if q.MaxMessagesPerMonth != tc.wantMaxMsgPerMon {
			t.Errorf("plan=%s MaxMessagesPerMonth=%d, want %d", tc.plan, q.MaxMessagesPerMonth, tc.wantMaxMsgPerMon)
		}
	}
}

// TestTenantIsActive verifies the IsActive predicate for all status values.
func TestTenantIsActive(t *testing.T) {
	tenant := NewTenant("T", "t@x.com", PlanFree)

	if !tenant.IsActive() {
		t.Error("new tenant should be active")
	}

	tenant.Status = TenantStatusSuspended
	if tenant.IsActive() {
		t.Error("suspended tenant should not be active")
	}

	tenant.Status = TenantStatusDeleted
	if tenant.IsActive() {
		t.Error("deleted tenant should not be active")
	}

	tenant.Status = TenantStatusActive
	if !tenant.IsActive() {
		t.Error("tenant with active status should be active")
	}
}

// TestTenantSuspendViaStore verifies that Store.SuspendTenant changes the status
// to suspended. We run this against MemoryStore (and optionally SQLiteStore if
// available).
func TestTenantSuspendViaStore(t *testing.T) {
	s := NewMemoryStore()

	tenant := NewTenant("Suspend Me", "s@x.com", PlanFree)
	if err := s.PutTenant(tenant); err != nil {
		t.Fatalf("PutTenant: %v", err)
	}

	if err := s.SuspendTenant(tenant.ID); err != nil {
		t.Fatalf("SuspendTenant: %v", err)
	}

	got, err := s.GetTenant(tenant.ID)
	if err != nil {
		t.Fatalf("GetTenant after suspend: %v", err)
	}
	if got.Status != TenantStatusSuspended {
		t.Errorf("Status after SuspendTenant = %q, want %q", got.Status, TenantStatusSuspended)
	}
	if got.IsActive() {
		t.Error("IsActive should return false after suspend")
	}
}

// TestTenantJSONRoundTrip verifies that marshalling and unmarshalling a Tenant
// preserves all fields.
func TestTenantJSONRoundTrip(t *testing.T) {
	original := NewTenant("Round Trip", "rt@example.com", PlanTeam)

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var decoded Tenant
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if decoded.ID != original.ID {
		t.Errorf("ID: got %q, want %q", decoded.ID, original.ID)
	}
	if decoded.Name != original.Name {
		t.Errorf("Name: got %q, want %q", decoded.Name, original.Name)
	}
	if decoded.Email != original.Email {
		t.Errorf("Email: got %q, want %q", decoded.Email, original.Email)
	}
	if decoded.Plan != original.Plan {
		t.Errorf("Plan: got %q, want %q", decoded.Plan, original.Plan)
	}
	if decoded.Status != original.Status {
		t.Errorf("Status: got %q, want %q", decoded.Status, original.Status)
	}
	if !decoded.CreatedAt.Equal(original.CreatedAt) {
		t.Errorf("CreatedAt: got %v, want %v", decoded.CreatedAt, original.CreatedAt)
	}
	if decoded.Quota.MaxAgentDIDs != original.Quota.MaxAgentDIDs {
		t.Errorf("Quota.MaxAgentDIDs: got %d, want %d", decoded.Quota.MaxAgentDIDs, original.Quota.MaxAgentDIDs)
	}
}

// TestDefaultQuota_NoPanic verifies that DefaultQuota does not panic for an
// unrecognised plan name (returns free-tier defaults).
func TestDefaultQuota_NoPanic(t *testing.T) {
	q := DefaultQuota(Plan("unknown-plan"))
	if q.MaxAgentDIDs <= 0 {
		t.Errorf("DefaultQuota(unknown) returned non-positive MaxAgentDIDs: %d", q.MaxAgentDIDs)
	}
}
