package main

import (
	"flag"
	"os"
	"reflect"
	"testing"
)

func withIsolatedFlags(t *testing.T, args ...string) {
	t.Helper()
	oldCommandLine := flag.CommandLine
	oldArgs := os.Args
	flag.CommandLine = flag.NewFlagSet(args[0], flag.ContinueOnError)
	os.Args = args
	t.Cleanup(func() {
		flag.CommandLine = oldCommandLine
		os.Args = oldArgs
	})
}

func TestParseAppConfigDefaults(t *testing.T) {
	withIsolatedFlags(t, "relay")

	cfg, err := parseAppConfig()
	if err != nil {
		t.Fatalf("parseAppConfig: %v", err)
	}
	if cfg.ListenAddr != ":8080" {
		t.Fatalf("ListenAddr = %q, want :8080", cfg.ListenAddr)
	}
	if cfg.StoreType != "memory" {
		t.Fatalf("StoreType = %q, want memory", cfg.StoreType)
	}
	if cfg.MaxConnections != 1000 {
		t.Fatalf("MaxConnections = %d, want 1000", cfg.MaxConnections)
	}
}

func TestParseAppConfigEnvAndCSV(t *testing.T) {
	withIsolatedFlags(t, "relay")
	t.Setenv("MSG2AGENT_CORS_ORIGINS", "https://app.example.com, https://admin.example.com")
	t.Setenv("MSG2AGENT_ALLOWED_DIDS", "did:wba:example.com:agent:one, did:wba:example.com:agent:two")
	t.Setenv("MSG2AGENT_STORE", "sqlite")
	t.Setenv("MSG2AGENT_STORE_FILE", "/tmp/relay.db")

	cfg, err := parseAppConfig()
	if err != nil {
		t.Fatalf("parseAppConfig: %v", err)
	}
	if cfg.StoreType != "sqlite" || cfg.StoreFile != "/tmp/relay.db" {
		t.Fatalf("store config = %q %q", cfg.StoreType, cfg.StoreFile)
	}
	wantOrigins := []string{"https://app.example.com", "https://admin.example.com"}
	if !reflect.DeepEqual(cfg.AllowedOrigins, wantOrigins) {
		t.Fatalf("AllowedOrigins = %#v, want %#v", cfg.AllowedOrigins, wantOrigins)
	}
	wantDIDs := []string{"did:wba:example.com:agent:one", "did:wba:example.com:agent:two"}
	if !reflect.DeepEqual(cfg.AllowedDIDs, wantDIDs) {
		t.Fatalf("AllowedDIDs = %#v, want %#v", cfg.AllowedDIDs, wantDIDs)
	}
}

func TestParseAppConfigTLSRequiresCertAndKey(t *testing.T) {
	withIsolatedFlags(t, "relay", "-tls")

	if _, err := parseAppConfig(); err == nil {
		t.Fatal("parseAppConfig succeeded with TLS enabled and no cert/key")
	}
}
