// Package main provides the relay hub executable.
package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/gianluca/msg2agent/pkg/config"
	"github.com/gianluca/msg2agent/pkg/crypto"
	"github.com/gianluca/msg2agent/pkg/identity"
	"github.com/gianluca/msg2agent/pkg/messaging"
	"github.com/gianluca/msg2agent/pkg/protocol"
	"github.com/gianluca/msg2agent/pkg/queue"
	"github.com/gianluca/msg2agent/pkg/registry"
	"github.com/gianluca/msg2agent/pkg/security"
	"github.com/gianluca/msg2agent/pkg/telemetry"
)

var (
	ErrClientNotRegistered = fmt.Errorf("client not registered")
	ErrSenderMismatch      = fmt.Errorf("sender mismatch")
	ErrRateLimited         = fmt.Errorf("rate limit exceeded")
	ErrMaxConnections      = fmt.Errorf("max connections reached")
	ErrDIDProofRequired    = fmt.Errorf("DID ownership proof required")
	ErrDIDProofInvalid     = fmt.Errorf("DID ownership proof invalid")
	ErrInvalidDIDFormat    = fmt.Errorf("invalid DID format: must be did:wba:*")
	ErrInvalidPath         = fmt.Errorf("invalid or unsafe file path")
)

// validateAndCleanPath validates a file path and returns a clean, absolute path.
// It rejects paths that contain traversal attempts or are otherwise unsafe.
func validateAndCleanPath(path string, logger *slog.Logger) (string, error) {
	if path == "" || path == ":memory:" {
		return path, nil // Empty path or in-memory database is OK
	}

	// Clean the path to remove any .. or . components
	cleanPath := filepath.Clean(path)

	// Convert to absolute path
	absPath, err := filepath.Abs(cleanPath)
	if err != nil {
		return "", fmt.Errorf("%w: failed to resolve absolute path: %v", ErrInvalidPath, err)
	}

	// Check for suspicious patterns in the original path
	if strings.Contains(path, "..") {
		logger.Warn("path contains directory traversal", "original", path, "resolved", absPath)
	}

	// Verify the resolved path doesn't escape expected directories
	// Allow paths that start with current directory or home directory
	cwd, _ := os.Getwd()
	home, _ := os.UserHomeDir()

	// Basic sanity check: path should be under cwd, home, or /tmp
	if !strings.HasPrefix(absPath, cwd) &&
		!strings.HasPrefix(absPath, home) &&
		!strings.HasPrefix(absPath, "/tmp") &&
		!strings.HasPrefix(absPath, os.TempDir()) {
		logger.Warn("file path is outside expected directories",
			"path", absPath, "cwd", cwd, "home", home)
	}

	return absPath, nil
}

// RegistrationRequest extends registry.Agent with proof of DID ownership.
type RegistrationRequest struct {
	registry.Agent

	// Proof contains a signature of (DID + Timestamp) using the agent's signing key.
	// This proves the registering party controls the private key for the DID.
	Proof     []byte `json:"proof,omitempty"`
	Timestamp int64  `json:"timestamp,omitempty"` // Unix timestamp used in proof
}

// RelayConfig holds relay hub configuration.
type RelayConfig struct {
	// Connection limits
	MaxConnections int

	// Rate limiting (per client)
	MessageRateLimit  float64 // messages per second
	MessageBurstSize  float64 // max burst
	RegisterRateLimit float64 // registrations per second
	DiscoverRateLimit float64 // discover requests per second

	// Timeouts
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration

	// Offline queue configuration
	EnableOfflineQueue bool
	QueueConfig        queue.Config

	// CORS configuration
	AllowedOrigins []string // Allowed CORS origins (empty = deny all external origins)

	// Security
	RequireDIDProof bool // Require agents to prove DID ownership during registration
}

// DefaultRelayConfig returns sensible defaults.
func DefaultRelayConfig() RelayConfig {
	return RelayConfig{
		MaxConnections:     1000,
		MessageRateLimit:   100.0, // 100 msg/sec
		MessageBurstSize:   200.0, // burst of 200
		RegisterRateLimit:  1.0,   // 1 reg/sec
		DiscoverRateLimit:  10.0,  // 10 discover/sec
		ReadTimeout:        5 * time.Minute,
		WriteTimeout:       10 * time.Second,
		IdleTimeout:        5 * time.Minute,
		EnableOfflineQueue: true,
		QueueConfig:        queue.DefaultConfig(),
		AllowedOrigins:     nil,  // Secure default: no external origins allowed
		RequireDIDProof:    true, // Secure default: require proof of DID ownership
	}
}

