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
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/gianlucamazza/msg2agent/adapters/a2a"
	mcpadapter "github.com/gianlucamazza/msg2agent/adapters/mcp"
	"github.com/gianlucamazza/msg2agent/pkg/agent"
	"github.com/gianlucamazza/msg2agent/pkg/billing"
	"github.com/gianlucamazza/msg2agent/pkg/buildinfo"
	"github.com/gianlucamazza/msg2agent/pkg/config"
	"github.com/gianlucamazza/msg2agent/pkg/httputil"
	"github.com/gianlucamazza/msg2agent/pkg/identity"
	"github.com/gianlucamazza/msg2agent/pkg/messaging"
	"github.com/gianlucamazza/msg2agent/pkg/registry"
)

// agentBridge adapts *agent.Agent to mcpadapter.AgentCaller.
type agentBridge struct {
	a *agent.Agent
}

func (b *agentBridge) DID() string             { return b.a.DID() }
func (b *agentBridge) Record() *registry.Agent { return b.a.Record() }
func (b *agentBridge) CallRelay(ctx context.Context, method string, params any) (json.RawMessage, error) {
	return b.a.CallRelay(ctx, method, params)
}
func (b *agentBridge) Send(ctx context.Context, to, method string, params any) (mcpadapter.AgentMessage, error) {
	msg, err := b.a.Send(ctx, to, method, params)
	if err != nil {
		return nil, err
	}
	return &messageWrapper{msg}, nil
}

