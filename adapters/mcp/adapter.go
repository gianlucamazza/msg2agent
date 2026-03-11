// Package mcp provides a testable MCP adapter with multi-transport support.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/gianluca/msg2agent/adapters/a2a"
	pkgmcp "github.com/gianluca/msg2agent/pkg/mcp"
	"github.com/gianluca/msg2agent/pkg/registry"
)

// TransportType selects the MCP transport.
type TransportType string

const (
	TransportStdio          TransportType = "stdio"
	TransportSSE            TransportType = "sse"
	TransportStreamableHTTP TransportType = "streamable-http"
)

// AgentCaller abstracts the agent methods used by the MCP server.
type AgentCaller interface {
	DID() string
	Send(ctx context.Context, to, method string, params any) (AgentMessage, error)
	CallRelay(ctx context.Context, method string, params any) (json.RawMessage, error)
	Record() *registry.Agent
}

// AgentMessage is the response interface returned by Send.
type AgentMessage interface {
	IsError() bool
	RawBody() json.RawMessage
}

// ServerConfig holds configuration for the MCP server.
type ServerConfig struct {
	Name      string
	Version   string
	Transport TransportType
	Addr      string
}

// MCPServer wraps a mcp-go MCPServer with an AgentCaller interface.
type MCPServer struct {
	caller AgentCaller
	mcp    *server.MCPServer
	logger *slog.Logger
	cfg    ServerConfig
	inbox  *pkgmcp.Inbox
}

// NewMCPServer creates a new MCP server backed by the given AgentCaller.
func NewMCPServer(caller AgentCaller, cfg ServerConfig, logger *slog.Logger) *MCPServer {
	name := cfg.Name
	if name == "" {
		name = "msg2agent-mcp"
	}
	version := cfg.Version
	if version == "" {
		version = "0.1.0"
	}

	s := &MCPServer{
		caller: caller,
		mcp:    server.NewMCPServer(name, version),
		logger: logger,
		cfg:    cfg,
		inbox:  pkgmcp.NewInbox(1000),
	}

	s.registerTools()
	s.registerResources()
	return s
}

// Inbox returns the server's message inbox.
func (s *MCPServer) Inbox() *pkgmcp.Inbox {
	return s.inbox
}

// HandleIncomingMessage stores a message in the inbox and notifies clients.
func (s *MCPServer) HandleIncomingMessage(from, method string, body json.RawMessage) string {
	msgID := s.inbox.Add(from, method, body)
	s.mcp.SendNotificationToAllClients(string(pkgmcp.NotificationMessage), map[string]any{
		"id":     msgID,
		"from":   from,
		"method": method,
		"body":   body,
	})
	return msgID
}

// ServeStdio starts the server on stdio.
func (s *MCPServer) ServeStdio() error {
	return server.ServeStdio(s.mcp)
}

// ServeSSE starts an SSE transport on the given address.
func (s *MCPServer) ServeSSE(addr string) error {
	sseServer := server.NewSSEServer(s.mcp)
	s.logger.Info("starting MCP SSE server", "addr", addr)
	return sseServer.Start(addr)
}

// ServeStreamableHTTP starts a Streamable HTTP transport on the given address.
func (s *MCPServer) ServeStreamableHTTP(addr string) error {
	httpServer := server.NewStreamableHTTPServer(s.mcp)
	s.logger.Info("starting MCP Streamable HTTP server", "addr", addr)
	return httpServer.Start(addr)
}

// Handler returns an http.Handler for the Streamable HTTP transport.
func (s *MCPServer) Handler() http.Handler {
	return server.NewStreamableHTTPServer(s.mcp)
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

// --- Tool registration ---

func (s *MCPServer) registerTools() {
	s.mcp.AddTool(mcp.NewTool("list_agents",
		mcp.WithDescription("List available agents on the network."),
		mcp.WithString("capability", mcp.Description("Optional capability to filter by")),
	), s.listAgentsHandler)

	s.mcp.AddTool(mcp.NewTool("send_message",
		mcp.WithDescription("Send a message to another agent."),
		mcp.WithString("to", mcp.Required(), mcp.Description("DID of the recipient agent")),
		mcp.WithString("method", mcp.Required(), mcp.Description("Method to call")),
		mcp.WithString("params", mcp.Required(), mcp.Description("JSON string of parameters")),
	), s.sendMessageHandler)

	s.mcp.AddTool(mcp.NewTool("get_agent_info",
		mcp.WithDescription("Get detailed information about a specific agent."),
		mcp.WithString("did", mcp.Required(), mcp.Description("DID of the agent to query")),
	), s.getAgentInfoHandler)

	s.mcp.AddTool(mcp.NewTool("get_self_info",
		mcp.WithDescription("Get information about this agent (self)."),
	), s.getSelfInfoHandler)
}

func (s *MCPServer) registerResources() {
	s.mcp.AddResource(mcp.NewResource(
		"msg2agent://inbox",
		"Inbox messages",
		mcp.WithResourceDescription("List of incoming messages from other agents"),
		mcp.WithMIMEType("application/json"),
	), s.inboxResourceHandler)
}

// --- Tool handlers ---

func (s *MCPServer) listAgentsHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args, _ := request.Params.Arguments.(map[string]any)
	capability, _ := args["capability"].(string)

	if capability != "" {
		if err := pkgmcp.ValidateCapability(capability); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid capability: %v", err)), nil
		}
	}

	params := map[string]string{}
	if capability != "" {
		params["capability"] = capability
	}

	result, err := s.caller.CallRelay(ctx, "relay.discover", params)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to discover agents: %v", err)), nil
	}

	var agents []*registry.Agent
	if err := json.Unmarshal(result, &agents); err != nil {
		return mcp.NewToolResultError("Failed to parse agent list"), nil
	}

	output, err := json.MarshalIndent(agents, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("%s: %v", pkgmcp.ErrEncodingFailed.Error(), err)), nil
	}
	return mcp.NewToolResultText(string(output)), nil
}

