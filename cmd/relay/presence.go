package main

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/gianluca/msg2agent/pkg/messaging"
	"github.com/gianluca/msg2agent/pkg/protocol"
	"github.com/gianluca/msg2agent/pkg/registry"
)

// PresenceManager manages presence state and subscriptions.
type PresenceManager struct {
	hub *RelayHub

	// Presence state per DID
	presence map[string]*registry.PresenceInfo
	mu       sync.RWMutex

	// Subscriptions: subscriber DID -> set of target DIDs
	subscriptions map[string]map[string]bool
	subMu         sync.RWMutex

	// Typing indicators: sender DID -> thread -> expiry time
	typingState map[string]map[string]time.Time
	typingMu    sync.RWMutex

	stopCh chan struct{}
}

// NewPresenceManager creates a new presence manager.
func NewPresenceManager(hub *RelayHub) *PresenceManager {
	pm := &PresenceManager{
		hub:           hub,
		presence:      make(map[string]*registry.PresenceInfo),
		subscriptions: make(map[string]map[string]bool),
		typingState:   make(map[string]map[string]time.Time),
		stopCh:        make(chan struct{}),
	}

	// Start typing expiry cleanup
	go pm.typingCleanupLoop()

	return pm
}

// UpdatePresence updates an agent's presence and notifies subscribers.
func (pm *PresenceManager) UpdatePresence(did string, status registry.AgentStatus, statusText string) {
	pm.mu.Lock()
	info, ok := pm.presence[did]
	if !ok {
		info = registry.NewPresenceInfo()
		pm.presence[did] = info
	}
	info.Status = status
	info.StatusText = statusText
	info.UpdateActivity()
	pm.mu.Unlock()

	// Notify subscribers
	pm.notifyPresenceChange(did, info)
}

// GetPresence returns the current presence for a DID.
func (pm *PresenceManager) GetPresence(did string) *registry.PresenceInfo {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if info, ok := pm.presence[did]; ok {
		// Return a copy
		cpy := *info
		return &cpy
	}
	return nil
}

// Subscribe adds a presence subscription.
func (pm *PresenceManager) Subscribe(subscriberDID, targetDID string) {
	pm.subMu.Lock()
	defer pm.subMu.Unlock()

	if pm.subscriptions[subscriberDID] == nil {
		pm.subscriptions[subscriberDID] = make(map[string]bool)
	}
	pm.subscriptions[subscriberDID][targetDID] = true
}

// Unsubscribe removes a presence subscription.
func (pm *PresenceManager) Unsubscribe(subscriberDID, targetDID string) {
	pm.subMu.Lock()
	defer pm.subMu.Unlock()

	if subs, ok := pm.subscriptions[subscriberDID]; ok {
		delete(subs, targetDID)
	}
}

// UnsubscribeAll removes all subscriptions for a subscriber.
func (pm *PresenceManager) UnsubscribeAll(subscriberDID string) {
	pm.subMu.Lock()
	defer pm.subMu.Unlock()

	delete(pm.subscriptions, subscriberDID)
}

// SetTyping sets the typing state for a sender in a thread.
func (pm *PresenceManager) SetTyping(senderDID, threadID string, typing bool) {
	pm.typingMu.Lock()
	defer pm.typingMu.Unlock()

	if typing {
		if pm.typingState[senderDID] == nil {
			pm.typingState[senderDID] = make(map[string]time.Time)
		}
		// Typing expires after 10 seconds
		pm.typingState[senderDID][threadID] = time.Now().Add(10 * time.Second)
	} else {
		if threads, ok := pm.typingState[senderDID]; ok {
			delete(threads, threadID)
		}
	}
}

// IsTyping returns true if the sender is typing in the thread.
func (pm *PresenceManager) IsTyping(senderDID, threadID string) bool {
	pm.typingMu.RLock()
	defer pm.typingMu.RUnlock()

	if threads, ok := pm.typingState[senderDID]; ok {
		if expiry, ok := threads[threadID]; ok {
			return time.Now().Before(expiry)
		}
	}
	return false
}

// notifyPresenceChange sends presence updates to all subscribers.
func (pm *PresenceManager) notifyPresenceChange(targetDID string, info *registry.PresenceInfo) {
	pm.subMu.RLock()
	defer pm.subMu.RUnlock()

	// Find all subscribers interested in this target
	for subscriberDID, targets := range pm.subscriptions {
		if targets[targetDID] {
			pm.sendPresenceNotification(subscriberDID, targetDID, info)
		}
	}
}

// sendPresenceNotification sends a presence notification to a subscriber.
func (pm *PresenceManager) sendPresenceNotification(subscriberDID, targetDID string, info *registry.PresenceInfo) {
	pm.hub.mu.RLock()
	client, ok := pm.hub.clients[subscriberDID]
	pm.hub.mu.RUnlock()

	if !ok {
		return
	}

	notification := PresenceNotification{
		DID:        targetDID,
		Status:     string(info.Status),
		StatusText: info.StatusText,
	}

	notifReq, _ := protocol.NewNotification("relay.presence.update", notification)
	data, _ := protocol.Encode(notifReq)

	select {
	case client.SendCh <- data:
	default:
		// Client buffer full, skip notification
	}
}