// Validate checks that the config has valid values.
func (c *RelayConfig) Validate() error {
	if c.MaxConnections <= 0 {
		return fmt.Errorf("max_connections must be positive, got %d", c.MaxConnections)
	}
	if c.MessageRateLimit <= 0 {
		return fmt.Errorf("message_rate_limit must be positive, got %f", c.MessageRateLimit)
	}
	if c.MessageBurstSize <= 0 {
		return fmt.Errorf("message_burst_size must be positive, got %f", c.MessageBurstSize)
	}
	if c.RegisterRateLimit <= 0 {
		return fmt.Errorf("register_rate_limit must be positive, got %f", c.RegisterRateLimit)
	}
	if c.DiscoverRateLimit <= 0 {
		return fmt.Errorf("discover_rate_limit must be positive, got %f", c.DiscoverRateLimit)
	}
	if c.ReadTimeout <= 0 {
		return fmt.Errorf("read_timeout must be positive, got %v", c.ReadTimeout)
	}
	if c.WriteTimeout <= 0 {
		return fmt.Errorf("write_timeout must be positive, got %v", c.WriteTimeout)
	}
	return nil
}

// RelayHub manages connections between agents.
type RelayHub struct {
	config      RelayConfig
	store       registry.Store
	queue       queue.Store      // Offline message queue
	presence    *PresenceManager // Presence manager
	channels    *ChannelManager  // Channel manager
	clients     map[string]*Client
	mu          sync.RWMutex
	logger      *slog.Logger
	connections atomic.Int32       // current connection count
	ctx         context.Context    // Hub-level context, canceled on Stop
	ctxCancel   context.CancelFunc // Cancel function for hub context
	stopCh      chan struct{}
	wg          sync.WaitGroup // Track background goroutines for clean shutdown

	// DID allowlist enforcement
	aclEnforcer  *security.ACLEnforcer
	allowlistACL *registry.ACLPolicy // nil = open relay (no allowlist)
}

// Client represents a connected agent.
type Client struct {
	ID     string
	DID    string
	Conn   *websocket.Conn
	SendCh chan []byte
	hub    *RelayHub

	// Rate limiters
	msgLimiter      *messaging.RateLimiter
	registerLimiter *messaging.RateLimiter
	discoverLimiter *messaging.RateLimiter

	// Connection tracking
	lastActivity time.Time
	mu           sync.Mutex

	// Stopped flag to prevent sends on closed channel
	stopped atomic.Bool

	// Context for cancellation
	ctx    context.Context
	cancel context.CancelFunc
}

// NewRelayHub creates a new relay hub with a memory store.
func NewRelayHub(config RelayConfig, logger *slog.Logger) *RelayHub {
	return NewRelayHubWithStore(config, registry.NewMemoryStore(), nil, logger)
}

// NewRelayHubWithStore creates a new relay hub with a custom store.
func NewRelayHubWithStore(config RelayConfig, store registry.Store, queueStore queue.Store, logger *slog.Logger) *RelayHub {
	ctx, ctxCancel := context.WithCancel(context.Background())
	hub := &RelayHub{
		config:    config,
		store:     store,
		queue:     queueStore,
		clients:   make(map[string]*Client),
		logger:    logger,
		ctx:       ctx,
		ctxCancel: ctxCancel,
		stopCh:    make(chan struct{}),
	}

	// Create default queue store if enabled and none provided
	if config.EnableOfflineQueue && queueStore == nil {
		hub.queue = queue.NewMemoryStore(config.QueueConfig)
	}

	// Start queue cleanup goroutine
	if hub.queue != nil {
		hub.wg.Add(1)
		go func() {
			defer hub.wg.Done()
			hub.queueCleanupLoop()
		}()
	}

	// Initialize presence manager
	hub.presence = NewPresenceManager(hub)

	// Initialize channel manager
	hub.channels = NewChannelManager(hub)

	return hub
}

// queueCleanupLoop periodically cleans up expired messages.
func (h *RelayHub) queueCleanupLoop() {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if h.queue != nil {
				if removed, err := h.queue.Cleanup(); err != nil {
					h.logger.Error("queue cleanup failed", "error", err)
				} else if removed > 0 {
					h.logger.Info("cleaned up expired queued messages", "count", removed)
				}
			}
		case <-h.stopCh:
			return
		}
	}
}

// Stop gracefully stops the relay hub.
func (h *RelayHub) Stop() {
	h.ctxCancel() // Cancel hub context, propagates to all client contexts
	close(h.stopCh)

	// Close all client connections to unblock writePump goroutines
	// Use a map to track closed channels to avoid double-close
	// (clients can be in the map twice: by ID and by DID)
	h.mu.Lock()
	closed := make(map[*Client]bool)
	for _, client := range h.clients {
		if !closed[client] {
			client.stopped.Store(true) // Prevent new sends before closing
			close(client.SendCh)       // This will cause writePump to exit
			closed[client] = true
		}
	}
	h.mu.Unlock()

	// Wait for goroutines with timeout
	done := make(chan struct{})
	go func() {
		h.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		h.logger.Info("all goroutines stopped")
	case <-time.After(5 * time.Second):
		h.logger.Warn("goroutine shutdown timeout")
	}

	if h.queue != nil {
		_ = h.queue.Close()
	}
	if h.presence != nil {
		h.presence.Stop()
	}
}