func (s *MCPServer) sendMessageHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args, ok := request.Params.Arguments.(map[string]any)
	if !ok {
		return mcp.NewToolResultError(pkgmcp.ErrInvalidArguments.Error()), nil
	}

	to, ok := args["to"].(string)
	if !ok {
		return mcp.NewToolResultError("Missing 'to' parameter"), nil
	}
	if err := pkgmcp.ValidateDID(to); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Invalid 'to' DID: %v", err)), nil
	}

	method, ok := args["method"].(string)
	if !ok {
		return mcp.NewToolResultError("Missing 'method' parameter"), nil
	}
	if err := pkgmcp.ValidateMethod(method); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Invalid method: %v", err)), nil
	}

	paramsStr, ok := args["params"].(string)
	if !ok {
		return mcp.NewToolResultError("Missing 'params' parameter"), nil
	}
	if err := pkgmcp.ValidateParams(paramsStr); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Invalid params: %v", err)), nil
	}

	var params any
	if err := json.Unmarshal([]byte(paramsStr), &params); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Invalid JSON params: %v", err)), nil
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	resp, err := s.caller.Send(ctx, to, method, params)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Send failed: %v", err)), nil
	}

	if resp.IsError() {
		return mcp.NewToolResultError(fmt.Sprintf("Agent returned error: %s", resp.RawBody())), nil
	}

	output, err := json.MarshalIndent(resp.RawBody(), "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("%s: %v", pkgmcp.ErrEncodingFailed.Error(), err)), nil
	}
	return mcp.NewToolResultText(string(output)), nil
}

func (s *MCPServer) getAgentInfoHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args, ok := request.Params.Arguments.(map[string]any)
	if !ok {
		return mcp.NewToolResultError(pkgmcp.ErrInvalidArguments.Error()), nil
	}

	did, ok := args["did"].(string)
	if !ok {
		return mcp.NewToolResultError("Missing 'did' parameter"), nil
	}
	if err := pkgmcp.ValidateDID(did); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Invalid DID: %v", err)), nil
	}

	result, err := s.caller.CallRelay(ctx, "relay.discover", map[string]string{})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to discover agents: %v", err)), nil
	}

	var agents []*registry.Agent
	if err := json.Unmarshal(result, &agents); err != nil {
		return mcp.NewToolResultError("Failed to parse agent list"), nil
	}

	var agent *registry.Agent
	for _, a := range agents {
		if a.DID == did {
			agent = a
			break
		}
	}
	if agent == nil {
		return mcp.NewToolResultError(fmt.Sprintf("Agent not found: %s", did)), nil
	}

	adapter := a2a.NewAdapter()
	card := adapter.ToA2AAgentCard(agent)

	output, err := json.MarshalIndent(card, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("%s: %v", pkgmcp.ErrEncodingFailed.Error(), err)), nil
	}
	return mcp.NewToolResultText(string(output)), nil
}

func (s *MCPServer) getSelfInfoHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	rec := s.caller.Record()
	info := map[string]any{
		"did":          s.caller.DID(),
		"display_name": rec.DisplayName,
		"endpoints":    rec.Endpoints,
		"capabilities": rec.Capabilities,
		"status":       rec.Status,
	}

	output, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("%s: %v", pkgmcp.ErrEncodingFailed.Error(), err)), nil
	}
	return mcp.NewToolResultText(string(output)), nil
}

// --- Resource handlers ---

func (s *MCPServer) inboxResourceHandler(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	messages := s.inbox.List(false)

	data, err := json.MarshalIndent(messages, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal inbox: %w", err)
	}

	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      "msg2agent://inbox",
			MIMEType: "application/json",
			Text:     string(data),
		},
	}, nil
}
