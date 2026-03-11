// Package agent provides the core agent implementation.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/gianluca/msg2agent/pkg/crypto"
	"github.com/gianluca/msg2agent/pkg/identity"
	"github.com/gianluca/msg2agent/pkg/messaging"
	"github.com/gianluca/msg2agent/pkg/protocol"
	"github.com/gianluca/msg2agent/pkg/registry"
	"github.com/gianluca/msg2agent/pkg/security"
	"github.com/gianluca/msg2agent/pkg/transport"
)

// Default configuration values.
const (
	DefaultShutdownTimeout = 10 * time.Second
	DefaultDrainTimeout    = 5 * time.Second
	DefaultDedupTTL        = 5 * time.Minute // How long to remember seen messages
	DefaultDedupCleanup    = 1 * time.Minute // How often to clean up dedup cache
)

// Agent errors.
var (
	ErrNotStarted         = errors.New("agent not started")
	ErrAlreadyStarted     = errors.New("agent already started")
	ErrPeerNotFound       = errors.New("peer not found")
	ErrSenderNotFound     = errors.New("sender not found in registry")
	ErrNoSigningKey       = errors.New("sender has no signing key")
	ErrSignatureInvalid   = errors.New("signature verification failed")
	ErrDecryptionFailed   = errors.New("message decryption failed")
	ErrNoEncryptionKey    = errors.New("recipient has no encryption key")
	ErrRecipientNotFound  = errors.New("recipient not found in registry")
	ErrSenderKeyNotFound  = errors.New("sender encryption key not found")
	ErrListenerFailed     = errors.New("listener failed to start")
	ErrAlreadyListening   = errors.New("already listening")
	ErrEncryptionRequired = errors.New("encryption required but failed")
)

// MethodHandler handles a specific method call.
type MethodHandler func(ctx context.Context, params json.RawMessage) (any, error)

// DeliveryAckHandler is called when a delivery acknowledgment is received.
type DeliveryAckHandler func(messageID uuid.UUID, delivered bool, err error)

// seenMessage tracks when a message was first seen for deduplication.
type seenMessage struct {
	seenAt time.Time
}

// Agent represents an LLM agent that can communicate with other agents.
type Agent struct {
	identity   *identity.Identity
	record     *registry.Agent
	store      registry.Store
	discovery  *registry.Discovery
	listener   transport.Listener
	httpServer *http.Server
	acl        *security.ACLEnforcer
	handlers   map[string]MethodHandler
	peers      map[string]transport.Transport
	peerCount  atomic.Int64
	pending    map[uuid.UUID]chan *messaging.Message
	pendingRPC map[string]chan *protocol.JSONRPCResponse
	logger     *slog.Logger
	mu         sync.RWMutex
	wg         sync.WaitGroup // Track active goroutines for graceful shutdown
	started    bool
	listening  bool
	stopping   atomic.Bool // Flag to indicate shutdown in progress
	ctx        context.Context
	cancel     context.CancelFunc
	relayAddr  string
	listenAddr string

	// Message deduplication
	seenMsgs   map[string]seenMessage // message ID -> seen info
	seenMsgsMu sync.RWMutex
	dedupTTL   time.Duration

	// Async delivery callbacks
	ackHandlers []DeliveryAckHandler
	ackMu       sync.RWMutex

	// Configuration
	requireEncryption bool
	shutdownTimeout   time.Duration
	tlsEnabled        bool
	tlsCertFile       string
	tlsKeyFile        string
	tlsSkipVerify     bool
}

// Config holds agent configuration.
type Config struct {
	Domain            string
	AgentID           string
	DisplayName       string
	ListenAddr        string
	RelayAddr         string
	Logger            *slog.Logger
	RequireEncryption bool          // If true, fail Send/Notify when encryption fails
	ShutdownTimeout   time.Duration // Timeout for graceful shutdown (default: 10s)
	DedupTTL          time.Duration // How long to remember seen messages (default: 5m)

	// TLS configuration for listener
	TLSEnabled  bool   // Enable TLS for the WebSocket listener
	TLSCertFile string // Path to TLS certificate file
	TLSKeyFile  string // Path to TLS key file

	// TLS configuration for relay connection
	TLSSkipVerify bool // Skip TLS certificate verification (for testing)
}

// New creates a new agent with the given configuration.
func New(cfg Config) (*Agent, error) {
	// Generate identity
	ident, err := identity.NewIdentity(cfg.Domain, cfg.AgentID)
	if err != nil {
		return nil, err
	}

	// Create agent record
	record := registry.NewAgent(ident.String(), cfg.DisplayName)
	record.AddPublicKey("signing", registry.KeyTypeEd25519, ident.SigningPublicKey(), "signing")
	record.AddPublicKey("encryption", registry.KeyTypeX25519, ident.EncryptionPublicKey(), "encryption")

	if cfg.ListenAddr != "" {
		record.AddEndpoint(registry.TransportWebSocket, cfg.ListenAddr, 1)
	}

	// Create store and discovery
	store := registry.NewMemoryStore()
	discovery := registry.NewDiscovery(store, record)

	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	shutdownTimeout := cfg.ShutdownTimeout
	if shutdownTimeout == 0 {
		shutdownTimeout = DefaultShutdownTimeout
	}

	dedupTTL := cfg.DedupTTL
	if dedupTTL == 0 {
		dedupTTL = DefaultDedupTTL
	}

	return &Agent{
		identity:          ident,
		record:            record,
		store:             store,
		discovery:         discovery,
		acl:               security.NewACLEnforcer(),
		handlers:          make(map[string]MethodHandler),
		peers:             make(map[string]transport.Transport),
		pending:           make(map[uuid.UUID]chan *messaging.Message),
		pendingRPC:        make(map[string]chan *protocol.JSONRPCResponse),
		seenMsgs:          make(map[string]seenMessage),
		logger:            logger,
		relayAddr:         cfg.RelayAddr,
		listenAddr:        cfg.ListenAddr,
		requireEncryption: cfg.RequireEncryption,
		shutdownTimeout:   shutdownTimeout,
		dedupTTL:          dedupTTL,
		tlsEnabled:        cfg.TLSEnabled,
		tlsCertFile:       cfg.TLSCertFile,
		tlsKeyFile:        cfg.TLSKeyFile,
		tlsSkipVerify:     cfg.TLSSkipVerify,
	}, nil
}