// Register registers a new client.
func (h *RelayHub) Register(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[client.ID] = client
	if client.DID != "" {
		h.clients[client.DID] = client
	}
	h.logger.Info("client registered", "id", client.ID, "did", client.DID)
}

// Unregister removes a client.
func (h *RelayHub) Unregister(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, client.ID)
	if client.DID != "" {
		delete(h.clients, client.DID)
	}
	h.logger.Info("client unregistered", "id", client.ID)
}

// Route routes a message to the appropriate client.
// If the recipient is offline and offline queue is enabled, the message is queued.
func (h *RelayHub) Route(msg *messaging.Message, data []byte) error {
	// Check if routing to a channel
	if IsChannelURI(msg.To) && h.channels != nil {
		return h.channels.RouteToChannel(msg, data)
	}

	h.mu.RLock()
	client, exists := h.clients[msg.To]
	senderClient := h.clients[msg.From]
	h.mu.RUnlock()

	if !exists {
		// Try to queue for offline delivery
		if h.queue != nil && h.config.EnableOfflineQueue {
			queuedMsg := &queue.QueuedMessage{
				ID:           msg.ID,
				RecipientDID: msg.To,
				SenderDID:    msg.From,
				Data:         data,
				QueuedAt:     time.Now(),
				ExpiresAt:    time.Now().Add(h.config.QueueConfig.MessageTTL),
			}

			if err := h.queue.Enqueue(queuedMsg); err != nil {
				if errors.Is(err, queue.ErrQueueFull) {
					h.logger.Warn("offline queue full", "to", msg.To)
					recordMessageDropped("queue_full")
				} else {
					h.logger.Error("failed to queue message", "error", err, "to", msg.To)
					recordMessageDropped("queue_error")
				}
				// Send negative ack if requested
				if msg.RequestAck && senderClient != nil {
					h.sendDeliveryAck(senderClient, msg.ID, false, "queue failed")
				}
				return fmt.Errorf("recipient offline and queue failed: %w", err)
			}

			h.logger.Debug("message queued for offline delivery", "from", msg.From, "to", msg.To, "id", msg.ID)
			recordMessageQueued()

			// Send ack indicating queued (not delivered yet) if requested
			if msg.RequestAck && senderClient != nil {
				h.sendDeliveryAck(senderClient, msg.ID, true, "queued")
			}
			return nil // Success - message queued
		}

		h.logger.Warn("recipient not found", "to", msg.To)
		recordMessageDropped("recipient_not_found")

		// Send negative ack if requested
		if msg.RequestAck && senderClient != nil {
			h.sendDeliveryAck(senderClient, msg.ID, false, "recipient not found")
		}
		return fmt.Errorf("recipient not found: %s", msg.To)
	}

	select {
	case client.SendCh <- data:
		h.logger.Debug("message routed", "from", msg.From, "to", msg.To, "method", msg.Method)
		recordMessageRouted()

		// Send delivery ack if requested
		if msg.RequestAck && senderClient != nil {
			h.sendDeliveryAck(senderClient, msg.ID, true, "delivered")
		}
		return nil
	default:
		h.logger.Warn("client send buffer full", "id", client.ID)
		recordMessageDropped("buffer_full")

		// Send negative ack if requested
		if msg.RequestAck && senderClient != nil {
			h.sendDeliveryAck(senderClient, msg.ID, false, "buffer full")
		}
		return fmt.Errorf("client send buffer full")
	}
}

// sendDeliveryAck sends a delivery acknowledgment notification to the sender.
func (h *RelayHub) sendDeliveryAck(sender *Client, messageID uuid.UUID, delivered bool, status string) {
	ackPayload := map[string]any{
		"message_id": messageID.String(),
		"delivered":  delivered,
		"status":     status,
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
	}

	notification, err := protocol.NewNotification("relay.ack", ackPayload)
	if err != nil {
		h.logger.Error("failed to create ack notification", "error", err)
		return
	}

	data, err := protocol.Encode(notification)
	if err != nil {
		h.logger.Error("failed to encode ack notification", "error", err)
		return
	}

	select {
	case sender.SendCh <- data:
		h.logger.Debug("sent delivery ack", "message_id", messageID, "delivered", delivered, "status", status)
	default:
		h.logger.Warn("failed to send delivery ack, buffer full", "message_id", messageID)
	}
}

// Broadcast sends a message to all connected clients except the sender.
func (h *RelayHub) Broadcast(senderID string, data []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	seen := make(map[*Client]bool)
	for _, client := range h.clients {
		if client.ID != senderID && !seen[client] {
			seen[client] = true
			if client.stopped.Load() {
				continue
			}
			select {
			case client.SendCh <- data:
			default:
			}
		}
	}
}

