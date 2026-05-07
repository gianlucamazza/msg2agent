package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/gianlucamazza/msg2agent/pkg/billing"
	"github.com/gianlucamazza/msg2agent/pkg/crypto"
	"github.com/gianlucamazza/msg2agent/pkg/identity"
	"github.com/gianlucamazza/msg2agent/pkg/messaging"
	"github.com/gianlucamazza/msg2agent/pkg/protocol"
	"github.com/gianlucamazza/msg2agent/pkg/registry"
)

// Client represents a connected agent.
type Client struct {
	ID       string
	DID      string
	TenantID string       // empty = self-hosted / no billing
	logger   *slog.Logger // tenant-scoped; falls back to hub.logger when TenantID is empty
	Conn     *websocket.Conn
	SendCh   chan []byte
	hub      *RelayHub

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

	// Gateway delegation — set when this client registers with role="gateway".
	// gatewayNamespace is a DID prefix (e.g. "did:wba:domain:tenant:*") that
	// lists which From DIDs this client is allowed to assert via ActorProof.
	// gatewaySigningKey is the Ed25519 public key used to verify ActorProof.
	gatewayNamespace  string
	gatewaySigningKey []byte
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
// For gateway clients (role="gateway"), delegation is allowed: the message From may be
// any DID within the client's gatewayNamespace, provided ActorProof is a valid signature
// of (actor_did + ":" + from + ":" + message_id) by the gateway's signing key.
func (c *Client) validateSender(msg *messaging.Message) error {
	if c.DID == "" {
		return ErrClientNotRegistered
	}
	if msg.From == c.DID {
		return nil
	}
	// Gateway delegation path.
	if msg.ActorDID != "" && msg.ActorDID == c.DID && c.gatewayNamespace != "" {
		if !didMatchesNamespace(msg.From, c.gatewayNamespace) {
			return fmt.Errorf("delegation denied: %q not in namespace %q", msg.From, c.gatewayNamespace)
		}
		proofInput := []byte(c.DID + ":" + msg.From + ":" + msg.ID.String())
		if !crypto.VerifySignature(c.gatewaySigningKey, proofInput, msg.ActorProof) {
			return fmt.Errorf("delegation proof invalid for actor %q asserting %q", c.DID, msg.From)
		}
		return nil
	}
	return fmt.Errorf("%w: message from %q but client registered as %q", ErrSenderMismatch, msg.From, c.DID)
}

// didMatchesNamespace checks if a DID falls within a namespace glob like "did:wba:domain:tenant:*".
func didMatchesNamespace(did, namespace string) bool {
	if strings.HasSuffix(namespace, "*") {
		return strings.HasPrefix(did, namespace[:len(namespace)-1])
	}
	return did == namespace
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
	case "relay.register_subordinate":
		c.handleRegisterSubordinate(req)
		return
	}

	// Check message rate limit (per-client baseline + per-tenant plan limit).
	if !c.msgLimiter.Allow() {
		c.hub.logger.Warn("message rate limit exceeded", "client_id", c.ID)
		recordRateLimitHit("message")
		c.sendError(req.ID, protocol.CodeRateLimited, "rate limit exceeded")
		return
	}
	if c.TenantID != "" && c.hub.tenantPool != nil {
		if !c.hub.tenantPool.Allow(c.TenantID) {
			c.logger.Warn("tenant rate limit exceeded")
			recordRateLimitHit("tenant_message")
			billing.RecordRateLimited(c.TenantID)
			c.sendError(req.ID, protocol.CodeRateLimited, "tenant rate limit exceeded")
			return
		}
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

	// Billing: enforce MaxAgentDIDs quota for the tenant.
	if c.TenantID != "" && c.hub.billingStore != nil {
		if tenant, err := c.hub.billingStore.GetTenant(c.TenantID); err == nil {
			count, err := c.hub.store.CountByTenant(c.TenantID)
			if err == nil && count >= tenant.Quota.MaxAgentDIDs {
				c.logger.Warn("MaxAgentDIDs quota exceeded", "count", count, "limit", tenant.Quota.MaxAgentDIDs)
				billing.RecordQuotaExceeded(c.TenantID, "agent_did")
				c.sendErrorWithData(req.ID, protocol.CodeQuotaExceeded, "DID quota exceeded for this tenant",
					billing.FormatQuotaErrorData(string(tenant.Plan), c.TenantID, int64(count), int64(tenant.Quota.MaxAgentDIDs)))
				return
			}
		}
		agent.TenantID = c.TenantID
	}

	c.DID = agent.DID
	agent.SetOnline()
	_ = c.hub.store.Put(agent) // Best effort store update

	// If registering as a gateway, cache delegation config for fast validation.
	if agent.Role == "gateway" && agent.DelegationNamespace != "" {
		var sigKey []byte
		for _, k := range agent.PublicKeys {
			if k.Purpose == "signing" {
				sigKey = k.Key
				break
			}
		}
		c.gatewayNamespace = agent.DelegationNamespace
		c.gatewaySigningKey = sigKey
	}

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

// SubordinateRegisterRequest is the params for relay.register_subordinate.
// A gateway sends this to register a tenant DID's public keys without a WS connection.
// The relay stores the keys so messages sent via delegation can be verified by recipients.
type SubordinateRegisterRequest struct {
	DID        string               `json:"did"`
	TenantID   string               `json:"tenant_id,omitempty"`
	PublicKeys []registry.PublicKey `json:"public_keys"`
}

// handleRegisterSubordinate allows a gateway client to register tenant DIDs and their
// public keys into the relay registry without the tenant holding a WS connection.
// The tenant agent entry is created as offline and owned by this gateway's tenant.
func (c *Client) handleRegisterSubordinate(req *protocol.JSONRPCRequest) {
	if c.DID == "" {
		c.sendError(req.ID, protocol.CodeSenderNotRegistered, "client must register before registering subordinates")
		return
	}
	if c.gatewayNamespace == "" {
		c.sendError(req.ID, protocol.CodeAccessDenied, "client is not registered as a gateway")
		return
	}

	var subReq SubordinateRegisterRequest
	if err := req.ParseParams(&subReq); err != nil {
		c.sendError(req.ID, protocol.CodeInvalidParams, "invalid subordinate data")
		return
	}

	// Validate DID format.
	parsedDID, err := identity.ParseDID(subReq.DID)
	if err != nil || parsedDID.Method != identity.MethodWBA {
		c.sendError(req.ID, protocol.CodeInvalidParams, ErrInvalidDIDFormat.Error())
		return
	}

	// Ensure the subordinate DID is within this gateway's namespace.
	if !didMatchesNamespace(subReq.DID, c.gatewayNamespace) {
		c.sendError(req.ID, protocol.CodeDelegationInvalid, "subordinate DID not in gateway namespace")
		return
	}

	// Check allowlist (same rule as direct registration).
	if c.hub.allowlistACL != nil {
		relay := &registry.Agent{ACL: c.hub.allowlistACL}
		if err := c.hub.aclEnforcer.CheckAccess(relay, subReq.DID, "register"); err != nil {
			c.sendError(req.ID, protocol.CodeAccessDenied, "DID not allowed to register")
			return
		}
	}

	agent := registry.NewAgent(subReq.DID, subReq.DID)
	agent.TenantID = c.TenantID
	agent.PublicKeys = subReq.PublicKeys
	// Status remains offline — tenant has no WS connection.
	_ = c.hub.store.Put(agent)

	c.sendResult(req.ID, map[string]string{"status": "registered", "did": subReq.DID})
	c.hub.logger.Info("subordinate registered", "gateway", c.DID, "sub_did", subReq.DID)
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
	if !c.discoverLimiter.Allow() {
		c.hub.logger.Warn("lookup rate limit exceeded", "client_id", c.ID)
		recordRateLimitHit("lookup")
		c.sendError(req.ID, protocol.CodeRateLimited, "rate limit exceeded")
		return
	}

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
	c.sendErrorWithData(id, code, message, nil)
}

func (c *Client) sendErrorWithData(id any, code int, message string, errData any) {
	resp := protocol.NewErrorResponse(id, code, message, errData)
	data, _ := protocol.Encode(resp)
	select {
	case c.SendCh <- data:
	default:
	}
}
