package main

import (
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/gianluca/msg2agent/pkg/messaging"
	"github.com/gianluca/msg2agent/pkg/protocol"
	"github.com/gianluca/msg2agent/pkg/queue"
	"github.com/gianluca/msg2agent/pkg/registry"
)

// ChannelManager manages channels and routing.
type ChannelManager struct {
	hub   *RelayHub
	store registry.ChannelStore
	mu    sync.RWMutex
}

// NewChannelManager creates a new channel manager.
func NewChannelManager(hub *RelayHub) *ChannelManager {
	return &ChannelManager{
		hub:   hub,
		store: registry.NewMemoryChannelStore(),
	}
}

// IsChannelURI checks if a destination is a channel URI.
// Channel URIs have the format: "channel:domain:name"
func IsChannelURI(to string) bool {
	return strings.HasPrefix(to, "channel:")
}

// RouteToChannel routes a message to all channel members.
func (cm *ChannelManager) RouteToChannel(msg *messaging.Message, data []byte) error {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	channel, err := cm.store.GetChannelByName(msg.To)
	if err != nil {
		return err
	}

	// Verify sender is a member and can send
	if !channel.CanSend(msg.From) {
		return registry.ErrNotChannelMember
	}

	// Fan-out to all members except sender
	for _, member := range channel.Members {
		if member.DID == msg.From {
			continue // Skip sender
		}

		// Try to route to member
		cm.hub.mu.RLock()
		client, exists := cm.hub.clients[member.DID]
		cm.hub.mu.RUnlock()

		if exists {
			select {
			case client.SendCh <- data:
				recordMessageRouted()
			default:
				// Buffer full, message dropped for this member
				recordMessageDropped("buffer_full")
			}
		} else if cm.hub.queue != nil && cm.hub.config.EnableOfflineQueue {
			// Queue for offline delivery
			cm.hub.queueForOffline(msg, data, member.DID)
		}
	}

	return nil
}

// queueForOffline queues a message for offline delivery to a specific recipient.
func (h *RelayHub) queueForOffline(msg *messaging.Message, data []byte, recipientDID string) {
	queuedMsg := &queue.QueuedMessage{
		ID:           uuid.Must(uuid.NewV7()),
		RecipientDID: recipientDID,
		SenderDID:    msg.From,
		Data:         data,
		QueuedAt:     time.Now(),
		ExpiresAt:    time.Now().Add(h.config.QueueConfig.MessageTTL),
	}

	if err := h.queue.Enqueue(queuedMsg); err != nil {
		h.logger.Warn("failed to queue channel message for offline delivery",
			"error", err, "recipient", recipientDID, "channel", msg.To)
	} else {
		recordMessageQueued()
	}
}

// Channel relay method handlers

// handleChannelCreate handles relay.channel.create requests.
func (c *Client) handleChannelCreate(req *protocol.JSONRPCRequest) {
	if c.DID == "" {
		c.sendError(req.ID, protocol.CodeSenderNotRegistered, "must register first")
		return
	}

	var params struct {
		Name        string `json:"name"`
		Type        string `json:"type"`
		Description string `json:"description,omitempty"`
	}
	if err := req.ParseParams(&params); err != nil {
		c.sendError(req.ID, protocol.CodeInvalidParams, "invalid params")
		return
	}

	// Validate channel type
	var channelType registry.ChannelType
	switch params.Type {
	case "group":
		channelType = registry.ChannelGroup
	case "broadcast":
		channelType = registry.ChannelBroadcast
	case "topic":
		channelType = registry.ChannelTopic
	default:
		c.sendError(req.ID, protocol.CodeInvalidParams, "invalid channel type")
		return
	}

	if c.hub.channels == nil {
		c.sendError(req.ID, protocol.CodeInternalError, "channels not supported")
		return
	}

	channel := registry.NewChannel(params.Name, channelType, c.DID)
	channel.Description = params.Description

	if err := c.hub.channels.store.CreateChannel(channel); err != nil {
		if err == registry.ErrChannelExists {
			c.sendError(req.ID, protocol.CodeInvalidParams, "channel already exists")
		} else {
			c.sendError(req.ID, protocol.CodeInternalError, err.Error())
		}
		return
	}

	c.sendResult(req.ID, map[string]any{
		"id":   channel.ID.String(),
		"name": channel.Name,
	})
	c.hub.logger.Info("channel created", "name", channel.Name, "owner", c.DID)
}