// ID returns the agent's UUID.
func (a *Agent) ID() uuid.UUID {
	return a.record.ID
}

// DID returns the agent's DID.
func (a *Agent) DID() string {
	return a.identity.String()
}

// Record returns the agent's registry record.
func (a *Agent) Record() *registry.Agent {
	return a.record
}

// RegisterMethod registers a handler for a method.
func (a *Agent) RegisterMethod(method string, handler MethodHandler) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.handlers[method] = handler
}

// AddCapability adds a capability to the agent.
func (a *Agent) AddCapability(name, description string, methods []string) {
	a.record.AddCapability(name, description, methods)
	for _, method := range methods {
		if _, exists := a.handlers[method]; !exists {
			a.logger.Warn("capability references unregistered method", "capability", name, "method", method)
		}
	}
}

// SetACL sets the agent's access control policy.
func (a *Agent) SetACL(policy *registry.ACLPolicy) {
	a.record.ACL = policy
}

// Start starts the agent.
func (a *Agent) Start(ctx context.Context) error {
	a.mu.Lock()
	if a.started {
		a.mu.Unlock()
		return ErrAlreadyStarted
	}
	a.started = true
	a.ctx, a.cancel = context.WithCancel(ctx)
	a.mu.Unlock()

	// Start dedup cleanup goroutine
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		a.dedupCleanupLoop()
	}()

	a.record.SetOnline()
	a.logger.Info("agent started", "did", a.DID())

	return nil
}

// dedupCleanupLoop periodically cleans up expired entries from the dedup cache.
func (a *Agent) dedupCleanupLoop() {
	ticker := time.NewTicker(DefaultDedupCleanup)
	defer ticker.Stop()

	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			a.cleanupDedup()
		}
	}
}

// cleanupDedup removes expired entries from the dedup cache.
func (a *Agent) cleanupDedup() {
	now := time.Now()

	a.seenMsgsMu.Lock()
	defer a.seenMsgsMu.Unlock()

	for id, seen := range a.seenMsgs {
		if now.Sub(seen.seenAt) > a.dedupTTL {
			delete(a.seenMsgs, id)
		}
	}
}

// isDuplicateMessage checks if a message ID has been seen before.
// Returns true if duplicate, false if new (and marks it as seen).
func (a *Agent) isDuplicateMessage(msgID string) bool {
	if msgID == "" {
		return false // Can't dedup without ID
	}

	a.seenMsgsMu.Lock()
	defer a.seenMsgsMu.Unlock()

	if _, exists := a.seenMsgs[msgID]; exists {
		return true
	}

	a.seenMsgs[msgID] = seenMessage{seenAt: time.Now()}
	return false
}

// Stop stops the agent gracefully.
func (a *Agent) Stop() error {
	a.mu.Lock()
	if !a.started {
		a.mu.Unlock()
		return ErrNotStarted
	}

	// Set stopping flag to signal goroutines
	a.stopping.Store(true)
	a.cancel()
	a.started = false
	a.listening = false
	a.record.SetOffline()

	// Close listener if active (this will unblock Accept)
	if a.listener != nil {
		if err := a.listener.Close(); err != nil {
			a.logger.Debug("listener close error", "error", err)
		}
		a.listener = nil
	}

	// Close HTTP server if active
	if a.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := a.httpServer.Shutdown(ctx); err != nil {
			a.logger.Debug("http server shutdown error", "error", err)
		}
		cancel()
		a.httpServer = nil
	}

	// Close all peer connections (this will unblock Receive)
	for _, t := range a.peers {
		if err := t.Close(); err != nil {
			a.logger.Debug("peer close error", "error", err)
		}
	}
	a.peers = make(map[string]transport.Transport)
	a.mu.Unlock()

	// Drain pending channels to unblock waiting goroutines
	a.drainPendingChannels()

	// Wait for goroutines to finish with timeout
	done := make(chan struct{})
	go func() {
		a.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		a.logger.Info("agent stopped gracefully", "did", a.DID())
	case <-time.After(a.shutdownTimeout):
		a.logger.Warn("agent shutdown timeout, some goroutines may still be running",
			"did", a.DID(), "timeout", a.shutdownTimeout)
	}

	return nil
}

// drainPendingChannels closes all pending response channels to unblock waiting goroutines.
func (a *Agent) drainPendingChannels() {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Drain pending message channels
	for id, ch := range a.pending {
		select {
		case ch <- nil: // Signal shutdown
		default:
		}
		delete(a.pending, id)
	}

	// Drain pending RPC channels
	for id, ch := range a.pendingRPC {
		select {
		case ch <- nil: // Signal shutdown
		default:
		}
		delete(a.pendingRPC, id)
	}
}

