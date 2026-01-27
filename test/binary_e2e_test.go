//go:build e2e

// Package test provides end-to-end tests for msg2agent binaries.
//
// Run these tests with: go test ./test/... -tags=e2e -v
package test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testBinDir holds the directory where test binaries are compiled.
var testBinDir string

// TestMain compiles the binaries before running E2E tests.
func TestMain(m *testing.M) {
	// Create temp directory for binaries
	var err error
	testBinDir, err = os.MkdirTemp("", "msg2agent-e2e-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(testBinDir)

	// Build binaries
	if err := buildBinaries(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to build binaries: %v\n", err)
		os.Exit(1)
	}

	// Run tests
	os.Exit(m.Run())
}

// buildBinaries compiles the relay and agent binaries.
func buildBinaries() error {
	// Find project root by looking for go.mod
	projectRoot, err := findProjectRoot()
	if err != nil {
		return fmt.Errorf("failed to find project root: %w", err)
	}

	binaries := []struct {
		name string
		pkg  string
	}{
		{"relay", "./cmd/relay"},
		{"agent", "./cmd/agent"},
	}

	for _, bin := range binaries {
		outPath := filepath.Join(testBinDir, bin.name)
		cmd := exec.Command("go", "build", "-o", outPath, bin.pkg)
		cmd.Dir = projectRoot
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to build %s: %w", bin.name, err)
		}
	}

	return nil
}

// findProjectRoot finds the project root by looking for go.mod.
func findProjectRoot() (string, error) {
	// Start from current working directory
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	// Walk up looking for go.mod
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found")
		}
		dir = parent
	}
}

// getFreePort returns an available TCP port.
func getFreePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}
	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// waitForHTTP waits for an HTTP endpoint to become available.
func waitForHTTP(url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for %s", url)
}