// handleChannelJoin handles relay.channel.join requests.
func (c *Client) handleChannelJoin(req *protocol.JSONRPCRequest) {
	if c.DID == "" {
		c.sendError(req.ID, protocol.CodeSenderNotRegistered, "must register first")
		return
	}

	var params struct {
		ChannelID string `json:"channel_id,omitempty"`
		Name      string `json:"name,omitempty"`
	}
	if err := req.ParseParams(&params); err != nil {
		c.sendError(req.ID, protocol.CodeInvalidParams, "invalid params")
		return
	}

	if c.hub.channels == nil {
		c.sendError(req.ID, protocol.CodeInternalError, "channels not supported")
		return
	}

	var channel *registry.Channel
	var err error

	if params.ChannelID != "" {
		id, parseErr := uuid.Parse(params.ChannelID)
		if parseErr != nil {
			c.sendError(req.ID, protocol.CodeInvalidParams, "invalid channel_id")
			return
		}
		channel, err = c.hub.channels.store.GetChannel(id)
	} else if params.Name != "" {
		channel, err = c.hub.channels.store.GetChannelByName(params.Name)
	} else {
		c.sendError(req.ID, protocol.CodeInvalidParams, "channel_id or name required")
		return
	}

	if err != nil {
		c.sendError(req.ID, protocol.CodeInvalidParams, "channel not found")
		return
	}

	if err := channel.AddMember(c.DID, registry.RoleMember); err != nil {
		if err == registry.ErrMemberAlreadyAdded {
			c.sendResult(req.ID, map[string]string{"status": "already_member"})
		} else {
			c.sendError(req.ID, protocol.CodeInternalError, err.Error())
		}
		return
	}

	if err := c.hub.channels.store.UpdateChannel(channel); err != nil {
		c.sendError(req.ID, protocol.CodeInternalError, err.Error())
		return
	}

	c.sendResult(req.ID, map[string]string{"status": "joined"})
	c.hub.logger.Info("member joined channel", "channel", channel.Name, "member", c.DID)
}

// handleChannelLeave handles relay.channel.leave requests.
func (c *Client) handleChannelLeave(req *protocol.JSONRPCRequest) {
	if c.DID == "" {
		c.sendError(req.ID, protocol.CodeSenderNotRegistered, "must register first")
		return
	}

	var params struct {
		ChannelID string `json:"channel_id,omitempty"`
		Name      string `json:"name,omitempty"`
	}
	if err := req.ParseParams(&params); err != nil {
		c.sendError(req.ID, protocol.CodeInvalidParams, "invalid params")
		return
	}

	if c.hub.channels == nil {
		c.sendError(req.ID, protocol.CodeInternalError, "channels not supported")
		return
	}

	var channel *registry.Channel
	var err error

	if params.ChannelID != "" {
		id, parseErr := uuid.Parse(params.ChannelID)
		if parseErr != nil {
			c.sendError(req.ID, protocol.CodeInvalidParams, "invalid channel_id")
			return
		}
		channel, err = c.hub.channels.store.GetChannel(id)
	} else if params.Name != "" {
		channel, err = c.hub.channels.store.GetChannelByName(params.Name)
	} else {
		c.sendError(req.ID, protocol.CodeInvalidParams, "channel_id or name required")
		return
	}

	if err != nil {
		c.sendError(req.ID, protocol.CodeInvalidParams, "channel not found")
		return
	}

	// Owner cannot leave (must delete channel)
	if channel.IsOwner(c.DID) {
		c.sendError(req.ID, protocol.CodeInvalidParams, "owner cannot leave, delete channel instead")
		return
	}

	if !channel.RemoveMember(c.DID) {
		c.sendResult(req.ID, map[string]string{"status": "not_member"})
		return
	}

	if err := c.hub.channels.store.UpdateChannel(channel); err != nil {
		c.sendError(req.ID, protocol.CodeInternalError, err.Error())
		return
	}

	c.sendResult(req.ID, map[string]string{"status": "left"})
	c.hub.logger.Info("member left channel", "channel", channel.Name, "member", c.DID)
}

