package a2a

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/gianlucamazza/msg2agent/pkg/billing"
	"github.com/gianlucamazza/msg2agent/pkg/protocol"
)

// AgentHandler wraps an A2A server with HTTP/WebSocket handling.
type AgentHandler struct {
	server      *Server
	httpServer  *http.Server
	cardConfig  AgentCardConfig
	agentInfo   *AgentInfo
	oauth2      *OAuth2Validator
	requireAuth bool
	mu          sync.Mutex

	// API key billing auth (alternative to OAuth2)
	billingStore     billing.Store
	billingMeter     *billing.UsageMeter
	billingAllowAnon bool
}

// AgentInfo provides agent identity info for the agent card.
type AgentInfo struct {
	DID         string
	Name        string
	Description string
	Skills      []Skill
}

// AgentHandlerOption configures an AgentHandler.
type AgentHandlerOption func(*AgentHandler)

// WithAgentCardConfig sets the agent card configuration.
func WithAgentCardConfig(cfg AgentCardConfig) AgentHandlerOption {
	return func(h *AgentHandler) {
		h.cardConfig = cfg
	}
}

// WithAgentInfo sets the agent identity information.
func WithAgentInfo(info *AgentInfo) AgentHandlerOption {
	return func(h *AgentHandler) {
		h.agentInfo = info
	}
}

// WithOAuth2 enables OAuth2 authentication with the given config.
func WithOAuth2(cfg OAuth2Config) AgentHandlerOption {
	return func(h *AgentHandler) {
		h.oauth2 = NewOAuth2Validator(cfg)
		h.requireAuth = true
	}
}

// WithAPIKeyBilling enables Bearer API key authentication backed by the billing store.
// Events are recorded to meter if provided. Set allowAnon to permit unauthenticated access.
func WithAPIKeyBilling(store billing.Store, meter *billing.UsageMeter, allowAnon bool) AgentHandlerOption {
	return func(h *AgentHandler) {
		h.billingStore = store
		h.billingMeter = meter
		h.billingAllowAnon = allowAnon
	}
}

// NewAgentHandler creates a new A2A agent handler.
func NewAgentHandler(taskHandler TaskHandler, opts ...AgentHandlerOption) *AgentHandler {
	h := &AgentHandler{
		server: NewServer(taskHandler),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Server returns the underlying A2A server.
func (h *AgentHandler) Server() *Server {
	return h.server
}

// ServeHTTP implements http.Handler for A2A requests.
func (h *AgentHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Handle CORS preflight
	if r.Method == http.MethodOptions {
		h.handleCORS(w)
		return
	}

	// Public endpoints (no auth required)
	switch r.URL.Path {
	case "/.well-known/agent.json":
		h.handleAgentCard(w, r)
		return
	case "/health":
		h.handleHealth(w, r)
		return
	}

	// API key billing auth (checked first if configured).
	if h.billingStore != nil {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" && !h.billingAllowAnon {
			http.Error(w, "missing Authorization header", http.StatusUnauthorized)
			return
		}
		if authHeader != "" {
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
				http.Error(w, "invalid Authorization format; expected: Bearer msg2a_...", http.StatusUnauthorized)
				return
			}
			hash, err := billing.HashAPIKey(parts[1])
			if err == nil {
				key, err := h.billingStore.GetAPIKeyByHash(hash)
				if err != nil || !key.IsValid() {
					http.Error(w, "invalid or revoked API key", http.StatusUnauthorized)
					return
				}
				tenant, err := h.billingStore.GetTenant(key.TenantID)
				if err != nil || !tenant.IsActive() {
					http.Error(w, "tenant not found or suspended", http.StatusForbidden)
					return
				}
				r = r.WithContext(context.WithValue(r.Context(), billing.TenantContextKey(), tenant))
			}
		}
	} else if h.requireAuth && h.oauth2 != nil {
		// Fall back to OAuth2 if billing store not configured.
		claims, err := h.validateAuth(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), claimsContextKey, claims))
	}

	switch r.URL.Path {
	case "/":
		if r.Header.Get("Upgrade") == "websocket" {
			h.handleWebSocket(w, r)
		} else {
			h.handleJSONRPC(w, r)
		}
	default:
		http.NotFound(w, r)
	}
}

