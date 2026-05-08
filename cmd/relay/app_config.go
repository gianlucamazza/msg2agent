package main

import (
	"flag"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/gianlucamazza/msg2agent/pkg/config"
)

// appConfig is the fully resolved relay process configuration. Flag values
// override MSG2AGENT_* environment variables, which override defaults.
type appConfig struct {
	ListenAddr       string
	MaxConnections   int
	MessageRateLimit float64

	TLSEnabled bool
	TLSCert    string
	TLSKey     string

	LogLevel     slog.Level
	StoreType    string
	StoreFile    string
	OTLPEndpoint string
	TraceStdout  bool

	AllowedOrigins      []string
	AllowedDIDs         []string
	SkipDIDVerification bool

	BillingDBPath         string
	AuditVerifierInterval time.Duration
	StripeReconcileEvery  time.Duration
	StripeAutoReconcile   bool

	OAuth2IssuerURL string
	OAuth2Audience  string
	OAuth2JWKSURL   string

	OAuthASBaseURL          string
	OAuthSigningKeyPath     string
	OAuthGoogleClientID     string
	OAuthGoogleClientSecret string
	AgentCardPath           string
	ConnectorManifestPath   string
	EnableSelfServiceSignup bool
}

func parseAppConfig() (appConfig, error) {
	addr := flag.String("addr", "", "Listen address (env: MSG2AGENT_RELAY_ADDR)")
	maxConns := flag.Int("max-connections", 0, "Maximum concurrent connections (env: MSG2AGENT_MAX_CONNECTIONS)")
	msgRate := flag.Float64("msg-rate", 0, "Message rate limit per client (msg/sec) (env: MSG2AGENT_MSG_RATE)")

	tlsEnabled := flag.Bool("tls", false, "Enable TLS (env: MSG2AGENT_TLS)")
	tlsCert := flag.String("tls-cert", "", "TLS certificate file (env: MSG2AGENT_TLS_CERT)")
	tlsKey := flag.String("tls-key", "", "TLS key file (env: MSG2AGENT_TLS_KEY)")

	logLevel := flag.String("log-level", "", "Log level: debug, info, warn, error (env: MSG2AGENT_LOG_LEVEL)")
	storeType := flag.String("store", "", "Store type: memory, file, sqlite (env: MSG2AGENT_STORE)")
	storeFile := flag.String("store-file", "", "Store file path for file/sqlite stores (env: MSG2AGENT_STORE_FILE)")

	otlpEndpoint := flag.String("otlp-endpoint", "", "OTLP endpoint for tracing (env: MSG2AGENT_OTLP_ENDPOINT)")
	traceStdout := flag.Bool("trace-stdout", false, "Enable stdout tracing for debugging (env: MSG2AGENT_TRACE_STDOUT)")
	corsOrigins := flag.String("cors-origins", "", "Comma-separated list of allowed CORS origins (env: MSG2AGENT_CORS_ORIGINS)")

	skipDIDProof := flag.Bool("skip-did-proof", false, "Skip DID ownership verification during registration (NOT recommended) (env: MSG2AGENT_SKIP_DID_PROOF)")
	allowedDIDs := flag.String("allowed-dids", "", "Comma-separated list of allowed DIDs (empty = open relay) (env: MSG2AGENT_ALLOWED_DIDS)")

	billingDBPath := flag.String("billing-db", "", "Path to billing SQLite DB; enables API key auth on WS register (env: MSG2AGENT_BILLING_DB)")
	auditVerifierInterval := flag.Duration("audit-verifier-interval", 6*time.Hour, "Interval for background audit chain verification (0 = disabled)")
	stripeReconcileInterval := flag.Duration("stripe-reconcile-interval", time.Hour, "Interval for Stripe subscription reconciler (0 = disabled)")
	stripeAutoReconcile := flag.Bool("stripe-auto-reconcile", false, "Automatically fix divergent billing state during reconciliation")

	oauth2IssuerURL := flag.String("oauth2-issuer-url", "", "OAuth2 issuer URL for JWT validation on relay WS (env: MSG2AGENT_OAUTH2_ISSUER_URL)")
	oauth2Audience := flag.String("oauth2-audience", "", "OAuth2 expected audience (env: MSG2AGENT_OAUTH2_AUDIENCE)")
	oauth2JWKSURL := flag.String("oauth2-jwks-url", "", "OAuth2 JWKS URL override (default: issuer/.well-known/jwks.json)")

	oauthASBaseURL := flag.String("oauth-as-base-url", "", "Base URL for the OAuth 2.1 Authorization Server (env: MSG2AGENT_OAUTH_AS_BASE_URL)")
	oauthSigningKeyPath := flag.String("oauth-signing-key", "/data/oauth-signing-key.pem", "Path to the Ed25519 signing key PEM for the OAuth AS (env: MSG2AGENT_OAUTH_SIGNING_KEY)")
	oauthGoogleClientID := flag.String("oauth-google-client-id", "", "Google OAuth2 client ID for the consent screen (env: MSG2AGENT_OAUTH_GOOGLE_CLIENT_ID)")
	oauthGoogleClientSecret := flag.String("oauth-google-client-secret", "", "Google OAuth2 client secret (env: MSG2AGENT_OAUTH_GOOGLE_CLIENT_SECRET)")

	agentCardPath := flag.String("agent-card", "", "Path to agent card JSON file to serve at /.well-known/agent.json")
	connectorManifestPath := flag.String("connector-manifest", "", "Path to connector manifest JSON to serve at /.well-known/mcp-connector.json")
	enableSignup := flag.Bool("enable-signup", false, "Enable self-service tenant signup endpoint POST /api/tenants (requires --billing-db)")

	flag.Parse()

	logLevelValue, err := parseLogLevel(config.FlagOrEnv(*logLevel, "LOG_LEVEL", "debug"))
	if err != nil {
		return appConfig{}, err
	}

	cfg := appConfig{
		ListenAddr:       config.FlagOrEnv(*addr, "RELAY_ADDR", ":8080"),
		MaxConnections:   config.FlagOrEnvInt(*maxConns, 0, "MAX_CONNECTIONS", 1000),
		MessageRateLimit: config.FlagOrEnvFloat(*msgRate, 0, "MSG_RATE", 100.0),
		TLSEnabled:       config.FlagOrEnvBool(*tlsEnabled, "TLS", false),
		TLSCert:          config.FlagOrEnv(*tlsCert, "TLS_CERT", ""),
		TLSKey:           config.FlagOrEnv(*tlsKey, "TLS_KEY", ""),
		LogLevel:         logLevelValue,
		StoreType:        config.FlagOrEnv(*storeType, "STORE", "memory"),
		StoreFile:        config.FlagOrEnv(*storeFile, "STORE_FILE", ""),
		OTLPEndpoint:     config.FlagOrEnv(*otlpEndpoint, "OTLP_ENDPOINT", ""),
		TraceStdout:      config.FlagOrEnvBool(*traceStdout, "TRACE_STDOUT", false),

		AllowedOrigins:      splitCSV(config.FlagOrEnv(*corsOrigins, "CORS_ORIGINS", "")),
		AllowedDIDs:         splitCSV(config.FlagOrEnv(*allowedDIDs, "ALLOWED_DIDS", "")),
		SkipDIDVerification: config.FlagOrEnvBool(*skipDIDProof, "SKIP_DID_PROOF", false),

		BillingDBPath:         config.FlagOrEnv(*billingDBPath, "BILLING_DB", ""),
		AuditVerifierInterval: *auditVerifierInterval,
		StripeReconcileEvery:  *stripeReconcileInterval,
		StripeAutoReconcile:   *stripeAutoReconcile,

		OAuth2IssuerURL: config.FlagOrEnv(*oauth2IssuerURL, "OAUTH2_ISSUER_URL", ""),
		OAuth2Audience:  config.FlagOrEnv(*oauth2Audience, "OAUTH2_AUDIENCE", ""),
		OAuth2JWKSURL:   config.FlagOrEnv(*oauth2JWKSURL, "OAUTH2_JWKS_URL", ""),

		OAuthASBaseURL:          config.FlagOrEnv(*oauthASBaseURL, "OAUTH_AS_BASE_URL", ""),
		OAuthSigningKeyPath:     config.FlagOrEnv(*oauthSigningKeyPath, "OAUTH_SIGNING_KEY", "/data/oauth-signing-key.pem"),
		OAuthGoogleClientID:     config.FlagOrEnv(*oauthGoogleClientID, "OAUTH_GOOGLE_CLIENT_ID", ""),
		OAuthGoogleClientSecret: config.FlagOrEnv(*oauthGoogleClientSecret, "OAUTH_GOOGLE_CLIENT_SECRET", ""),
		AgentCardPath:           *agentCardPath,
		ConnectorManifestPath:   *connectorManifestPath,
		EnableSelfServiceSignup: *enableSignup,
	}

	if cfg.TLSEnabled && (cfg.TLSCert == "" || cfg.TLSKey == "") {
		return appConfig{}, fmt.Errorf("TLS enabled but certificate or key file not specified")
	}
	return cfg, nil
}

func parseLogLevel(value string) (slog.Level, error) {
	switch value {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelDebug, nil
	}
}

func splitCSV(value string) []string {
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