// PresenceNotification is the notification payload for presence updates.
type PresenceNotification struct {
	DID        string `json:"did"`
	Status     string `json:"status"`
	StatusText string `json:"status_text,omitempty"`
}

// typingCleanupLoop periodically cleans up expired typing states.
func (pm *PresenceManager) typingCleanupLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			pm.cleanupExpiredTyping()
		case <-pm.stopCh:
			return
		}
	}
}

// cleanupExpiredTyping removes expired typing states.
func (pm *PresenceManager) cleanupExpiredTyping() {
	pm.typingMu.Lock()
	defer pm.typingMu.Unlock()

	now := time.Now()
	for senderDID, threads := range pm.typingState {
		for threadID, expiry := range threads {
			if now.After(expiry) {
				delete(threads, threadID)
			}
		}
		if len(threads) == 0 {
			delete(pm.typingState, senderDID)
		}
	}
}

// Stop stops the presence manager.
func (pm *PresenceManager) Stop() {
	close(pm.stopCh)
}

// Relay method handlers for presence

// handlePresenceUpdate handles relay.presence.update requests.
func (c *Client) handlePresenceUpdate(req *protocol.JSONRPCRequest) {
	if c.DID == "" {
		c.sendError(req.ID, protocol.CodeSenderNotRegistered, "must register first")
		return
	}

	var params struct {
		Status     string `json:"status"`
		StatusText string `json:"status_text,omitempty"`
	}
	if err := req.ParseParams(&params); err != nil {
		c.sendError(req.ID, protocol.CodeInvalidParams, "invalid params")
		return
	}

	// Validate status
	status := registry.AgentStatus(params.Status)
	switch status {
	case registry.StatusOnline, registry.StatusOffline, registry.StatusBusy,
		registry.StatusAway, registry.StatusDND:
		// Valid
	default:
		c.sendError(req.ID, protocol.CodeInvalidParams, "invalid status")
		return
	}

	if c.hub.presence != nil {
		c.hub.presence.UpdatePresence(c.DID, status, params.StatusText)
	}

	c.sendResult(req.ID, map[string]string{"status": "updated"})
}

// handlePresenceSubscribe handles relay.presence.subscribe requests.
func (c *Client) handlePresenceSubscribe(req *protocol.JSONRPCRequest) {
	if c.DID == "" {
		c.sendError(req.ID, protocol.CodeSenderNotRegistered, "must register first")
		return
	}

	var params struct {
		DIDs []string `json:"dids"`
	}
	if err := req.ParseParams(&params); err != nil {
		c.sendError(req.ID, protocol.CodeInvalidParams, "invalid params")
		return
	}

	if c.hub.presence != nil {
		for _, targetDID := range params.DIDs {
			c.hub.presence.Subscribe(c.DID, targetDID)
		}
	}

	c.sendResult(req.ID, map[string]string{"status": "subscribed"})
}

// handlePresenceUnsubscribe handles relay.presence.unsubscribe requests.
func (c *Client) handlePresenceUnsubscribe(req *protocol.JSONRPCRequest) {
	if c.DID == "" {
		c.sendError(req.ID, protocol.CodeSenderNotRegistered, "must register first")
		return
	}

	var params struct {
		DIDs []string `json:"dids"`
	}
	if err := req.ParseParams(&params); err != nil {
		c.sendError(req.ID, protocol.CodeInvalidParams, "invalid params")
		return
	}

	if c.hub.presence != nil {
		for _, targetDID := range params.DIDs {
			c.hub.presence.Unsubscribe(c.DID, targetDID)
		}
	}

	c.sendResult(req.ID, map[string]string{"status": "unsubscribed"})
}

// handlePresenceQuery handles relay.presence.query requests.
func (c *Client) handlePresenceQuery(req *protocol.JSONRPCRequest) {
	if c.DID == "" {
		c.sendError(req.ID, protocol.CodeSenderNotRegistered, "must register first")
		return
	}

	var params struct {
		DIDs []string `json:"dids"`
	}
	if err := req.ParseParams(&params); err != nil {
		c.sendError(req.ID, protocol.CodeInvalidParams, "invalid params")
		return
	}

	results := make(map[string]*PresenceNotification)
	if c.hub.presence != nil {
		for _, targetDID := range params.DIDs {
			if info := c.hub.presence.GetPresence(targetDID); info != nil {
				results[targetDID] = &PresenceNotification{
					DID:        targetDID,
					Status:     string(info.Status),
					StatusText: info.StatusText,
				}
			}
		}
	}

	c.sendResult(req.ID, results)
}

// handleTypingIndicator handles incoming typing indicator messages.
func (c *Client) handleTypingIndicator(msg *messaging.Message, data []byte) {
	if c.hub.presence == nil {
		return
	}

	var indicator messaging.TypingIndicator
	if err := json.Unmarshal(msg.Body, &indicator); err != nil {
		return
	}

	threadID := ""
	if msg.ThreadID != nil {
		threadID = msg.ThreadID.String()
	}

	c.hub.presence.SetTyping(msg.From, threadID, indicator.Typing)

	// Forward typing indicator to recipient
	_ = c.hub.Route(msg, data)
}
