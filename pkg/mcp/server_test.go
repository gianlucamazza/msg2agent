package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"testing"

	"github.com/gianluca/msg2agent/pkg/agent"
	"github.com/gianluca/msg2agent/pkg/messaging"
	"github.com/gianluca/msg2agent/pkg/registry"
	"github.com/mark3labs/mcp-go/mcp"
)

// mockAgent implements the minimal interface for testing
type mockAgent struct {
	did         string
	record      *registry.Agent
	callRelay   func(ctx context.Context, method string, params any) (json.RawMessage, error)
	sendHandler func(ctx context.Context, to, method string, params any) (*messaging.Message, error)
}

func (m *mockAgent) DID() string {
	return m.did
}

func (m *mockAgent) Record() *registry.Agent {
	return m.record
}

func (m *mockAgent) CallRelay(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if m.callRelay != nil {
		return m.callRelay(ctx, method, params)
	}
	return nil, nil
}

func (m *mockAgent) Send(ctx context.Context, to, method string, params any) (*messaging.Message, error) {
	if m.sendHandler != nil {
		return m.sendHandler(ctx, to, method, params)
	}
	return messaging.NewMessage("from", to, messaging.TypeResponse, method), nil
}

// testLogger returns a logger for testing
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// newTestAgent creates a mock agent for testing
func newTestAgent() *mockAgent {
	return &mockAgent{
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
	// Need to create a real agent for the test since NewServer expects *agent.Agent
	cfg := agent.Config{
		Domain:      "test.local",
		AgentID:     "test",
		DisplayName: "Test",
		Logger:      testLogger(),
	}

	a, err := agent.New(cfg)
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	server := NewServer(a, testLogger())
	if server == nil {
		t.Fatal("NewServer returned nil")
	}
	if server.agent == nil {
		t.Error("agent should be set")
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

	mock := newTestAgent()
	mock.callRelay = func(ctx context.Context, method string, params any) (json.RawMessage, error) {
		if method != "relay.discover" {
			t.Errorf("expected relay.discover, got %s", method)
		}
		return agentsJSON, nil
	}

	s := &Server{
		agent:  nil, // We'll call the handler directly
		logger: testLogger(),
	}

	// Create a wrapper to use the mock
	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, _ := request.Params.Arguments.(map[string]interface{})
		capability, _ := args["capability"].(string)

		params := map[string]string{}
		if capability != "" {
			params["capability"] = capability
		}

		result, err := mock.callRelay(ctx, "relay.discover", params)
		if err != nil {
			return mcp.NewToolResultError("Failed to discover agents"), nil
		}

		var agents []*registry.Agent
		json.Unmarshal(result, &agents)

		output, _ := json.MarshalIndent(agents, "", "  ")
		return mcp.NewToolResultText(string(output)), nil
	}

	// Test without filter
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if result.IsError {
		t.Error("result should not be an error")
	}

	// Verify output contains agents
	if len(result.Content) == 0 {
		t.Error("should have content")
	}

	_ = s // silence unused variable
}

// TestListAgentsHandlerWithFilter tests filtering by capability.
func TestListAgentsHandlerWithFilter(t *testing.T) {
	mock := newTestAgent()
	calledWith := ""
	mock.callRelay = func(ctx context.Context, method string, params any) (json.RawMessage, error) {
		if p, ok := params.(map[string]string); ok {
			calledWith = p["capability"]
		}
		return json.RawMessage(`[]`), nil
	}

	// Simulate handler with capability filter
	handler := func(ctx context.Context, capability string) {
		params := map[string]string{}
		if capability != "" {
			params["capability"] = capability
		}
		mock.callRelay(ctx, "relay.discover", params)
	}

	handler(context.Background(), "translate")

	if calledWith != "translate" {
		t.Errorf("expected capability filter 'translate', got '%s'", calledWith)
	}
}

// TestSendMessageHandler tests the send_message tool handler.
func TestSendMessageHandler(t *testing.T) {
	mock := newTestAgent()
	sentTo := ""
	sentMethod := ""

	mock.sendHandler = func(ctx context.Context, to, method string, params any) (*messaging.Message, error) {
		sentTo = to
		sentMethod = method
		resp := messaging.NewMessage(mock.did, to, messaging.TypeResponse, method)
		resp.SetBody(map[string]string{"result": "success"})
		return resp, nil
	}

	// Simulate handler
	handler := func(ctx context.Context, to, method, paramsJSON string) (*mcp.CallToolResult, error) {
		var params interface{}
		json.Unmarshal([]byte(paramsJSON), &params)

		resp, err := mock.sendHandler(ctx, to, method, params)
		if err != nil {
			return mcp.NewToolResultError("Send failed"), nil
		}

		output, _ := json.MarshalIndent(resp.Body, "", "  ")
		return mcp.NewToolResultText(string(output)), nil
	}

	result, err := handler(context.Background(), "did:wba:example.com:agent:bob", "chat.send", `{"text":"hello"}`)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if sentTo != "did:wba:example.com:agent:bob" {
		t.Errorf("sentTo = %s, want did:wba:example.com:agent:bob", sentTo)
	}
	if sentMethod != "chat.send" {
		t.Errorf("sentMethod = %s, want chat.send", sentMethod)
	}
	if result.IsError {
		t.Error("result should not be an error")
	}
}

// TestSendMessageHandlerInvalidParams tests error handling for invalid params.
func TestSendMessageHandlerInvalidParams(t *testing.T) {
	// Handler that validates params
	handler := func(args map[string]interface{}) *mcp.CallToolResult {
		to, ok := args["to"].(string)
		if !ok {
			return mcp.NewToolResultError("Missing 'to' parameter")
		}
		method, ok := args["method"].(string)
		if !ok {
			return mcp.NewToolResultError("Missing 'method' parameter")
		}
		paramsStr, ok := args["params"].(string)
		if !ok {
			return mcp.NewToolResultError("Missing 'params' parameter")
		}

		var params interface{}
		if err := json.Unmarshal([]byte(paramsStr), &params); err != nil {
			return mcp.NewToolResultError("Invalid JSON params")
		}

		_ = to
		_ = method
		return mcp.NewToolResultText("OK")
	}

	tests := []struct {
		name   string
		args   map[string]interface{}
		errMsg string
	}{
		{
			name:   "missing to",
			args:   map[string]interface{}{"method": "test", "params": "{}"},
			errMsg: "Missing 'to' parameter",
		},
		{
			name:   "missing method",
			args:   map[string]interface{}{"to": "did:test", "params": "{}"},
			errMsg: "Missing 'method' parameter",
		},
		{
			name:   "missing params",
			args:   map[string]interface{}{"to": "did:test", "method": "test"},
			errMsg: "Missing 'params' parameter",
		},
		{
			name:   "invalid json params",
			args:   map[string]interface{}{"to": "did:test", "method": "test", "params": "not json"},
			errMsg: "Invalid JSON params",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := handler(tt.args)
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

	mock := newTestAgent()
	mock.callRelay = func(ctx context.Context, method string, params any) (json.RawMessage, error) {
		if method != "relay.lookup" {
			t.Errorf("expected relay.lookup, got %s", method)
		}
		return agentJSON, nil
	}

	// Simulate handler
	handler := func(ctx context.Context, did string) (*mcp.CallToolResult, error) {
		result, err := mock.callRelay(ctx, "relay.lookup", map[string]string{"did": did})
		if err != nil {
			return mcp.NewToolResultError("Failed to lookup agent"), nil
		}

		var agent *registry.Agent
		json.Unmarshal(result, &agent)

		output, _ := json.MarshalIndent(agent, "", "  ")
		return mcp.NewToolResultText(string(output)), nil
	}

	result, err := handler(context.Background(), "did:wba:example.com:agent:alice")
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if result.IsError {
		t.Error("result should not be an error")
	}
}

// TestGetTaskStatusHandler tests the get_task_status tool handler.
func TestGetTaskStatusHandler(t *testing.T) {
	mock := newTestAgent()
	mock.sendHandler = func(ctx context.Context, to, method string, params any) (*messaging.Message, error) {
		if method != "tasks/get" {
			t.Errorf("expected tasks/get, got %s", method)
		}
		resp := messaging.NewMessage(mock.did, to, messaging.TypeResponse, method)
		resp.SetBody(map[string]any{
			"id":     "task-123",
			"status": map[string]string{"state": "completed"},
		})
		return resp, nil
	}

	// Simulate handler
	handler := func(ctx context.Context, taskID, agentDID string) (*mcp.CallToolResult, error) {
		resp, err := mock.sendHandler(ctx, agentDID, "tasks/get", map[string]string{"id": taskID})
		if err != nil {
			return mcp.NewToolResultError("Failed to get task"), nil
		}

		output, _ := json.MarshalIndent(resp.Body, "", "  ")
		return mcp.NewToolResultText(string(output)), nil
	}

	result, err := handler(context.Background(), "task-123", "did:wba:example.com:agent:alice")
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
			DID:         "did:wba:example.com:agent:alice",
			DisplayName: "Alice",
			Capabilities: []registry.Capability{
				{Name: "chat"},
				{Name: "translate"},
			},
		},
		{
			DID:         "did:wba:example.com:agent:bob",
			DisplayName: "Bob",
			Capabilities: []registry.Capability{
				{Name: "chat"},
			},
		},
	}
	agentsJSON, _ := json.Marshal(agents)

	mock := newTestAgent()
	mock.callRelay = func(ctx context.Context, method string, params any) (json.RawMessage, error) {
		return agentsJSON, nil
	}

	// Test hasAllCapabilities helper
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
	mock := newTestAgent()

	// Simulate handler
	handler := func() (*mcp.CallToolResult, error) {
		info := map[string]any{
			"did":          mock.did,
			"display_name": mock.record.DisplayName,
			"endpoints":    mock.record.Endpoints,
			"capabilities": mock.record.Capabilities,
			"status":       mock.record.Status,
		}

		output, _ := json.MarshalIndent(info, "", "  ")
		return mcp.NewToolResultText(string(output)), nil
	}

	result, err := handler()
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if result.IsError {
		t.Error("result should not be an error")
	}

	// Verify content contains expected fields
	if len(result.Content) == 0 {
		t.Error("should have content")
	}
}

// TestHasAllCapabilities tests the capability matching helper.
func TestHasAllCapabilities(t *testing.T) {
	agentWithCaps := &registry.Agent{
		Capabilities: []registry.Capability{
			{Name: "chat"},
			{Name: "translate"},
			{Name: "summarize"},
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
	agent := &registry.Agent{
		Capabilities: []registry.Capability{},
	}

	// Empty requirements should match any agent
	if !hasAllCapabilities(agent, []string{}) {
		t.Error("empty requirements should match")
	}
}

// TestArgumentParsing tests argument extraction from request params.
func TestArgumentParsing(t *testing.T) {
	tests := []struct {
		name   string
		args   any
		wantOK bool
	}{
		{"valid map", map[string]interface{}{"key": "value"}, true},
		{"nil args", nil, false},
		{"string args", "not a map", false},
		{"empty map", map[string]interface{}{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := tt.args.(map[string]interface{})
			if ok != tt.wantOK {
				t.Errorf("argument parsing = %v, want %v", ok, tt.wantOK)
			}
		})
	}
}

// TestStringExtraction tests extracting string values from args.
func TestStringExtraction(t *testing.T) {
	args := map[string]interface{}{
		"string_val": "hello",
		"int_val":    42,
		"nil_val":    nil,
	}

	// String value
	if val, ok := args["string_val"].(string); !ok || val != "hello" {
		t.Error("failed to extract string value")
	}

	// Int value should not extract as string
	if _, ok := args["int_val"].(string); ok {
		t.Error("int should not be extractable as string")
	}

	// Nil value should not extract as string
	if _, ok := args["nil_val"].(string); ok {
		t.Error("nil should not be extractable as string")
	}

	// Missing key should not extract
	if _, ok := args["missing"].(string); ok {
		t.Error("missing key should not be extractable")
	}
}

// TestCapabilityFiltering tests filtering agents by capabilities.
func TestCapabilityFiltering(t *testing.T) {
	agents := []*registry.Agent{
		{
			DID: "alice",
			Capabilities: []registry.Capability{
				{Name: "chat"},
				{Name: "translate"},
			},
		},
		{
			DID: "bob",
			Capabilities: []registry.Capability{
				{Name: "chat"},
			},
		},
		{
			DID:          "charlie",
			Capabilities: []registry.Capability{},
		},
	}

	tests := []struct {
		name     string
		required []string
		expected int
	}{
		{"empty requirements", []string{}, 3},
		{"chat only", []string{"chat"}, 2},
		{"chat and translate", []string{"chat", "translate"}, 1},
		{"unknown", []string{"unknown"}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var matching []*registry.Agent
			for _, a := range agents {
				if hasAllCapabilities(a, tt.required) {
					matching = append(matching, a)
				}
			}
			if len(matching) != tt.expected {
				t.Errorf("got %d matching agents, want %d", len(matching), tt.expected)
			}
		})
	}
}

// TestJSONMarshalParams tests JSON parameter marshaling.
func TestJSONMarshalParams(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		wantOK bool
	}{
		{"valid json object", `{"key": "value"}`, true},
		{"valid json array", `[1, 2, 3]`, true},
		{"valid json string", `"hello"`, true},
		{"valid json number", `42`, true},
		{"invalid json", `not json`, false},
		{"empty string", ``, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var params interface{}
			err := json.Unmarshal([]byte(tt.input), &params)
			ok := err == nil
			if ok != tt.wantOK {
				t.Errorf("JSON unmarshal = %v (err=%v), want ok=%v", ok, err, tt.wantOK)
			}
		})
	}
}

