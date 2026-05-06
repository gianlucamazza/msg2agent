// Package mcp provides a testable MCP adapter with multi-transport support.
package mcp

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/mark3labs/mcp-go/server"

	pkgmcp "github.com/gianlucamazza/msg2agent/pkg/mcp"
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
	authMiddleware func(http.Handler) http.Handler // optional auth wrapper
}

// NewMCPServer creates a new MCP server backed by the given AgentCaller.
// Optional ServerOptions (e.g. billing tool middleware) are forwarded to mcp-go.
func NewMCPServer(caller AgentCaller, cfg ServerConfig, logger *slog.Logger, opts ...server.ServerOption) *MCPServer {
	return &MCPServer{
		Server: pkgmcp.NewServer(caller, logger, opts...),
		logger: logger,
		cfg:    cfg,
	}
}

// WithAuthMiddleware wraps the HTTP handler with the given middleware (e.g. API key auth).
// Must be called before Serve/Handler/RegisterWithMux.
func (s *MCPServer) WithAuthMiddleware(mw func(http.Handler) http.Handler) {
	s.authMiddleware = mw
}

// ServeSSE starts an SSE transport on the given address.
func (s *MCPServer) ServeSSE(addr string) error {
	sseServer := server.NewSSEServer(s.Internal())
	s.logger.Info("starting MCP SSE server", "addr", addr)
	return sseServer.Start(addr)
}

// ServeStreamableHTTP starts a Streamable HTTP transport on the given address.
func (s *MCPServer) ServeStreamableHTTP(addr string) error {
	s.logger.Info("starting MCP Streamable HTTP server", "addr", addr)
	if s.authMiddleware == nil {
		return server.NewStreamableHTTPServer(s.Internal()).Start(addr)
	}
	// Wrap the mcp-go handler with auth middleware and serve via net/http.
	mux := http.NewServeMux()
	s.RegisterWithMux(mux, "/mcp")
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	return srv.ListenAndServe()
}

// Handler returns an http.Handler for the Streamable HTTP transport,
// wrapped with auth middleware if one was set via WithAuthMiddleware.
func (s *MCPServer) Handler() http.Handler {
	var h http.Handler = server.NewStreamableHTTPServer(s.Internal())
	if s.authMiddleware != nil {
		h = s.authMiddleware(h)
	}
	return h
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
