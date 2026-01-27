package main

import (
	"context"
	"encoding/json"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/gianluca/msg2agent/pkg/agent"
	"github.com/gianluca/msg2agent/pkg/mcp"
)

func main() {
	// Parse flags
	name := flag.String("name", "mcp-agent", "Agent name")
	domain := flag.String("domain", "localhost", "Agent domain")
	relay := flag.String("relay", "ws://localhost:8080", "Relay hub address")
	flag.Parse()

	// Setup logging to stderr because stdout is used for MCP JSON-RPC
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	logger := slog.New(handler)

	// Create agent configuration
	cfg := agent.Config{
		Domain:      *domain,
		AgentID:     *name,
		DisplayName: *name,
		RelayAddr:   *relay,
		Logger:      logger,
	}

	// Create and start agent
	a, err := agent.New(cfg)
	if err != nil {
		logger.Error("failed to create agent", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := a.Start(ctx); err != nil {
		logger.Error("failed to start agent", "error", err)
		cancel()
		os.Exit(1)
	}

	// Connect to relay
	logger.Info("connecting to relay", "addr", *relay)
	if err := a.Connect(ctx, *relay); err != nil {
		logger.Error("failed to connect to relay", "error", err)
		os.Exit(1)
	}

	// Create MCP server
	mcpServer := mcp.NewServer(a, logger)

	// Register a catch-all handler to capture incoming messages for the inbox
	a.RegisterMethod("*", func(ctx context.Context, params json.RawMessage) (any, error) {
		// This is a catch-all handler - store message in inbox
		mcpServer.HandleIncomingMessage("unknown", "*", params)
		return map[string]string{"status": "received"}, nil
	})

	// Handle shutdown signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		logger.Info("shutting down...")
		if err := a.Stop(); err != nil {
			logger.Error("agent stop error", "error", err)
		}
		cancel()
		os.Exit(0)
	}()

	// Start MCP server (blocks)
	logger.Info("starting MCP server on stdio")
	if err := mcpServer.ServeStdio(); err != nil {
		logger.Error("mcp server error", "error", err)
		os.Exit(1)
	}
}