// handleWebSocket handles a WebSocket connection.
func (h *RelayHub) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Check connection limit
	if int(h.connections.Load()) >= h.config.MaxConnections {
		h.logger.Warn("max connections reached", "max", h.config.MaxConnections)
		recordConnectionRejected()
		http.Error(w, "max connections reached", http.StatusServiceUnavailable)
		return
	}

	// Configure CORS: use allowed origins or reject cross-origin requests
	acceptOpts := &websocket.AcceptOptions{}
	if len(h.config.AllowedOrigins) > 0 {
		acceptOpts.OriginPatterns = h.config.AllowedOrigins
	}
	// When AllowedOrigins is nil/empty, OriginPatterns defaults to same-origin only

	conn, err := websocket.Accept(w, r, acceptOpts)
	if err != nil {
		h.logger.Error("websocket accept failed", "error", err)
		recordError("websocket_accept")
		return
	}

	// Increment connection count
	h.connections.Add(1)
	recordConnectionAccepted()

	// Create client context derived from hub context, canceled when connection closes or hub stops
	ctx, cancel := context.WithCancel(h.ctx)

	client := &Client{
		ID:              uuid.New().String(),
		Conn:            conn,
		SendCh:          make(chan []byte, 256),
		hub:             h,
		msgLimiter:      messaging.NewRateLimiter(h.config.MessageRateLimit, h.config.MessageBurstSize),
		registerLimiter: messaging.NewRateLimiter(h.config.RegisterRateLimit, 2),
		discoverLimiter: messaging.NewRateLimiter(h.config.DiscoverRateLimit, 20),
		lastActivity:    time.Now(),
		ctx:             ctx,
		cancel:          cancel,
	}

	h.Register(client)

	// Start writer goroutine with WaitGroup tracking
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		client.writePump()
	}()

	// Handle incoming messages
	client.readPump()

	// Cleanup: cancel context to stop writePump and close connection
	client.cancel()
	h.Unregister(client)
	h.connections.Add(-1)
	recordConnectionClosed()
	_ = conn.Close(websocket.StatusNormalClosure, "") // Best effort close
}

// readPump reads messages from the WebSocket connection.
func (c *Client) readPump() {
	for {
		// Create context with read timeout, inheriting from client context
		ctx, cancel := context.WithTimeout(c.ctx, c.hub.config.ReadTimeout)
		_, data, err := c.Conn.Read(ctx)
		cancel()

		if err != nil {
			c.hub.logger.Debug("read error", "id", c.ID, "error", err)
			return
		}

		c.updateActivity()
		c.handleMessage(data)
	}
}

// updateActivity updates the last activity timestamp.
func (c *Client) updateActivity() {
	c.mu.Lock()
	c.lastActivity = time.Now()
	c.mu.Unlock()
}

// writePump writes messages to the WebSocket connection.
func (c *Client) writePump() {
	for {
		select {
		case <-c.ctx.Done():
			return
		case data, ok := <-c.SendCh:
			if !ok {
				return // Channel closed
			}
			// Create context with write timeout, inheriting from client context
			ctx, cancel := context.WithTimeout(c.ctx, c.hub.config.WriteTimeout)
			err := c.Conn.Write(ctx, websocket.MessageBinary, data)
			cancel()

			if err != nil {
				c.hub.logger.Debug("write error", "id", c.ID, "error", err)
				return
			}
		}
	}
}

// validateSender checks that the client is registered and the message From field matches.
func (c *Client) validateSender(msg *messaging.Message) error {
	if c.DID == "" {
		return ErrClientNotRegistered
	}
	if msg.From != c.DID {
		return fmt.Errorf("%w: message from %q but client registered as %q", ErrSenderMismatch, msg.From, c.DID)
	}
	return nil
}

