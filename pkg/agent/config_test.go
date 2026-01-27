package agent

import (
	"testing"
	"time"
)

func TestConfigDefaults(t *testing.T) {
	cfg := Config{
		Domain:      "test.com",
		AgentID:     "alice",
		DisplayName: "Alice",
	}

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Check default values
	if a.shutdownTimeout != DefaultShutdownTimeout {
		t.Errorf("shutdownTimeout = %v, want %v", a.shutdownTimeout, DefaultShutdownTimeout)
	}
	if a.dedupTTL != DefaultDedupTTL {
		t.Errorf("dedupTTL = %v, want %v", a.dedupTTL, DefaultDedupTTL)
	}
	if a.requireEncryption != false {
		t.Errorf("requireEncryption = %v, want %v", a.requireEncryption, false)
	}
	if a.tlsEnabled != false {
		t.Errorf("tlsEnabled = %v, want %v", a.tlsEnabled, false)
	}
	if a.tlsSkipVerify != false {
		t.Errorf("tlsSkipVerify = %v, want %v", a.tlsSkipVerify, false)
	}
}

func TestConfigCustomValues(t *testing.T) {
	cfg := Config{
		Domain:            "test.com",
		AgentID:           "alice",
		DisplayName:       "Alice",
		RequireEncryption: true,
		ShutdownTimeout:   30 * time.Second,
		DedupTTL:          10 * time.Minute,
		TLSEnabled:        true,
		TLSCertFile:       "/path/to/cert.pem",
		TLSKeyFile:        "/path/to/key.pem",
		TLSSkipVerify:     true,
	}

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Check custom values are applied
	if a.shutdownTimeout != 30*time.Second {
		t.Errorf("shutdownTimeout = %v, want %v", a.shutdownTimeout, 30*time.Second)
	}
	if a.dedupTTL != 10*time.Minute {
		t.Errorf("dedupTTL = %v, want %v", a.dedupTTL, 10*time.Minute)
	}
	if a.requireEncryption != true {
		t.Errorf("requireEncryption = %v, want %v", a.requireEncryption, true)
	}
	if a.tlsEnabled != true {
		t.Errorf("tlsEnabled = %v, want %v", a.tlsEnabled, true)
	}
	if a.tlsCertFile != "/path/to/cert.pem" {
		t.Errorf("tlsCertFile = %v, want %v", a.tlsCertFile, "/path/to/cert.pem")
	}
	if a.tlsKeyFile != "/path/to/key.pem" {
		t.Errorf("tlsKeyFile = %v, want %v", a.tlsKeyFile, "/path/to/key.pem")
	}
	if a.tlsSkipVerify != true {
		t.Errorf("tlsSkipVerify = %v, want %v", a.tlsSkipVerify, true)
	}
}

func TestConfigListenAddr(t *testing.T) {
	cfg := Config{
		Domain:      "test.com",
		AgentID:     "alice",
		DisplayName: "Alice",
		ListenAddr:  ":8080",
	}

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if a.listenAddr != ":8080" {
		t.Errorf("listenAddr = %v, want %v", a.listenAddr, ":8080")
	}

	// Should have endpoint in record
	if len(a.record.Endpoints) != 1 {
		t.Errorf("Endpoints count = %d, want 1", len(a.record.Endpoints))
	}
}

func TestConfigRelayAddr(t *testing.T) {
	cfg := Config{
		Domain:      "test.com",
		AgentID:     "alice",
		DisplayName: "Alice",
		RelayAddr:   "ws://relay.test.com:8080",
	}

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if a.relayAddr != "ws://relay.test.com:8080" {
		t.Errorf("relayAddr = %v, want %v", a.relayAddr, "ws://relay.test.com:8080")
	}
}
