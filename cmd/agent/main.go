// Package main provides the agent executable.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/gianluca/msg2agent/pkg/agent"
	"github.com/gianluca/msg2agent/pkg/config"
	"github.com/gianluca/msg2agent/pkg/registry"
	"github.com/gianluca/msg2agent/pkg/security"
	"github.com/gianluca/msg2agent/pkg/telemetry"
)

// ChatMessage represents a chat message for history.
type ChatMessage struct {
	ID        string    `json:"id"`
	From      string    `json:"from"`
	To        string    `json:"to,omitempty"`
	Text      string    `json:"text"`
	ThreadID  string    `json:"thread_id,omitempty"`
	Timestamp time.Time `json:"timestamp"`
	Received  bool      `json:"received"`
}

func main() {
	// Parse flags - defaults can be overridden by env vars
	name := flag.String("name", "", "Agent name (env: MSG2AGENT_NAME)")
	domain := flag.String("domain", "", "Agent domain (env: MSG2AGENT_DOMAIN)")
	listen := flag.String("listen", "", "WebSocket listen address (env: MSG2AGENT_LISTEN)")
	httpAddr := flag.String("http", "", "HTTP address for agent card (env: MSG2AGENT_HTTP)")
	relay := flag.String("relay", "", "Relay hub address (env: MSG2AGENT_RELAY)")

	// TLS flags for listener
	tlsEnabled := flag.Bool("tls", false, "Enable TLS for listener (env: MSG2AGENT_TLS)")
	tlsCert := flag.String("tls-cert", "", "TLS certificate file (env: MSG2AGENT_TLS_CERT)")
	tlsKey := flag.String("tls-key", "", "TLS key file (env: MSG2AGENT_TLS_KEY)")

	// TLS flags for HTTP agent card server
	httpTLS := flag.Bool("http-tls", false, "Enable TLS for HTTP server (env: MSG2AGENT_HTTP_TLS)")

	// TLS for relay connection
	tlsSkipVerify := flag.Bool("tls-skip-verify", false, "Skip TLS verification for relay (env: MSG2AGENT_TLS_SKIP_VERIFY)")

	// Other settings
	requireEncryption := flag.Bool("require-encryption", false, "Require message encryption (env: MSG2AGENT_REQUIRE_ENCRYPTION)")
	logLevel := flag.String("log-level", "", "Log level: debug, info, warn, error (env: MSG2AGENT_LOG_LEVEL)")

	// ACL settings
	trustedDIDs := flag.String("trusted-dids", "", "Comma-separated list of trusted agent DIDs (env: MSG2AGENT_TRUSTED_DIDS)")
	openACL := flag.Bool("open-acl", false, "Use open ACL policy (allow all) - NOT recommended for production (env: MSG2AGENT_OPEN_ACL)")

	// Observability settings
	metricsAddr := flag.String("metrics", "", "Metrics server address (env: MSG2AGENT_METRICS)")
	otlpEndpoint := flag.String("otlp-endpoint", "", "OTLP endpoint for tracing (env: MSG2AGENT_OTLP_ENDPOINT)")
	traceStdout := flag.Bool("trace-stdout", false, "Enable stdout tracing for debugging (env: MSG2AGENT_TRACE_STDOUT)")

	flag.Parse()

	// Resolve configuration: flags override env vars override defaults
	agentName := config.FlagOrEnv(*name, "NAME", "agent")
	agentDomain := config.FlagOrEnv(*domain, "DOMAIN", "localhost")
	listenAddr := config.FlagOrEnv(*listen, "LISTEN", "")
	httpAddress := config.FlagOrEnv(*httpAddr, "HTTP", "")
	relayAddr := config.FlagOrEnv(*relay, "RELAY", "")
	useTLS := config.FlagOrEnvBool(*tlsEnabled, "TLS", false)
	certFile := config.FlagOrEnv(*tlsCert, "TLS_CERT", "")
	keyFile := config.FlagOrEnv(*tlsKey, "TLS_KEY", "")
	useHTTPTLS := config.FlagOrEnvBool(*httpTLS, "HTTP_TLS", false)
	skipVerify := config.FlagOrEnvBool(*tlsSkipVerify, "TLS_SKIP_VERIFY", false)
	reqEncryption := config.FlagOrEnvBool(*requireEncryption, "REQUIRE_ENCRYPTION", false)
	logLevelStr := config.FlagOrEnv(*logLevel, "LOG_LEVEL", "debug")
	metricsAddress := config.FlagOrEnv(*metricsAddr, "METRICS", "")
	otlpAddr := config.FlagOrEnv(*otlpEndpoint, "OTLP_ENDPOINT", "")
	useTraceStdout := config.FlagOrEnvBool(*traceStdout, "TRACE_STDOUT", false)
	trustedDIDsStr := config.FlagOrEnv(*trustedDIDs, "TRUSTED_DIDS", "")
	useOpenACL := config.FlagOrEnvBool(*openACL, "OPEN_ACL", false)

	// Parse trusted DIDs
	var trustedDIDsList []string
	if trustedDIDsStr != "" {
		for _, did := range strings.Split(trustedDIDsStr, ",") {
			did = strings.TrimSpace(did)
			if did != "" {
				trustedDIDsList = append(trustedDIDsList, did)
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

	// Setup logger
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slogLevel,
	}))

	// Validate TLS configuration for listener
	if useTLS {
		if certFile == "" || keyFile == "" {
			logger.Error("TLS enabled for listener but certificate or key file not specified")
			os.Exit(1)
		}
	}

	// Validate TLS configuration for HTTP server
	if useHTTPTLS {
		if certFile == "" || keyFile == "" {
			logger.Error("TLS enabled for HTTP server but certificate or key file not specified")
			os.Exit(1)
		}
	}

	// Log TLS settings if skip verify is enabled
	if skipVerify {
		logger.Warn("TLS verification disabled for relay connection - not recommended for production")
	}

	// Initialize tracing if configured
	var tracerProvider *telemetry.TracerProvider
	if otlpAddr != "" || useTraceStdout {
		var err error
		tracerProvider, err = telemetry.InitTracer(context.Background(), telemetry.TracerConfig{
			ServiceName:  "msg2agent-agent",
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

	// Start metrics server if address specified
	var metricsServer *http.Server
	if metricsAddress != "" {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.Handler())

		// Health endpoints for the metrics server
		mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		})

		mux.HandleFunc("/ready", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
		})

		mux.HandleFunc("/live", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("live"))
		})

		metricsServer = &http.Server{
			Addr:              metricsAddress,
			Handler:           mux,
			ReadHeaderTimeout: 10 * time.Second,
		}

		go func() {
			logger.Info("metrics server starting", "addr", metricsAddress)
			if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				logger.Error("metrics server error", "error", err)
			}
		}()
	}

	// Create agent
	cfg := agent.Config{
		Domain:            agentDomain,
		AgentID:           agentName,
		DisplayName:       agentName,
		ListenAddr:        listenAddr,
		RelayAddr:         relayAddr,
		Logger:            logger,
		RequireEncryption: reqEncryption,
		TLSEnabled:        useTLS,
		TLSCertFile:       certFile,
		TLSKeyFile:        keyFile,
		TLSSkipVerify:     skipVerify,
	}

	a, err := agent.New(cfg)
	if err != nil {
		logger.Error("failed to create agent", "error", err)
		os.Exit(1)
	}

	// Chat message history (in-memory for demo)
	var chatHistory []ChatMessage
	var chatMu sync.Mutex

	// Register some example methods
	a.RegisterMethod("ping", func(ctx context.Context, params json.RawMessage) (any, error) {
		return map[string]string{"response": "pong"}, nil
	})

	a.RegisterMethod("echo", func(ctx context.Context, params json.RawMessage) (any, error) {
		var input map[string]any
		if err := json.Unmarshal(params, &input); err != nil {
			return nil, err
		}
		return input, nil
	})

	a.RegisterMethod("agent.info", func(ctx context.Context, params json.RawMessage) (any, error) {
		return map[string]any{
			"id":   a.ID().String(),
			"did":  a.DID(),
			"name": a.Record().DisplayName,
		}, nil
	})

	// Chat method - receives incoming chat messages
	a.RegisterMethod("chat", func(ctx context.Context, params json.RawMessage) (any, error) {
		var msg struct {
			From      string `json:"from"`
			Text      string `json:"text"`
			ThreadID  string `json:"thread_id,omitempty"`
			Timestamp string `json:"timestamp"`
		}
		if err := json.Unmarshal(params, &msg); err != nil {
			return nil, err
		}

		chatMu.Lock()
		chatHistory = append(chatHistory, ChatMessage{
			From:      msg.From,
			Text:      msg.Text,
			ThreadID:  msg.ThreadID,
			Timestamp: time.Now(),
			Received:  true,
		})
		chatMu.Unlock()

		logger.Info("received chat message", "from", msg.From, "text", msg.Text)
		return map[string]string{"status": "received"}, nil
	})

	// Add capabilities
	a.AddCapability("core", "Core agent functionality", []string{"ping", "echo", "agent.info"})
	a.AddCapability("chat", "Chat messaging capability", []string{"chat"})

	// Set ACL policy based on configuration
	var aclPolicy *registry.ACLPolicy
	if useOpenACL {
		logger.Warn("using open ACL policy - all agents can communicate (not recommended for production)")
		aclPolicy = security.DefaultOpenPolicy()
	} else if len(trustedDIDsList) > 0 {
		logger.Info("using trusted agents ACL policy", "trusted_count", len(trustedDIDsList))
		aclPolicy = security.TrustedAgentsPolicy(trustedDIDsList)
	} else {
		logger.Warn("using closed ACL policy - no agents can communicate (use -trusted-dids or -open-acl to configure)")
		aclPolicy = security.DefaultClosedPolicy()
	}
	a.SetACL(aclPolicy)

	// Start agent
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := a.Start(ctx); err != nil {
		logger.Error("failed to start agent", "error", err)
		cancel()
		os.Exit(1)
	}

	// Start listening for P2P connections if address specified
	if listenAddr != "" {
		if err := a.Listen(ctx); err != nil {
			logger.Error("failed to start listener", "error", err)
		}
	}

	// Create HTTP server with chat endpoints
	var httpServer *http.Server
	if httpAddress != "" {
		mux := http.NewServeMux()

		// Agent card endpoint
		mux.HandleFunc("/.well-known/agent.json", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Access-Control-Allow-Origin", "*")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name":    a.Record().DisplayName,
				"did":     a.DID(),
				"version": "1.0.0",
			})
		})

		// Health check
		mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		})

		// Send chat message
		mux.HandleFunc("/send-chat", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}

			var req struct {
				To   string `json:"to"`
				Text string `json:"text"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "invalid request body", http.StatusBadRequest)
				return
			}

			// Create chat message body
			body := map[string]any{
				"from":      a.DID(),
				"text":      req.Text,
				"timestamp": time.Now().UTC().Format(time.RFC3339),
			}

			// Send via agent
			msg, err := a.SendChat(ctx, req.To, body)
			if err != nil {
				logger.Error("failed to send chat", "error", err, "to", req.To)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			// Store in history
			chatMu.Lock()
			chatHistory = append(chatHistory, ChatMessage{
				ID:        msg.ID.String(),
				From:      a.DID(),
				To:        req.To,
				Text:      req.Text,
				ThreadID:  msg.ThreadID.String(),
				Timestamp: time.Now(),
				Received:  false,
			})
			chatMu.Unlock()

			logger.Info("sent chat message", "to", req.To, "id", msg.ID)

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":    "sent",
				"id":        msg.ID.String(),
				"thread_id": msg.ThreadID.String(),
			})
		})

		// Send async chat (fire-and-forget with ack)
		mux.HandleFunc("/send-async", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}

			var req struct {
				To     string `json:"to"`
				Method string `json:"method"`
				Params any    `json:"params"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "invalid request body", http.StatusBadRequest)
				return
			}

			msgID, err := a.SendAsync(ctx, req.To, req.Method, req.Params)
			if err != nil {
				logger.Error("failed to send async", "error", err, "to", req.To)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			logger.Info("sent async message", "to", req.To, "method", req.Method, "id", msgID)

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "sent",
				"id":     msgID.String(),
			})
		})

		// Get chat history
		mux.HandleFunc("/chat-history", func(w http.ResponseWriter, r *http.Request) {
			chatMu.Lock()
			history := make([]ChatMessage, len(chatHistory))
			copy(history, chatHistory)
			chatMu.Unlock()

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(history)
		})

		// Send typing indicator
		mux.HandleFunc("/typing", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}

			var req struct {
				To     string `json:"to"`
				Typing bool   `json:"typing"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "invalid request body", http.StatusBadRequest)
				return
			}

			if err := a.SendTypingIndicator(ctx, req.To, nil, req.Typing); err != nil {
				logger.Error("failed to send typing indicator", "error", err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "sent"})
		})

		// Discover agents
		mux.HandleFunc("/discover", func(w http.ResponseWriter, r *http.Request) {
			result, err := a.CallRelay(ctx, "relay.discover", nil)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(result)
		})

		// RPC call
		mux.HandleFunc("/call", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}

			var req struct {
				To     string `json:"to"`
				Method string `json:"method"`
				Params any    `json:"params"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "invalid request body", http.StatusBadRequest)
				return
			}

			resp, err := a.Send(ctx, req.To, req.Method, req.Params)
			if err != nil {
				logger.Error("RPC call failed", "error", err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":     resp.ID.String(),
				"result": string(resp.Body),
			})
		})

		httpServer = &http.Server{
			Addr:              httpAddress,
			Handler:           mux,
			ReadHeaderTimeout: 10 * time.Second,
		}

		go func() {
			var err error
			if useHTTPTLS {
				logger.Info("HTTP server starting with TLS", "addr", httpAddress)
				err = httpServer.ListenAndServeTLS(certFile, keyFile)
			} else {
				logger.Info("HTTP server starting", "addr", httpAddress)
				err = httpServer.ListenAndServe()
			}
			if err != nil && err != http.ErrServerClosed {
				logger.Error("HTTP server error", "error", err)
			}
		}()
	}

	// Connect to relay if specified (with retry logic)
	if relayAddr != "" {
		go connectToRelayWithRetry(ctx, a, relayAddr, logger)
	}

	// Register delivery ack handler
	a.OnDeliveryAck(func(messageID uuid.UUID, delivered bool, err error) {
		if delivered {
			logger.Info("message delivered", "id", messageID)
		} else {
			logger.Warn("message delivery failed", "id", messageID, "error", err)
		}
	})

	// Print agent info
	fmt.Println("Agent started")
	fmt.Printf("  ID:  %s\n", a.ID())
	fmt.Printf("  DID: %s\n", a.DID())
	if listenAddr != "" && a.ListenAddr() != "" {
		wsProtocol := "ws"
		if useTLS {
			wsProtocol = "wss"
		}
		fmt.Printf("  WebSocket: %s://%s\n", wsProtocol, a.ListenAddr())
	}
	if httpAddress != "" {
		httpProtocol := "http"
		if useHTTPTLS {
			httpProtocol = "https"
		}
		fmt.Printf("  Agent Card: %s://%s/.well-known/agent.json\n", httpProtocol, httpAddress)
		fmt.Printf("  HTTP Endpoints:\n")
		fmt.Printf("    POST /send-chat   - Send chat message {to, text}\n")
		fmt.Printf("    POST /send-async  - Fire-and-forget {to, method, params}\n")
		fmt.Printf("    POST /call        - RPC call {to, method, params}\n")
		fmt.Printf("    POST /typing      - Typing indicator {to, typing}\n")
		fmt.Printf("    GET  /discover    - List agents on relay\n")
		fmt.Printf("    GET  /chat-history - Get chat history\n")
	}

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	logger.Info("shutting down...")

	// Shutdown HTTP server
	if httpServer != nil {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			logger.Error("HTTP server shutdown error", "error", err)
		}
		shutdownCancel()
	}

	// Shutdown metrics server
	if metricsServer != nil {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := metricsServer.Shutdown(shutdownCtx); err != nil {
			logger.Error("metrics server shutdown error", "error", err)
		}
		shutdownCancel()
	}

	// Shutdown tracer
	if tracerProvider != nil {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := tracerProvider.Shutdown(shutdownCtx); err != nil {
			logger.Error("tracer shutdown error", "error", err)
		}
		shutdownCancel()
	}

	if err := a.Stop(); err != nil {
		logger.Error("agent stop error", "error", err)
	}
}