func (b *agentBridge) SendAsync(ctx context.Context, to, method string, params any) (string, error) {
	id, err := b.a.SendAsync(ctx, to, method, params)
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

// messageWrapper adapts *messaging.Message to mcpadapter.AgentMessage.
type messageWrapper struct {
	m *messaging.Message
}

func (w *messageWrapper) IsError() bool            { return w.m.IsError() }
func (w *messageWrapper) RawBody() json.RawMessage { return json.RawMessage(w.m.Body) }

func main() {
	// Flags — all accept MSG2AGENT_* env fallback (applied after flag.Parse).
	name := flag.String("name", "", "Agent name (env: MSG2AGENT_NAME)")
	domain := flag.String("domain", "", "Agent domain (env: MSG2AGENT_DOMAIN)")
	relay := flag.String("relay", "", "Relay hub address (env: MSG2AGENT_RELAY_URL)")
	transport := flag.String("transport", "stdio", "MCP transport: stdio, sse, streamable-http")
	addr := flag.String("addr", "", "Listen address for SSE/HTTP transports (env: MSG2AGENT_HTTP_ADDR)")
	identFile := flag.String("identity-file", "", "Path to identity key file for persistence")
	billingDB := flag.String("billing-db", "", "Path to billing DB (enables API key auth when set) (env: MSG2AGENT_BILLING_DB)")
	billingDriver := flag.String("billing-driver", "", "Billing store driver: sqlite, postgres (env: MSG2AGENT_BILLING_DRIVER)")
	allowAnon := flag.Bool("allow-anon", false, "Allow unauthenticated MCP requests (only valid without --billing-db)")
	oauth2IssuerURL := flag.String("oauth2-issuer-url", "", "OAuth2 OIDC issuer URL (env: MSG2AGENT_OAUTH2_ISSUER_URL)")
	oauth2Audience := flag.String("oauth2-audience", "", "Expected OAuth2 token audience (env: MSG2AGENT_OAUTH2_AUDIENCE)")
	oauth2JWKSUrl := flag.String("oauth2-jwks-url", "", "JWKS endpoint URL (env: MSG2AGENT_OAUTH2_JWKS_URL)")
	oauthASBaseURL := flag.String("oauth-as-base-url", "", "OAuth 2.1 AS base URL for JWT access token verification (env: MSG2AGENT_OAUTH_AS_BASE_URL)")
	oauthSigningKeyPath := flag.String("oauth-signing-key", "/data/oauth-signing-key.pem", "Path to Ed25519 signing key PEM shared with relay (env: MSG2AGENT_OAUTH_SIGNING_KEY)")
	shutdownTimeout := flag.Duration("shutdown-timeout", 30*time.Second, "Graceful shutdown timeout for HTTP server")
	auditVerifierIntervalFlag := flag.Duration("audit-verifier-interval", 6*time.Hour, "Interval for background audit chain verification (0 = disabled)")
	flag.Parse()

	// Apply env fallbacks.
	agentName := config.FlagOrEnv(*name, "NAME", "mcp-agent")
	agentDomain := config.FlagOrEnv(*domain, "DOMAIN", "localhost")
	relayAddr := config.FlagOrEnv(*relay, "RELAY_URL", "ws://localhost:8080")
	listenAddr := config.FlagOrEnv(*addr, "HTTP_ADDR", ":8081")
	dbPath := config.FlagOrEnv(*billingDB, "BILLING_DB", "")
	dbDriver := config.FlagOrEnv(*billingDriver, "BILLING_DRIVER", "sqlite")
	issuerURL := config.FlagOrEnv(*oauth2IssuerURL, "OAUTH2_ISSUER_URL", "")
	audience := config.FlagOrEnv(*oauth2Audience, "OAUTH2_AUDIENCE", "")
	jwksURL := config.FlagOrEnv(*oauth2JWKSUrl, "OAUTH2_JWKS_URL", "")

	// Validate transport flag.
	tp := mcpadapter.TransportType(*transport)
	switch tp {
	case mcpadapter.TransportStdio, mcpadapter.TransportSSE, mcpadapter.TransportStreamableHTTP:
	default:
		fmt.Fprintf(os.Stderr, "unknown transport: %s\n", *transport)
		os.Exit(1)
	}

	// F5: --allow-anon is mutually exclusive with --billing-db.
	if dbPath != "" && *allowAnon {
		fmt.Fprintln(os.Stderr, "error: --allow-anon and --billing-db are mutually exclusive; "+
			"--allow-anon bypasses billing enforcement. Remove one of these flags.")
		os.Exit(1)
	}

	// Setup logging to stderr (stdout is used for MCP JSON-RPC in stdio mode).
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	logger := slog.New(handler)
	logger.Info("starting mcp-server", "version", buildinfo.Version, "commit", buildinfo.Commit, "date", buildinfo.Date)

	// Load or create persistent identity.
	keyPath := *identFile
	if keyPath == "" {
		home, _ := os.UserHomeDir()
		keyPath = filepath.Join(home, ".msg2agent", agentName+".key")
	}

	var ident *identity.Identity
	if existing, err := identity.LoadFromFile(keyPath, agentDomain, agentName); err == nil {
		ident = existing
		logger.Info("loaded identity from file", "path", keyPath)
	} else {
		ident, err = identity.NewIdentity(agentDomain, agentName)
		if err != nil {
			logger.Error("failed to create identity", "error", err)
			os.Exit(1)
		}
		if err := os.MkdirAll(filepath.Dir(keyPath), 0700); err != nil {
			logger.Warn("failed to create identity directory", "error", err)
		} else if err := identity.SaveToFile(ident, keyPath); err != nil {
			logger.Warn("failed to save identity", "error", err)
		} else {
			logger.Info("created new identity", "path", keyPath)
		}
	}

	// Create agent.
	cfg := agent.Config{
		Domain:           agentDomain,
		AgentID:          agentName,
		DisplayName:      agentName,
		RelayAddr:        relayAddr,
		Logger:           logger,
		Identity:         ident,
		RelayBearerToken: os.Getenv("MSG2AGENT_SERVICE_TOKEN"),
	}

	a, err := agent.New(cfg)
	if err != nil {
		logger.Error("failed to create agent", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := a.Start(ctx); err != nil {
		logger.Error("failed to start agent", "error", err)
		cancel()
		os.Exit(1)
	}

	// Connect to relay and register with DID ownership proof.
	logger.Info("connecting to relay", "addr", relayAddr)
	if err := a.Connect(ctx, relayAddr); err != nil {
		logger.Error("failed to connect to relay", "error", err)
		os.Exit(1)
	}

	// Register with relay (proof of DID ownership).
	{
		tsSec := time.Now().Unix()
		proofMessage := fmt.Sprintf("%s:%d", a.DID(), tsSec)
		proof := a.Sign([]byte(proofMessage))

		regReq := map[string]any{
			"id":                   a.Record().ID,
			"did":                  a.Record().DID,
			"display_name":         a.Record().DisplayName,
			"public_keys":          a.Record().PublicKeys,
			"endpoints":            a.Record().Endpoints,
			"capabilities":         a.Record().Capabilities,
			"status":               a.Record().Status,
			"proof":                proof,
			"timestamp":            tsSec,
			"role":                 "gateway",
			"delegation_namespace": fmt.Sprintf("did:wba:%s:tenant:*", agentDomain),
		}

		result, err := a.CallRelay(ctx, "relay.register", regReq)
		if err != nil {
			logger.Error("failed to register with relay", "error", err)
			os.Exit(1)
		}
		logger.Info("registered with relay", "result", string(result))
	}

	// Discover existing peers for signature verification.
	{
		discoverResult, err := a.CallRelay(ctx, "relay.discover", nil)
		if err != nil {
			logger.Warn("failed to discover peers", "error", err)
		} else {
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
			} else {
				for _, peer := range peers {
					if peer.DID == a.DID() {
						continue
					}
					var peerKeys []registry.PeerKey
					for _, k := range peer.PublicKeys {
						peerKeys = append(peerKeys, registry.PeerKey{
							ID:      k.ID,
							Type:    k.Type,
							Key:     k.Key,
							Purpose: k.Purpose,
						})
					}
					if err := a.Discovery().AddPeer(peer.DID, peer.DisplayName, peerKeys); err != nil {
						logger.Debug("failed to add peer", "did", peer.DID, "error", err)
					} else {
						logger.Info("discovered peer", "did", peer.DID, "name", peer.DisplayName)
					}
				}
			}
		}
	}

	// Prepare billing when a DB path is provided (streamable-http only).
	var billingStore billing.Store
	var billingCloser func() error

	if dbPath != "" && tp == mcpadapter.TransportStreamableHTTP {
		bStore, _, err := billing.NewStore(dbDriver, dbPath)
		if err != nil {
			logger.Error("failed to open billing store", "driver", dbDriver, "path", dbPath, "error", err)
			os.Exit(1)
		}
		billingStore = bStore
		if c, ok := bStore.(interface{ Close() error }); ok {
			billingCloser = c.Close
		}

		auditInterval := *auditVerifierIntervalFlag
		if auditInterval > 0 {
			bAdmin, _ := bStore.(billing.AdminStore) // *SQLiteStore implements AdminStore
			billing.StartPeriodicVerifier(ctx, billingStore, bAdmin, auditInterval, logger)
			logger.Info("audit chain verifier started", "interval", auditInterval)
		}
		eventStore, ok := bStore.(billing.EventStore)
		if !ok {
			logger.Error("billing store does not implement EventStore")
			os.Exit(1)
		}
		meter := billing.NewUsageMeter()
		if err := meter.RestoreFromAggregates(eventStore); err != nil {
			logger.Warn("billing: failed to restore usage counters", "error", err)
		}
		meter.WithStore(ctx, eventStore, logger)
		if n := billing.NewWebhookNotifierFromEnv(logger); n != nil {
			meter.WithNotifier(n)
			logger.Info("billing webhook notifier enabled", "url", n.URL)
		}
		logger.Info("billing enabled", "driver", dbDriver, "db", dbPath)
	}

	// Build caller: use gatewayBridge (per-tenant DID) when billing is enabled,
	// plain agentBridge (process-wide DID) when running without billing.
	var caller mcpadapter.AgentCaller
	if billingStore != nil {
		caller = &gatewayBridge{a: a, domain: agentDomain, store: billingStore}
	} else {
		caller = &agentBridge{a: a}
	}

	// Create MCP server via adapter.
	mcpServer := mcpadapter.NewMCPServer(
		caller,
		mcpadapter.ServerConfig{
			Name:      agentName,
			Version:   "0.1.0",
			Transport: tp,
			Addr:      listenAddr,
		},
		logger,
	)

	// Wire HTTP-level auth: BearerMiddleware supports both API keys and JWT access tokens.
	if billingStore != nil {
		// Resolve OAuth 2.1 AS JWT validator (our own AS).
		var accessTokenVal billing.AccessTokenValidator
		asBase := config.FlagOrEnv(*oauthASBaseURL, "OAUTH_AS_BASE_URL", "")
		if asBase != "" {
			skPath := config.FlagOrEnv(*oauthSigningKeyPath, "OAUTH_SIGNING_KEY", "/data/oauth-signing-key.pem")
			privKey, err := oauthLoadKey(skPath)
			if err != nil {
				logger.Error("mcp-server: failed to load OAuth signing key", "path", skPath, "error", err)
				os.Exit(1)
			}
			_, kid, err := oauthBuildKID(privKey)
			if err != nil {
				logger.Error("mcp-server: failed to build JWK kid", "error", err)
				os.Exit(1)
			}
			accessTokenVal = oauthNewVerifier(privKey, kid, asBase)
			logger.Info("OAuth 2.1 JWT access token auth enabled", "as", asBase)
		}

		bearerMW := billing.BearerMiddleware(billingStore, accessTokenVal, false)

		// Optionally wrap with Google OIDC middleware (for A2A / human login flow).
		if issuerURL != "" {
			if jwksURL == "" {
				jwksURL = strings.TrimRight(issuerURL, "/") + "/.well-known/jwks.json"
			}
			oauthCfg := a2a.OAuth2Config{
				JWKSURL:  jwksURL,
				Issuer:   issuerURL,
				Audience: audience,
			}
			validator := a2a.NewBillingValidator(a2a.NewOAuth2Validator(oauthCfg))
			autoProvisionPlan := billing.Plan(os.Getenv("MSG2AGENT_OAUTH_AUTO_PROVISION"))
			oauthMW := billing.OAuth2Middleware(validator, billingStore, autoProvisionPlan)
			mcpServer.WithAuthMiddleware(func(h http.Handler) http.Handler {
				return oauthMW(bearerMW(h))
			})
			logger.Info("Google OIDC + bearer auth enabled", "issuer", issuerURL)
		} else {
			mcpServer.WithAuthMiddleware(bearerMW)
			logger.Info("bearer auth enabled (API key + OAuth 2.1 JWT)")
		}
	}

	// Register catch-all handler for incoming messages.
	a.RegisterMethod("*", func(ctx context.Context, params json.RawMessage) (any, error) {
		from := agent.MessageFrom(ctx)
		method := agent.MessageMethod(ctx)
		if from == "" {
			from = "unknown"
		}
		if method == "" {
			method = "*"
		}
		mcpServer.HandleIncomingMessage(from, method, params)
		return map[string]string{"status": "received"}, nil
	})

	// For non-HTTP transports, use simple Serve() (stdio/SSE handle their own lifecycle).
	if tp != mcpadapter.TransportStreamableHTTP {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-sigCh
			logger.Info("shutting down...")
			_ = a.Stop()
			cancel()
		}()
		logger.Info("starting MCP server", "transport", *transport)
		if err := mcpServer.Serve(); err != nil {
			logger.Error("mcp server error", "error", err)
			os.Exit(1)
		}
		return
	}

	// F1: Build HTTP mux with health/ready/metrics alongside /mcp.
	mux := http.NewServeMux()
	mcpServer.RegisterWithMux(mux, "/mcp")

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if billingStore != nil {
			if err := billingStore.Ping(); err != nil {
				http.Error(w, "billing store unavailable: "+err.Error(), http.StatusServiceUnavailable)
				return
			}
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	mux.HandleFunc("/ready", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":    "ready",
			"agent_did": a.DID(),
		})
	})

	// /metrics exposed internally only (not routed through nginx to public).
	mux.Handle("/metrics", promhttp.Handler())

	// RFC 9728 — OAuth 2.0 Protected Resource Metadata (public, no auth).
	{
		type rsMeta struct {
			Resource             string   `json:"resource"`
			AuthorizationServers []string `json:"authorization_servers"`
			BearerMethods        []string `json:"bearer_methods_supported"`
			Scopes               []string `json:"scopes_supported"`
		}
		asBase := config.FlagOrEnv(*oauthASBaseURL, "OAUTH_AS_BASE_URL", "")
		m := rsMeta{
			Resource:      asBase + "/mcp",
			BearerMethods: []string{"header"},
			Scopes:        []string{"mcp:tools:read", "mcp:tools:write", "mcp:tools:destructive"},
		}
		if asBase != "" {
			m.AuthorizationServers = []string{asBase}
		}
		oauthResourceMeta, _ := json.Marshal(m)
		serveOAuthResource := func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-cache")
			_, _ = w.Write(oauthResourceMeta)
		}
		mux.HandleFunc("/.well-known/oauth-protected-resource", serveOAuthResource)
		mux.HandleFunc("/.well-known/oauth-protected-resource/mcp", serveOAuthResource)
	}

	// Stripe webhook is owned by the relay (POST /api/billing/webhook on the
	// public NPM-exposed port). mcp-server is loopback-only and shares the
	// billing DB, so it sees relay's writes without needing its own handler.

	srv := &http.Server{
		Addr:              listenAddr,
		Handler:           httputil.SecurityHeaders(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		logger.Info("starting MCP server", "transport", *transport, "addr", listenAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("mcp server error", "error", err)
			os.Exit(1)
		}
	}()

	// F2: Wait for signal then drain in-flight requests before exiting.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	logger.Info("shutting down...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), *shutdownTimeout)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("http server shutdown error", "error", err)
	}

	if err := a.Stop(); err != nil {
		logger.Error("agent stop error", "error", err)
	}
	if billingCloser != nil {
		if err := billingCloser(); err != nil {
			logger.Error("billing store close error", "error", err)
		}
	}
	cancel()
}
