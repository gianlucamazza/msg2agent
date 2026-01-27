package registry

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Channel errors
var (
	ErrChannelNotFound    = errors.New("channel not found")
	ErrChannelExists      = errors.New("channel already exists")
	ErrNotChannelMember   = errors.New("not a channel member")
	ErrNotChannelOwner    = errors.New("not channel owner")
	ErrMemberAlreadyAdded = errors.New("member already added")
)

// ChannelType identifies the type of channel.
type ChannelType string

const (
	// ChannelGroup allows all members to send and receive messages.
	ChannelGroup ChannelType = "group"

	// ChannelBroadcast allows only the owner to send, members only receive.
	ChannelBroadcast ChannelType = "broadcast"

	// ChannelTopic is a pub/sub channel where agents subscribe to topics.
	ChannelTopic ChannelType = "topic"
)

// Channel represents a group communication channel.
type Channel struct {
	ID          uuid.UUID       `json:"id"`
	Name        string          `json:"name"` // Format: "channel:domain:name"
	Type        ChannelType     `json:"type"`
	OwnerDID    string          `json:"owner_did"`
	Description string          `json:"description,omitempty"`
	Members     []ChannelMember `json:"members"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	Metadata    map[string]any  `json:"metadata,omitempty"`
}

// ChannelMember represents a member of a channel.
type ChannelMember struct {
	DID       string      `json:"did"`
	JoinedAt  time.Time   `json:"joined_at"`
	Role      ChannelRole `json:"role"`
	SenderKey *SenderKey  `json:"sender_key,omitempty"` // For E2E encryption
}

// ChannelRole defines the role of a member in a channel.
type ChannelRole string

const (
	RoleOwner  ChannelRole = "owner"
	RoleAdmin  ChannelRole = "admin"
	RoleMember ChannelRole = "member"
)

// SenderKey is used for group E2E encryption (Signal Sender Keys protocol).
type SenderKey struct {
	ChainKey     []byte `json:"chain_key"`     // Derives message keys
	SignatureKey []byte `json:"signature_key"` // Verifies sender (Ed25519 public key)
	Iteration    uint32 `json:"iteration"`     // Ratchet counter
}

// NewChannel creates a new channel.
func NewChannel(name string, channelType ChannelType, ownerDID string) *Channel {
	now := time.Now()
	return &Channel{
		ID:       uuid.Must(uuid.NewV7()),
		Name:     name,
		Type:     channelType,
		OwnerDID: ownerDID,
		Members: []ChannelMember{{
			DID:      ownerDID,
			JoinedAt: now,
			Role:     RoleOwner,
		}},
		CreatedAt: now,
		UpdatedAt: now,
		Metadata:  make(map[string]any),
	}
}

// AddMember adds a new member to the channel.
func (c *Channel) AddMember(did string, role ChannelRole) error {
	if c.IsMember(did) {
		return ErrMemberAlreadyAdded
	}
	c.Members = append(c.Members, ChannelMember{
		DID:      did,
		JoinedAt: time.Now(),
		Role:     role,
	})
	c.UpdatedAt = time.Now()
	return nil
}

// RemoveMember removes a member from the channel.
func (c *Channel) RemoveMember(did string) bool {
	for i, m := range c.Members {
		if m.DID == did {
			c.Members = append(c.Members[:i], c.Members[i+1:]...)
			c.UpdatedAt = time.Now()
			return true
		}
	}
	return false
}

// IsMember checks if a DID is a member of the channel.
func (c *Channel) IsMember(did string) bool {
	for _, m := range c.Members {
		if m.DID == did {
			return true
		}
	}
	return false
}

// IsOwner checks if a DID is the owner of the channel.
func (c *Channel) IsOwner(did string) bool {
	return c.OwnerDID == did
}

// IsAdmin checks if a DID is an admin or owner of the channel.
func (c *Channel) IsAdmin(did string) bool {
	if c.IsOwner(did) {
		return true
	}
	for _, m := range c.Members {
		if m.DID == did && m.Role == RoleAdmin {
			return true
		}
	}
	return false
}

// CanSend checks if a DID can send messages to this channel.
func (c *Channel) CanSend(did string) bool {
	if !c.IsMember(did) {
		return false
	}
	// Broadcast channels: only owner can send
	if c.Type == ChannelBroadcast {
		return c.IsOwner(did)
	}
	return true
}

// GetMember returns the member info for a DID.
func (c *Channel) GetMember(did string) *ChannelMember {
	for i := range c.Members {
		if c.Members[i].DID == did {
			return &c.Members[i]
		}
	}
	return nil
}

// MemberDIDs returns a list of all member DIDs.
func (c *Channel) MemberDIDs() []string {
	dids := make([]string, len(c.Members))
	for i, m := range c.Members {
		dids[i] = m.DID
	}
	return dids
}

// SetSenderKey sets the sender key for a member.
func (c *Channel) SetSenderKey(did string, key *SenderKey) bool {
	for i := range c.Members {
		if c.Members[i].DID == did {
			c.Members[i].SenderKey = key
			c.UpdatedAt = time.Now()
			return true
		}
	}
	return false
}

// GetSenderKey returns the sender key for a member.
func (c *Channel) GetSenderKey(did string) *SenderKey {
	member := c.GetMember(did)
	if member != nil {
		return member.SenderKey
	}
	return nil
}

// ChannelStore defines the interface for channel persistence.
type ChannelStore interface {
	// CreateChannel creates a new channel.
	CreateChannel(channel *Channel) error

	// GetChannel retrieves a channel by ID.
	GetChannel(id uuid.UUID) (*Channel, error)

	// GetChannelByName retrieves a channel by name.
	GetChannelByName(name string) (*Channel, error)

	// UpdateChannel updates an existing channel.
	UpdateChannel(channel *Channel) error

	// DeleteChannel removes a channel.
	DeleteChannel(id uuid.UUID) error

	// ListChannels lists all channels a DID is a member of.
	ListChannels(memberDID string) ([]*Channel, error)

	// ListAllChannels lists all channels.
	ListAllChannels() ([]*Channel, error)
}

// MemoryChannelStore is an in-memory implementation of ChannelStore.
type MemoryChannelStore struct {
	channels map[uuid.UUID]*Channel
	byName   map[string]uuid.UUID
}

// NewMemoryChannelStore creates a new in-memory channel store.
func NewMemoryChannelStore() *MemoryChannelStore {
	return &MemoryChannelStore{
		channels: make(map[uuid.UUID]*Channel),
		byName:   make(map[string]uuid.UUID),
	}
}

// CreateChannel creates a new channel.
func (s *MemoryChannelStore) CreateChannel(channel *Channel) error {
	if _, exists := s.byName[channel.Name]; exists {
		return ErrChannelExists
	}
	// Clone channel
	c := *channel
	c.Members = make([]ChannelMember, len(channel.Members))
	copy(c.Members, channel.Members)
	s.channels[channel.ID] = &c
	s.byName[channel.Name] = channel.ID
	return nil
}

// GetChannel retrieves a channel by ID.
func (s *MemoryChannelStore) GetChannel(id uuid.UUID) (*Channel, error) {
	channel, ok := s.channels[id]
	if !ok {
		return nil, ErrChannelNotFound
	}
	// Return copy
	c := *channel
	c.Members = make([]ChannelMember, len(channel.Members))
	copy(c.Members, channel.Members)
	return &c, nil
}

// GetChannelByName retrieves a channel by name.
func (s *MemoryChannelStore) GetChannelByName(name string) (*Channel, error) {
	id, ok := s.byName[name]
	if !ok {
		return nil, ErrChannelNotFound
	}
	return s.GetChannel(id)
}

// UpdateChannel updates an existing channel.
func (s *MemoryChannelStore) UpdateChannel(channel *Channel) error {
	if _, ok := s.channels[channel.ID]; !ok {
		return ErrChannelNotFound
	}
	c := *channel
	c.Members = make([]ChannelMember, len(channel.Members))
	copy(c.Members, channel.Members)
	s.channels[channel.ID] = &c
	return nil
}

// DeleteChannel removes a channel.
func (s *MemoryChannelStore) DeleteChannel(id uuid.UUID) error {
	channel, ok := s.channels[id]
	if !ok {
		return ErrChannelNotFound
	}
	delete(s.byName, channel.Name)
	delete(s.channels, id)
	return nil
}

// ListChannels lists all channels a DID is a member of.
func (s *MemoryChannelStore) ListChannels(memberDID string) ([]*Channel, error) {
	var result []*Channel
	for _, channel := range s.channels {
		if channel.IsMember(memberDID) {
			c := *channel
			c.Members = make([]ChannelMember, len(channel.Members))
			copy(c.Members, channel.Members)
			result = append(result, &c)
		}
	}
	return result, nil
}

// ListAllChannels lists all channels.
func (s *MemoryChannelStore) ListAllChannels() ([]*Channel, error) {
	result := make([]*Channel, 0, len(s.channels))
	for _, channel := range s.channels {
		c := *channel
		c.Members = make([]ChannelMember, len(channel.Members))
		copy(c.Members, channel.Members)
		result = append(result, &c)
	}
	return result, nil
}
