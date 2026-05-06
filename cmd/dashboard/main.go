package main

import (
	"context"
	"embed"
	"flag"
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
	"github.com/gianlucamazza/msg2agent/pkg/config"
)

//go:embed web
var webFS embed.FS

func main() {
	var (
		addr            = flag.String("addr", "", "listen address (default :8082)")
		relayURL        = flag.String("relay-url", "", "relay base URL")
		billingDB       = flag.String("billing-db", "", "billing SQLite path")
		billingDriver   = flag.String("billing-driver", "", "billing store driver (sqlite|postgres)")
		oauth2IssuerURL = flag.String("oauth2-issuer-url", "", "OAuth2 issuer URL")
		oauth2Audience  = flag.String("oauth2-audience", "", "OAuth2 audience")
		oauth2JWKSURL   = flag.String("oauth2-jwks-url", "", "OAuth2 JWKS URL")
		shutdownTimeout = flag.Duration("shutdown-timeout", 30*time.Second, "graceful shutdown timeout")
	)
	flag.Parse()

	addr_ := config.FlagOrEnv(*addr, "DASHBOARD_ADDR", ":8082")
	relayURL_ := config.FlagOrEnv(*relayURL, "RELAY_URL", "http://localhost:8080")
	billingDB_ := config.FlagOrEnv(*billingDB, "BILLING_DB", "")
	billingDriver_ := config.FlagOrEnv(*billingDriver, "BILLING_DRIVER", "sqlite")
	issuerURL := config.FlagOrEnv(*oauth2IssuerURL, "OAUTH2_ISSUER_URL", "")
	audience := config.FlagOrEnv(*oauth2Audience, "OAUTH2_AUDIENCE", "")
	jwksURL := config.FlagOrEnv(*oauth2JWKSURL, "OAUTH2_JWKS_URL", "")
	autoProvision := billing.Plan(os.Getenv("MSG2AGENT_OAUTH_AUTO_PROVISION"))

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Open billing store (optional).
	var store billing.Store
	var eventStore billing.EventStore
	if billingDB_ != "" {
		s, _, err := billing.NewStore(billingDriver_, billingDB_)
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

	// Build OAuth2 validator.
	oauth2Cfg := a2a.OAuth2Config{
		Issuer:   issuerURL,
		Audience: audience,
		JWKSURL:  jwksURL,
		// Skip validation when no JWKS URL is configured (dev mode).
		SkipValidation: jwksURL == "",
	}
	validator := a2a.NewBillingValidator(a2a.NewOAuth2Validator(oauth2Cfg))

	// Effective auto-provision plan (default free if the env is set but blank).
	if autoProvision == "" && os.Getenv("MSG2AGENT_OAUTH_AUTO_PROVISION") != "" {
		autoProvision = billing.PlanFree
	}

	app := &application{
		store:      store,
		eventStore: eventStore,
		relayURL:   relayURL_,
		logger:     logger,
	}

	mux := http.NewServeMux()

	// Health check — no auth.
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// API routes — OAuth2 protected.
	var apiHandler http.Handler = app.apiRouter()
	if store != nil {
		apiHandler = billing.OAuth2Middleware(validator, store, autoProvision)(apiHandler)
	}
	mux.Handle("/api/dashboard/", apiHandler)

	// Static files from embedded FS.
	webSub, err := fs.Sub(webFS, "web")
	if err != nil {
		logger.Error("failed to sub embedded FS", "error", err)
		os.Exit(1)
	}
	fileServer := http.FileServer(http.FS(webSub))

	// Root and known static assets.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		// Serve index for root.
		if path == "/" {
			r.URL.Path = "/index.html"
			fileServer.ServeHTTP(w, r)
			return
		}
		// Serve static files that have a file extension.
		if ext := filepath.Ext(path); ext != "" && !strings.HasPrefix(path, "/api/") {
			fileServer.ServeHTTP(w, r)
			return
		}
		// SPA fallback — serve index.html.
		r.URL.Path = "/index.html"
		fileServer.ServeHTTP(w, r)
	})

	srv := &http.Server{
		Addr:         addr_,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Graceful shutdown.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		logger.Info("dashboard starting", "addr", addr_, "relay_url", relayURL_)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-stop
	logger.Info("shutting down dashboard")
	ctx, cancel := context.WithTimeout(context.Background(), *shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("shutdown error", "error", err)
	}
}
