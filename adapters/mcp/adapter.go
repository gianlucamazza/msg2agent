// Package mcp provides a testable MCP adapter with multi-transport support.
package mcp

import (
	"log/slog"
	"net/http"

	"github.com/mark3labs/mcp-go/server"

	pkgmcp "github.com/gianluca/msg2agent/pkg/mcp"
)

// TransportType selects the MCP transport.
type TransportType string

const (
	TransportStdio          TransportType = "stdio"
	TransportSSE            TransportType = "sse"
	TransportStreamableHTTP TransportType = "streamable-http"
)

// AgentCaller is an alias for pkgmcp.AgentCaller.
type AgentCaller = pkgmcp.AgentCaller

// AgentMessage is an alias for pkgmcp.AgentMessage.
type AgentMessage = pkgmcp.AgentMessage

// ServerConfig holds configuration for the MCP server.
type ServerConfig struct {
	Name      string
	Version   string
	Transport TransportType
	Addr      string
}

// MCPServer wraps pkg/mcp.Server adding transport concerns.
type MCPServer struct {
	*pkgmcp.Server // provides all tools, resources, inbox, notifications
	logger         *slog.Logger
	cfg            ServerConfig
}

// NewMCPServer creates a new MCP server backed by the given AgentCaller.
func NewMCPServer(caller AgentCaller, cfg ServerConfig, logger *slog.Logger) *MCPServer {
	return &MCPServer{
		Server: pkgmcp.NewServer(caller, logger),
		logger: logger,
		cfg:    cfg,
	}
}

// ServeSSE starts an SSE transport on the given address.
func (s *MCPServer) ServeSSE(addr string) error {
	sseServer := server.NewSSEServer(s.Internal())
	s.logger.Info("starting MCP SSE server", "addr", addr)
	return sseServer.Start(addr)
}

// ServeStreamableHTTP starts a Streamable HTTP transport on the given address.
func (s *MCPServer) ServeStreamableHTTP(addr string) error {
	httpServer := server.NewStreamableHTTPServer(s.Internal())
	s.logger.Info("starting MCP Streamable HTTP server", "addr", addr)
	return httpServer.Start(addr)
}

// Handler returns an http.Handler for the Streamable HTTP transport.
func (s *MCPServer) Handler() http.Handler {
	return server.NewStreamableHTTPServer(s.Internal())
}

// RegisterWithMux registers the MCP Streamable HTTP handler on a mux.
func (s *MCPServer) RegisterWithMux(mux *http.ServeMux, prefix string) {
	mux.Handle(prefix, s.Handler())
}

// Serve starts the server using the configured transport.
func (s *MCPServer) Serve() error {
	switch s.cfg.Transport {
	case TransportSSE:
		return s.ServeSSE(s.cfg.Addr)
	case TransportStreamableHTTP:
		return s.ServeStreamableHTTP(s.cfg.Addr)
	default:
		return s.ServeStdio()
	}
}