// handleCORS handles CORS preflight requests.
func (h *AgentHandler) handleCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	w.Header().Set("Access-Control-Max-Age", "86400")
	w.WriteHeader(http.StatusNoContent)
}

// handleHealth serves the health check endpoint.
func (h *AgentHandler) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "ok",
	})
}

// validateAuth validates the Authorization header and returns claims.
func (h *AgentHandler) validateAuth(r *http.Request) (*Claims, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return nil, ErrMissingToken
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return nil, ErrInvalidToken
	}

	return h.oauth2.ValidateToken(parts[1])
}

// handleAgentCard serves the agent card.
func (h *AgentHandler) handleAgentCard(w http.ResponseWriter, r *http.Request) {
	card := h.buildAgentCard(r)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	if err := json.NewEncoder(w).Encode(card); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
}

// buildAgentCard constructs the agent card from config and agent info.
func (h *AgentHandler) buildAgentCard(r *http.Request) *AgentCard {
	// Determine agent ID
	agentID := ""
	name := "A2A Agent"
	description := ""
	var skills []Skill

	if h.agentInfo != nil {
		agentID = h.agentInfo.DID
		name = h.agentInfo.Name
		description = h.agentInfo.Description
		skills = h.agentInfo.Skills
	}

	// Determine base URL
	baseURL := h.cardConfig.BaseURL
	if baseURL == "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		// Check X-Forwarded-Proto header for reverse proxy
		if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
			scheme = proto
		}
		baseURL = fmt.Sprintf("%s://%s/", scheme, r.Host)
	}

	card := &AgentCard{
		AgentID:          agentID,
		Name:             name,
		Description:      description,
		Version:          "1.0.0",
		ProtocolVersions: []string{"1.0"},
		URL:              baseURL,
		Capabilities: Capabilities{
			Streaming:              true,
			PushNotifications:      false,
			StateTransitionHistory: true,
		},
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
		Skills:             skills,
	}

	// Set documentation URL if configured
	if h.cardConfig.DocumentationURL != "" {
		card.DocumentationURL = h.cardConfig.DocumentationURL
	}

	// Set provider if configured
	if h.cardConfig.ProviderOrganization != "" {
		card.Provider = &Provider{
			Organization: h.cardConfig.ProviderOrganization,
			URL:          h.cardConfig.ProviderURL,
		}
	}

	// Configure OAuth2 security scheme if enabled
	if h.cardConfig.OAuth2Enabled {
		authURL := h.cardConfig.OAuth2AuthURL
		tokenURL := h.cardConfig.OAuth2TokenURL
		scopes := h.cardConfig.OAuth2Scopes

		// Use defaults if not specified
		if authURL == "" {
			authURL = "https://accounts.google.com/o/oauth2/auth"
		}
		if tokenURL == "" {
			tokenURL = "https://oauth2.googleapis.com/token"
		}
		if scopes == nil {
			scopes = map[string]string{
				"openid": "OpenID Connect",
			}
		}

		card.SecuritySchemes = map[string]SecurityScheme{
			"oauth2": {
				Type:        "oauth2",
				Description: "OAuth 2.0 authorization code flow for Gemini Enterprise",
				Flows: &OAuthFlows{
					AuthorizationCode: &OAuthFlow{
						AuthorizationURL: authURL,
						TokenURL:         tokenURL,
						Scopes:           scopes,
					},
				},
			},
		}
		card.Security = []map[string][]string{
			{"oauth2": {"openid"}},
		}
	}

	return card
}