// handleMessage processes an incoming message.
func (c *Client) handleMessage(data []byte) {
	// Try to decode as JSON-RPC request
	req, err := protocol.DecodeRequest(data)
	if err != nil {
		c.hub.logger.Warn("invalid message", "error", err)
		return
	}

	// Handle special relay methods
	switch req.Method {
	case "keepalive":
		// Keepalive notification - just ignore, the read already reset the timeout
		return
	case "relay.register":
		c.handleRegister(req)
		return
	case "relay.discover":
		c.handleDiscover(req)
		return
	case "relay.lookup":
		c.handleLookup(req)
		return
	case "relay.presence.update":
		c.handlePresenceUpdate(req)
		return
	case "relay.presence.subscribe":
		c.handlePresenceSubscribe(req)
		return
	case "relay.presence.unsubscribe":
		c.handlePresenceUnsubscribe(req)
		return
	case "relay.presence.query":
		c.handlePresenceQuery(req)
		return
	case "relay.channel.create":
		c.handleChannelCreate(req)
		return
	case "relay.channel.join":
		c.handleChannelJoin(req)
		return
	case "relay.channel.leave":
		c.handleChannelLeave(req)
		return
	case "relay.channel.list":
		c.handleChannelList(req)
		return
	case "relay.channel.members":
		c.handleChannelMembers(req)
		return
	case "relay.channel.delete":
		c.handleChannelDelete(req)
		return
	case "relay.channel.sender_key":
		c.handleSenderKeyDistribute(req)
		return
	}

	// Check message rate limit
	if !c.msgLimiter.Allow() {
		c.hub.logger.Warn("message rate limit exceeded", "client_id", c.ID)
		recordRateLimitHit("message")
		c.sendError(req.ID, protocol.CodeRateLimited, "rate limit exceeded")
		return
	}

	// Parse message params
	var msg messaging.Message
	if err := req.ParseParams(&msg); err != nil {
		c.sendError(req.ID, protocol.CodeInvalidParams, "invalid params")
		return
	}

	// Validate message fields
	if err := msg.Validate(); err != nil {
		c.sendError(req.ID, protocol.CodeInvalidParams, err.Error())
		return
	}

	// Validate sender - must be registered and message From must match
	if err := c.validateSender(&msg); err != nil {
		if errors.Is(err, ErrClientNotRegistered) {
			c.sendError(req.ID, protocol.CodeSenderNotRegistered, "client must register before sending messages")
		} else {
			c.sendError(req.ID, protocol.CodeSenderMismatch, err.Error())
		}
		c.hub.logger.Warn("sender validation failed", "error", err, "client_id", c.ID)
		return
	}

	// Handle typing indicator specially (update presence manager)
	if msg.Type == messaging.TypeTyping {
		c.handleTypingIndicator(&msg, data)
		return
	}

	// Route the message
	if err := c.hub.Route(&msg, data); err != nil {
		c.sendError(req.ID, protocol.CodeRoutingError, err.Error())
		return
	}
}

// handleRegister handles agent registration.
func (c *Client) handleRegister(req *protocol.JSONRPCRequest) {
	// Check register rate limit
	if !c.registerLimiter.Allow() {
		c.hub.logger.Warn("register rate limit exceeded", "client_id", c.ID)
		recordRateLimitHit("register")
		c.sendError(req.ID, protocol.CodeRateLimited, "registration rate limit exceeded")
		return
	}

	var regReq RegistrationRequest
	if err := req.ParseParams(&regReq); err != nil {
		c.sendError(req.ID, protocol.CodeInvalidParams, "invalid agent data")
		return
	}

	agent := &regReq.Agent

	// Validate DID format (must be did:wba:*)
	parsedDID, err := identity.ParseDID(agent.DID)
	if err != nil || parsedDID.Method != identity.MethodWBA {
		c.hub.logger.Warn("invalid DID format", "did", agent.DID)
		c.sendError(req.ID, protocol.CodeInvalidParams, ErrInvalidDIDFormat.Error())
		return
	}

	// Verify DID ownership proof if required
	if c.hub.config.RequireDIDProof {
		if err := c.verifyDIDProof(&regReq); err != nil {
			c.hub.logger.Warn("DID proof verification failed", "did", agent.DID, "error", err)
			c.sendError(req.ID, protocol.CodeSignatureInvalid, err.Error())
			return
		}
	}

	// Check DID allowlist if configured
	if c.hub.allowlistACL != nil {
		relay := &registry.Agent{ACL: c.hub.allowlistACL}
		if err := c.hub.aclEnforcer.CheckAccess(relay, agent.DID, "register"); err != nil {
			c.hub.logger.Warn("DID not in allowlist", "did", agent.DID)
			c.sendError(req.ID, protocol.CodeAccessDenied, "DID not allowed to register")
			return
		}
	}

	c.DID = agent.DID
	agent.SetOnline()
	_ = c.hub.store.Put(agent) // Best effort store update

	// Update client mapping
	c.hub.mu.Lock()
	c.hub.clients[agent.DID] = c
	c.hub.mu.Unlock()

	// Broadcast announcement
	announcement, _ := json.Marshal(registry.DiscoveryMessage{
		Type:    registry.DiscoveryAnnounce,
		AgentID: agent.ID,
		DID:     agent.DID,
		Agent:   agent,
	})
	announceReq, _ := protocol.NewNotification("discovery.announce", json.RawMessage(announcement))
	announceData, _ := protocol.Encode(announceReq)
	c.hub.Broadcast(c.ID, announceData)

	c.sendResult(req.ID, map[string]string{"status": "registered"})
	recordRegistration()
	c.hub.logger.Info("agent registered", "did", agent.DID)

	// Deliver any queued messages
	c.deliverQueuedMessages()
}

// verifyDIDProof verifies that the registering agent owns the claimed DID.
// The proof is a signature of (DID + Timestamp) using the agent's signing key.
func (c *Client) verifyDIDProof(regReq *RegistrationRequest) error {
	// Require proof to be present
	if len(regReq.Proof) == 0 {
		return ErrDIDProofRequired
	}

	// Get the agent's signing key
	signingKey := regReq.GetSigningKey()
	if signingKey == nil {
		return fmt.Errorf("no signing key provided")
	}

	// Verify timestamp is recent (within 5 minutes) to prevent replay attacks
	proofAge := time.Now().Unix() - regReq.Timestamp
	if proofAge < -60 || proofAge > 300 { // Allow 1 min clock skew, 5 min max age
		return fmt.Errorf("proof timestamp out of range")
	}

	// Reconstruct the signed message: DID + timestamp
	message := fmt.Sprintf("%s:%d", regReq.DID, regReq.Timestamp)

	// Verify the signature
	if !crypto.VerifySignature(signingKey.Key, []byte(message), regReq.Proof) {
		return ErrDIDProofInvalid
	}

	return nil
}