// Listen starts listening for incoming connections (P2P mode).
// This allows other agents to connect directly to this agent.
func (a *Agent) Listen(ctx context.Context) error {
	a.mu.Lock()
	if a.listening {
		a.mu.Unlock()
		return ErrAlreadyListening
	}
	if a.listenAddr == "" {
		a.mu.Unlock()
		return nil // No listen address configured, skip
	}
	a.listening = true
	a.mu.Unlock()

	// Create WebSocket listener with TLS config
	cfg := transport.DefaultConfig(a.listenAddr)
	cfg.TLSEnabled = a.tlsEnabled
	cfg.TLSCertFile = a.tlsCertFile
	cfg.TLSKeyFile = a.tlsKeyFile
	listener := transport.NewWebSocketListener(cfg)

	if err := listener.Start(ctx); err != nil {
		a.mu.Lock()
		a.listening = false
		a.mu.Unlock()
		return errors.Join(ErrListenerFailed, err)
	}

	a.mu.Lock()
	a.listener = listener
	// Update the endpoint with the actual bound address
	actualAddr := listener.Addr()
	a.record.Endpoints = nil // Clear old endpoints
	protocol := "ws://"
	if a.tlsEnabled {
		protocol = "wss://"
	}
	a.record.AddEndpoint(registry.TransportWebSocket, protocol+actualAddr, 1)
	a.mu.Unlock()

	a.logger.Info("agent listening", "addr", actualAddr, "did", a.DID(), "tls", a.tlsEnabled)

	// Start accept loop with WaitGroup tracking
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		a.acceptLoop(ctx)
	}()

	return nil
}

// acceptLoop accepts incoming connections and starts receive loops.
func (a *Agent) acceptLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		a.mu.RLock()
		listener := a.listener
		a.mu.RUnlock()

		if listener == nil {
			return
		}

		conn, err := listener.Accept(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, transport.ErrClosed) {
				return
			}
			a.logger.Error("accept failed", "error", err)
			continue
		}

		// Generate unique peer ID
		peerID := uuid.New().String()
		a.peerCount.Add(1)
		peerAddr := "peer:" + peerID

		a.mu.Lock()
		a.peers[peerAddr] = conn
		a.mu.Unlock()

		a.logger.Debug("accepted connection", "peer", peerAddr, "remote", conn.RemoteAddr())

		// Start receive loop for this connection with WaitGroup tracking
		a.wg.Add(1)
		go func(t transport.Transport, id string) {
			defer a.wg.Done()
			a.receiveLoop(t)
			// Clean up on disconnect
			a.mu.Lock()
			delete(a.peers, id)
			a.mu.Unlock()
			a.peerCount.Add(-1)
			a.logger.Debug("peer disconnected", "peer", id)
		}(conn, peerAddr)
	}
}

// ListenAddr returns the actual address the agent is listening on.
// Returns empty string if not listening.
func (a *Agent) ListenAddr() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.listener == nil {
		return ""
	}
	return a.listener.Addr()
}

// IsListening returns true if the agent is accepting connections.
func (a *Agent) IsListening() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.listening && a.listener != nil
}

// PeerCount returns the number of connected peers.
func (a *Agent) PeerCount() int64 {
	return a.peerCount.Load()
}

// ServeAgentCard starts an HTTP server to serve the agent card at /.well-known/agent.json.
// This implements A2A protocol discovery.
func (a *Agent) ServeAgentCard(_ context.Context, addr string) error {
	mux := http.NewServeMux()

	// Serve agent card
	mux.HandleFunc("/.well-known/agent.json", func(w http.ResponseWriter, _ *http.Request) {
		card := a.buildAgentCard()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		_ = json.NewEncoder(w).Encode(card)
	})

	// Health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":    "ok",
			"did":       a.DID(),
			"listening": a.IsListening(),
			"peers":     a.PeerCount(),
		})
	})

	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	a.mu.Lock()
	a.httpServer = server
	a.mu.Unlock()

	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			a.logger.Error("HTTP server error", "error", err)
		}
	}()

	a.logger.Info("serving agent card", "addr", addr)
	return nil
}

// ServeAgentCardTLS starts an HTTPS server to serve the agent card at /.well-known/agent.json.
// This implements A2A protocol discovery with TLS.
func (a *Agent) ServeAgentCardTLS(_ context.Context, addr, certFile, keyFile string) error {
	mux := http.NewServeMux()

	// Serve agent card
	mux.HandleFunc("/.well-known/agent.json", func(w http.ResponseWriter, _ *http.Request) {
		card := a.buildAgentCard()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		_ = json.NewEncoder(w).Encode(card)
	})

	// Health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":    "ok",
			"did":       a.DID(),
			"listening": a.IsListening(),
			"peers":     a.PeerCount(),
			"tls":       true,
		})
	})

	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	a.mu.Lock()
	a.httpServer = server
	a.mu.Unlock()

	go func() {
		if err := server.ListenAndServeTLS(certFile, keyFile); err != nil && !errors.Is(err, http.ErrServerClosed) {
			a.logger.Error("HTTPS server error", "error", err)
		}
	}()

	a.logger.Info("serving agent card with TLS", "addr", addr)
	return nil
}