// TestRelayStartup verifies the relay starts and responds to health checks.
func TestRelayStartup(t *testing.T) {
	port, err := getFreePort()
	if err != nil {
		t.Fatalf("failed to get free port: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Start relay
	relayBin := filepath.Join(testBinDir, "relay")
	cmd := exec.CommandContext(ctx, relayBin,
		"-addr", fmt.Sprintf(":%d", port),
		"-log-level", "error",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start relay: %v", err)
	}
	defer cmd.Process.Kill()

	// Wait for health endpoint
	healthURL := fmt.Sprintf("http://localhost:%d/health", port)
	if err := waitForHTTP(healthURL, 10*time.Second); err != nil {
		t.Fatalf("relay health check failed: %v", err)
	}

	// Check health response
	resp, err := http.Get(healthURL)
	if err != nil {
		t.Fatalf("failed to get health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("health status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ok" {
		t.Errorf("health body = %q, want %q", body, "ok")
	}
}

// TestRelayReadiness verifies the relay readiness endpoint.
func TestRelayReadiness(t *testing.T) {
	port, err := getFreePort()
	if err != nil {
		t.Fatalf("failed to get free port: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Start relay
	relayBin := filepath.Join(testBinDir, "relay")
	cmd := exec.CommandContext(ctx, relayBin,
		"-addr", fmt.Sprintf(":%d", port),
		"-log-level", "error",
		"-max-connections", "500",
	)

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start relay: %v", err)
	}
	defer cmd.Process.Kill()

	// Wait for ready endpoint
	readyURL := fmt.Sprintf("http://localhost:%d/ready", port)
	if err := waitForHTTP(readyURL, 10*time.Second); err != nil {
		t.Fatalf("relay readiness check failed: %v", err)
	}

	// Check readiness response
	resp, err := http.Get(readyURL)
	if err != nil {
		t.Fatalf("failed to get readiness: %v", err)
	}
	defer resp.Body.Close()

	var readiness map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&readiness); err != nil {
		t.Fatalf("failed to decode readiness: %v", err)
	}

	if readiness["status"] != "ready" {
		t.Errorf("readiness status = %v, want 'ready'", readiness["status"])
	}

	// Verify max connections is configured
	if maxVal, ok := readiness["max"].(float64); !ok || int(maxVal) != 500 {
		t.Errorf("readiness max = %v, want 500", readiness["max"])
	}
}

// TestRelayMetrics verifies the relay exposes Prometheus metrics.
func TestRelayMetrics(t *testing.T) {
	port, err := getFreePort()
	if err != nil {
		t.Fatalf("failed to get free port: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Start relay
	relayBin := filepath.Join(testBinDir, "relay")
	cmd := exec.CommandContext(ctx, relayBin,
		"-addr", fmt.Sprintf(":%d", port),
		"-log-level", "error",
	)

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start relay: %v", err)
	}
	defer cmd.Process.Kill()

	// Wait for metrics endpoint
	metricsURL := fmt.Sprintf("http://localhost:%d/metrics", port)
	if err := waitForHTTP(metricsURL, 10*time.Second); err != nil {
		t.Fatalf("relay metrics check failed: %v", err)
	}

	// Check metrics response
	resp, err := http.Get(metricsURL)
	if err != nil {
		t.Fatalf("failed to get metrics: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	metrics := string(body)

	// Verify key metrics are present
	expectedMetrics := []string{
		"relay_connections_total",
		"relay_connections_current",
		"relay_messages_routed_total",
		"relay_registrations_total",
	}

	for _, metric := range expectedMetrics {
		if !strings.Contains(metrics, metric) {
			t.Errorf("metrics missing %q", metric)
		}
	}
}

// TestRelaySQLiteStore verifies the relay works with SQLite store.
func TestRelaySQLiteStore(t *testing.T) {
	port, err := getFreePort()
	if err != nil {
		t.Fatalf("failed to get free port: %v", err)
	}

	// Create temp file for SQLite database
	dbFile := filepath.Join(t.TempDir(), "relay-test.db")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Start relay with SQLite store
	relayBin := filepath.Join(testBinDir, "relay")
	cmd := exec.CommandContext(ctx, relayBin,
		"-addr", fmt.Sprintf(":%d", port),
		"-log-level", "error",
		"-store", "sqlite",
		"-store-file", dbFile,
	)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start relay: %v", err)
	}
	defer cmd.Process.Kill()

	// Wait for health endpoint
	healthURL := fmt.Sprintf("http://localhost:%d/health", port)
	if err := waitForHTTP(healthURL, 10*time.Second); err != nil {
		t.Fatalf("relay health check failed: %v\nstderr: %s", err, stderr.String())
	}

	// Verify SQLite database file was created
	if _, err := os.Stat(dbFile); os.IsNotExist(err) {
		t.Error("SQLite database file should have been created")
	}
}

// TestAgentStartup verifies the agent starts and serves its agent card.
func TestAgentStartup(t *testing.T) {
	httpPort, err := getFreePort()
	if err != nil {
		t.Fatalf("failed to get free port: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Start agent
	agentBin := filepath.Join(testBinDir, "agent")
	cmd := exec.CommandContext(ctx, agentBin,
		"-name", "test-agent",
		"-domain", "test.local",
		"-http", fmt.Sprintf(":%d", httpPort),
		"-log-level", "error",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start agent: %v", err)
	}
	defer cmd.Process.Kill()

	// Wait for agent card endpoint
	cardURL := fmt.Sprintf("http://localhost:%d/.well-known/agent.json", httpPort)
	if err := waitForHTTP(cardURL, 10*time.Second); err != nil {
		t.Fatalf("agent card check failed: %v", err)
	}

	// Fetch agent card
	resp, err := http.Get(cardURL)
	if err != nil {
		t.Fatalf("failed to get agent card: %v", err)
	}
	defer resp.Body.Close()

	var card map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&card); err != nil {
		t.Fatalf("failed to decode agent card: %v", err)
	}

	// Verify card contents
	if card["name"] != "test-agent" {
		t.Errorf("agent name = %v, want 'test-agent'", card["name"])
	}

	did, ok := card["did"].(string)
	if !ok || !strings.HasPrefix(did, "did:wba:") {
		t.Errorf("agent DID = %v, want did:wba:...", card["did"])
	}
}

// TestAgentHealthEndpoint verifies the agent exposes health endpoints.
func TestAgentHealthEndpoint(t *testing.T) {
	httpPort, err := getFreePort()
	if err != nil {
		t.Fatalf("failed to get free port: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Start agent
	agentBin := filepath.Join(testBinDir, "agent")
	cmd := exec.CommandContext(ctx, agentBin,
		"-name", "health-test",
		"-http", fmt.Sprintf(":%d", httpPort),
		"-log-level", "error",
	)

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start agent: %v", err)
	}
	defer cmd.Process.Kill()

	// Wait for health endpoint
	healthURL := fmt.Sprintf("http://localhost:%d/health", httpPort)
	if err := waitForHTTP(healthURL, 10*time.Second); err != nil {
		t.Fatalf("agent health check failed: %v", err)
	}

	// Check health response
	resp, err := http.Get(healthURL)
	if err != nil {
		t.Fatalf("failed to get health: %v", err)
	}
	defer resp.Body.Close()

	var health map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		t.Fatalf("failed to decode health: %v", err)
	}

	if health["status"] != "ok" {
		t.Errorf("health status = %v, want 'ok'", health["status"])
	}
}

// TestAgentWithMetrics verifies the agent metrics server.
func TestAgentWithMetrics(t *testing.T) {
	httpPort, err := getFreePort()
	if err != nil {
		t.Fatalf("failed to get free port: %v", err)
	}

	metricsPort, err := getFreePort()
	if err != nil {
		t.Fatalf("failed to get free port for metrics: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Start agent with metrics
	agentBin := filepath.Join(testBinDir, "agent")
	cmd := exec.CommandContext(ctx, agentBin,
		"-name", "metrics-test",
		"-http", fmt.Sprintf(":%d", httpPort),
		"-metrics", fmt.Sprintf(":%d", metricsPort),
		"-log-level", "error",
	)

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start agent: %v", err)
	}
	defer cmd.Process.Kill()

	// Wait for metrics endpoint
	metricsURL := fmt.Sprintf("http://localhost:%d/metrics", metricsPort)
	if err := waitForHTTP(metricsURL, 10*time.Second); err != nil {
		t.Fatalf("agent metrics check failed: %v", err)
	}

	// Check metrics response
	resp, err := http.Get(metricsURL)
	if err != nil {
		t.Fatalf("failed to get metrics: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	metrics := string(body)

	// Should contain Go runtime metrics at minimum
	if !strings.Contains(metrics, "go_goroutines") {
		t.Error("metrics should contain go_goroutines")
	}

	// Check metrics server health endpoints
	healthURL := fmt.Sprintf("http://localhost:%d/health", metricsPort)
	resp, err = http.Get(healthURL)
	if err != nil {
		t.Fatalf("failed to get metrics health: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("metrics health status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// Check readiness
	readyURL := fmt.Sprintf("http://localhost:%d/ready", metricsPort)
	resp, err = http.Get(readyURL)
	if err != nil {
		t.Fatalf("failed to get metrics ready: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("metrics ready status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

// TestEnvVarConfiguration verifies configuration via environment variables.
func TestEnvVarConfiguration(t *testing.T) {
	port, err := getFreePort()
	if err != nil {
		t.Fatalf("failed to get free port: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Start relay with env vars only (no flags)
	relayBin := filepath.Join(testBinDir, "relay")
	cmd := exec.CommandContext(ctx, relayBin)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("MSG2AGENT_RELAY_ADDR=:%d", port),
		"MSG2AGENT_LOG_LEVEL=error",
		"MSG2AGENT_MAX_CONNECTIONS=100",
	)

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start relay: %v", err)
	}
	defer cmd.Process.Kill()

	// Wait for health endpoint
	healthURL := fmt.Sprintf("http://localhost:%d/health", port)
	if err := waitForHTTP(healthURL, 10*time.Second); err != nil {
		t.Fatalf("relay health check failed: %v", err)
	}

	// Verify config was applied via readiness
	readyURL := fmt.Sprintf("http://localhost:%d/ready", port)
	resp, err := http.Get(readyURL)
	if err != nil {
		t.Fatalf("failed to get readiness: %v", err)
	}
	defer resp.Body.Close()

	var readiness map[string]any
	json.NewDecoder(resp.Body).Decode(&readiness)

	if maxVal, ok := readiness["max"].(float64); !ok || int(maxVal) != 100 {
		t.Errorf("readiness max = %v, want 100 (from env var)", readiness["max"])
	}
}

// TestGracefulShutdown verifies the relay shuts down gracefully.
func TestGracefulShutdown(t *testing.T) {
	port, err := getFreePort()
	if err != nil {
		t.Fatalf("failed to get free port: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Start relay
	relayBin := filepath.Join(testBinDir, "relay")
	cmd := exec.CommandContext(ctx, relayBin,
		"-addr", fmt.Sprintf(":%d", port),
		"-log-level", "info",
	)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start relay: %v", err)
	}

	// Wait for health endpoint
	healthURL := fmt.Sprintf("http://localhost:%d/health", port)
	if err := waitForHTTP(healthURL, 10*time.Second); err != nil {
		t.Fatalf("relay health check failed: %v", err)
	}

	// Send SIGTERM
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatalf("failed to send SIGTERM: %v", err)
	}

	// Wait for process to exit
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		// Process exited
		if err != nil {
			// Exit with signal is expected
			t.Logf("process exited: %v", err)
		}
	case <-time.After(10 * time.Second):
		cmd.Process.Kill()
		t.Fatal("relay did not shut down within 10 seconds")
	}

	// Check stderr for shutdown message
	output := stderr.String()
	if !strings.Contains(output, "shutting down") {
		t.Logf("stderr: %s", output)
		// Not a fatal error, just informational
	}
}
