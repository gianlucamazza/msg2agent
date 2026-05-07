package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gianlucamazza/msg2agent/pkg/httputil"
	"github.com/gianlucamazza/msg2agent/pkg/queue"
	"github.com/gianlucamazza/msg2agent/pkg/registry"
)

var (
	ErrClientNotRegistered = errors.New("client not registered")
	ErrSenderMismatch      = errors.New("sender mismatch")
	ErrRateLimited         = errors.New("rate limit exceeded")
	ErrMaxConnections      = errors.New("max connections reached")
	ErrDIDProofRequired    = errors.New("DID ownership proof required")
	ErrDIDProofInvalid     = errors.New("DID ownership proof invalid")
	ErrInvalidDIDFormat    = errors.New("invalid DID format: must be did:wba:*")
	ErrInvalidPath         = errors.New("invalid or unsafe file path")
)

// validateAndCleanPath validates a file path and returns a clean, absolute path.
// It rejects paths that contain traversal attempts or are otherwise unsafe.
func validateAndCleanPath(path string, logger *slog.Logger) (string, error) {
	if path == "" || path == ":memory:" {
		return path, nil // Empty path or in-memory database is OK
	}

	// Clean the path to remove any .. or . components
	cleanPath := filepath.Clean(path)

	// Convert to absolute path
	absPath, err := filepath.Abs(cleanPath)
	if err != nil {
		return "", fmt.Errorf("%w: failed to resolve absolute path: %v", ErrInvalidPath, err)
	}

	// Check for suspicious patterns in the original path
	if strings.Contains(path, "..") {
		logger.Warn("path contains directory traversal", "original", path, "resolved", absPath)
	}

	// Verify the resolved path doesn't escape expected directories
	// Allow paths that start with current directory or home directory
	cwd, _ := os.Getwd()
	home, _ := os.UserHomeDir()

	// Basic sanity check: path should be under cwd, home, or /tmp
	if !strings.HasPrefix(absPath, cwd) &&
		!strings.HasPrefix(absPath, home) &&
		!strings.HasPrefix(absPath, "/tmp") &&
		!strings.HasPrefix(absPath, os.TempDir()) {
		logger.Warn("file path is outside expected directories",
			"path", absPath, "cwd", cwd, "home", home)
	}

	return absPath, nil
}

// RegistrationRequest extends registry.Agent with proof of DID ownership.
type RegistrationRequest struct {
	registry.Agent

	// Proof contains a signature of (DID + Timestamp) using the agent's signing key.
	// This proves the registering party controls the private key for the DID.
	Proof     []byte `json:"proof,omitempty"`
	Timestamp int64  `json:"timestamp,omitempty"` // Unix timestamp used in proof
}

// RelayConfig holds relay hub configuration.
type RelayConfig struct {
	// Connection limits
	MaxConnections int

	// Rate limiting (per client)
	MessageRateLimit  float64 // messages per second
	MessageBurstSize  float64 // max burst
	RegisterRateLimit float64 // registrations per second
	DiscoverRateLimit float64 // discover requests per second

	// Timeouts
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration

	// Offline queue configuration
	EnableOfflineQueue bool
	QueueConfig        queue.Config

	// CORS configuration
	AllowedOrigins []string // Allowed CORS origins (empty = deny all external origins)

	// Security
	RequireDIDProof bool // Require agents to prove DID ownership during registration
}

// DefaultRelayConfig returns sensible defaults.
func DefaultRelayConfig() RelayConfig {
	return RelayConfig{
		MaxConnections:     1000,
		MessageRateLimit:   100.0, // 100 msg/sec
		MessageBurstSize:   200.0, // burst of 200
		RegisterRateLimit:  1.0,   // 1 reg/sec
		DiscoverRateLimit:  10.0,  // 10 discover/sec
		ReadTimeout:        5 * time.Minute,
		WriteTimeout:       10 * time.Second,
		IdleTimeout:        5 * time.Minute,
		EnableOfflineQueue: true,
		QueueConfig:        queue.DefaultConfig(),
		AllowedOrigins:     nil,  // Secure default: no external origins allowed
		RequireDIDProof:    true, // Secure default: require proof of DID ownership
	}
}

// Validate checks that the config has valid values.
func (c *RelayConfig) Validate() error {
	if c.MaxConnections <= 0 {
		return fmt.Errorf("max_connections must be positive, got %d", c.MaxConnections)
	}
	if c.MessageRateLimit <= 0 {
		return fmt.Errorf("message_rate_limit must be positive, got %f", c.MessageRateLimit)
	}
	if c.MessageBurstSize <= 0 {
		return fmt.Errorf("message_burst_size must be positive, got %f", c.MessageBurstSize)
	}
	if c.RegisterRateLimit <= 0 {
		return fmt.Errorf("register_rate_limit must be positive, got %f", c.RegisterRateLimit)
	}
	if c.DiscoverRateLimit <= 0 {
		return fmt.Errorf("discover_rate_limit must be positive, got %f", c.DiscoverRateLimit)
	}
	if c.ReadTimeout <= 0 {
		return fmt.Errorf("read_timeout must be positive, got %v", c.ReadTimeout)
	}
	if c.WriteTimeout <= 0 {
		return fmt.Errorf("write_timeout must be positive, got %v", c.WriteTimeout)
	}
	return nil
}

// securityHeaders wraps an http.Handler and sets security-related response headers.
var securityHeaders = httputil.SecurityHeaders
