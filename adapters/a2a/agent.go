package a2a

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/gianluca/msg2agent/pkg/protocol"
)

// AgentHandler wraps an A2A server with HTTP/WebSocket handling.
type AgentHandler struct {
	server     *Server
	httpServer *http.Server
	mu         sync.Mutex
}

// NewAgentHandler creates a new A2A agent handler.
func NewAgentHandler(taskHandler TaskHandler) *AgentHandler {
	return &AgentHandler{
		server: NewServer(taskHandler),
	}
}

// Server returns the underlying A2A server.
func (h *AgentHandler) Server() *Server {
	return h.server
}

// ServeHTTP implements http.Handler for A2A requests.
func (h *AgentHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/.well-known/agent.json":
		h.handleAgentCard(w, r)
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

// handleAgentCard serves the agent card.
func (h *AgentHandler) handleAgentCard(w http.ResponseWriter, r *http.Request) {
	// This would be populated from the actual agent
	card := &AgentCard{
		Name:    "A2A Agent",
		Version: "1.0.0",
		URL:     fmt.Sprintf("http://%s/", r.Host),
		Capabilities: Capabilities{
			Streaming:              true,
			PushNotifications:      false,
			StateTransitionHistory: true,
		},
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if err := json.NewEncoder(w).Encode(card); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
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

	respData, err := h.server.HandleRequest(r.Context(), reqData)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(respData) // Error handling not possible after headers sent
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
			h.handleStreamingRequest(ctx, conn, req)
			continue
		}

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
