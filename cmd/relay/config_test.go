package main

import (
	"os"
	"testing"

	"github.com/gianluca/msg2agent/pkg/config"
)

func TestEnvVarConfiguration(t *testing.T) {
	tests := []struct {
		name     string
		envKey   string
		envValue string
		expected string
	}{
		{
			name:     "relay addr from env",
			envKey:   "MSG2AGENT_RELAY_ADDR",
			envValue: ":9090",
			expected: ":9090",
		},
		{
			name:     "tls cert from env",
			envKey:   "MSG2AGENT_TLS_CERT",
			envValue: "/path/to/cert.pem",
			expected: "/path/to/cert.pem",
		},
		{
			name:     "tls key from env",
			envKey:   "MSG2AGENT_TLS_KEY",
			envValue: "/path/to/key.pem",
			expected: "/path/to/key.pem",
		},
		{
			name:     "log level from env",
			envKey:   "MSG2AGENT_LOG_LEVEL",
			envValue: "info",
			expected: "info",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv(tt.envKey, tt.envValue)
			defer os.Unsetenv(tt.envKey)

			// Extract key without prefix for config package
			key := tt.envKey[len("MSG2AGENT_"):]
			got := config.GetEnv(key)
			if got != tt.expected {
				t.Errorf("GetEnv(%q) = %q, want %q", key, got, tt.expected)
			}
		})
	}
}

func TestEnvVarIntConfiguration(t *testing.T) {
	os.Setenv("MSG2AGENT_MAX_CONNECTIONS", "500")
	defer os.Unsetenv("MSG2AGENT_MAX_CONNECTIONS")

	got := config.GetEnvInt("MAX_CONNECTIONS", 1000)
	if got != 500 {
		t.Errorf("GetEnvInt() = %d, want %d", got, 500)
	}
}

func TestEnvVarFloatConfiguration(t *testing.T) {
	os.Setenv("MSG2AGENT_MSG_RATE", "50.5")
	defer os.Unsetenv("MSG2AGENT_MSG_RATE")

	got := config.GetEnvFloat("MSG_RATE", 100.0)
	if got != 50.5 {
		t.Errorf("GetEnvFloat() = %f, want %f", got, 50.5)
	}
}

func TestEnvVarBoolConfiguration(t *testing.T) {
	tests := []struct {
		name     string
		envKey   string
		envValue string
		expected bool
	}{
		{
			name:     "tls enabled true",
			envKey:   "MSG2AGENT_TLS",
			envValue: "true",
			expected: true,
		},
		{
			name:     "tls enabled 1",
			envKey:   "MSG2AGENT_TLS",
			envValue: "1",
			expected: true,
		},
		{
			name:     "tls enabled false",
			envKey:   "MSG2AGENT_TLS",
			envValue: "false",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv(tt.envKey, tt.envValue)
			defer os.Unsetenv(tt.envKey)

			got := config.GetEnvBool("TLS", false)
			if got != tt.expected {
				t.Errorf("GetEnvBool() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestFlagOrEnvPriority(t *testing.T) {
	// Set env var
	os.Setenv("MSG2AGENT_RELAY_ADDR", ":9090")
	defer os.Unsetenv("MSG2AGENT_RELAY_ADDR")

	// Flag should override env var
	flagValue := ":8080"
	got := config.FlagOrEnv(flagValue, "RELAY_ADDR", ":7070")
	if got != ":8080" {
		t.Errorf("FlagOrEnv() = %q, want %q (flag should take priority)", got, ":8080")
	}

	// Empty flag should use env var
	got = config.FlagOrEnv("", "RELAY_ADDR", ":7070")
	if got != ":9090" {
		t.Errorf("FlagOrEnv() = %q, want %q (env should be used when flag empty)", got, ":9090")
	}

	// Without env var, should use default
	os.Unsetenv("MSG2AGENT_RELAY_ADDR")
	got = config.FlagOrEnv("", "RELAY_ADDR", ":7070")
	if got != ":7070" {
		t.Errorf("FlagOrEnv() = %q, want %q (default should be used)", got, ":7070")
	}
}

func TestDefaultRelayConfigValues(t *testing.T) {
	cfg := DefaultRelayConfig()

	if cfg.MaxConnections != 1000 {
		t.Errorf("MaxConnections = %d, want %d", cfg.MaxConnections, 1000)
	}
	if cfg.MessageRateLimit != 100.0 {
		t.Errorf("MessageRateLimit = %f, want %f", cfg.MessageRateLimit, 100.0)
	}
	if cfg.MessageBurstSize != 200.0 {
		t.Errorf("MessageBurstSize = %f, want %f", cfg.MessageBurstSize, 200.0)
	}
	if cfg.RegisterRateLimit != 1.0 {
		t.Errorf("RegisterRateLimit = %f, want %f", cfg.RegisterRateLimit, 1.0)
	}
	if cfg.DiscoverRateLimit != 10.0 {
		t.Errorf("DiscoverRateLimit = %f, want %f", cfg.DiscoverRateLimit, 10.0)
	}
}

func TestStoreEnvConfiguration(t *testing.T) {
	tests := []struct {
		name     string
		envKey   string
		envValue string
		expected string
	}{
		{
			name:     "store type from env",
			envKey:   "MSG2AGENT_STORE",
			envValue: "sqlite",
			expected: "sqlite",
		},
		{
			name:     "store file from env",
			envKey:   "MSG2AGENT_STORE_FILE",
			envValue: "/data/relay.db",
			expected: "/data/relay.db",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv(tt.envKey, tt.envValue)
			defer os.Unsetenv(tt.envKey)

			key := tt.envKey[len("MSG2AGENT_"):]
			got := config.GetEnv(key)
			if got != tt.expected {
				t.Errorf("GetEnv(%q) = %q, want %q", key, got, tt.expected)
			}
		})
	}
}