// deliverQueuedMessages delivers queued messages to the client.
func (c *Client) deliverQueuedMessages() {
	if c.hub.queue == nil || c.DID == "" {
		return
	}

	// Dequeue messages in batches
	const batchSize = 100
	for {
		msgs, err := c.hub.queue.Dequeue(c.DID, batchSize)
		if err != nil {
			c.hub.logger.Error("failed to dequeue messages", "error", err, "did", c.DID)
			return
		}

		if len(msgs) == 0 {
			return
		}

		for _, msg := range msgs {
			if msg.IsExpired() {
				continue
			}

			select {
			case c.SendCh <- msg.Data:
				c.hub.logger.Debug("delivered queued message", "id", msg.ID, "to", c.DID)
				recordMessageDeliveredFromQueue()
			default:
				// Buffer full, re-queue the message
				c.hub.logger.Warn("client buffer full, re-queuing message", "id", msg.ID, "to", c.DID)
				_ = c.hub.queue.Enqueue(msg)
				return
			}
		}

		if len(msgs) < batchSize {
			return
		}
	}
}

// handleDiscover handles discovery queries.
func (c *Client) handleDiscover(req *protocol.JSONRPCRequest) {
	// Check discover rate limit
	if !c.discoverLimiter.Allow() {
		c.hub.logger.Warn("discover rate limit exceeded", "client_id", c.ID)
		recordRateLimitHit("discover")
		c.sendError(req.ID, protocol.CodeRateLimited, "discovery rate limit exceeded")
		return
	}

	var query struct {
		Capability string `json:"capability,omitempty"`
	}
	_ = req.ParseParams(&query) // Query params are optional

	var agents []*registry.Agent
	var err error

	if query.Capability != "" {
		agents, err = c.hub.store.Search(query.Capability)
	} else {
		agents, err = c.hub.store.List()
	}

	if err != nil {
		recordError("discovery")
		c.sendError(req.ID, protocol.CodeInternalError, err.Error())
		return
	}

	recordDiscovery()
	c.sendResult(req.ID, agents)
}

// handleLookup handles looking up a single agent by DID.
func (c *Client) handleLookup(req *protocol.JSONRPCRequest) {
	var query struct {
		DID string `json:"did"`
	}
	if err := req.ParseParams(&query); err != nil || query.DID == "" {
		c.sendError(req.ID, protocol.CodeInvalidParams, "missing 'did' parameter")
		return
	}

	agent, err := c.hub.store.GetByDID(query.DID)
	if err != nil {
		c.sendError(req.ID, protocol.CodeInternalError, err.Error())
		return
	}

	c.sendResult(req.ID, agent)
}

func (c *Client) sendResult(id any, result any) {
	resp, _ := protocol.NewResponse(id, result)
	data, _ := protocol.Encode(resp)
	select {
	case c.SendCh <- data:
	default:
	}
}

func (c *Client) sendError(id any, code int, message string) {
	resp := protocol.NewErrorResponse(id, code, message, nil)
	data, _ := protocol.Encode(resp)
	select {
	case c.SendCh <- data:
	default:
	}
}

// securityHeaders wraps an http.Handler and sets security-related response headers.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}

