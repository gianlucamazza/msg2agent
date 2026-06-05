package main

import (
	"context"
	"embed"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gianlucamazza/msg2agent/adapters/a2a"
	"github.com/gianlucamazza/msg2agent/pkg/billing"
	"github.com/gianlucamazza/msg2agent/pkg/buildinfo"
	"github.com/gianlucamazza/msg2agent/pkg/webui"
)

//go:embed web
var webFS embed.FS

func main() {
	cfg := parseAppConfig()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	logger.Info("starting dashboard", "version", buildinfo.Version, "commit", buildinfo.Commit, "date", buildinfo.Date)

	// Parse CORS allowed origins from environment (comma-separated).
	corsOrigins := strings.Split(os.Getenv("MSG2AGENT_DASHBOARD_CORS_ORIGINS"), ",")
	var validOrigins []string
	for _, o := range corsOrigins {
		o = strings.TrimSpace(o)
		if o != "" {
			validOrigins = append(validOrigins, o)
		}
	}

	// Open billing store (optional).
	var store billing.Store
	var eventStore billing.EventStore
	if cfg.BillingDB != "" {
		s, _, err := billing.NewStore(cfg.BillingDriver, cfg.BillingDB)
		if err != nil {
			logger.Error("failed to open billing store", "error", err)
			os.Exit(1)
		}
		store = s
		if es, ok := s.(billing.EventStore); ok {
			eventStore = es
		}
		defer s.Close()
	}

	// Build OAuth2 validator. The dashboard's /api/dashboard/* needs OAuth2
	// to identify the calling tenant; if no issuer is configured, the API
	// mount returns 503 below instead of running unauthenticated. JWKS URL
	// defaults to <issuer>/.well-known/jwks.json when only the issuer is set
	// — same convention used by relay and mcp-server.
	var validator billing.JWTValidator
	jwksURL := cfg.OAuth2JWKSURL
	if cfg.OAuth2IssuerURL != "" {
		if jwksURL == "" {
			jwksURL = strings.TrimRight(cfg.OAuth2IssuerURL, "/") + "/.well-known/jwks.json"
		}
		validator = a2a.NewBillingValidator(a2a.NewOAuth2Validator(a2a.OAuth2Config{
			Issuer:   cfg.OAuth2IssuerURL,
			Audience: cfg.OAuth2Audience,
			JWKSURL:  jwksURL,
		}))
		logger.Info("OAuth2 validator enabled", "issuer", cfg.OAuth2IssuerURL, "jwks_url", jwksURL)
	} else {
		logger.Warn("OAuth2 not configured (MSG2AGENT_OAUTH2_ISSUER_URL empty); /api/dashboard/* will return 503")
	}

	var adminStore billing.AdminStore
	if s, ok := store.(billing.AdminStore); ok {
		adminStore = s
	}
	app := &application{
		store:      store,
		adminStore: adminStore,
		eventStore: eventStore,
		relayURL:   cfg.RelayURL,
		domain:     cfg.Domain,
		logger:     logger,
	}

	mux := http.NewServeMux()

	// Health check — no auth; verifies billing store if configured.
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if store != nil {
			if err := store.Ping(); err != nil {
				http.Error(w, "billing store unavailable: "+err.Error(), http.StatusServiceUnavailable)
				return
			}
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// Version endpoint — no auth.
	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{
			"version": buildinfo.Version,
			"commit":  buildinfo.Commit,
			"date":    buildinfo.Date,
		})
	})

	// API routes — OAuth2 protected. Both the billing store and the OAuth2
	// validator are required: without the store we can't resolve tenants;
	// without the validator we can't authenticate. Either missing piece
	// disables the API mount with a clear 503 instead of exposing it
	// unauthenticated or crashing the binary.
	if store != nil && validator != nil {
		apiHandler := billing.OAuth2Middleware(validator, store, cfg.AutoProvision)(app.apiRouter())
		mux.Handle("/api/dashboard/", apiHandler)
	} else {
		reason := "billing store not configured"
		if validator == nil {
			reason = "OAuth2 not configured (set MSG2AGENT_OAUTH2_ISSUER_URL)"
		}
		mux.Handle("/api/dashboard/", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "dashboard API unavailable: "+reason, http.StatusServiceUnavailable)
		}))
		logger.Warn("dashboard API disabled", "reason", reason)
	}

	// Static files from embedded FS.
	webSub, err := fs.Sub(webFS, "web")
	if err != nil {
		logger.Error("failed to sub embedded FS", "error", err)
		os.Exit(1)
	}
	fileServer := http.FileServer(http.FS(webSub))

	// servePage returns a handler that serves a single embedded HTML file.
	servePage := func(name string) http.HandlerFunc {
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

	// App SPA — authenticated dashboard at /app/.
	mux.HandleFunc("/app", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/app/", http.StatusMovedPermanently)
	})
	mux.HandleFunc("/app/", servePage("index.html"))

	// Shared stylesheet served from pkg/webui (canonical, single source of truth).
	mux.HandleFunc("/style.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		_, _ = w.Write(webui.CSS())
	})

	// Static assets (CSS/JS/images) and redirect root to main domain.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if ext := filepath.Ext(r.URL.Path); ext != "" && !strings.HasPrefix(r.URL.Path, "/api/") {
			fileServer.ServeHTTP(w, r)
			return
		}
		http.NotFound(w, r)
	})

	srv := &http.Server{
		Addr:         cfg.Addr,
		Handler:      requestIDMiddleware(securityHeaders(corsMiddleware(validOrigins...)(mux))),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Graceful shutdown.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		logger.Info("dashboard starting", "addr", cfg.Addr, "relay_url", cfg.RelayURL)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-stop
	logger.Info("shutting down dashboard")
	ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("shutdown error", "error", err)
	}
}
