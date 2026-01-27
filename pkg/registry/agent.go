// Package registry provides agent registration and discovery functionality.
package registry

import (
	"time"

	"github.com/google/uuid"
)

// AgentStatus represents the current status of an agent.
type AgentStatus string

const (
	StatusOnline  AgentStatus = "online"
	StatusOffline AgentStatus = "offline"
	StatusBusy    AgentStatus = "busy"
	StatusAway    AgentStatus = "away" // Auto-set after idle timeout
	StatusDND     AgentStatus = "dnd"  // Do Not Disturb
)

// PresenceInfo contains detailed presence information for an agent.
type PresenceInfo struct {
	Status       AgentStatus `json:"status"`
	StatusText   string      `json:"status_text,omitempty"` // e.g., "In a meeting"
	LastActivity time.Time   `json:"last_activity"`
	IdleSince    *time.Time  `json:"idle_since,omitempty"` // When agent went idle
}

// NewPresenceInfo creates a new PresenceInfo with online status.
func NewPresenceInfo() *PresenceInfo {
	return &PresenceInfo{
		Status:       StatusOnline,
		LastActivity: time.Now(),
	}
}

// UpdateActivity updates the last activity time and clears idle state.
func (p *PresenceInfo) UpdateActivity() {
	p.LastActivity = time.Now()
	p.IdleSince = nil
	if p.Status == StatusAway {
		p.Status = StatusOnline
	}
}

// SetIdle marks the agent as idle/away.
func (p *PresenceInfo) SetIdle() {
	if p.Status == StatusOnline {
		p.Status = StatusAway
		now := time.Now()
		p.IdleSince = &now
	}
}

// IsIdle returns true if the agent has been idle longer than the given duration.
func (p *PresenceInfo) IsIdle(idleTimeout time.Duration) bool {
	return time.Since(p.LastActivity) > idleTimeout
}

// PresenceConfig holds privacy settings for presence sharing.
type PresenceConfig struct {
	// SharePresence enables sharing presence status with others.
	SharePresence bool `json:"share_presence"`

	// ReceivePresence enables receiving presence updates from others.
	ReceivePresence bool `json:"receive_presence"`

	// ShareTyping enables sharing typing indicators.
	ShareTyping bool `json:"share_typing"`

	// HideLastSeen hides the last activity timestamp from others.
	HideLastSeen bool `json:"hide_last_seen"`

	// AwayTimeoutSecs is the idle time before auto-setting away status.
	// 0 means disabled.
	AwayTimeoutSecs int `json:"away_timeout_secs"`
}

// DefaultPresenceConfig returns the default presence settings.
func DefaultPresenceConfig() PresenceConfig {
	return PresenceConfig{
		SharePresence:   true,
		ReceivePresence: true,
		ShareTyping:     true,
		HideLastSeen:    false,
		AwayTimeoutSecs: 300, // 5 minutes
	}
}

// KeyType identifies the type of cryptographic key.
type KeyType string

const (
	KeyTypeEd25519 KeyType = "Ed25519"
	KeyTypeX25519  KeyType = "X25519"
)

// PublicKey represents a public key with its type and purpose.
type PublicKey struct {
	ID      string  `json:"id"`
	Type    KeyType `json:"type"`
	Key     []byte  `json:"key"`
	Purpose string  `json:"purpose"` // signing, encryption
}

// TransportType identifies the transport protocol.
type TransportType string

const (
	TransportWebSocket TransportType = "websocket"
	TransportGRPC      TransportType = "grpc"
	TransportHTTPSSE   TransportType = "http+sse"
	TransportStdio     TransportType = "stdio"
)

// Endpoint describes how to reach an agent.
type Endpoint struct {
	Transport TransportType `json:"transport"`
	URL       string        `json:"url"`
	Priority  int           `json:"priority"`
}

// Capability describes what an agent can do.
type Capability struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Methods     []string       `json:"methods,omitempty"`
	Schema      map[string]any `json:"schema,omitempty"`
}

// ACLPolicy defines access control for an agent.
type ACLPolicy struct {
	DefaultAllow bool      `json:"default_allow"`
	Rules        []ACLRule `json:"rules"`
}

// ACLRule is a single access control rule.
type ACLRule struct {
	Principal  string   `json:"principal"` // DID or "*"
	Actions    []string `json:"actions"`   // method names or "*"
	Effect     string   `json:"effect"`    // allow, deny
	Conditions []string `json:"conditions,omitempty"`
}

