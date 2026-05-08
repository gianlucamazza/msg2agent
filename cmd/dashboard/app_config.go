package main

import (
	"flag"
	"os"
	"time"

	"github.com/gianlucamazza/msg2agent/pkg/billing"
	"github.com/gianlucamazza/msg2agent/pkg/config"
)

type appConfig struct {
	Addr            string
	RelayURL        string
	BillingDB       string
	BillingDriver   string
	OAuth2IssuerURL string
	OAuth2Audience  string
	OAuth2JWKSURL   string
	Domain          string
	AutoProvision   billing.Plan
	ShutdownTimeout time.Duration
}

func parseAppConfig() appConfig {
	addr := flag.String("addr", "", "listen address (default :8082)")
	relayURL := flag.String("relay-url", "", "relay base URL")
	billingDB := flag.String("billing-db", "", "billing SQLite path")
	billingDriver := flag.String("billing-driver", "", "billing store driver (sqlite|postgres)")
	oauth2IssuerURL := flag.String("oauth2-issuer-url", "", "OAuth2 issuer URL")
	oauth2Audience := flag.String("oauth2-audience", "", "OAuth2 audience")
	oauth2JWKSURL := flag.String("oauth2-jwks-url", "", "OAuth2 JWKS URL")
	domain := flag.String("domain", "", "DID domain for tenant identity (env: DOMAIN)")
	shutdownTimeout := flag.Duration("shutdown-timeout", 30*time.Second, "graceful shutdown timeout")
	flag.Parse()

	autoProvision := billing.Plan(os.Getenv("MSG2AGENT_OAUTH_AUTO_PROVISION"))
	if autoProvision == "" && os.Getenv("MSG2AGENT_OAUTH_AUTO_PROVISION") != "" {
		autoProvision = billing.PlanFree
	}

	return appConfig{
		Addr:            config.FlagOrEnv(*addr, "DASHBOARD_ADDR", ":8082"),
		RelayURL:        config.FlagOrEnv(*relayURL, "RELAY_URL", "http://localhost:8080"),
		BillingDB:       config.FlagOrEnv(*billingDB, "BILLING_DB", ""),
		BillingDriver:   config.FlagOrEnv(*billingDriver, "BILLING_DRIVER", "sqlite"),
		OAuth2IssuerURL: config.FlagOrEnv(*oauth2IssuerURL, "OAUTH2_ISSUER_URL", ""),
		OAuth2Audience:  config.FlagOrEnv(*oauth2Audience, "OAUTH2_AUDIENCE", ""),
		OAuth2JWKSURL:   config.FlagOrEnv(*oauth2JWKSURL, "OAUTH2_JWKS_URL", ""),
		Domain:          config.FlagOrEnv(*domain, "DOMAIN", "localhost"),
		AutoProvision:   autoProvision,
		ShutdownTimeout: *shutdownTimeout,
	}
}