// Card represents an A2A-compatible agent card.
type Card struct {
	Name               string         `json:"name"`
	Description        string         `json:"description,omitempty"`
	URL                string         `json:"url"`
	Version            string         `json:"version"`
	DID                string         `json:"did"`
	Capabilities       CardCapability `json:"capabilities"`
	DefaultInputModes  []string       `json:"defaultInputModes,omitempty"`
	DefaultOutputModes []string       `json:"defaultOutputModes,omitempty"`
	Skills             []CardSkill    `json:"skills,omitempty"`
	PublicKeys         []CardKey      `json:"publicKeys,omitempty"`
}

// CardCapability describes agent capabilities.
type CardCapability struct {
	Streaming              bool `json:"streaming,omitempty"`
	PushNotifications      bool `json:"pushNotifications,omitempty"`
	StateTransitionHistory bool `json:"stateTransitionHistory,omitempty"`
	EndToEndEncryption     bool `json:"endToEndEncryption,omitempty"`
}

// CardSkill describes an agent skill.
type CardSkill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Methods     []string `json:"methods,omitempty"`
}

// CardKey describes a public key.
type CardKey struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Purpose string `json:"purpose"`
	Key     string `json:"publicKeyBase58"`
}

// buildAgentCard creates an A2A-compatible agent card.
func (a *Agent) buildAgentCard() *Card {
	a.mu.RLock()
	defer a.mu.RUnlock()

	card := &Card{
		Name:    a.record.DisplayName,
		Version: "1.0.0",
		DID:     a.identity.String(),
		Capabilities: CardCapability{
			Streaming:          true,
			EndToEndEncryption: true,
		},
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
	}

	// Set URL from first endpoint
	if len(a.record.Endpoints) > 0 {
		card.URL = a.record.Endpoints[0].URL
	}

	// Convert capabilities to skills
	for _, cap := range a.record.Capabilities {
		skill := CardSkill{
			ID:          cap.Name,
			Name:        cap.Name,
			Description: cap.Description,
			Methods:     cap.Methods,
		}
		card.Skills = append(card.Skills, skill)
	}

	// Add public keys
	for _, key := range a.record.PublicKeys {
		card.PublicKeys = append(card.PublicKeys, CardKey{
			ID:      key.ID,
			Type:    string(key.Type),
			Purpose: key.Purpose,
			Key:     encodeBase58(key.Key),
		})
	}

	return card
}

// Simple base58 encoding (Bitcoin alphabet) for agent card keys.
const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

func encodeBase58(data []byte) string {
	if len(data) == 0 {
		return ""
	}

	// Count leading zeros
	zeros := 0
	for _, b := range data {
		if b == 0 {
			zeros++
		} else {
			break
		}
	}

	// Allocate enough space
	size := len(data)*138/100 + 1
	buf := make([]byte, size)

	// Process bytes
	for _, b := range data {
		carry := int(b)
		for j := size - 1; j >= 0; j-- {
			carry += 256 * int(buf[j])
			buf[j] = byte(carry % 58)
			carry /= 58
		}
	}

	// Skip leading zeros in buf
	i := 0
	for i < size && buf[i] == 0 {
		i++
	}

	// Build result
	result := make([]byte, zeros+size-i)
	for j := 0; j < zeros; j++ {
		result[j] = '1'
	}
	for j := zeros; i < size; i, j = i+1, j+1 {
		result[j] = base58Alphabet[buf[i]]
	}

	return string(result)
}

// Connect connects to a peer agent.
func (a *Agent) Connect(ctx context.Context, addr string) error {
	cfg := transport.DefaultConfig(addr)
	cfg.TLSSkipVerify = a.tlsSkipVerify
	t := transport.NewWebSocketTransport(cfg)

	if err := t.Connect(ctx); err != nil {
		return err
	}

	a.mu.Lock()
	a.peers[addr] = t
	a.mu.Unlock()

	// Start receiving messages from this peer with WaitGroup tracking
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		a.receiveLoop(t)
	}()

	return nil
}

// Send sends a message to another agent and waits for response.
func (a *Agent) Send(ctx context.Context, to, method string, params any) (*messaging.Message, error) {
	msg, err := messaging.NewRequest(a.DID(), to, method, params)
	if err != nil {
		return nil, err
	}

	// Encrypt body before signing (if recipient is known)
	if err := a.encryptMessageBody(msg); err != nil {
		if a.requireEncryption {
			return nil, errors.Join(ErrEncryptionRequired, err)
		}
		// If recipient not in registry, send unencrypted (relay will route it)
		a.logger.Warn("sending unencrypted message", "to", to, "reason", err.Error())
	}

	// Sign the message (including encrypted body)
	msgBytes, _ := json.Marshal(msg)
	msg.Signature = a.identity.Sign(msgBytes)

	return a.sendAndWait(ctx, msg)
}

// Notify sends a notification (no response expected).
func (a *Agent) Notify(_ context.Context, to, method string, params any) error {
	msg, err := messaging.NewNotification(a.DID(), to, method, params)
	if err != nil {
		return err
	}

	// Encrypt body before signing (if recipient is known)
	if err := a.encryptMessageBody(msg); err != nil {
		if a.requireEncryption {
			return errors.Join(ErrEncryptionRequired, err)
		}
		a.logger.Warn("sending unencrypted notification", "to", to, "reason", err.Error())
	}

	// Sign the message (including encrypted body)
	msgBytes, _ := json.Marshal(msg)
	msg.Signature = a.identity.Sign(msgBytes)

	return a.sendMessage(msg)
}

