package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"testing"

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
	did         string
	record      *registry.Agent
	sendFn      func(ctx context.Context, to, method string, params any) (AgentMessage, error)
	sendAsyncFn func(ctx context.Context, to, method string, params any) (string, error)
	callRelay   func(ctx context.Context, method string, params any) (json.RawMessage, error)
}

func (m *mockAgentCaller) DID() string             { return m.did }
func (m *mockAgentCaller) Record() *registry.Agent { return m.record }
func (m *mockAgentCaller) Send(ctx context.Context, to, method string, params any) (AgentMessage, error) {
	if m.sendFn != nil {
		return m.sendFn(ctx, to, method, params)
	}
	return &mockMessage{body: json.RawMessage(`{"status":"ok"}`)}, nil
}
func (m *mockAgentCaller) SendAsync(ctx context.Context, to, method string, params any) (string, error) {
	if m.sendAsyncFn != nil {
		return m.sendAsyncFn(ctx, to, method, params)
	}
	return "00000000-0000-0000-0000-000000000000", nil
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
	if srv.Server == nil {
		t.Error("embedded Server should be set")
	}
	if srv.Inbox() == nil {
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

func TestHandleIncomingMessage(t *testing.T) {
	caller := newMockCaller()
	srv := NewMCPServer(caller, ServerConfig{}, testLogger())

	msgID := srv.HandleIncomingMessage("did:wba:example.com:agent:bob", "chat.send", json.RawMessage(`{"text":"hi"}`))
	if msgID == "" {
		t.Error("expected non-empty message ID")
	}

	// Verify it's in the inbox
	msg := srv.Inbox().Get(msgID)
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
