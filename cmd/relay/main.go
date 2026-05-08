// Package main provides the relay hub executable.
package main

import (
	"context"
	"crypto/tls"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/gianlucamazza/msg2agent/adapters/a2a"
	"github.com/gianlucamazza/msg2agent/pkg/billing"
	"github.com/gianlucamazza/msg2agent/pkg/buildinfo"
	"github.com/gianlucamazza/msg2agent/pkg/email"
	"github.com/gianlucamazza/msg2agent/pkg/httputil"
	"github.com/gianlucamazza/msg2agent/pkg/oauth"
	"github.com/gianlucamazza/msg2agent/pkg/queue"
	"github.com/gianlucamazza/msg2agent/pkg/registry"
	"github.com/gianlucamazza/msg2agent/pkg/security"
	"github.com/gianlucamazza/msg2agent/pkg/telemetry"
	"github.com/gianlucamazza/msg2agent/pkg/webui"
)

//go:embed web
var webFS embed.FS

func main() {
	cfg, err := parseAppConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "invalid configuration:", err)
		os.Exit(1)
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.LogLevel,
	}))
	logger.Info("starting relay", "version", buildinfo.Version, "commit", buildinfo.Commit, "date", buildinfo.Date)

	// Initialize tracing if configured
	var tracerProvider *telemetry.TracerProvider
	if cfg.OTLPEndpoint != "" || cfg.TraceStdout {
		var err error
		tracerProvider, err = telemetry.InitTracer(context.Background(), telemetry.TracerConfig{
			ServiceName:  "msg2agent-relay",
			Environment:  "development",
			OTLPEndpoint: cfg.OTLPEndpoint,
			UseStdout:    cfg.TraceStdout,
			Logger:       logger,
		})
		if err != nil {
			logger.Error("failed to initialize tracing", "error", err)
			os.Exit(1)
		}
		logger.Info("tracing initialized")
	}

	relayCfg := DefaultRelayConfig()
	relayCfg.MaxConnections = cfg.MaxConnections
	relayCfg.MessageRateLimit = cfg.MessageRateLimit
	relayCfg.AllowedOrigins = cfg.AllowedOrigins
	relayCfg.RequireDIDProof = !cfg.SkipDIDVerification

	// Validate configuration
	if err := relayCfg.Validate(); err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	// Log security configuration
	if cfg.SkipDIDVerification {
		logger.Warn("DID proof verification disabled - not recommended for production")
	} else {
		logger.Info("DID proof verification enabled")
	}

	// Log CORS configuration
	if len(cfg.AllowedOrigins) > 0 {
		logger.Info("CORS configured", "origins", cfg.AllowedOrigins)
	} else {
		logger.Warn("CORS: no external origins allowed (same-origin only)")
	}

	// Create store based on configuration
	var store registry.Store
	var storeCloser func() error // for stores that need cleanup
	storeFilePath := cfg.StoreFile

	switch cfg.StoreType {
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
		switch cfg.StoreType {
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

	// Configure billing (optional)
	if cfg.BillingDBPath != "" {
		bStore, err := billing.NewSQLiteStore(cfg.BillingDBPath)
		if err != nil {
			logger.Error("failed to open billing store", "path", cfg.BillingDBPath, "error", err)
			os.Exit(1)
		}
		hub.billingStore = bStore
		hub.tenantPool = billing.NewTenantRateLimiterPool(bStore)
		logger.Info("relay billing enabled", "db", cfg.BillingDBPath)

		auditInterval := cfg.AuditVerifierInterval
		if auditInterval > 0 {
			// bStore is *billing.SQLiteStore which implements billing.AdminStore directly.
			billing.StartPeriodicVerifier(hub.ctx, hub.billingStore, bStore, auditInterval, logger)
			logger.Info("audit chain verifier started", "interval", auditInterval)
		}

		// Optional: JWT validator for OAuth2 WS auth alongside API keys.
		if cfg.OAuth2IssuerURL != "" {
			jwksURL := cfg.OAuth2JWKSURL
			if jwksURL == "" {
				jwksURL = strings.TrimRight(cfg.OAuth2IssuerURL, "/") + "/.well-known/jwks.json"
			}
			oauthCfg := a2a.OAuth2Config{
				JWKSURL:  jwksURL,
				Issuer:   cfg.OAuth2IssuerURL,
				Audience: cfg.OAuth2Audience,
			}
			hub.jwtValidator = a2a.NewBillingValidator(a2a.NewOAuth2Validator(oauthCfg))
			logger.Info("relay OAuth2 JWT auth enabled", "issuer", cfg.OAuth2IssuerURL)
		}
	}

	// OAuth 2.1 Authorization Server (Phase B — our own AS).
	// Activated when MSG2AGENT_OAUTH_AS_BASE_URL is set.
	// Vars are declared here so the mux section can reference them.
	var (
		oauthAccessTokenVal billing.AccessTokenValidator
		oauthAuthzSrv       *oauth.AuthorizeServer
		oauthJWKSHandler    http.Handler
		oauthDCRHandler     http.Handler
		oauthTokenHandler   http.Handler
		oauthRevokeHandler  http.Handler
		oauthASMeta         *oauth.ASMetadata
		oauthASBaseURLStr   string
	)
	oauthASBaseURLStr = cfg.OAuthASBaseURL
	if oauthASBaseURLStr != "" && hub.billingStore != nil {
		privKey, err := oauth.LoadOrGenerateEd25519(cfg.OAuthSigningKeyPath)
		if err != nil {
			logger.Error("oauth AS: failed to load/generate signing key", "path", cfg.OAuthSigningKeyPath, "error", err)
			os.Exit(1)
		}
		jwkSet, kid, err := oauth.BuildJWK(privKey)
		if err != nil {
			logger.Error("oauth AS: failed to build JWK set", "error", err)
			os.Exit(1)
		}
		jwtIssuer := oauth.NewJWTIssuer(privKey, kid, oauthASBaseURLStr)
		jwtVerifier := oauth.NewJWTVerifier(privKey, oauthASBaseURLStr, oauthASBaseURLStr+"/mcp")
		oauthAccessTokenVal = jwtVerifier

		oauthCombinedStore, ok := hub.billingStore.(billingOAuthStore)
		if !ok {
			logger.Error("oauth AS: billing store does not implement OAuth store (only SQLiteStore supported)")
			os.Exit(1)
		}
		oauthASMeta = oauth.NewASMetadata(oauthASBaseURLStr)

		var idp oauth.IdentityProvider
		if cfg.OAuthGoogleClientID != "" {
			idp = oauth.NewGoogleIDP(cfg.OAuthGoogleClientID, cfg.OAuthGoogleClientSecret, oauthASBaseURLStr+"/oauth/google-callback")
		}

		tenantLookup := &billingTenantLookup{store: oauthCombinedStore}
		identityReg := &billingIdentityRegistrar{store: oauthCombinedStore}
		oauthStore := oauth.Store(oauthCombinedStore)
		oauthAuthzSrv = oauth.NewAuthorizeServer(oauthStore, idp, tenantLookup, jwtIssuer, jwtVerifier, oauthASBaseURLStr).
			WithIdentityRegistrar(identityReg)
		oauthJWKSHandler = oauth.JWKSHandler(jwkSet)
		oauthDCRHandler = oauth.DCRHandler(oauthStore)
		oauthTokenHandler = oauth.TokenHandler(oauthStore, jwtIssuer, oauthASBaseURLStr+"/mcp", tenantLookup)
		oauthRevokeHandler = oauth.RevokeHandler(oauthStore)

		logger.Info("oauth AS enabled", "base_url", oauthASBaseURLStr, "kid", kid, "google_idp", cfg.OAuthGoogleClientID != "")
	}

	// Internal service token — lets mcp-server connect without billing API key.
	if svcToken := os.Getenv("MSG2AGENT_SERVICE_TOKEN"); svcToken != "" {
		hub.serviceToken = svcToken
		logger.Info("relay service token configured for internal agent auth")
	}

	// Configure DID allowlist
	hub.aclEnforcer = security.NewACLEnforcer()
	if len(cfg.AllowedDIDs) > 0 {
		hub.allowlistACL = security.TrustedAgentsPolicy(cfg.AllowedDIDs)
		logger.Info("DID allowlist enabled", "count", len(cfg.AllowedDIDs))
	} else {
		logger.Info("DID allowlist disabled (open relay)")
	}

	mux := http.NewServeMux()

	// Public static pages — served from embedded web/ FS, no auth.
	webSub, err := fs.Sub(webFS, "web")
	if err != nil {
		logger.Error("failed to sub embedded web FS", "error", err)
		os.Exit(1)
	}

	// paidEnabled gates Stripe-dependent UI (Starter/Team CTAs). Auto-activates
	// when STRIPE_SECRET_KEY is present in the environment at startup.
	paidEnabled := billing.StripeConfigFromEnv() != nil
	logger.Info("public ui", "paid_enabled", paidEnabled)

	serveHTML := func(name string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			data, err := fs.ReadFile(webSub, name)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(data)
		}
	}
	serveAsset := func(name, contentType string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			data, err := fs.ReadFile(webSub, name)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", contentType)
			w.Header().Set("Cache-Control", "public, max-age=86400")
			_, _ = w.Write(data)
		}
	}
	mux.HandleFunc("/pricing", serveHTML("pricing.html"))
	mux.HandleFunc("/privacy", serveHTML("privacy.html"))
	mux.HandleFunc("/terms", serveHTML("terms.html"))
	mux.HandleFunc("/favicon.svg", serveAsset("favicon.svg", "image/svg+xml"))
	mux.HandleFunc("/logo-512.png", serveAsset("logo-512.png", "image/png"))
	mux.HandleFunc("/logo-180.png", serveAsset("logo-180.png", "image/png"))
	mux.HandleFunc("/robots.txt", serveAsset("robots.txt", "text/plain; charset=utf-8"))
	mux.HandleFunc("/sitemap-index.xml", serveAsset("sitemap-index.xml", "application/xml"))
	mux.HandleFunc("/sitemap-0.xml", serveAsset("sitemap-0.xml", "application/xml"))
	mux.HandleFunc("/style.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		_, _ = w.Write(webui.CSS())
	})
	mux.HandleFunc("/api/public/config", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{"paid_enabled": paidEnabled})
	})

	// Astro JS/CSS chunks — served with long-lived cache (content-hashed filenames).
	astroFileServer := http.FileServer(http.FS(webSub))
	mux.HandleFunc("/_astro/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		astroFileServer.ServeHTTP(w, r)
	})

	// Root: WebSocket upgrade for agents, landing page for browsers.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") == "websocket" {
			hub.handleWebSocket(w, r)
			return
		}
		if r.URL.Path == "/" {
			serveHTML("index.html")(w, r)
			return
		}
		http.NotFound(w, r)
	})

	// Health check endpoint — also verifies billing store if enabled.
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if hub.billingStore != nil {
			if err := hub.billingStore.Ping(); err != nil {
				http.Error(w, "billing store unavailable: "+err.Error(), http.StatusServiceUnavailable)
				return
			}
		}
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

	// RFC 9728 — OAuth 2.0 Protected Resource Metadata (public, no auth).
	// authorization_servers is populated when the OAuth 2.1 AS is active.
	{
		type rsMeta struct {
			Resource             string   `json:"resource"`
			AuthorizationServers []string `json:"authorization_servers"`
			BearerMethods        []string `json:"bearer_methods_supported"`
			Scopes               []string `json:"scopes_supported"`
		}
		m := rsMeta{
			Resource:      oauthASBaseURLStr + "/mcp",
			BearerMethods: []string{"header"},
			Scopes:        []string{"mcp:tools:read", "mcp:tools:write", "mcp:tools:destructive"},
		}
		if oauthASBaseURLStr != "" {
			m.AuthorizationServers = []string{oauthASBaseURLStr}
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

	// OAuth 2.1 AS endpoints — only mounted when AS is enabled.
	if oauthAuthzSrv != nil {
		mux.Handle("/.well-known/oauth-authorization-server", oauth.ASMetadataHandler(oauthASMeta))
		mux.Handle("/.well-known/jwks.json", oauthJWKSHandler)
		mux.Handle("/oauth/register", oauthDCRHandler)
		mux.HandleFunc("/oauth/authorize", oauthAuthzSrv.HandleAuthorize)
		mux.HandleFunc("/oauth/google-callback", oauthAuthzSrv.HandleGoogleCallback)
		mux.HandleFunc("/oauth/authorize/confirm", oauthAuthzSrv.HandleConfirm)
		mux.Handle("/oauth/token", oauthTokenHandler)
		mux.Handle("/oauth/revoke", oauthRevokeHandler)
		logger.Info("oauth AS routes mounted",
			"authorize", "/oauth/authorize",
			"token", "/oauth/token",
			"register", "/oauth/register",
			"jwks", "/.well-known/jwks.json",
		)
	}

	// A2A AgentCard — public, no auth, served at /.well-known/agent.json
	if cfg.AgentCardPath != "" {
		mux.HandleFunc("/.well-known/agent.json", func(w http.ResponseWriter, r *http.Request) {
			data, err := os.ReadFile(cfg.AgentCardPath)
			if err != nil {
				http.Error(w, "agent card not found", http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "public, max-age=300")
			_, _ = w.Write(data)
		})
		logger.Info("agent card endpoint enabled", "path", "/.well-known/agent.json", "file", cfg.AgentCardPath)
	}

	// Connector manifest — Anthropic Connector Directory discovery
	if cfg.ConnectorManifestPath != "" {
		manifestBytes, err := os.ReadFile(cfg.ConnectorManifestPath)
		if err != nil {
			logger.Warn("connector manifest not found, endpoint disabled", "file", cfg.ConnectorManifestPath, "err", err)
		} else {
			mux.HandleFunc("/.well-known/mcp-connector.json", func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Cache-Control", "public, max-age=300")
				_, _ = w.Write(manifestBytes)
			})
			logger.Info("connector manifest endpoint enabled", "path", "/.well-known/mcp-connector.json", "file", cfg.ConnectorManifestPath)
		}
	}

	// Stripe billing endpoints (opt-in; requires STRIPE_SECRET_KEY env var)
	var stripeClient *billing.StripeClient
	if hub.billingStore != nil {
		// Period rollover checker: resets usage counters when billing periods end.
		meter := billing.NewUsageMeter()
		billing.StartPeriodRolloverChecker(hub.ctx, hub.billingStore, meter, logger)

		if stripeCfg := billing.StripeConfigFromEnv(); stripeCfg != nil {
			stripeClient = billing.NewStripeClient(*stripeCfg)

			// Stripe reconciler: periodically syncs subscription state from Stripe.
			billing.StartStripeReconciler(hub.ctx, hub.billingStore, stripeClient, cfg.StripeReconcileEvery, cfg.StripeAutoReconcile, logger)
			if cfg.StripeReconcileEvery > 0 {
				logger.Info("Stripe reconciler started",
					"interval", cfg.StripeReconcileEvery,
					"auto_fix", cfg.StripeAutoReconcile)
			}

			authMW := billing.BearerMiddleware(hub.billingStore, oauthAccessTokenVal, false)
			mux.Handle("/api/billing/checkout", authMW(checkoutHandler(hub.billingStore, stripeClient, logger)))
			mux.Handle("/api/billing/portal", authMW(portalHandler(hub.billingStore, stripeClient, logger)))
			// Webhook has no auth middleware — Stripe verifies via signature.
			mux.Handle("/api/billing/webhook", stripeWebhookHandler(hub.billingStore, stripeClient, logger))
			logger.Info("Stripe billing endpoints enabled",
				"checkout", "/api/billing/checkout",
				"portal", "/api/billing/portal",
				"webhook", "/api/billing/webhook")
		}
	}

	// Email sender — optional; no-op if SMTP not configured.
	emailSender := email.NewSMTPSenderFromEnv()

	// Self-service signup endpoint (opt-in; requires billing store).
	// Declared after Stripe setup so stripeClient is available for paid plans.
	if cfg.EnableSelfServiceSignup {
		if hub.billingStore == nil {
			logger.Error("--enable-signup requires --billing-db to be set")
			os.Exit(1)
		}
		mux.HandleFunc("/api/tenants", signupHandler(hub.billingStore, stripeClient, emailSender, oauthASBaseURLStr, logger))
		logger.Info("signup endpoint enabled", "path", "/api/tenants", "stripe", stripeClient != nil)
	}

	// Email verification endpoint — always mounted when billing store is present.
	if hub.billingStore != nil {
		dashURL := strings.Replace(oauthASBaseURLStr, "://", "://app.", 1)
		mux.HandleFunc("/oauth/verify", verifyEmailHandler(hub.billingStore, dashURL, logger))
		mux.HandleFunc("/api/email/resend", resendVerificationHandler(hub.billingStore, emailSender, oauthASBaseURLStr, logger))
	}

	// Read CSP script-src hashes generated by split-dist.mjs for Astro's inline scripts.
	var cspHashes []string
	if hdata, err := fs.ReadFile(webSub, "csp-hashes.json"); err == nil {
		_ = json.Unmarshal(hdata, &cspHashes)
	}

	server := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           httputil.CSP(securityHeaders(mux), cspHashes...),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Start server in goroutine
	go func() {
		var err error
		if cfg.TLSEnabled {
			logger.Info("relay hub starting with TLS", "addr", cfg.ListenAddr, "cert", cfg.TLSCert)
			// Configure TLS
			tlsConfig := &tls.Config{
				MinVersion: tls.VersionTLS12,
			}
			server.TLSConfig = tlsConfig
			err = server.ListenAndServeTLS(cfg.TLSCert, cfg.TLSKey)
		} else {
			logger.Info("relay hub starting", "addr", cfg.ListenAddr)
			err = server.ListenAndServe()
		}
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	protocol := "ws"
	if cfg.TLSEnabled {
		protocol = "wss"
	}
	fmt.Printf("Relay Hub started on %s://%s\n", protocol, cfg.ListenAddr)

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