// handleChannelList handles relay.channel.list requests.
func (c *Client) handleChannelList(req *protocol.JSONRPCRequest) {
	if c.DID == "" {
		c.sendError(req.ID, protocol.CodeSenderNotRegistered, "must register first")
		return
	}

	if c.hub.channels == nil {
		c.sendError(req.ID, protocol.CodeInternalError, "channels not supported")
		return
	}

	channels, err := c.hub.channels.store.ListChannels(c.DID)
	if err != nil {
		c.sendError(req.ID, protocol.CodeInternalError, err.Error())
		return
	}

	// Return simplified channel info
	result := make([]map[string]any, len(channels))
	for i, ch := range channels {
		result[i] = map[string]any{
			"id":           ch.ID.String(),
			"name":         ch.Name,
			"type":         string(ch.Type),
			"owner":        ch.OwnerDID,
			"description":  ch.Description,
			"member_count": len(ch.Members),
		}
	}

	c.sendResult(req.ID, result)
}

// handleChannelMembers handles relay.channel.members requests.
func (c *Client) handleChannelMembers(req *protocol.JSONRPCRequest) {
	if c.DID == "" {
		c.sendError(req.ID, protocol.CodeSenderNotRegistered, "must register first")
		return
	}

	var params struct {
		ChannelID string `json:"channel_id,omitempty"`
		Name      string `json:"name,omitempty"`
	}
	if err := req.ParseParams(&params); err != nil {
		c.sendError(req.ID, protocol.CodeInvalidParams, "invalid params")
		return
	}

	if c.hub.channels == nil {
		c.sendError(req.ID, protocol.CodeInternalError, "channels not supported")
		return
	}

	var channel *registry.Channel
	var err error

	if params.ChannelID != "" {
		id, parseErr := uuid.Parse(params.ChannelID)
		if parseErr != nil {
			c.sendError(req.ID, protocol.CodeInvalidParams, "invalid channel_id")
			return
		}
		channel, err = c.hub.channels.store.GetChannel(id)
	} else if params.Name != "" {
		channel, err = c.hub.channels.store.GetChannelByName(params.Name)
	} else {
		c.sendError(req.ID, protocol.CodeInvalidParams, "channel_id or name required")
		return
	}

	if err != nil {
		c.sendError(req.ID, protocol.CodeInvalidParams, "channel not found")
		return
	}

	// Only members can see member list
	if !channel.IsMember(c.DID) {
		c.sendError(req.ID, protocol.CodeAccessDenied, "not a member")
		return
	}

	members := make([]map[string]any, len(channel.Members))
	for i, m := range channel.Members {
		members[i] = map[string]any{
			"did":       m.DID,
			"role":      string(m.Role),
			"joined_at": m.JoinedAt,
		}
	}

	c.sendResult(req.ID, members)
}

// handleChannelDelete handles relay.channel.delete requests.
func (c *Client) handleChannelDelete(req *protocol.JSONRPCRequest) {
	if c.DID == "" {
		c.sendError(req.ID, protocol.CodeSenderNotRegistered, "must register first")
		return
	}

	var params struct {
		ChannelID string `json:"channel_id,omitempty"`
		Name      string `json:"name,omitempty"`
	}
	if err := req.ParseParams(&params); err != nil {
		c.sendError(req.ID, protocol.CodeInvalidParams, "invalid params")
		return
	}

	if c.hub.channels == nil {
		c.sendError(req.ID, protocol.CodeInternalError, "channels not supported")
		return
	}

	var channel *registry.Channel
	var err error

	if params.ChannelID != "" {
		id, parseErr := uuid.Parse(params.ChannelID)
		if parseErr != nil {
			c.sendError(req.ID, protocol.CodeInvalidParams, "invalid channel_id")
			return
		}
		channel, err = c.hub.channels.store.GetChannel(id)
	} else if params.Name != "" {
		channel, err = c.hub.channels.store.GetChannelByName(params.Name)
	} else {
		c.sendError(req.ID, protocol.CodeInvalidParams, "channel_id or name required")
		return
	}

	if err != nil {
		c.sendError(req.ID, protocol.CodeInvalidParams, "channel not found")
		return
	}

	// Only owner can delete
	if !channel.IsOwner(c.DID) {
		c.sendError(req.ID, protocol.CodeAccessDenied, "only owner can delete channel")
		return
	}

	if err := c.hub.channels.store.DeleteChannel(channel.ID); err != nil {
		c.sendError(req.ID, protocol.CodeInternalError, err.Error())
		return
	}

	// Notify all members
	notification := map[string]any{
		"channel_id": channel.ID.String(),
		"name":       channel.Name,
		"event":      "deleted",
	}
	notifReq, _ := protocol.NewNotification("relay.channel.deleted", notification)
	data, _ := protocol.Encode(notifReq)

	for _, member := range channel.Members {
		c.hub.mu.RLock()
		client, exists := c.hub.clients[member.DID]
		c.hub.mu.RUnlock()
		if exists {
			select {
			case client.SendCh <- data:
			default:
			}
		}
	}

	c.sendResult(req.ID, map[string]string{"status": "deleted"})
	c.hub.logger.Info("channel deleted", "name", channel.Name, "by", c.DID)
}