// connectToRelayWithRetry connects to the relay with exponential backoff retry.
func connectToRelayWithRetry(ctx context.Context, a *agent.Agent, relayAddr string, logger *slog.Logger) {
	maxRetries := 10
	baseDelay := 1 * time.Second
	maxDelay := 30 * time.Second

	for attempt := 1; attempt <= maxRetries; attempt++ {
		select {
		case <-ctx.Done():
			return
		default:
		}

		logger.Info("connecting to relay", "addr", relayAddr, "attempt", attempt)

		if err := a.Connect(ctx, relayAddr); err != nil {
			shift := attempt - 1
			if shift < 0 {
				shift = 0
			}
			delay := time.Duration(1<<shift) * baseDelay
			if delay > maxDelay {
				delay = maxDelay
			}
			logger.Warn("failed to connect to relay, retrying", "error", err, "delay", delay)

			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
				continue
			}
		}

		// Successfully connected, now register with proof of DID ownership
		timestamp := time.Now().Unix()
		proofMessage := fmt.Sprintf("%s:%d", a.DID(), timestamp)
		proof := a.Sign([]byte(proofMessage))

		// Create registration request with proof
		regReq := map[string]any{
			"id":           a.Record().ID,
			"did":          a.Record().DID,
			"display_name": a.Record().DisplayName,
			"public_keys":  a.Record().PublicKeys,
			"endpoints":    a.Record().Endpoints,
			"capabilities": a.Record().Capabilities,
			"status":       a.Record().Status,
			"proof":        proof,
			"timestamp":    timestamp,
		}

		result, err := a.CallRelay(ctx, "relay.register", regReq)
		if err != nil {
			logger.Error("failed to register with relay", "error", err)
			return
		}
		logger.Info("registered with relay", "result", string(result))

		// Discover and cache other agents for signature verification
		time.Sleep(2 * time.Second) // Wait for other agents to register
		discoverPeers(ctx, a, logger)
		return
	}

	logger.Error("failed to connect to relay after max retries", "attempts", maxRetries)
}

