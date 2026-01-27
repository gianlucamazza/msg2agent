package registry

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
)

var (
	ErrDiscoveryTimeout = errors.New("discovery timeout")
	ErrNoAgentsFound    = errors.New("no agents found")
)

// DiscoveryMessage types
const (
	DiscoveryAnnounce = "announce"
	DiscoveryQuery    = "query"
	DiscoveryResponse = "response"
	DiscoveryLeave    = "leave"
)

// DiscoveryMessage represents a discovery protocol message.
type DiscoveryMessage struct {
	Type       string    `json:"type"`
	AgentID    uuid.UUID `json:"agent_id"`
	DID        string    `json:"did,omitempty"`
	Capability string    `json:"capability,omitempty"`
	Agent      *Agent    `json:"agent,omitempty"`
	Timestamp  time.Time `json:"timestamp"`
}

// DiscoveryHandler handles discovery messages.
type DiscoveryHandler func(msg *DiscoveryMessage) error

// Discovery provides agent discovery functionality.
type Discovery struct {
	store      Store
	localAgent *Agent
	handlers   []DiscoveryHandler
	mu         sync.RWMutex
}

// NewDiscovery creates a new discovery service.
func NewDiscovery(store Store, localAgent *Agent) *Discovery {
	return &Discovery{
		store:      store,
		localAgent: localAgent,
		handlers:   make([]DiscoveryHandler, 0),
	}
}

// OnMessage registers a handler for discovery messages.
func (d *Discovery) OnMessage(handler DiscoveryHandler) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.handlers = append(d.handlers, handler)
}

// Announce broadcasts the local agent's presence.
func (d *Discovery) Announce() (*DiscoveryMessage, error) {
	msg := &DiscoveryMessage{
		Type:      DiscoveryAnnounce,
		AgentID:   d.localAgent.ID,
		DID:       d.localAgent.DID,
		Agent:     d.localAgent,
		Timestamp: time.Now(),
	}
	return msg, nil
}

// Leave broadcasts that the local agent is leaving.
func (d *Discovery) Leave() (*DiscoveryMessage, error) {
	msg := &DiscoveryMessage{
		Type:      DiscoveryLeave,
		AgentID:   d.localAgent.ID,
		DID:       d.localAgent.DID,
		Timestamp: time.Now(),
	}
	return msg, nil
}

// Query creates a query message for finding agents.
func (d *Discovery) Query(capability string) (*DiscoveryMessage, error) {
	msg := &DiscoveryMessage{
		Type:       DiscoveryQuery,
		AgentID:    d.localAgent.ID,
		Capability: capability,
		Timestamp:  time.Now(),
	}
	return msg, nil
}

// HandleMessage processes an incoming discovery message.
func (d *Discovery) HandleMessage(data []byte) error {
	var msg DiscoveryMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return err
	}

	switch msg.Type {
	case DiscoveryAnnounce:
		return d.handleAnnounce(&msg)
	case DiscoveryQuery:
		return d.handleQuery(&msg)
	case DiscoveryResponse:
		return d.handleResponse(&msg)
	case DiscoveryLeave:
		return d.handleLeave(&msg)
	default:
		return errors.New("unknown discovery message type")
	}
}

func (d *Discovery) handleAnnounce(msg *DiscoveryMessage) error {
	if msg.Agent == nil {
		return nil
	}

	// Don't store ourselves
	if msg.AgentID == d.localAgent.ID {
		return nil
	}

	msg.Agent.SetOnline()
	return d.store.Put(msg.Agent)
}

func (d *Discovery) handleQuery(msg *DiscoveryMessage) error {
	// Check if we match the query
	if msg.Capability != "" && !d.localAgent.HasCapability(msg.Capability) {
		return nil
	}

	// Create response
	response := &DiscoveryMessage{
		Type:      DiscoveryResponse,
		AgentID:   d.localAgent.ID,
		DID:       d.localAgent.DID,
		Agent:     d.localAgent,
		Timestamp: time.Now(),
	}

	// Notify handlers
	d.mu.RLock()
	handlers := d.handlers
	d.mu.RUnlock()

	for _, handler := range handlers {
		if err := handler(response); err != nil {
			return err
		}
	}

	return nil
}

func (d *Discovery) handleResponse(msg *DiscoveryMessage) error {
	if msg.Agent == nil {
		return nil
	}

	msg.Agent.SetOnline()
	return d.store.Put(msg.Agent)
}

func (d *Discovery) handleLeave(msg *DiscoveryMessage) error {
	agent, err := d.store.Get(msg.AgentID)
	if err != nil {
		return nil // Agent not in store, nothing to do
	}

	agent.SetOffline()
	return d.store.Put(agent)
}

// FindByCapability searches for agents with a specific capability.
func (d *Discovery) FindByCapability(ctx context.Context, capability string) ([]*Agent, error) {
	return d.store.Search(capability)
}

// FindByDID finds an agent by DID.
func (d *Discovery) FindByDID(ctx context.Context, did string) (*Agent, error) {
	return d.store.GetByDID(did)
}

// GetOnlineAgents returns all online agents.
func (d *Discovery) GetOnlineAgents() ([]*Agent, error) {
	agents, err := d.store.List()
	if err != nil {
		return nil, err
	}

	var online []*Agent
	for _, agent := range agents {
		if agent.Status == StatusOnline {
			online = append(online, agent)
		}
	}
	return online, nil
}

// PeerKey represents a public key for a peer agent.
type PeerKey struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Key     string `json:"key"`
	Purpose string `json:"purpose"`
}

// AddPeer adds a discovered peer to the local registry.
func (d *Discovery) AddPeer(did, displayName string, keys []PeerKey) error {
	agent := NewAgent(did, displayName)
	agent.SetOnline()

	// Decode and add public keys
	for _, key := range keys {
		keyType := KeyTypeEd25519
		switch key.Type {
		case "Ed25519":
			keyType = KeyTypeEd25519
		case "X25519":
			keyType = KeyTypeX25519
		}

		// Decode base64 key
		keyBytes, err := decodeKey(key.Key)
		if err != nil {
			continue
		}

		agent.AddPublicKey(key.ID, keyType, keyBytes, key.Purpose)
	}

	return d.store.Put(agent)
}

// decodeKey decodes a base64-encoded key.
func decodeKey(encoded string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(encoded)
}