func main() {
	// Define flags with defaults that can be overridden by env vars
	addr := flag.String("addr", "", "Listen address (env: MSG2AGENT_RELAY_ADDR)")
	maxConns := flag.Int("max-connections", 0, "Maximum concurrent connections (env: MSG2AGENT_MAX_CONNECTIONS)")
	msgRate := flag.Float64("msg-rate", 0, "Message rate limit per client (msg/sec) (env: MSG2AGENT_MSG_RATE)")

	// TLS flags
	tlsEnabled := flag.Bool("tls", false, "Enable TLS (env: MSG2AGENT_TLS)")
	tlsCert := flag.String("tls-cert", "", "TLS certificate file (env: MSG2AGENT_TLS_CERT)")
	tlsKey := flag.String("tls-key", "", "TLS key file (env: MSG2AGENT_TLS_KEY)")

	// Log level flag
	logLevel := flag.String("log-level", "", "Log level: debug, info, warn, error (env: MSG2AGENT_LOG_LEVEL)")

	// Store configuration
	storeType := flag.String("store", "", "Store type: memory, file, sqlite (env: MSG2AGENT_STORE)")
	storeFile := flag.String("store-file", "", "Store file path for file/sqlite stores (env: MSG2AGENT_STORE_FILE)")

	// Observability settings
	otlpEndpoint := flag.String("otlp-endpoint", "", "OTLP endpoint for tracing (env: MSG2AGENT_OTLP_ENDPOINT)")
	traceStdout := flag.Bool("trace-stdout", false, "Enable stdout tracing for debugging (env: MSG2AGENT_TRACE_STDOUT)")

	// CORS settings
	corsOrigins := flag.String("cors-origins", "", "Comma-separated list of allowed CORS origins (env: MSG2AGENT_CORS_ORIGINS)")

	// Security settings
	skipDIDProof := flag.Bool("skip-did-proof", false, "Skip DID ownership verification during registration (NOT recommended) (env: MSG2AGENT_SKIP_DID_PROOF)")
	allowedDIDs := flag.String("allowed-dids", "", "Comma-separated list of allowed DIDs (empty = open relay) (env: MSG2AGENT_ALLOWED_DIDS)")

	flag.Parse()

	// Resolve configuration: flags override env vars override defaults
	listenAddr := config.FlagOrEnv(*addr, "RELAY_ADDR", ":8080")
	maxConnections := config.FlagOrEnvInt(*maxConns, 0, "MAX_CONNECTIONS", 1000)
	messageRate := config.FlagOrEnvFloat(*msgRate, 0, "MSG_RATE", 100.0)
	useTLS := config.FlagOrEnvBool(*tlsEnabled, "TLS", false)
	certFile := config.FlagOrEnv(*tlsCert, "TLS_CERT", "")
	keyFile := config.FlagOrEnv(*tlsKey, "TLS_KEY", "")
	logLevelStr := config.FlagOrEnv(*logLevel, "LOG_LEVEL", "debug")
	storeTypeStr := config.FlagOrEnv(*storeType, "STORE", "memory")
	storeFilePath := config.FlagOrEnv(*storeFile, "STORE_FILE", "")
	otlpAddr := config.FlagOrEnv(*otlpEndpoint, "OTLP_ENDPOINT", "")
	useTraceStdout := config.FlagOrEnvBool(*traceStdout, "TRACE_STDOUT", false)
	corsOriginsStr := config.FlagOrEnv(*corsOrigins, "CORS_ORIGINS", "")
	skipDIDVerification := config.FlagOrEnvBool(*skipDIDProof, "SKIP_DID_PROOF", false)
	allowedDIDsStr := config.FlagOrEnv(*allowedDIDs, "ALLOWED_DIDS", "")

	// Parse allowed DIDs
	var allowedDIDList []string
	if allowedDIDsStr != "" {
		for _, d := range strings.Split(allowedDIDsStr, ",") {
			d = strings.TrimSpace(d)
			if d != "" {
				allowedDIDList = append(allowedDIDList, d)
			}
		}
	}

	// Parse CORS origins
	var allowedOrigins []string
	if corsOriginsStr != "" {
		for _, origin := range strings.Split(corsOriginsStr, ",") {
			origin = strings.TrimSpace(origin)
			if origin != "" {
				allowedOrigins = append(allowedOrigins, origin)
			}
		}
	}

	// Parse log level
	var slogLevel slog.Level
	switch logLevelStr {
	case "debug":
		slogLevel = slog.LevelDebug
	case "info":
		slogLevel = slog.LevelInfo
	case "warn":
		slogLevel = slog.LevelWarn
	case "error":
		slogLevel = slog.LevelError
	default:
		slogLevel = slog.LevelDebug
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slogLevel,
	}))

	// Validate TLS configuration
	if useTLS {
		if certFile == "" || keyFile == "" {
			logger.Error("TLS enabled but certificate or key file not specified")
			os.Exit(1)
		}
	}

	// Initialize tracing if configured
	var tracerProvider *telemetry.TracerProvider
	if otlpAddr != "" || useTraceStdout {
		var err error
		tracerProvider, err = telemetry.InitTracer(context.Background(), telemetry.TracerConfig{
			ServiceName:  "msg2agent-relay",
			Environment:  "development",
			OTLPEndpoint: otlpAddr,
			UseStdout:    useTraceStdout,
			Logger:       logger,
		})
		if err != nil {
			logger.Error("failed to initialize tracing", "error", err)
			os.Exit(1)
		}
		logger.Info("tracing initialized")
	}

	relayCfg := DefaultRelayConfig()
	relayCfg.MaxConnections = maxConnections
	relayCfg.MessageRateLimit = messageRate
	relayCfg.AllowedOrigins = allowedOrigins
	relayCfg.RequireDIDProof = !skipDIDVerification

	// Validate configuration
	if err := relayCfg.Validate(); err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	// Log security configuration
	if skipDIDVerification {
		logger.Warn("DID proof verification disabled - not recommended for production")
	} else {
		logger.Info("DID proof verification enabled")
	}

	// Log CORS configuration
	if len(allowedOrigins) > 0 {
		logger.Info("CORS configured", "origins", allowedOrigins)
	} else {
		logger.Warn("CORS: no external origins allowed (same-origin only)")
	}

	// Create store based on configuration
	var store registry.Store
	var storeCloser func() error // for stores that need cleanup

	switch storeTypeStr {
	case "sqlite":
		if storeFilePath == "" {
			storeFilePath = "./relay.db"
		}
		// Validate and canonicalize path
		validatedPath, err := validateAndCleanPath(storeFilePath, logger)
		if err != nil {
			logger.Error("invalid store file path", "error", err, "path", storeFilePath)
			os.Exit(1)
		}
		storeFilePath = validatedPath
		sqliteStore, err := registry.NewSQLiteStore(registry.SQLiteConfig{
			Path:   storeFilePath,
			Logger: logger,
		})
		if err != nil {
			logger.Error("failed to create SQLite store", "error", err, "path", storeFilePath)
			os.Exit(1)
		}
		store = sqliteStore
		storeCloser = sqliteStore.Close
		logger.Info("using SQLite store", "path", storeFilePath)

	case "file":
		if storeFilePath == "" {
			storeFilePath = "./relay-agents.json"
		}
		// Validate and canonicalize path
		validatedPath, err := validateAndCleanPath(storeFilePath, logger)
		if err != nil {
			logger.Error("invalid store file path", "error", err, "path", storeFilePath)
			os.Exit(1)
		}
		storeFilePath = validatedPath
		fileStore, err := registry.NewFileStore(storeFilePath)
		if err != nil {
			logger.Error("failed to create file store", "error", err, "path", storeFilePath)
			os.Exit(1)
		}
		store = fileStore
		logger.Info("using file store", "path", storeFilePath)

	default: // "memory" or unspecified
		store = registry.NewMemoryStore()
		logger.Info("using in-memory store")
	}

	// Create queue store (uses same storage backend as registry)
	var queueStore queue.Store
	if relayCfg.EnableOfflineQueue {
		switch storeTypeStr {
		case "sqlite":
			queuePath := storeFilePath
			if queuePath != ":memory:" {
				queuePath = storeFilePath + ".queue"
				// Validate and canonicalize queue path
				validatedQueuePath, err := validateAndCleanPath(queuePath, logger)
				if err != nil {
					logger.Error("invalid queue file path", "error", err, "path", queuePath)
					os.Exit(1)
				}
				queuePath = validatedQueuePath
			}
			sqliteQueue, err := queue.NewSQLiteStore(queue.SQLiteConfig{
				Path:        queuePath,
				Logger:      logger,
				QueueConfig: relayCfg.QueueConfig,
			})
			if err != nil {
				logger.Error("failed to create SQLite queue store", "error", err)
				os.Exit(1)
			}
			queueStore = sqliteQueue
			logger.Info("using SQLite queue store", "path", queuePath)
		default:
			queueStore = queue.NewMemoryStore(relayCfg.QueueConfig)
			logger.Info("using in-memory queue store")
		}
	}

	hub := NewRelayHubWithStore(relayCfg, store, queueStore, logger)

	// Configure DID allowlist
	hub.aclEnforcer = security.NewACLEnforcer()
	if len(allowedDIDList) > 0 {
		hub.allowlistACL = security.TrustedAgentsPolicy(allowedDIDList)
		logger.Info("DID allowlist enabled", "count", len(allowedDIDList))
	} else {
		logger.Info("DID allowlist disabled (open relay)")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", hub.handleWebSocket)

	// Health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// Readiness endpoint (for Kubernetes)
	mux.HandleFunc("/ready", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":      "ready",
			"connections": hub.connections.Load(),
			"max":         relayCfg.MaxConnections,
		})
	})

	// Prometheus metrics endpoint
	mux.Handle("/metrics", promhttp.Handler())

	server := &http.Server{
		Addr:              listenAddr,
		Handler:           securityHeaders(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Start server in goroutine
	go func() {
		var err error
		if useTLS {
			logger.Info("relay hub starting with TLS", "addr", listenAddr, "cert", certFile)
			// Configure TLS
			tlsConfig := &tls.Config{
				MinVersion: tls.VersionTLS12,
			}
			server.TLSConfig = tlsConfig
			err = server.ListenAndServeTLS(certFile, keyFile)
		} else {
			logger.Info("relay hub starting", "addr", listenAddr)
			err = server.ListenAndServe()
		}
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	protocol := "ws"
	if useTLS {
		protocol = "wss"
	}
	fmt.Printf("Relay Hub started on %s://%s\n", protocol, listenAddr)

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	logger.Info("shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("server shutdown error", "error", err)
	}

	// Stop relay hub (closes queue store)
	hub.Stop()

	// Close store if it has a closer
	if storeCloser != nil {
		if err := storeCloser(); err != nil {
			logger.Error("failed to close store", "error", err)
		}
	}

	// Shutdown tracer
	if tracerProvider != nil {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := tracerProvider.Shutdown(shutdownCtx); err != nil {
			logger.Error("tracer shutdown error", "error", err)
		}
		shutdownCancel()
	}
}
