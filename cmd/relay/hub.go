package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"github.com/gianlucamazza/msg2agent/pkg/billing"
	"github.com/gianlucamazza/msg2agent/pkg/messaging"
	"github.com/gianlucamazza/msg2agent/pkg/protocol"
	"github.com/gianlucamazza/msg2agent/pkg/queue"
	"github.com/gianlucamazza/msg2agent/pkg/registry"
	"github.com/gianlucamazza/msg2agent/pkg/security"
)

const (
	queueCleanupInterval = time.Hour
	stopGraceTimeout     = 5 * time.Second
	wsSendBufferSize     = 256
)

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
	stopOnce    sync.Once
	wg          sync.WaitGroup // Track background goroutines for clean shutdown

	// DID allowlist enforcement
	aclEnforcer  *security.ACLEnforcer
	allowlistACL *registry.ACLPolicy // nil = open relay (no allowlist)

	// Billing (optional; nil = self-hosted, no billing)
	billingStore billing.Store
	tenantPool   *billing.TenantRateLimiterPool
	jwtValidator billing.JWTValidator // optional OAuth2 JWT validator for WS auth

	// serviceToken allows internal agents to bypass billing auth on WebSocket connect.
	serviceToken string
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
	ticker := time.NewTicker(queueCleanupInterval)
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

// Stop gracefully stops the relay hub. Safe to call multiple times.
func (h *RelayHub) Stop() {
	h.stopOnce.Do(func() {
		h.ctxCancel() // Cancel hub context, propagates to all client contexts
		close(h.stopCh)

		// Close all client connections to unblock writePump goroutines.
		// Clients can appear twice in the map (by ID and by DID), so
		// track already-closed ones to avoid a double-close panic.
		h.mu.Lock()
		closed := make(map[*Client]bool)
		for _, client := range h.clients {
			if !closed[client] {
				client.stopped.Store(true)
				close(client.SendCh)
				closed[client] = true
			}
		}
		h.mu.Unlock()

		// Wait for goroutines with timeout.
		done := make(chan struct{})
		go func() {
			h.wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			h.logger.Info("all goroutines stopped")
		case <-time.After(stopGraceTimeout):
			h.logger.Warn("goroutine shutdown timeout")
		}

		if h.queue != nil {
			_ = h.queue.Close()
		}
		if h.presence != nil {
			h.presence.Stop()
		}
	})
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

	// Billing auth: resolve tenant from Bearer API key or JWT if billing is active.
	// Internal agents (e.g. mcp-server) bypass billing by presenting the service token.
	isSvcConn := h.serviceToken != "" &&
		r.Header.Get("Authorization") == "Bearer "+h.serviceToken
	var tenantID string
	if h.billingStore != nil && !isSvcConn {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "missing Authorization header", http.StatusUnauthorized)
			return
		}
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
			http.Error(w, "invalid Authorization header format", http.StatusUnauthorized)
			return
		}
		token := parts[1]
		var resolvedTenantID string
		if billing.IsAPIKeyToken(token) {
			// API key path
			hash, err := billing.HashAPIKey(token)
			if err != nil {
				http.Error(w, "invalid API key format", http.StatusUnauthorized)
				return
			}
			key, err := h.billingStore.GetAPIKeyByHash(hash)
			if err != nil || !key.IsValid() {
				http.Error(w, "invalid or revoked API key", http.StatusUnauthorized)
				return
			}
			resolvedTenantID = key.TenantID
		} else if h.jwtValidator != nil {
			// JWT / OAuth2 path
			claims, err := h.jwtValidator.ValidateTokenToBillingClaims(token)
			if err != nil {
				http.Error(w, "invalid OAuth2 token", http.StatusUnauthorized)
				return
			}
			tid, err := h.billingStore.GetOAuthIdentityTenant(claims.Issuer, claims.Subject)
			if err != nil {
				http.Error(w, "OAuth identity not registered; contact support", http.StatusForbidden)
				return
			}
			resolvedTenantID = tid
		} else {
			http.Error(w, "invalid authorization token", http.StatusUnauthorized)
			return
		}
		tenant, err := h.billingStore.GetTenant(resolvedTenantID)
		if err != nil || !tenant.IsActive() {
			http.Error(w, "tenant not found or suspended", http.StatusForbidden)
			return
		}
		tenantID = tenant.ID
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

	clientLogger := h.logger
	if tenantID != "" {
		clientLogger = h.logger.With("tenant", tenantID)
	}
	client := &Client{
		ID:              uuid.New().String(),
		TenantID:        tenantID,
		logger:          clientLogger,
		Conn:            conn,
		SendCh:          make(chan []byte, wsSendBufferSize),
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
