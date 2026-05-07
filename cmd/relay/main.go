// Package main provides the relay hub executable.
package main

import (
	"context"
	"crypto/tls"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html/template"
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
	"github.com/gianlucamazza/msg2agent/pkg/config"
	"github.com/gianlucamazza/msg2agent/pkg/email"
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

	// Billing (optional)
	billingDBPath := flag.String("billing-db", "", "Path to billing SQLite DB; enables API key auth on WS register (env: MSG2AGENT_BILLING_DB)")
	auditVerifierIntervalFlag := flag.Duration("audit-verifier-interval", 6*time.Hour, "Interval for background audit chain verification (0 = disabled)")
	stripeReconcileInterval := flag.Duration("stripe-reconcile-interval", time.Hour, "Interval for Stripe subscription reconciler (0 = disabled)")
	stripeAutoReconcile := flag.Bool("stripe-auto-reconcile", false, "Automatically fix divergent billing state during reconciliation")

	// OAuth2 (optional; enables JWT auth on WS in addition to API keys)
	oauth2IssuerURL := flag.String("oauth2-issuer-url", "", "OAuth2 issuer URL for JWT validation on relay WS (env: MSG2AGENT_OAUTH2_ISSUER_URL)")
	oauth2Audience := flag.String("oauth2-audience", "", "OAuth2 expected audience (env: MSG2AGENT_OAUTH2_AUDIENCE)")
	oauth2JWKSUrl := flag.String("oauth2-jwks-url", "", "OAuth2 JWKS URL override (default: issuer/.well-known/jwks.json)")

	// OAuth 2.1 Authorization Server (Phase B — our own AS, optional)
	oauthASBaseURL := flag.String("oauth-as-base-url", "", "Base URL for the OAuth 2.1 Authorization Server (env: MSG2AGENT_OAUTH_AS_BASE_URL)")
	oauthSigningKeyPath := flag.String("oauth-signing-key", "/data/oauth-signing-key.pem", "Path to the Ed25519 signing key PEM for the OAuth AS (env: MSG2AGENT_OAUTH_SIGNING_KEY)")
	oauthGoogleClientID := flag.String("oauth-google-client-id", "", "Google OAuth2 client ID for the consent screen (env: MSG2AGENT_OAUTH_GOOGLE_CLIENT_ID)")
	oauthGoogleClientSecret := flag.String("oauth-google-client-secret", "", "Google OAuth2 client secret (env: MSG2AGENT_OAUTH_GOOGLE_CLIENT_SECRET)")

	// A2A AgentCard — served at /.well-known/agent.json (public, no auth)
	agentCardPath := flag.String("agent-card", "", "Path to agent card JSON file to serve at /.well-known/agent.json")
	connectorManifestPath := flag.String("connector-manifest", "", "Path to connector manifest JSON to serve at /.well-known/mcp-connector.json")

	// Self-service signup (optional; requires billing store)
	enableSignup := flag.Bool("enable-signup", false, "Enable self-service tenant signup endpoint POST /api/tenants (requires --billing-db)")

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
	logger.Info("starting relay", "version", buildinfo.Version, "commit", buildinfo.Commit, "date", buildinfo.Date)

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

	// Configure billing (optional)
	billingDBStr := config.FlagOrEnv(*billingDBPath, "BILLING_DB", "")
	if billingDBStr != "" {
		bStore, err := billing.NewSQLiteStore(billingDBStr)
		if err != nil {
			logger.Error("failed to open billing store", "path", billingDBStr, "error", err)
			os.Exit(1)
		}
		hub.billingStore = bStore
		hub.tenantPool = billing.NewTenantRateLimiterPool(bStore)
		logger.Info("relay billing enabled", "db", billingDBStr)

		auditInterval := *auditVerifierIntervalFlag
		if auditInterval > 0 {
			// bStore is *billing.SQLiteStore which implements billing.AdminStore directly.
			billing.StartPeriodicVerifier(hub.ctx, hub.billingStore, bStore, auditInterval, logger)
			logger.Info("audit chain verifier started", "interval", auditInterval)
		}

		// Optional: JWT validator for OAuth2 WS auth alongside API keys.
		issuerURL := config.FlagOrEnv(*oauth2IssuerURL, "OAUTH2_ISSUER_URL", "")
		if issuerURL != "" {
			jwksURL := config.FlagOrEnv(*oauth2JWKSUrl, "", strings.TrimRight(issuerURL, "/")+
				"/.well-known/jwks.json")
			oauthCfg := a2a.OAuth2Config{
				JWKSURL:  jwksURL,
				Issuer:   issuerURL,
				Audience: config.FlagOrEnv(*oauth2Audience, "OAUTH2_AUDIENCE", ""),
			}
			hub.jwtValidator = a2a.NewBillingValidator(a2a.NewOAuth2Validator(oauthCfg))
			logger.Info("relay OAuth2 JWT auth enabled", "issuer", issuerURL)
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
	oauthASBaseURLStr = config.FlagOrEnv(*oauthASBaseURL, "OAUTH_AS_BASE_URL", "")
	if oauthASBaseURLStr != "" && hub.billingStore != nil {
		signingKeyPath := config.FlagOrEnv(*oauthSigningKeyPath, "OAUTH_SIGNING_KEY", "/data/oauth-signing-key.pem")
		privKey, err := oauth.LoadOrGenerateEd25519(signingKeyPath)
		if err != nil {
			logger.Error("oauth AS: failed to load/generate signing key", "path", signingKeyPath, "error", err)
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
		googleClientID := config.FlagOrEnv(*oauthGoogleClientID, "OAUTH_GOOGLE_CLIENT_ID", "")
		googleClientSecret := config.FlagOrEnv(*oauthGoogleClientSecret, "OAUTH_GOOGLE_CLIENT_SECRET", "")
		if googleClientID != "" {
			idp = oauth.NewGoogleIDP(googleClientID, googleClientSecret, oauthASBaseURLStr+"/oauth/google-callback")
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

		logger.Info("oauth AS enabled", "base_url", oauthASBaseURLStr, "kid", kid, "google_idp", googleClientID != "")
	}

	// Internal service token — lets mcp-server connect without billing API key.
	if svcToken := os.Getenv("MSG2AGENT_SERVICE_TOKEN"); svcToken != "" {
		hub.serviceToken = svcToken
		logger.Info("relay service token configured for internal agent auth")
	}

	// Configure DID allowlist
	hub.aclEnforcer = security.NewACLEnforcer()
	if len(allowedDIDList) > 0 {
		hub.allowlistACL = security.TrustedAgentsPolicy(allowedDIDList)
		logger.Info("DID allowlist enabled", "count", len(allowedDIDList))
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
	type pageData struct{ PaidEnabled bool }
	paidEnabled := billing.StripeConfigFromEnv() != nil
	logger.Info("public ui", "paid_enabled", paidEnabled)

	servePage := func(name string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			data, err := fs.ReadFile(webSub, name)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			tmpl, err := template.New(name).Parse(string(data))
			if err != nil {
				http.Error(w, "template error", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_ = tmpl.Execute(w, pageData{PaidEnabled: paidEnabled})
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
	mux.HandleFunc("/pricing", servePage("pricing.html"))
	mux.HandleFunc("/privacy", servePage("privacy.html"))
	mux.HandleFunc("/terms", servePage("terms.html"))
	mux.HandleFunc("/favicon.svg", serveAsset("favicon.svg", "image/svg+xml"))
	mux.HandleFunc("/logo-512.png", serveAsset("logo-512.png", "image/png"))
	mux.HandleFunc("/logo-180.png", serveAsset("logo-180.png", "image/png"))
	mux.HandleFunc("/style.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		_, _ = w.Write(webui.CSS())
	})

	// Root: WebSocket upgrade for agents, landing page for browsers.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") == "websocket" {
			hub.handleWebSocket(w, r)
			return
		}
		if r.URL.Path == "/" {
			servePage("landing.html")(w, r)
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
	if *agentCardPath != "" {
		mux.HandleFunc("/.well-known/agent.json", func(w http.ResponseWriter, r *http.Request) {
			data, err := os.ReadFile(*agentCardPath)
			if err != nil {
				http.Error(w, "agent card not found", http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "public, max-age=300")
			_, _ = w.Write(data)
		})
		logger.Info("agent card endpoint enabled", "path", "/.well-known/agent.json", "file", *agentCardPath)
	}

	// Connector manifest — Anthropic Connector Directory discovery
	if *connectorManifestPath != "" {
		manifestBytes, err := os.ReadFile(*connectorManifestPath)
		if err != nil {
			logger.Warn("connector manifest not found, endpoint disabled", "file", *connectorManifestPath, "err", err)
		} else {
			mux.HandleFunc("/.well-known/mcp-connector.json", func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Cache-Control", "public, max-age=300")
				_, _ = w.Write(manifestBytes)
			})
			logger.Info("connector manifest endpoint enabled", "path", "/.well-known/mcp-connector.json", "file", *connectorManifestPath)
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
			billing.StartStripeReconciler(hub.ctx, hub.billingStore, stripeClient, *stripeReconcileInterval, *stripeAutoReconcile, logger)
			if *stripeReconcileInterval > 0 {
				logger.Info("Stripe reconciler started",
					"interval", *stripeReconcileInterval,
					"auto_fix", *stripeAutoReconcile)
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
	if *enableSignup {
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
	}

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