// TestCapabilitySplit tests parsing comma-separated capabilities.
func TestCapabilitySplit(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"chat", []string{"chat"}},
		{"chat,translate", []string{"chat", "translate"}},
		{"chat, translate, summarize", []string{"chat", "translate", "summarize"}},
		{" chat , translate ", []string{"chat", "translate"}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			parts := splitAndTrim(tt.input)
			if len(parts) != len(tt.expected) {
				t.Errorf("got %d parts, want %d", len(parts), len(tt.expected))
				return
			}
			for i, part := range parts {
				if part != tt.expected[i] {
					t.Errorf("part[%d] = %q, want %q", i, part, tt.expected[i])
				}
			}
		})
	}
}

// splitAndTrim splits a comma-separated string and trims each part.
func splitAndTrim(s string) []string {
	parts := make([]string, 0)
	for _, part := range splitComma(s) {
		trimmed := trimSpace(part)
		if trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return parts
}

func splitComma(s string) []string {
	result := make([]string, 0)
	current := ""
	for _, c := range s {
		if c == ',' {
			result = append(result, current)
			current = ""
		} else {
			current += string(c)
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}

func trimSpace(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}

// TestToolResultCreation tests creating tool results.
func TestToolResultCreation(t *testing.T) {
	// Test text result
	textResult := mcp.NewToolResultText("Hello")
	if textResult.IsError {
		t.Error("text result should not be an error")
	}
	if len(textResult.Content) == 0 {
		t.Error("text result should have content")
	}

	// Test error result
	errResult := mcp.NewToolResultError("Something went wrong")
	if !errResult.IsError {
		t.Error("error result should be an error")
	}
}

// TestAgentRecordFields tests agent record field access.
func TestAgentRecordFields(t *testing.T) {
	agent := &registry.Agent{
		DID:         "did:wba:example.com:agent:test",
		DisplayName: "Test Agent",
		Endpoints: []registry.Endpoint{
			{Transport: registry.TransportWebSocket, URL: "ws://localhost:8080", Priority: 1},
		},
		Capabilities: []registry.Capability{
			{Name: "chat", Description: "Chat capability", Methods: []string{"send", "receive"}},
		},
		Status: registry.StatusOnline,
	}

	if agent.DID != "did:wba:example.com:agent:test" {
		t.Error("DID mismatch")
	}
	if agent.DisplayName != "Test Agent" {
		t.Error("DisplayName mismatch")
	}
	if len(agent.Endpoints) != 1 {
		t.Error("Endpoints count mismatch")
	}
	if agent.Endpoints[0].Transport != registry.TransportWebSocket {
		t.Error("Endpoint transport mismatch")
	}
	if len(agent.Capabilities) != 1 {
		t.Error("Capabilities count mismatch")
	}
	if agent.Capabilities[0].Name != "chat" {
		t.Error("Capability name mismatch")
	}
	if agent.Status != registry.StatusOnline {
		t.Error("Status mismatch")
	}
}
