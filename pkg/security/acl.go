// Package security provides access control and rate limiting.
package security

import (
	"errors"
	"strings"

	"github.com/gianluca/msg2agent/pkg/registry"
)

// ACL errors.
var (
	ErrAccessDenied    = errors.New("access denied")
	ErrInvalidPolicy   = errors.New("invalid ACL policy")
	ErrNoPolicyDefined = errors.New("no ACL policy defined")
)

// Effect represents the result of an ACL evaluation.
type Effect string

// ACL effects.
const (
	EffectAllow Effect = "allow"
	EffectDeny  Effect = "deny"
)

// ACLEnforcer evaluates access control policies.
type ACLEnforcer struct{}

// NewACLEnforcer creates a new ACL enforcer.
func NewACLEnforcer() *ACLEnforcer {
	return &ACLEnforcer{}
}

// CheckAccess evaluates whether a principal can perform an action on an agent.
func (e *ACLEnforcer) CheckAccess(agent *registry.Agent, principalDID, action string) error {
	if agent.ACL == nil {
		// No ACL defined - use permissive default
		return nil
	}

	// Evaluate rules in order
	for _, rule := range agent.ACL.Rules {
		if e.matchesPrincipal(rule.Principal, principalDID) && e.matchesAction(rule.Actions, action) {
			if rule.Effect == string(EffectDeny) {
				return ErrAccessDenied
			}
			if rule.Effect == string(EffectAllow) {
				return nil
			}
		}
	}

	// Fall back to default
	if agent.ACL.DefaultAllow {
		return nil
	}
	return ErrAccessDenied
}

// matchesPrincipal checks if the principal matches the rule.
func (e *ACLEnforcer) matchesPrincipal(pattern, principal string) bool {
	if pattern == "*" {
		return true
	}
	// Support wildcard suffix matching
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(principal, prefix)
	}
	return pattern == principal
}

// matchesAction checks if the action matches the rule.
func (e *ACLEnforcer) matchesAction(patterns []string, action string) bool {
	for _, pattern := range patterns {
		if pattern == "*" {
			return true
		}
		// Support wildcard suffix matching
		if strings.HasSuffix(pattern, "*") {
			prefix := strings.TrimSuffix(pattern, "*")
			if strings.HasPrefix(action, prefix) {
				return true
			}
		}
		if pattern == action {
			return true
		}
	}
	return false
}

// PolicyBuilder helps construct ACL policies.
type PolicyBuilder struct {
	policy *registry.ACLPolicy
}

// NewPolicyBuilder creates a new policy builder.
func NewPolicyBuilder() *PolicyBuilder {
	return &PolicyBuilder{
		policy: &registry.ACLPolicy{
			DefaultAllow: false,
			Rules:        make([]registry.ACLRule, 0),
		},
	}
}

// SetDefaultAllow sets the default policy.
func (b *PolicyBuilder) SetDefaultAllow(allow bool) *PolicyBuilder {
	b.policy.DefaultAllow = allow
	return b
}

// Allow adds an allow rule.
func (b *PolicyBuilder) Allow(principal string, actions ...string) *PolicyBuilder {
	b.policy.Rules = append(b.policy.Rules, registry.ACLRule{
		Principal: principal,
		Actions:   actions,
		Effect:    string(EffectAllow),
	})
	return b
}

// Deny adds a deny rule.
func (b *PolicyBuilder) Deny(principal string, actions ...string) *PolicyBuilder {
	b.policy.Rules = append(b.policy.Rules, registry.ACLRule{
		Principal: principal,
		Actions:   actions,
		Effect:    string(EffectDeny),
	})
	return b
}

// Build returns the constructed policy.
func (b *PolicyBuilder) Build() *registry.ACLPolicy {
	return b.policy
}

// DefaultOpenPolicy returns a permissive policy (allow all).
func DefaultOpenPolicy() *registry.ACLPolicy {
	return NewPolicyBuilder().
		SetDefaultAllow(true).
		Build()
}

// DefaultClosedPolicy returns a restrictive policy (deny all).
func DefaultClosedPolicy() *registry.ACLPolicy {
	return NewPolicyBuilder().
		SetDefaultAllow(false).
		Build()
}

// TrustedAgentsPolicy returns a policy that only allows specific agents.
func TrustedAgentsPolicy(trustedDIDs []string) *registry.ACLPolicy {
	builder := NewPolicyBuilder().SetDefaultAllow(false)
	for _, did := range trustedDIDs {
		builder.Allow(did, "*")
	}
	return builder.Build()
}