// SendAsync sends a message asynchronously and returns immediately.
// The message will have RequestAck set to true, and delivery acknowledgment
// will be sent to registered ack handlers.
func (a *Agent) SendAsync(ctx context.Context, to, method string, params any) (uuid.UUID, error) {
	msg, err := messaging.NewRequest(a.DID(), to, method, params)
	if err != nil {
		return uuid.Nil, err
	}

	// Request delivery acknowledgment
	msg.RequestAck = true

	// Encrypt body before signing (if recipient is known)
	if err := a.encryptMessageBody(msg); err != nil {
		if a.requireEncryption {
			return uuid.Nil, errors.Join(ErrEncryptionRequired, err)
		}
		a.logger.Warn("sending unencrypted async message", "to", to, "reason", err.Error())
	}

	// Sign the message (including encrypted body)
	msgBytes, _ := json.Marshal(msg)
	msg.Signature = a.identity.Sign(msgBytes)

	// Send without waiting for response
	if err := a.sendMessage(msg); err != nil {
		return uuid.Nil, err
	}

	return msg.ID, nil
}

// OnDeliveryAck registers a handler to be called when delivery acknowledgments are received.
func (a *Agent) OnDeliveryAck(handler DeliveryAckHandler) {
	a.ackMu.Lock()
	defer a.ackMu.Unlock()
	a.ackHandlers = append(a.ackHandlers, handler)
}

// notifyDeliveryAck notifies all registered handlers of a delivery ack.
func (a *Agent) notifyDeliveryAck(messageID uuid.UUID, delivered bool, err error) {
	a.ackMu.RLock()
	handlers := make([]DeliveryAckHandler, len(a.ackHandlers))
	copy(handlers, a.ackHandlers)
	a.ackMu.RUnlock()

	for _, handler := range handlers {
		handler(messageID, delivered, err)
	}
}

// SendChat sends a chat message within a conversation thread.
func (a *Agent) SendChat(ctx context.Context, to string, body any) (*messaging.Message, error) {
	msg, err := messaging.NewChatMessage(a.DID(), to, body)
	if err != nil {
		return nil, err
	}

	// Encrypt body before signing
	if err := a.encryptMessageBody(msg); err != nil {
		if a.requireEncryption {
			return nil, errors.Join(ErrEncryptionRequired, err)
		}
		a.logger.Warn("sending unencrypted chat message", "to", to, "reason", err.Error())
	}

	// Sign the message
	msgBytes, _ := json.Marshal(msg)
	msg.Signature = a.identity.Sign(msgBytes)

	if err := a.sendMessage(msg); err != nil {
		return nil, err
	}

	return msg, nil
}

// SendChatReply sends a reply to a chat message in a thread.
func (a *Agent) SendChatReply(ctx context.Context, to string, threadID uuid.UUID, parentID *uuid.UUID, seqNo int, body any) (*messaging.Message, error) {
	msg, err := messaging.NewChatReply(a.DID(), to, threadID, parentID, seqNo, body)
	if err != nil {
		return nil, err
	}

	// Encrypt body before signing
	if err := a.encryptMessageBody(msg); err != nil {
		if a.requireEncryption {
			return nil, errors.Join(ErrEncryptionRequired, err)
		}
		a.logger.Warn("sending unencrypted chat reply", "to", to, "reason", err.Error())
	}

	// Sign the message
	msgBytes, _ := json.Marshal(msg)
	msg.Signature = a.identity.Sign(msgBytes)

	if err := a.sendMessage(msg); err != nil {
		return nil, err
	}

	return msg, nil
}

// SendTypingIndicator sends a typing indicator to a recipient.
func (a *Agent) SendTypingIndicator(ctx context.Context, to string, threadID *uuid.UUID, typing bool) error {
	msg := messaging.NewTypingIndicator(a.DID(), to, threadID, typing)

	// Typing indicators are ephemeral, don't encrypt
	msgBytes, _ := json.Marshal(msg)
	msg.Signature = a.identity.Sign(msgBytes)

	return a.sendMessage(msg)
}

// SendReceipt sends a delivery or read receipt.
func (a *Agent) SendReceipt(ctx context.Context, to string, messageID uuid.UUID, receiptType messaging.ReceiptType) error {
	msg := messaging.NewReceipt(a.DID(), to, messageID, receiptType)

	msgBytes, _ := json.Marshal(msg)
	msg.Signature = a.identity.Sign(msgBytes)

	return a.sendMessage(msg)
}