// discoverPeers discovers and caches peer public keys for signature verification.
func discoverPeers(ctx context.Context, a *agent.Agent, logger *slog.Logger) {
	discoverResult, err := a.CallRelay(ctx, "relay.discover", nil)
	if err != nil {
		logger.Warn("failed to discover peers", "error", err)
		return
	}

	var peers []struct {
		DID         string `json:"did"`
		DisplayName string `json:"display_name"`
		PublicKeys  []struct {
			ID      string `json:"id"`
			Type    string `json:"type"`
			Key     string `json:"key"`
			Purpose string `json:"purpose"`
		} `json:"public_keys"`
	}
	if err := json.Unmarshal(discoverResult, &peers); err != nil {
		logger.Warn("failed to parse discovered peers", "error", err)
		return
	}

	for _, peer := range peers {
		if peer.DID == a.DID() {
			continue // Skip self
		}
		// Convert to registry.PeerKey
		var peerKeys []registry.PeerKey
		for _, k := range peer.PublicKeys {
			peerKeys = append(peerKeys, registry.PeerKey{
				ID:      k.ID,
				Type:    k.Type,
				Key:     k.Key,
				Purpose: k.Purpose,
			})
		}
		// Add peer to local registry
		if err := a.Discovery().AddPeer(peer.DID, peer.DisplayName, peerKeys); err != nil {
			logger.Debug("failed to add peer", "did", peer.DID, "error", err)
		} else {
			logger.Info("discovered peer", "did", peer.DID, "name", peer.DisplayName)
		}
	}
}