// handleSenderKeyDistribute handles relay.channel.sender_key requests.
// Members distribute their sender keys to other members for E2E encryption.
func (c *Client) handleSenderKeyDistribute(req *protocol.JSONRPCRequest) {
	if c.DID == "" {
		c.sendError(req.ID, protocol.CodeSenderNotRegistered, "must register first")
		return
	}

	var params struct {
		ChannelID    string `json:"channel_id,omitempty"`
		Name         string `json:"name,omitempty"`
		ChainKey     []byte `json:"chain_key"`
		SignatureKey []byte `json:"signature_key"`
	}
	if err := req.ParseParams(&params); err != nil {
		c.sendError(req.ID, protocol.CodeInvalidParams, "invalid params")
		return
	}

	if c.hub.channels == nil {
		c.sendError(req.ID, protocol.CodeInternalError, "channels not supported")
		return
	}

	var channel *registry.Channel
	var err error

	if params.ChannelID != "" {
		id, parseErr := uuid.Parse(params.ChannelID)
		if parseErr != nil {
			c.sendError(req.ID, protocol.CodeInvalidParams, "invalid channel_id")
			return
		}
		channel, err = c.hub.channels.store.GetChannel(id)
	} else if params.Name != "" {
		channel, err = c.hub.channels.store.GetChannelByName(params.Name)
	} else {
		c.sendError(req.ID, protocol.CodeInvalidParams, "channel_id or name required")
		return
	}

	if err != nil {
		c.sendError(req.ID, protocol.CodeInvalidParams, "channel not found")
		return
	}

	if !channel.IsMember(c.DID) {
		c.sendError(req.ID, protocol.CodeAccessDenied, "not a member")
		return
	}

	// Store sender key
	senderKey := &registry.SenderKey{
		ChainKey:     params.ChainKey,
		SignatureKey: params.SignatureKey,
		Iteration:    0,
	}
	channel.SetSenderKey(c.DID, senderKey)
	if err := c.hub.channels.store.UpdateChannel(channel); err != nil {
		c.sendError(req.ID, protocol.CodeInternalError, err.Error())
		return
	}

	// Notify other members about the new sender key
	// Each member needs to store this key to decrypt messages from this sender
	notification := map[string]any{
		"channel_id":    channel.ID.String(),
		"sender_did":    c.DID,
		"chain_key":     params.ChainKey,
		"signature_key": params.SignatureKey,
	}
	notifReq, _ := protocol.NewNotification("relay.channel.sender_key", notification)
	data, _ := protocol.Encode(notifReq)

	for _, member := range channel.Members {
		if member.DID == c.DID {
			continue // Skip sender
		}
		c.hub.mu.RLock()
		client, exists := c.hub.clients[member.DID]
		c.hub.mu.RUnlock()
		if exists {
			select {
			case client.SendCh <- data:
			default:
			}
		}
	}

	c.sendResult(req.ID, map[string]string{"status": "distributed"})
}

// Placeholder for JSON import usage check
var _ = json.Marshal