// sendAndWait sends a message and waits for the response.
func (a *Agent) sendAndWait(ctx context.Context, msg *messaging.Message) (*messaging.Message, error) {
	// Create response channel
	respCh := make(chan *messaging.Message, 1)
	a.mu.Lock()
	a.pending[msg.ID] = respCh
	a.mu.Unlock()

	defer func() {
		a.mu.Lock()
		delete(a.pending, msg.ID)
		a.mu.Unlock()
	}()

	// Send the message
	if err := a.sendMessage(msg); err != nil {
		return nil, err
	}

	// Wait for response
	select {
	case resp := <-respCh:
		if resp == nil {
			// nil indicates shutdown
			return nil, context.Canceled
		}
		return resp, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// sendMessage sends a message to the appropriate peer.
func (a *Agent) sendMessage(msg *messaging.Message) error {
	// Encode to JSON-RPC
	rpcMsg, err := protocol.NewRequest(msg.ID.String(), msg.Method, msg)
	if err != nil {
		return err
	}

	data, err := protocol.Encode(rpcMsg)
	if err != nil {
		return err
	}

	// Find transport for recipient
	a.mu.RLock()
	t, found, isRelay := a.findTransportForRecipient(msg.To)
	a.mu.RUnlock()

	if !found {
		return ErrPeerNotFound
	}

	if isRelay {
		a.logger.Info("routing message via relay", "to", msg.To, "method", msg.Method)
	}

	return t.Send(a.ctx, data)
}

// findTransportForRecipient finds the transport to reach a recipient.
// Returns (transport, found, isRelay) where isRelay indicates routing via relay.
func (a *Agent) findTransportForRecipient(to string) (transport.Transport, bool, bool) {
	// First try to find agent in store with direct connection
	agent, err := a.store.GetByDID(to)
	if err == nil && len(agent.Endpoints) > 0 {
		// Try each endpoint for direct connection
		for _, ep := range agent.Endpoints {
			if t, ok := a.peers[ep.URL]; ok {
				return t, true, false // Direct connection, not relay
			}
		}
	}

	// No direct connection found - try relay if configured
	if a.relayAddr != "" {
		if t, ok := a.peers[a.relayAddr]; ok {
			a.logger.Debug("using configured relay for recipient",
				"to", to, "relay", a.relayAddr)
			return t, true, true
		}
	}

	// Fallback to first available peer as relay (with warning)
	for addr, t := range a.peers {
		a.logger.Warn("using fallback peer as relay (no direct route or configured relay)",
			"to", to, "peer", addr)
		return t, true, true
	}

	return nil, false, false
}

// CallRelay sends a raw JSON-RPC request to the relay (or any peer).
// It bypasses the messaging.Message envelope system.
func (a *Agent) CallRelay(ctx context.Context, method string, params any) (json.RawMessage, error) {
	// Generate ID
	id := uuid.New().String()

	// Create request
	req, err := protocol.NewRequest(id, method, params)
	if err != nil {
		return nil, err
	}

	data, err := protocol.Encode(req)
	if err != nil {
		return nil, err
	}

	// Register channel
	respCh := make(chan *protocol.JSONRPCResponse, 1)
	a.mu.Lock()
	a.pendingRPC[id] = respCh

	// Find relay transport. We assume the explicit RelayAddr is the relay.
	// If RelayAddr is empty, we try strictly valid peers.
	var t transport.Transport
	if a.relayAddr != "" {
		// Check if we have a connection to the configured relay
		if peer, ok := a.peers[a.relayAddr]; ok {
			t = peer
		} else {
			// Try fallback to first peer if not found (maybe normalized addr differs)
			for _, peer := range a.peers {
				t = peer
				break
			}
		}
	} else {
		// Fallback
		for _, peer := range a.peers {
			t = peer
			break
		}
	}
	a.mu.Unlock()

	if t == nil {
		a.mu.Lock()
		delete(a.pendingRPC, id)
		a.mu.Unlock()
		return nil, ErrPeerNotFound
	}

	defer func() {
		a.mu.Lock()
		delete(a.pendingRPC, id)
		a.mu.Unlock()
	}()

	// Send
	if err := t.Send(ctx, data); err != nil {
		return nil, err
	}

	// Wait
	select {
	case resp := <-respCh:
		if resp == nil {
			// nil indicates shutdown
			return nil, context.Canceled
		}
		if resp.IsError() {
			return nil, errors.New(resp.Error.Message)
		}
		return resp.Result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// receiveLoop handles incoming messages from a transport.
func (a *Agent) receiveLoop(t transport.Transport) {
	for {
		// Check if shutting down
		if a.stopping.Load() {
			return
		}

		select {
		case <-a.ctx.Done():
			return
		default:
		}

		data, err := t.Receive(a.ctx)
		if err != nil {
			// Suppress errors during shutdown
			if !errors.Is(err, context.Canceled) && !a.stopping.Load() {
				a.logger.Error("receive error", "error", err)
			}
			return
		}

		go a.handleIncoming(t, data)
	}
}

// handleIncoming processes an incoming message.
func (a *Agent) handleIncoming(t transport.Transport, data []byte) {
	// Try to decode as JSON-RPC request
	req, err := protocol.DecodeRequest(data)
	if err == nil {
		// Handle relay notifications specially
		if req.ID == nil {
			a.handleNotification(req)
			return
		}
		a.handleRequest(t, req)
		return
	}

	// Try as response
	resp, err := protocol.DecodeResponse(data)
	if err == nil {
		a.handleResponse(resp)
		return
	}

	a.logger.Warn("failed to decode message", "error", err)
}

// handleNotification processes incoming notifications (no response expected).
func (a *Agent) handleNotification(req *protocol.JSONRPCRequest) {
	switch req.Method {
	case "relay.ack":
		a.handleRelayAck(req)
	case "relay.presence":
		a.handlePresenceNotification(req)
	case "relay.typing":
		a.handleTypingNotification(req)
	default:
		// Parse as a regular message notification
		var msg messaging.Message
		if err := req.ParseParams(&msg); err != nil {
			a.logger.Debug("failed to parse notification params", "method", req.Method, "error", err)
			return
		}

		// Find handler for notification method
		a.mu.RLock()
		handler, exists := a.handlers[msg.Method]
		a.mu.RUnlock()

		if exists {
			_, _ = handler(a.ctx, msg.Body)
		}
	}
}

// handleRelayAck processes relay.ack notifications for delivery acknowledgment.
func (a *Agent) handleRelayAck(req *protocol.JSONRPCRequest) {
	var ack struct {
		MessageID string `json:"message_id"`
		Delivered bool   `json:"delivered"`
		Status    string `json:"status"`
		Timestamp string `json:"timestamp"`
	}

	if err := req.ParseParams(&ack); err != nil {
		a.logger.Debug("failed to parse relay.ack", "error", err)
		return
	}

	msgID, err := uuid.Parse(ack.MessageID)
	if err != nil {
		a.logger.Debug("invalid message_id in relay.ack", "message_id", ack.MessageID)
		return
	}

	a.logger.Debug("received delivery ack", "message_id", msgID, "delivered", ack.Delivered, "status", ack.Status)

	// Notify registered handlers
	var ackErr error
	if !ack.Delivered {
		ackErr = errors.New(ack.Status)
	}
	a.notifyDeliveryAck(msgID, ack.Delivered, ackErr)
}

// handlePresenceNotification processes relay.presence notifications.
func (a *Agent) handlePresenceNotification(req *protocol.JSONRPCRequest) {
	// Presence notifications can be handled by registered method handlers
	a.mu.RLock()
	handler, exists := a.handlers["relay.presence"]
	a.mu.RUnlock()

	if exists {
		_, _ = handler(a.ctx, req.Params)
	}
}

// handleTypingNotification processes relay.typing notifications.
func (a *Agent) handleTypingNotification(req *protocol.JSONRPCRequest) {
	// Typing notifications can be handled by registered method handlers
	a.mu.RLock()
	handler, exists := a.handlers["relay.typing"]
	a.mu.RUnlock()

	if exists {
		_, _ = handler(a.ctx, req.Params)
	}
}

// handleRequest processes an incoming request.
func (a *Agent) handleRequest(t transport.Transport, req *protocol.JSONRPCRequest) {
	// Parse message from params
	var msg messaging.Message
	if err := req.ParseParams(&msg); err != nil {
		// Can't parse message, so no routing info available — send raw error
		a.sendErrorResponse(t, req.ID, protocol.CodeInvalidParams, "invalid params")
		return
	}

	// Check for duplicate message
	if a.isDuplicateMessage(msg.ID.String()) {
		a.logger.Debug("ignoring duplicate message", "id", msg.ID, "from", msg.From)
		return // Silently ignore duplicates
	}

	// Verify message signature
	if err := a.verifyMessageSignature(&msg); err != nil {
		a.logger.Warn("signature verification failed", "from", msg.From, "error", err)
		a.sendErrorToSender(t, req, &msg, protocol.CodeSignatureInvalid, "signature verification failed")
		return
	}

	// Decrypt message body if encrypted
	if err := a.decryptMessageBody(&msg); err != nil {
		a.logger.Warn("message decryption failed", "from", msg.From, "error", err)
		a.sendErrorToSender(t, req, &msg, protocol.CodeDecryptionFailed, "decryption failed")
		return
	}

	// Handle response messages (responses routed through relay)
	if msg.IsResponse() || msg.IsError() {
		a.handleMessageResponse(&msg)
		return
	}

	// Check ACL
	if err := a.acl.CheckAccess(a.record, msg.From, msg.Method); err != nil {
		a.sendErrorToSender(t, req, &msg, protocol.CodeAccessDenied, "access denied")
		return
	}

	// Find handler (fall back to wildcard if exact match not found)
	a.mu.RLock()
	handler, exists := a.handlers[msg.Method]
	if !exists {
		handler, exists = a.handlers["*"]
	}
	a.mu.RUnlock()

	if !exists {
		a.sendErrorToSender(t, req, &msg, protocol.CodeMethodNotFound, "method not found")
		return
	}

	// Execute handler
	result, err := handler(a.ctx, msg.Body)
	if err != nil {
		a.sendErrorToSender(t, req, &msg, protocol.CodeInternalError, err.Error())
		return
	}

	// Check if this is a relay connection (needs message routing)
	a.mu.RLock()
	relayT, hasRelay := a.peers[a.relayAddr]
	isRelay := hasRelay && t == relayT
	a.mu.RUnlock()

	if isRelay {
		// Send response as a routable message through relay
		respMsg, err := messaging.NewResponse(&msg, result)
		if err != nil {
			a.logger.Error("failed to create response message", "error", err)
			return
		}

		// Sign the response
		msgBytes, _ := json.Marshal(respMsg)
		respMsg.Signature = a.identity.Sign(msgBytes)

		// Send through relay
		if err := a.sendMessage(respMsg); err != nil {
			a.logger.Error("failed to send response via relay", "error", err)
		}
	} else {
		// Direct P2P: send raw JSON-RPC response
		resp, _ := protocol.NewResponse(req.ID, result)
		respData, _ := protocol.Encode(resp)
		_ = t.Send(a.ctx, respData) // Best effort
	}
}

// handleMessageResponse processes a response message routed through the relay.
func (a *Agent) handleMessageResponse(msg *messaging.Message) {
	if msg.CorrelationID == nil {
		a.logger.Warn("response message missing correlation ID", "from", msg.From)
		return
	}

	// Find pending request
	a.mu.RLock()
	ch, exists := a.pending[*msg.CorrelationID]
	a.mu.RUnlock()

	if !exists {
		a.logger.Debug("no pending request for response", "correlation_id", msg.CorrelationID)
		return
	}

	select {
	case ch <- msg:
	default:
	}
}

// handleResponse processes an incoming response.
func (a *Agent) handleResponse(resp *protocol.JSONRPCResponse) {
	// Get correlation ID
	idStr, ok := resp.ID.(string)
	if !ok {
		return
	}

	id, err := uuid.Parse(idStr)

	// Check pendingRPC first (string keys)
	a.mu.RLock()
	rpcCh, rpcExists := a.pendingRPC[idStr]
	a.mu.RUnlock()

	if rpcExists {
		select {
		case rpcCh <- resp:
		default:
		}
		return
	}

	if err != nil {
		return
	}

	// Find pending request
	a.mu.RLock()
	ch, exists := a.pending[id]
	a.mu.RUnlock()

	if !exists {
		return
	}

	// Parse result into message
	var msg messaging.Message
	if resp.IsError() {
		msg = messaging.Message{
			ID:   id,
			Type: messaging.TypeError,
		}
		_ = msg.SetBody(resp.Error) // Best effort
	} else {
		_ = resp.ParseResult(&msg) // Best effort
	}

	select {
	case ch <- &msg:
	default:
	}
}

// sendErrorResponse sends an error response.
// sendErrorToSender sends an error response, routing through relay if needed.
func (a *Agent) sendErrorToSender(t transport.Transport, req *protocol.JSONRPCRequest, msg *messaging.Message, code int, errMsg string) {
	a.mu.RLock()
	relayT, hasRelay := a.peers[a.relayAddr]
	isRelay := hasRelay && t == relayT
	a.mu.RUnlock()

	if isRelay && msg != nil {
		// Send as routable error message through relay
		errResp, err := messaging.NewErrorResponse(msg, code, errMsg)
		if err != nil {
			a.logger.Error("failed to create error response", "error", err)
			return
		}

		// Sign the response
		msgBytes, _ := json.Marshal(errResp)
		errResp.Signature = a.identity.Sign(msgBytes)

		// Send through relay
		if err := a.sendMessage(errResp); err != nil {
			a.logger.Error("failed to send error via relay", "error", err)
		}
	} else {
		// Direct P2P: send raw JSON-RPC error response
		a.sendErrorResponse(t, req.ID, code, errMsg)
	}
}

func (a *Agent) sendErrorResponse(t transport.Transport, id any, code int, message string) {
	resp := protocol.NewErrorResponse(id, code, message, nil)
	data, _ := protocol.Encode(resp)
	_ = t.Send(a.ctx, data) // Best effort
}

// Discovery returns the agent's discovery service.
func (a *Agent) Discovery() *registry.Discovery {
	return a.discovery
}

// Store returns the agent's registry store.
func (a *Agent) Store() registry.Store {
	return a.store
}

// EncryptFor encrypts data for another agent.
func (a *Agent) EncryptFor(recipientPubKey []byte, data []byte) ([]byte, error) {
	return crypto.Encrypt(data, a.identity.Keys.Encryption.PrivateKey, recipientPubKey)
}

// Decrypt decrypts data from another agent.
func (a *Agent) Decrypt(senderPubKey []byte, data []byte) ([]byte, error) {
	return crypto.Decrypt(data, a.identity.Keys.Encryption.PrivateKey, senderPubKey)
}

// Sign signs data using the agent's signing key.
func (a *Agent) Sign(data []byte) []byte {
	return a.identity.Sign(data)
}

// verifyMessageSignature verifies the Ed25519 signature of an incoming message.
func (a *Agent) verifyMessageSignature(msg *messaging.Message) error {
	if len(msg.Signature) == 0 {
		return ErrSignatureInvalid
	}

	// Look up sender in registry
	sender, err := a.store.GetByDID(msg.From)
	if err != nil {
		return ErrSenderNotFound
	}

	// Get sender's signing key
	signingKey := sender.GetSigningKey()
	if signingKey == nil {
		return ErrNoSigningKey
	}

	// Clone message and remove signature for verification
	msgCopy := msg.Clone()
	msgCopy.Signature = nil

	// Marshal the message without signature
	msgBytes, err := json.Marshal(msgCopy)
	if err != nil {
		return err
	}

	// Verify the signature
	if !crypto.VerifySignature(signingKey.Key, msgBytes, msg.Signature) {
		return ErrSignatureInvalid
	}

	return nil
}

// encryptMessageBody encrypts the message body for the recipient.
// Returns the message with encrypted body and Encrypted flag set.
func (a *Agent) encryptMessageBody(msg *messaging.Message) error {
	if len(msg.Body) == 0 {
		return nil // Nothing to encrypt
	}

	// Look up recipient in registry
	recipient, err := a.store.GetByDID(msg.To)
	if err != nil {
		return ErrRecipientNotFound
	}

	// Get recipient's encryption key
	encKey := recipient.GetEncryptionKey()
	if encKey == nil {
		return ErrNoEncryptionKey
	}

	// Encrypt the body
	encrypted, err := crypto.Encrypt(msg.Body, a.identity.Keys.Encryption.PrivateKey, encKey.Key)
	if err != nil {
		return err
	}

	msg.Body = encrypted
	msg.Encrypted = true
	return nil
}

// decryptMessageBody decrypts an encrypted message body from the sender.
func (a *Agent) decryptMessageBody(msg *messaging.Message) error {
	if !msg.Encrypted || len(msg.Body) == 0 {
		return nil // Not encrypted or empty
	}

	// Look up sender in registry
	sender, err := a.store.GetByDID(msg.From)
	if err != nil {
		return ErrSenderNotFound
	}

	// Get sender's encryption key
	encKey := sender.GetEncryptionKey()
	if encKey == nil {
		return ErrSenderKeyNotFound
	}

	// Decrypt the body
	decrypted, err := crypto.Decrypt(msg.Body, a.identity.Keys.Encryption.PrivateKey, encKey.Key)
	if err != nil {
		return ErrDecryptionFailed
	}

	msg.Body = decrypted
	msg.Encrypted = false
	return nil
}
