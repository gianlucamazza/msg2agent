package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	mcpadapter "github.com/gianluca/msg2agent/adapters/mcp"
	"github.com/gianluca/msg2agent/pkg/agent"
	"github.com/gianluca/msg2agent/pkg/messaging"
	"github.com/gianluca/msg2agent/pkg/registry"
)

// agentBridge adapts *agent.Agent to mcpadapter.AgentCaller.
type agentBridge struct {
	a *agent.Agent
}

func (b *agentBridge) DID() string             { return b.a.DID() }
func (b *agentBridge) Record() *registry.Agent { return b.a.Record() }
func (b *agentBridge) CallRelay(ctx context.Context, method string, params any) (json.RawMessage, error) {
	return b.a.CallRelay(ctx, method, params)
}
func (b *agentBridge) Send(ctx context.Context, to, method string, params any) (mcpadapter.AgentMessage, error) {
	msg, err := b.a.Send(ctx, to, method, params)
	if err != nil {
		return nil, err
	}
	return &messageWrapper{msg}, nil
}

// messageWrapper adapts *messaging.Message to mcpadapter.AgentMessage.
type messageWrapper struct {
	m *messaging.Message
}

func (w *messageWrapper) IsError() bool            { return w.m.IsError() }
func (w *messageWrapper) RawBody() json.RawMessage { return json.RawMessage(w.m.Body) }

func main() {
	name := flag.String("name", "mcp-agent", "Agent name")
	domain := flag.String("domain", "localhost", "Agent domain")
	relay := flag.String("relay", "ws://localhost:8080", "Relay hub address")
	transport := flag.String("transport", "stdio", "MCP transport: stdio, sse, streamable-http")
	addr := flag.String("addr", ":8081", "Listen address for SSE/HTTP transports")
	flag.Parse()

	// Validate transport flag
	tp := mcpadapter.TransportType(*transport)
	switch tp {
	case mcpadapter.TransportStdio, mcpadapter.TransportSSE, mcpadapter.TransportStreamableHTTP:
	default:
		fmt.Fprintf(os.Stderr, "unknown transport: %s\n", *transport)
		os.Exit(1)
	}

	// Setup logging to stderr (stdout is used for MCP JSON-RPC in stdio mode)
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	logger := slog.New(handler)

	// Create agent
	cfg := agent.Config{
		Domain:      *domain,
		AgentID:     *name,
		DisplayName: *name,
		RelayAddr:   *relay,
		Logger:      logger,
	}

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

	// Connect to relay and register with DID ownership proof
	logger.Info("connecting to relay", "addr", *relay)
	if err := a.Connect(ctx, *relay); err != nil {
		logger.Error("failed to connect to relay", "error", err)
		os.Exit(1)
	}

	// Register with relay (proof of DID ownership)
	{
		ts := time.Now().Unix()
		proofMessage := fmt.Sprintf("%s:%d", a.DID(), ts)
		proof := a.Sign([]byte(proofMessage))

		regReq := map[string]any{
			"id":           a.Record().ID,
			"did":          a.Record().DID,
			"display_name": a.Record().DisplayName,
			"public_keys":  a.Record().PublicKeys,
			"endpoints":    a.Record().Endpoints,
			"capabilities": a.Record().Capabilities,
			"status":       a.Record().Status,
			"proof":        proof,
			"timestamp":    ts,
		}

		result, err := a.CallRelay(ctx, "relay.register", regReq)
		if err != nil {
			logger.Error("failed to register with relay", "error", err)
			os.Exit(1)
		}
		logger.Info("registered with relay", "result", string(result))
	}

	// Create MCP server via adapter
	mcpServer := mcpadapter.NewMCPServer(
		&agentBridge{a: a},
		mcpadapter.ServerConfig{
			Name:      *name,
			Version:   "0.1.0",
			Transport: tp,
			Addr:      *addr,
		},
		logger,
	)

	// Register catch-all handler for incoming messages
	a.RegisterMethod("*", func(ctx context.Context, params json.RawMessage) (any, error) {
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
	logger.Info("starting MCP server", "transport", *transport, "addr", *addr)
	if err := mcpServer.Serve(); err != nil {
		logger.Error("mcp server error", "error", err)
		os.Exit(1)
	}
}
