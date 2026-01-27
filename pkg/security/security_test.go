package security

import (
	"testing"

	"github.com/gianluca/msg2agent/pkg/registry"
)

func TestACLEnforcer(t *testing.T) {
	enforcer := NewACLEnforcer()

	// Agent with no ACL (permissive default)
	agent := registry.NewAgent("did:wba:test", "Test")
	if err := enforcer.CheckAccess(agent, "did:wba:anyone", "any.method"); err != nil {
		t.Errorf("expected access with no ACL, got error: %v", err)
	}

	// Agent with default deny
	agent.ACL = NewPolicyBuilder().
		SetDefaultAllow(false).
		Allow("did:wba:trusted", "*").
		Build()

	if err := enforcer.CheckAccess(agent, "did:wba:trusted", "any.method"); err != nil {
		t.Errorf("trusted agent should be allowed: %v", err)
	}

	if err := enforcer.CheckAccess(agent, "did:wba:untrusted", "any.method"); err != ErrAccessDenied {
		t.Errorf("untrusted agent should be denied")
	}
}

func TestACLWildcards(t *testing.T) {
	enforcer := NewACLEnforcer()

	agent := registry.NewAgent("did:wba:test", "Test")
	agent.ACL = NewPolicyBuilder().
		SetDefaultAllow(false).
		Allow("did:wba:org.example:*", "read.*").
		Deny("*", "admin.*").
		Build()

	// Wildcard principal match
	if err := enforcer.CheckAccess(agent, "did:wba:org.example:user1", "read.data"); err != nil {
		t.Errorf("wildcard principal should match: %v", err)
	}

	// Wildcard action match
	if err := enforcer.CheckAccess(agent, "did:wba:org.example:user1", "read.config"); err != nil {
		t.Errorf("wildcard action should match: %v", err)
	}

	// Deny rule takes precedence (processed first in this case)
	if err := enforcer.CheckAccess(agent, "did:wba:org.example:admin", "admin.delete"); err != ErrAccessDenied {
		t.Error("admin actions should be denied")
	}
}

func TestPolicyBuilder(t *testing.T) {
	policy := NewPolicyBuilder().
		SetDefaultAllow(false).
		Allow("did:wba:alice", "read", "write").
		Deny("did:wba:bob", "*").
		Build()

	if policy.DefaultAllow {
		t.Error("default should be deny")
	}
	if len(policy.Rules) != 2 {
		t.Errorf("expected 2 rules, got %d", len(policy.Rules))
	}

	// Check first rule
	if policy.Rules[0].Principal != "did:wba:alice" {
		t.Error("first rule should be for alice")
	}
	if policy.Rules[0].Effect != "allow" {
		t.Error("first rule should allow")
	}
	if len(policy.Rules[0].Actions) != 2 {
		t.Error("first rule should have 2 actions")
	}
}

func TestDefaultPolicies(t *testing.T) {
	enforcer := NewACLEnforcer()

	// Open policy
	open := DefaultOpenPolicy()
	agent := registry.NewAgent("did:wba:test", "Test")
	agent.ACL = open

	if err := enforcer.CheckAccess(agent, "anyone", "anything"); err != nil {
		t.Errorf("open policy should allow all: %v", err)
	}

	// Closed policy
	closed := DefaultClosedPolicy()
	agent.ACL = closed

	if err := enforcer.CheckAccess(agent, "anyone", "anything"); err != ErrAccessDenied {
		t.Error("closed policy should deny all")
	}
}

func TestTrustedAgentsPolicy(t *testing.T) {
	enforcer := NewACLEnforcer()

	trusted := []string{"did:wba:alice", "did:wba:bob"}
	policy := TrustedAgentsPolicy(trusted)

	agent := registry.NewAgent("did:wba:test", "Test")
	agent.ACL = policy

	if err := enforcer.CheckAccess(agent, "did:wba:alice", "any.method"); err != nil {
		t.Errorf("alice should be trusted: %v", err)
	}

	if err := enforcer.CheckAccess(agent, "did:wba:charlie", "any.method"); err != ErrAccessDenied {
		t.Error("charlie should not be trusted")
	}
}
