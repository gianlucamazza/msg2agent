package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"testing"

	"github.com/gianluca/msg2agent/pkg/registry"
	"github.com/mark3labs/mcp-go/mcp"
)

// testLogger returns a logger for testing
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// newTestCaller creates a mock AgentCaller for testing
func newTestCaller() *mockCaller {
	return &mockCaller{
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

// TestNewServer tests server creation.
func TestNewServer(t *testing.T) {
	caller := newTestCaller()
	server := NewServer(caller, testLogger())
	if server == nil {
		t.Fatal("NewServer returned nil")
	}
	if server.caller == nil {
		t.Error("caller should be set")
	}
	if server.mcp == nil {
		t.Error("mcp server should be set")
	}
	if server.logger == nil {
		t.Error("logger should be set")
	}
}

// TestListAgentsHandler tests the list_agents tool handler.
func TestListAgentsHandler(t *testing.T) {
	agents := []*registry.Agent{
		{DID: "did:wba:example.com:agent:alice", DisplayName: "Alice"},
		{DID: "did:wba:example.com:agent:bob", DisplayName: "Bob"},
	}
	agentsJSON, _ := json.Marshal(agents)

	caller := newTestCaller()
	caller.callRelay = func(ctx context.Context, method string, params any) (json.RawMessage, error) {
		if method != "relay.discover" {
			t.Errorf("expected relay.discover, got %s", method)
		}
		return agentsJSON, nil
	}

	server := NewServer(caller, testLogger())

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{}

	result, err := server.listAgentsHandler(context.Background(), req)
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

// TestListAgentsHandlerWithFilter tests filtering by capability.
func TestListAgentsHandlerWithFilter(t *testing.T) {
	caller := newTestCaller()
	calledWith := ""
	caller.callRelay = func(ctx context.Context, method string, params any) (json.RawMessage, error) {
		if p, ok := params.(map[string]string); ok {
			calledWith = p["capability"]
		}
		return json.RawMessage(`[]`), nil
	}

	server := NewServer(caller, testLogger())

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{
		"capability": "translate",
	}

	server.listAgentsHandler(context.Background(), req)

	if calledWith != "translate" {
		t.Errorf("expected capability filter 'translate', got '%s'", calledWith)
	}
}

// TestSendMessageHandler tests the send_message tool handler.
func TestSendMessageHandler(t *testing.T) {
	caller := newTestCaller()
	sentTo := ""
	sentMethod := ""

	caller.sendFn = func(ctx context.Context, to, method string, params any) (AgentMessage, error) {
		sentTo = to
		sentMethod = method
		return &mockMsg{body: json.RawMessage(`{"result":"success"}`)}, nil
	}

	server := NewServer(caller, testLogger())

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{
		"to":     "did:wba:example.com:agent:bob",
		"method": "chat.send",
		"params": `{"text":"hello"}`,
	}

	result, err := server.sendMessageHandler(context.Background(), req)
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

// TestSendMessageHandlerInvalidParams tests error handling for invalid params.
func TestSendMessageHandlerInvalidParams(t *testing.T) {
	caller := newTestCaller()
	server := NewServer(caller, testLogger())

	tests := []struct {
		name string
		args map[string]interface{}
	}{
		{"missing to", map[string]interface{}{"method": "test", "params": "{}"}},
		{"missing method", map[string]interface{}{"to": "did:test", "params": "{}"}},
		{"missing params", map[string]interface{}{"to": "did:test", "method": "test"}},
		{"invalid json params", map[string]interface{}{"to": "did:test", "method": "test", "params": "not json"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := mcp.CallToolRequest{}
			req.Params.Arguments = tt.args

			result, err := server.sendMessageHandler(context.Background(), req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !result.IsError {
				t.Error("expected error result")
			}
		})
	}
}

// TestGetAgentInfoHandler tests the get_agent_info tool handler.
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

	caller := newTestCaller()
	caller.callRelay = func(ctx context.Context, method string, params any) (json.RawMessage, error) {
		if method != "relay.lookup" {
			t.Errorf("expected relay.lookup, got %s", method)
		}
		return agentJSON, nil
	}

	server := NewServer(caller, testLogger())

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{
		"did": "did:wba:example.com:agent:alice",
	}

	result, err := server.getAgentInfoHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result.IsError {
		t.Error("result should not be an error")
	}
}

// TestGetTaskStatusHandler tests the get_task_status tool handler.
func TestGetTaskStatusHandler(t *testing.T) {
	caller := newTestCaller()
	caller.sendFn = func(ctx context.Context, to, method string, params any) (AgentMessage, error) {
		if method != "tasks/get" {
			t.Errorf("expected tasks/get, got %s", method)
		}
		return &mockMsg{body: json.RawMessage(`{"id":"task-123","status":"completed"}`)}, nil
	}

	server := NewServer(caller, testLogger())

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{
		"task_id":   "task-123",
		"agent_did": "did:wba:example.com:agent:alice",
	}

	result, err := server.getTaskStatusHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result.IsError {
		t.Error("result should not be an error")
	}
}

// TestQueryCapabilitiesHandler tests the query_capabilities tool handler.
func TestQueryCapabilitiesHandler(t *testing.T) {
	agents := []*registry.Agent{
		{
			DID: "did:wba:example.com:agent:alice",
			Capabilities: []registry.Capability{
				{Name: "chat"}, {Name: "translate"},
			},
		},
		{
			DID:          "did:wba:example.com:agent:bob",
			Capabilities: []registry.Capability{{Name: "chat"}},
		},
	}

	tests := []struct {
		agent    *registry.Agent
		required []string
		expected bool
	}{
		{agents[0], []string{"chat"}, true},
		{agents[0], []string{"chat", "translate"}, true},
		{agents[0], []string{"unknown"}, false},
		{agents[1], []string{"chat"}, true},
		{agents[1], []string{"chat", "translate"}, false},
	}

	for _, tt := range tests {
		result := hasAllCapabilities(tt.agent, tt.required)
		if result != tt.expected {
			t.Errorf("hasAllCapabilities(%s, %v) = %v, want %v",
				tt.agent.DID, tt.required, result, tt.expected)
		}
	}
}

// TestGetSelfInfoHandler tests the get_self_info tool handler.
func TestGetSelfInfoHandler(t *testing.T) {
	caller := newTestCaller()
	server := NewServer(caller, testLogger())

	req := mcp.CallToolRequest{}
	result, err := server.getSelfInfoHandler(context.Background(), req)
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

// TestHasAllCapabilities tests the capability matching helper.
func TestHasAllCapabilities(t *testing.T) {
	agentWithCaps := &registry.Agent{
		Capabilities: []registry.Capability{
			{Name: "chat"}, {Name: "translate"}, {Name: "summarize"},
		},
	}

	tests := []struct {
		name     string
		required []string
		expected bool
	}{
		{"empty requirements", []string{}, true},
		{"single match", []string{"chat"}, true},
		{"multiple match", []string{"chat", "translate"}, true},
		{"all match", []string{"chat", "translate", "summarize"}, true},
		{"unknown", []string{"unknown"}, false},
		{"partial unknown", []string{"chat", "unknown"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasAllCapabilities(agentWithCaps, tt.required)
			if result != tt.expected {
				t.Errorf("hasAllCapabilities(%v) = %v, want %v", tt.required, result, tt.expected)
			}
		})
	}
}

// TestHasAllCapabilitiesEmpty tests with empty requirements.
func TestHasAllCapabilitiesEmpty(t *testing.T) {
	agent := &registry.Agent{Capabilities: []registry.Capability{}}
	if !hasAllCapabilities(agent, []string{}) {
		t.Error("empty requirements should match")
	}
}

// TestToolResultCreation tests creating tool results.
func TestToolResultCreation(t *testing.T) {
	textResult := mcp.NewToolResultText("Hello")
	if textResult.IsError {
		t.Error("text result should not be an error")
	}
	if len(textResult.Content) == 0 {
		t.Error("text result should have content")
	}

	errResult := mcp.NewToolResultError("Something went wrong")
	if !errResult.IsError {
		t.Error("error result should be an error")
	}
}