// Agent represents a registered agent in the system.
type Agent struct {
	ID           uuid.UUID       `json:"id"`
	DID          string          `json:"did"`
	DisplayName  string          `json:"display_name"`
	PublicKeys   []PublicKey     `json:"public_keys"`
	Endpoints    []Endpoint      `json:"endpoints"`
	Capabilities []Capability    `json:"capabilities"`
	ACL          *ACLPolicy      `json:"acl,omitempty"`
	Status       AgentStatus     `json:"status"`
	LastSeen     time.Time       `json:"last_seen"`
	Metadata     map[string]any  `json:"metadata,omitempty"`
	Presence     *PresenceInfo   `json:"presence,omitempty"`
	PresenceCfg  *PresenceConfig `json:"presence_config,omitempty"`
}

// NewAgent creates a new agent with a generated UUID v7.
func NewAgent(did, displayName string) *Agent {
	return &Agent{
		ID:           uuid.Must(uuid.NewV7()),
		DID:          did,
		DisplayName:  displayName,
		PublicKeys:   make([]PublicKey, 0),
		Endpoints:    make([]Endpoint, 0),
		Capabilities: make([]Capability, 0),
		Status:       StatusOffline,
		LastSeen:     time.Now(),
		Metadata:     make(map[string]any),
	}
}

// AddEndpoint adds a new endpoint to the agent.
func (a *Agent) AddEndpoint(transport TransportType, url string, priority int) {
	a.Endpoints = append(a.Endpoints, Endpoint{
		Transport: transport,
		URL:       url,
		Priority:  priority,
	})
}

// AddCapability adds a new capability to the agent.
func (a *Agent) AddCapability(name, description string, methods []string) {
	a.Capabilities = append(a.Capabilities, Capability{
		Name:        name,
		Description: description,
		Methods:     methods,
	})
}

// AddPublicKey adds a public key to the agent.
func (a *Agent) AddPublicKey(id string, keyType KeyType, key []byte, purpose string) {
	a.PublicKeys = append(a.PublicKeys, PublicKey{
		ID:      id,
		Type:    keyType,
		Key:     key,
		Purpose: purpose,
	})
}

// SetOnline marks the agent as online.
func (a *Agent) SetOnline() {
	a.Status = StatusOnline
	a.LastSeen = time.Now()
	if a.Presence == nil {
		a.Presence = NewPresenceInfo()
	} else {
		a.Presence.Status = StatusOnline
		a.Presence.UpdateActivity()
	}
}

// SetOffline marks the agent as offline.
func (a *Agent) SetOffline() {
	a.Status = StatusOffline
	a.LastSeen = time.Now()
	if a.Presence != nil {
		a.Presence.Status = StatusOffline
	}
}

// SetBusy marks the agent as busy.
func (a *Agent) SetBusy() {
	a.Status = StatusBusy
	a.LastSeen = time.Now()
	if a.Presence != nil {
		a.Presence.Status = StatusBusy
	}
}

// SetAway marks the agent as away.
func (a *Agent) SetAway() {
	a.Status = StatusAway
	a.LastSeen = time.Now()
	if a.Presence != nil {
		a.Presence.SetIdle()
	}
}

// SetDND marks the agent as Do Not Disturb.
func (a *Agent) SetDND() {
	a.Status = StatusDND
	a.LastSeen = time.Now()
	if a.Presence != nil {
		a.Presence.Status = StatusDND
	}
}

// UpdatePresence updates the agent's presence with a status and optional text.
func (a *Agent) UpdatePresence(status AgentStatus, statusText string) {
	a.Status = status
	a.LastSeen = time.Now()
	if a.Presence == nil {
		a.Presence = &PresenceInfo{}
	}
	a.Presence.Status = status
	a.Presence.StatusText = statusText
	a.Presence.LastActivity = time.Now()
}

// ShouldSharePresence returns true if presence should be shared.
func (a *Agent) ShouldSharePresence() bool {
	if a.PresenceCfg == nil {
		return true // Default is to share
	}
	return a.PresenceCfg.SharePresence
}

// ShouldShareTyping returns true if typing indicators should be shared.
func (a *Agent) ShouldShareTyping() bool {
	if a.PresenceCfg == nil {
		return true // Default is to share
	}
	return a.PresenceCfg.ShareTyping
}

// HasCapability checks if the agent has a specific capability.
func (a *Agent) HasCapability(name string) bool {
	for _, cap := range a.Capabilities {
		if cap.Name == name {
			return true
		}
	}
	return false
}

// GetSigningKey returns the first signing key, if any.
func (a *Agent) GetSigningKey() *PublicKey {
	for i := range a.PublicKeys {
		if a.PublicKeys[i].Purpose == "signing" {
			return &a.PublicKeys[i]
		}
	}
	return nil
}

// GetEncryptionKey returns the first encryption key, if any.
func (a *Agent) GetEncryptionKey() *PublicKey {
	for i := range a.PublicKeys {
		if a.PublicKeys[i].Purpose == "encryption" {
			return &a.PublicKeys[i]
		}
	}
	return nil
}
