package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"

	"github.com/gianluca/msg2agent/pkg/registry"
)

// mockMessage implements AgentMessage for testing.
type mockMessage struct {
	isError bool
	body    json.RawMessage
}

func (m *mockMessage) IsError() bool            { return m.isError }
func (m *mockMessage) RawBody() json.RawMessage { return m.body }

// mockAgentCaller implements AgentCaller for testing.
type mockAgentCaller struct {
	did       string
	record    *registry.Agent
	sendFn    func(ctx context.Context, to, method string, params any) (AgentMessage, error)
	callRelay func(ctx context.Context, method string, params any) (json.RawMessage, error)
}

func (m *mockAgentCaller) DID() string { return m.did }
func (m *mockAgentCaller) Record() *registry.Agent {
	return m.record
}
func (m *mockAgentCaller) Send(ctx context.Context, to, method string, params any) (AgentMessage, error) {
	if m.sendFn != nil {
		return m.sendFn(ctx, to, method, params)
	}
	return &mockMessage{body: json.RawMessage(`{"status":"ok"}`)}, nil
}
func (m *mockAgentCaller) CallRelay(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if m.callRelay != nil {
		return m.callRelay(ctx, method, params)
	}
	return json.RawMessage(`[]`), nil
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func newMockCaller() *mockAgentCaller {
	return &mockAgentCaller{
		did: "did:wba:example.com:agent:test",
		record: &registry.Agent{
			DID:         "did:wba:example.com:agent:test",
			DisplayName: "Test Agent",
			Endpoints: []registry.Endpoint{
				{Transport: registry.TransportWebSocket, URL: "ws://localhost:8080"},
			},
			Capabilities: []registry.Capability{
				{Name: "chat", Description: "Chat capability"},
			},
			Status: registry.StatusOnline,
		},
	}
}

func TestNewMCPServer(t *testing.T) {
	caller := newMockCaller()
	cfg := ServerConfig{Name: "test", Version: "1.0.0"}

	srv := NewMCPServer(caller, cfg, testLogger())
	if srv == nil {
		t.Fatal("NewMCPServer returned nil")
	}
	if srv.caller == nil {
		t.Error("caller should be set")
	}
	if srv.mcp == nil {
		t.Error("mcp server should be set")
	}
	if srv.inbox == nil {
		t.Error("inbox should be set")
	}
}

func TestNewMCPServerDefaults(t *testing.T) {
	caller := newMockCaller()
	srv := NewMCPServer(caller, ServerConfig{}, testLogger())
	if srv == nil {
		t.Fatal("NewMCPServer returned nil")
	}
}

func TestListAgentsHandler(t *testing.T) {
	agents := []*registry.Agent{
		{DID: "did:wba:example.com:agent:alice", DisplayName: "Alice"},
		{DID: "did:wba:example.com:agent:bob", DisplayName: "Bob"},
	}
	agentsJSON, _ := json.Marshal(agents)

	caller := newMockCaller()
	caller.callRelay = func(ctx context.Context, method string, params any) (json.RawMessage, error) {
		if method != "relay.discover" {
			t.Errorf("expected relay.discover, got %s", method)
		}
		return agentsJSON, nil
	}

	srv := NewMCPServer(caller, ServerConfig{}, testLogger())

	req := mcpgo.CallToolRequest{}
	req.Params.Name = "list_agents"
	req.Params.Arguments = map[string]any{}

	result, err := srv.listAgentsHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result.IsError {
		t.Error("result should not be an error")
	}
	if len(result.Content) == 0 {
		t.Error("should have content")
	}
}

func TestSendMessageHandler(t *testing.T) {
	caller := newMockCaller()
	var sentTo, sentMethod string
	caller.sendFn = func(ctx context.Context, to, method string, params any) (AgentMessage, error) {
		sentTo = to
		sentMethod = method
		return &mockMessage{body: json.RawMessage(`{"result":"success"}`)}, nil
	}

	srv := NewMCPServer(caller, ServerConfig{}, testLogger())

	req := mcpgo.CallToolRequest{}
	req.Params.Name = "send_message"
	req.Params.Arguments = map[string]any{
		"to":     "did:wba:example.com:agent:bob",
		"method": "chat.send",
		"params": `{"text":"hello"}`,
	}

	result, err := srv.sendMessageHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result.IsError {
		t.Error("result should not be an error")
	}
	if sentTo != "did:wba:example.com:agent:bob" {
		t.Errorf("sentTo = %s, want did:wba:example.com:agent:bob", sentTo)
	}
	if sentMethod != "chat.send" {
		t.Errorf("sentMethod = %s, want chat.send", sentMethod)
	}
}

func TestSendMessageHandlerMissingParams(t *testing.T) {
	caller := newMockCaller()
	srv := NewMCPServer(caller, ServerConfig{}, testLogger())

	tests := []struct {
		name string
		args map[string]any
		want string
	}{
		{
			name: "missing to",
			args: map[string]any{"method": "test", "params": "{}"},
			want: "Missing 'to'",
		},
		{
			name: "missing method",
			args: map[string]any{"to": "did:wba:example.com:agent:x", "params": "{}"},
			want: "Missing 'method'",
		},
		{
			name: "missing params",
			args: map[string]any{"to": "did:wba:example.com:agent:x", "method": "test"},
			want: "Missing 'params'",
		},
		{
			name: "invalid json params",
			args: map[string]any{"to": "did:wba:example.com:agent:x", "method": "test", "params": "not json"},
			want: "Invalid JSON params",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := mcpgo.CallToolRequest{}
			req.Params.Arguments = tt.args

			result, err := srv.sendMessageHandler(context.Background(), req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !result.IsError {
				t.Error("expected error result")
			}
		})
	}
}

func TestGetSelfInfoHandler(t *testing.T) {
	caller := newMockCaller()
	srv := NewMCPServer(caller, ServerConfig{}, testLogger())

	req := mcpgo.CallToolRequest{}
	req.Params.Name = "get_self_info"

	result, err := srv.getSelfInfoHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result.IsError {
		t.Error("result should not be an error")
	}
	if len(result.Content) == 0 {
		t.Error("should have content")
	}

	// Verify the output contains expected fields
	text := result.Content[0].(mcpgo.TextContent).Text
	if !strings.Contains(text, "did:wba:example.com:agent:test") {
		t.Error("output should contain agent DID")
	}
	if !strings.Contains(text, "Test Agent") {
		t.Error("output should contain display name")
	}
}

func TestGetAgentInfoHandler(t *testing.T) {
	testAgent := &registry.Agent{
		DID:         "did:wba:example.com:agent:alice",
		DisplayName: "Alice",
		Endpoints: []registry.Endpoint{
			{Transport: registry.TransportWebSocket, URL: "ws://alice.example.com"},
		},
		Capabilities: []registry.Capability{
			{Name: "chat", Description: "Chat capability"},
		},
	}
	agentJSON, _ := json.Marshal(testAgent)

	caller := newMockCaller()
	caller.callRelay = func(ctx context.Context, method string, params any) (json.RawMessage, error) {
		if method != "relay.lookup" {
			t.Errorf("expected relay.lookup, got %s", method)
		}
		return agentJSON, nil
	}

	srv := NewMCPServer(caller, ServerConfig{}, testLogger())

	req := mcpgo.CallToolRequest{}
	req.Params.Name = "get_agent_info"
	req.Params.Arguments = map[string]any{
		"did": "did:wba:example.com:agent:alice",
	}

	result, err := srv.getAgentInfoHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result.IsError {
		t.Error("result should not be an error")
	}

	text := result.Content[0].(mcpgo.TextContent).Text
	if !strings.Contains(text, "Alice") {
		t.Error("output should contain agent name")
	}
}

func TestInboxResourceHandler(t *testing.T) {
	caller := newMockCaller()
	srv := NewMCPServer(caller, ServerConfig{}, testLogger())

	// Add a message to the inbox
	srv.inbox.Add("did:wba:example.com:agent:alice", "chat.send", json.RawMessage(`{"text":"hello"}`))

	req := mcpgo.ReadResourceRequest{}
	contents, err := srv.inboxResourceHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if len(contents) == 0 {
		t.Fatal("should have content")
	}

	text := contents[0].(mcpgo.TextResourceContents).Text
	if !strings.Contains(text, "hello") {
		t.Error("inbox should contain the message")
	}
}

func TestHandleIncomingMessage(t *testing.T) {
	caller := newMockCaller()
	srv := NewMCPServer(caller, ServerConfig{}, testLogger())

	msgID := srv.HandleIncomingMessage("did:wba:example.com:agent:bob", "chat.send", json.RawMessage(`{"text":"hi"}`))
	if msgID == "" {
		t.Error("expected non-empty message ID")
	}

	// Verify it's in the inbox
	msg := srv.inbox.Get(msgID)
	if msg == nil {
		t.Fatal("message should be in inbox")
	}
	if msg.From != "did:wba:example.com:agent:bob" {
		t.Errorf("from = %s, want did:wba:example.com:agent:bob", msg.From)
	}
	if msg.Method != "chat.send" {
		t.Errorf("method = %s, want chat.send", msg.Method)
	}
}

func TestServeDispatch(t *testing.T) {
	caller := newMockCaller()

	tests := []struct {
		transport TransportType
	}{
		{TransportStdio},
		{TransportSSE},
		{TransportStreamableHTTP},
	}

	for _, tt := range tests {
		t.Run(string(tt.transport), func(t *testing.T) {
			cfg := ServerConfig{Transport: tt.transport, Addr: ":0"}
			srv := NewMCPServer(caller, cfg, testLogger())
			if srv.cfg.Transport != tt.transport {
				t.Errorf("transport = %s, want %s", srv.cfg.Transport, tt.transport)
			}
		})
	}
}

func TestHandler(t *testing.T) {
	caller := newMockCaller()
	srv := NewMCPServer(caller, ServerConfig{}, testLogger())

	handler := srv.Handler()
	if handler == nil {
		t.Error("Handler() should return non-nil http.Handler")
	}
}
