package transport

import (
	"testing"
)

func TestConfigTLSFields(t *testing.T) {
	cfg := Config{
		Address:       ":8080",
		TLSEnabled:    true,
		TLSCertFile:   "/path/to/cert.pem",
		TLSKeyFile:    "/path/to/key.pem",
		TLSSkipVerify: true,
	}

	if cfg.TLSEnabled != true {
		t.Errorf("TLSEnabled = %v, want %v", cfg.TLSEnabled, true)
	}
	if cfg.TLSCertFile != "/path/to/cert.pem" {
		t.Errorf("TLSCertFile = %v, want %v", cfg.TLSCertFile, "/path/to/cert.pem")
	}
	if cfg.TLSKeyFile != "/path/to/key.pem" {
		t.Errorf("TLSKeyFile = %v, want %v", cfg.TLSKeyFile, "/path/to/key.pem")
	}
	if cfg.TLSSkipVerify != true {
		t.Errorf("TLSSkipVerify = %v, want %v", cfg.TLSSkipVerify, true)
	}
}

func TestDefaultConfigNoTLS(t *testing.T) {
	cfg := DefaultConfig(":8080")

	if cfg.TLSEnabled != false {
		t.Errorf("TLSEnabled = %v, want %v", cfg.TLSEnabled, false)
	}
	if cfg.TLSCertFile != "" {
		t.Errorf("TLSCertFile = %v, want empty", cfg.TLSCertFile)
	}
	if cfg.TLSKeyFile != "" {
		t.Errorf("TLSKeyFile = %v, want empty", cfg.TLSKeyFile)
	}
	if cfg.TLSSkipVerify != false {
		t.Errorf("TLSSkipVerify = %v, want %v", cfg.TLSSkipVerify, false)
	}
}

func TestWebSocketTransportTLSConfig(t *testing.T) {
	cfg := Config{
		Address:       "wss://localhost:8443",
		TLSSkipVerify: true,
	}

	transport := NewWebSocketTransport(cfg)

	if transport.config.TLSSkipVerify != true {
		t.Errorf("TLSSkipVerify = %v, want %v", transport.config.TLSSkipVerify, true)
	}
}

func TestWebSocketListenerTLSConfig(t *testing.T) {
	cfg := Config{
		Address:     ":0",
		TLSEnabled:  true,
		TLSCertFile: "/path/to/cert.pem",
		TLSKeyFile:  "/path/to/key.pem",
	}

	listener := NewWebSocketListener(cfg)

	if listener.config.TLSEnabled != true {
		t.Errorf("TLSEnabled = %v, want %v", listener.config.TLSEnabled, true)
	}
	if listener.config.TLSCertFile != "/path/to/cert.pem" {
		t.Errorf("TLSCertFile = %v, want %v", listener.config.TLSCertFile, "/path/to/cert.pem")
	}
	if listener.config.TLSKeyFile != "/path/to/key.pem" {
		t.Errorf("TLSKeyFile = %v, want %v", listener.config.TLSKeyFile, "/path/to/key.pem")
	}
}