// handleJSONRPC handles JSON-RPC requests over HTTP POST.
func (h *AgentHandler) handleJSONRPC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var reqData json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&reqData); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	h.meterA2ARequest(r.Context(), reqData)

	respData, err := h.server.HandleRequest(r.Context(), reqData)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(respData) //nolint:gosec // JSON-RPC response from internal handler, not user-tainted
}

// handleWebSocket handles WebSocket connections for streaming.
func (h *AgentHandler) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"*"},
	})
	if err != nil {
		return
	}
	defer func() {
		_ = conn.Close(websocket.StatusNormalClosure, "") // Best effort close
	}()

	ctx := r.Context()

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}

		// Decode request
		req, err := protocol.DecodeRequest(data)
		if err != nil {
			resp := protocol.NewErrorResponse(nil, ErrCodeInvalidRequest, "invalid request", nil)
			respData, _ := protocol.Encode(resp)
			_ = conn.Write(ctx, websocket.MessageText, respData) // Best effort
			continue
		}

		// Handle streaming requests
		if req.Method == A2AMessageStream {
			h.meterA2ARequest(ctx, data)
			h.handleStreamingRequest(ctx, conn, req)
			continue
		}

		h.meterA2ARequest(ctx, data)

		// Handle regular requests
		respData, err := h.server.HandleRequest(ctx, data)
		if err != nil {
			resp := protocol.NewErrorResponse(req.ID, ErrCodeInternalError, err.Error(), nil)
			respData, _ = protocol.Encode(resp)
		}

		if err := conn.Write(ctx, websocket.MessageText, respData); err != nil {
			return
		}
	}
}

// handleStreamingRequest handles a streaming request over WebSocket.
func (h *AgentHandler) handleStreamingRequest(ctx context.Context, conn *websocket.Conn, req *protocol.JSONRPCRequest) {
	sendFn := func(event *StreamEvent) error {
		// Send as JSON-RPC notification
		notification, _ := protocol.NewNotification("task/status", event)
		data, _ := protocol.Encode(notification)
		return conn.Write(ctx, websocket.MessageText, data)
	}

	if err := h.server.HandleStreamRequest(ctx, req.Params, sendFn); err != nil {
		resp := protocol.NewErrorResponse(req.ID, ErrCodeInternalError, err.Error(), nil)
		respData, _ := protocol.Encode(resp)
		_ = conn.Write(ctx, websocket.MessageText, respData) // Best effort
	}
}

// meterA2ARequest records a billing event for an A2A JSON-RPC request if metering is configured.
// It peeks the method without full parsing so decode errors are silently skipped.
func (h *AgentHandler) meterA2ARequest(ctx context.Context, data json.RawMessage) {
	if h.billingMeter == nil {
		return
	}
	tenant := billing.TenantFromContext(ctx)
	if tenant == nil {
		return
	}
	var peek struct {
		Method string `json:"method"`
	}
	if err := json.Unmarshal(data, &peek); err != nil || peek.Method == "" {
		return
	}
	h.billingMeter.RecordAudit(tenant.ID, a2aMethodEvent(peek.Method), peek.Method, "", 1)
}

// a2aMethodEvent maps an A2A JSON-RPC method name to its billing event type.
func a2aMethodEvent(method string) billing.UsageEvent {
	switch method {
	case A2AMessageSend, A2AMessageStream:
		return billing.EventMessage
	default:
		return billing.EventToolCall
	}
}

// Start starts the HTTP server.
func (h *AgentHandler) Start(addr string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.httpServer = &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		if err := h.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("A2A server error: %v\n", err)
		}
	}()
	return nil
}

// Stop stops the HTTP server.
func (h *AgentHandler) Stop(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.httpServer != nil {
		return h.httpServer.Shutdown(ctx)
	}
	return nil
}

// RegisterWithMux registers the handler with an http.ServeMux.
func (h *AgentHandler) RegisterWithMux(mux *http.ServeMux, prefix string) {
	mux.Handle(prefix+"/", http.StripPrefix(prefix, h))
	mux.Handle(prefix+"/.well-known/agent.json", http.HandlerFunc(h.handleAgentCard))
}
