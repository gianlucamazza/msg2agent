package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"testing"

	"github.com/gianluca/msg2agent/pkg/registry"
	"github.com/mark3labs/mcp-go/mcp"
)

// mockCaller implements AgentCaller for handler tests.
type mockCaller struct {
	did         string
	record      *registry.Agent
	sendFn      func(ctx context.Context, to, method string, params any) (AgentMessage, error)
	sendAsyncFn func(ctx context.Context, to, method string, params any) (string, error)
	callRelay   func(ctx context.Context, method string, params any) (json.RawMessage, error)
}

func (m *mockCaller) DID() string             { return m.did }
func (m *mockCaller) Record() *registry.Agent { return m.record }
func (m *mockCaller) Send(ctx context.Context, to, method string, params any) (AgentMessage, error) {
	if m.sendFn != nil {
		return m.sendFn(ctx, to, method, params)
	}
	return &mockMsg{body: json.RawMessage(`{"status":"ok"}`)}, nil
}
func (m *mockCaller) SendAsync(ctx context.Context, to, method string, params any) (string, error) {
	if m.sendAsyncFn != nil {
		return m.sendAsyncFn(ctx, to, method, params)
	}
	return "00000000-0000-0000-0000-000000000000", nil
}
func (m *mockCaller) CallRelay(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if m.callRelay != nil {
		return m.callRelay(ctx, method, params)
	}
	return json.RawMessage(`[]`), nil
}

type mockMsg struct {
	isError bool
	body    json.RawMessage
}

func (m *mockMsg) IsError() bool            { return m.isError }
func (m *mockMsg) RawBody() json.RawMessage { return m.body }

func newHandlerTestCaller() *mockCaller {
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

func handlerTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestSendMessageHandlerValidation(t *testing.T) {
	caller := newHandlerTestCaller()
	server := NewServer(caller, handlerTestLogger())

	tests := []struct {
		name    string
		args    interface{}
		wantErr bool
	}{
		{
			name:    "invalid args format",
			args:    nil,
			wantErr: true,
		},
		{
			name:    "missing to",
			args:    map[string]interface{}{"method": "test", "params": "{}"},
			wantErr: true,
		},
		{
			name:    "missing method",
			args:    map[string]interface{}{"to": "did:test", "params": "{}"},
			wantErr: true,
		},
		{
			name:    "missing params",
			args:    map[string]interface{}{"to": "did:test", "method": "test"},
			wantErr: true,
		},
		{
			name:    "invalid json params",
			args:    map[string]interface{}{"to": "did:test", "method": "test", "params": "not json"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := mcp.CallToolRequest{}
			req.Params.Arguments = tt.args

			result, err := server.sendMessageHandler(context.Background(), req)
			if err != nil {
				t.Fatalf("handler returned error: %v", err)
			}
			if tt.wantErr && !result.IsError {
				t.Error("expected error result")
			}
		})
	}
}

func TestGetAgentInfoHandlerValidation(t *testing.T) {
	caller := newHandlerTestCaller()
	server := NewServer(caller, handlerTestLogger())

	tests := []struct {
		name    string
		args    interface{}
		wantErr bool
	}{
		{
			name:    "invalid args format",
			args:    "not a map",
			wantErr: true,
		},
		{
			name:    "missing did",
			args:    map[string]interface{}{},
			wantErr: true,
		},
		{
			name:    "empty did",
			args:    map[string]interface{}{"did": ""},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := mcp.CallToolRequest{}
			req.Params.Arguments = tt.args

			result, err := server.getAgentInfoHandler(context.Background(), req)
			if err != nil {
				t.Fatalf("handler returned error: %v", err)
			}
			if tt.wantErr && !result.IsError {
				t.Error("expected error result")
			}
		})
	}
}

func TestGetTaskStatusHandlerValidation(t *testing.T) {
	caller := newHandlerTestCaller()
	server := NewServer(caller, handlerTestLogger())

	tests := []struct {
		name    string
		args    interface{}
		wantErr bool
	}{
		{
			name:    "invalid args format",
			args:    "not a map",
			wantErr: true,
		},
		{
			name:    "missing task_id",
			args:    map[string]interface{}{"agent_did": "did:test"},
			wantErr: true,
		},
		{
			name:    "empty task_id",
			args:    map[string]interface{}{"task_id": "", "agent_did": "did:test"},
			wantErr: true,
		},
		{
			name:    "missing agent_did",
			args:    map[string]interface{}{"task_id": "task-123"},
			wantErr: true,
		},
		{
			name:    "empty agent_did",
			args:    map[string]interface{}{"task_id": "task-123", "agent_did": ""},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := mcp.CallToolRequest{}
			req.Params.Arguments = tt.args

			result, err := server.getTaskStatusHandler(context.Background(), req)
			if err != nil {
				t.Fatalf("handler returned error: %v", err)
			}
			if tt.wantErr && !result.IsError {
				t.Error("expected error result")
			}
		})
	}
}

func TestQueryCapabilitiesHandlerValidation(t *testing.T) {
	caller := newHandlerTestCaller()
	server := NewServer(caller, handlerTestLogger())

	tests := []struct {
		name    string
		args    interface{}
		wantErr bool
	}{
		{
			name:    "invalid args format",
			args:    "not a map",
			wantErr: true,
		},
		{
			name:    "missing capabilities",
			args:    map[string]interface{}{},
			wantErr: true,
		},
		{
			name:    "empty capabilities",
			args:    map[string]interface{}{"capabilities": ""},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := mcp.CallToolRequest{}
			req.Params.Arguments = tt.args

			result, err := server.queryCapabilitiesHandler(context.Background(), req)
			if err != nil {
				t.Fatalf("handler returned error: %v", err)
			}
			if tt.wantErr && !result.IsError {
				t.Error("expected error result")
			}
		})
	}
}

func TestGetSelfInfoHandlerDirect(t *testing.T) {
	caller := newHandlerTestCaller()
	server := NewServer(caller, handlerTestLogger())

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{}

	result, err := server.getSelfInfoHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if result.IsError {
		t.Error("result should not be an error")
	}
	if len(result.Content) == 0 {
		t.Fatal("should have content")
	}

	textContent, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}

	var info map[string]interface{}
	if err := json.Unmarshal([]byte(textContent.Text), &info); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if info["did"] == nil || info["did"] == "" {
		t.Error("did should be set")
	}
	if info["display_name"] != "Test Agent" {
		t.Errorf("display_name = %v, want 'Test Agent'", info["display_name"])
	}
}

func TestListAgentsHandlerWithMock(t *testing.T) {
	agents := []*registry.Agent{
		{DID: "did:wba:example.com:agent:alice", DisplayName: "Alice"},
		{DID: "did:wba:example.com:agent:bob", DisplayName: "Bob"},
	}
	agentsJSON, _ := json.Marshal(agents)

	caller := newHandlerTestCaller()
	caller.callRelay = func(ctx context.Context, method string, params any) (json.RawMessage, error) {
		if method != "relay.discover" {
			t.Errorf("expected relay.discover, got %s", method)
		}
		return agentsJSON, nil
	}

	server := NewServer(caller, handlerTestLogger())

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{}

	result, err := server.listAgentsHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if result.IsError {
		t.Error("result should not be an error")
	}
	if len(result.Content) == 0 {
		t.Error("should have content")
	}
}

func TestListAgentsHandlerRelayError(t *testing.T) {
	caller := newHandlerTestCaller()
	caller.callRelay = func(ctx context.Context, method string, params any) (json.RawMessage, error) {
		return nil, errors.New("relay unavailable")
	}

	server := NewServer(caller, handlerTestLogger())

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{}

	result, err := server.listAgentsHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error result when relay fails")
	}
}

func TestSendMessageHandlerWithValidParams(t *testing.T) {
	caller := newHandlerTestCaller()
	var sentTo, sentMethod string
	caller.sendAsyncFn = func(ctx context.Context, to, method string, params any) (string, error) {
		sentTo = to
		sentMethod = method
		return "22222222-2222-2222-2222-222222222222", nil
	}

	server := NewServer(caller, handlerTestLogger())

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"to":     "did:wba:example.com:agent:bob",
		"method": "chat.send",
		"params": `{"message": "hello"}`,
	}

	result, err := server.sendMessageHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
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

func TestSendMessageHandlerErrorResponse(t *testing.T) {
	caller := newHandlerTestCaller()
	caller.sendAsyncFn = func(ctx context.Context, to, method string, params any) (string, error) {
		return "", errors.New("send failed: connection refused")
	}

	server := NewServer(caller, handlerTestLogger())

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"to":     "did:wba:example.com:agent:bob",
		"method": "error.test",
		"params": `{"trigger": "error"}`,
	}

	result, err := server.sendMessageHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error result when send fails")
	}
}

func TestGetAgentInfoHandlerWithDID(t *testing.T) {
	caller := newHandlerTestCaller()
	caller.callRelay = func(ctx context.Context, method string, params any) (json.RawMessage, error) {
		if method == "relay.lookup" {
			agent := &registry.Agent{
				DID:         "did:wba:example.com:agent:alice",
				DisplayName: "Alice",
				Endpoints: []registry.Endpoint{
					{Transport: registry.TransportWebSocket, URL: "ws://alice.example.com"},
				},
			}
			data, _ := json.Marshal(agent)
			return data, nil
		}
		return json.RawMessage(`[]`), nil
	}

	server := NewServer(caller, handlerTestLogger())

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{
		"did": "did:wba:example.com:agent:alice",
	}

	result, err := server.getAgentInfoHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if result.IsError {
		t.Error("result should not be an error")
	}
}

func TestGetTaskStatusHandlerWithParams(t *testing.T) {
	caller := newHandlerTestCaller()
	caller.sendFn = func(ctx context.Context, to, method string, params any) (AgentMessage, error) {
		return &mockMsg{body: json.RawMessage(`{"id":"task-123","status":"completed"}`)}, nil
	}

	server := NewServer(caller, handlerTestLogger())

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{
		"task_id":   "task-123",
		"agent_did": "did:wba:example.com:agent:alice",
	}

	result, err := server.getTaskStatusHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if result.IsError {
		t.Error("result should not be an error")
	}
}

func TestQueryCapabilitiesHandlerWithCaps(t *testing.T) {
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
	agentsJSON, _ := json.Marshal(agents)

	caller := newHandlerTestCaller()
	caller.callRelay = func(ctx context.Context, method string, params any) (json.RawMessage, error) {
		return agentsJSON, nil
	}

	server := NewServer(caller, handlerTestLogger())

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{
		"capabilities": "chat,translate",
	}

	result, err := server.queryCapabilitiesHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if result.IsError {
		t.Error("result should not be an error")
	}

	// Parse result and check only alice matches
	textContent := result.Content[0].(mcp.TextContent)
	var matching []*registry.Agent
	json.Unmarshal([]byte(textContent.Text), &matching)
	if len(matching) != 1 {
		t.Errorf("expected 1 matching agent, got %d", len(matching))
	}
}

func TestSubmitTaskHandlerValidation(t *testing.T) {
	caller := newHandlerTestCaller()
	server := NewServer(caller, handlerTestLogger())

	tests := []struct {
		name    string
		args    interface{}
		wantErr bool
	}{
		{
			name:    "invalid args format",
			args:    "not a map",
			wantErr: true,
		},
		{
			name:    "missing agent_did",
			args:    map[string]interface{}{"message": `{"text":"hello"}`},
			wantErr: true,
		},
		{
			name:    "missing message",
			args:    map[string]interface{}{"agent_did": "did:wba:example.com:agent:bob"},
			wantErr: true,
		},
		{
			name:    "invalid json message",
			args:    map[string]interface{}{"agent_did": "did:wba:example.com:agent:bob", "message": "not json"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := mcp.CallToolRequest{}
			req.Params.Arguments = tt.args

			result, err := server.submitTaskHandler(context.Background(), req)
			if err != nil {
				t.Fatalf("handler returned error: %v", err)
			}
			if tt.wantErr && !result.IsError {
				t.Error("expected error result")
			}
		})
	}
}

func TestCancelTaskHandlerValidation(t *testing.T) {
	caller := newHandlerTestCaller()
	server := NewServer(caller, handlerTestLogger())

	tests := []struct {
		name    string
		args    interface{}
		wantErr bool
	}{
		{
			name:    "invalid args format",
			args:    "not a map",
			wantErr: true,
		},
		{
			name:    "missing task_id",
			args:    map[string]interface{}{"agent_did": "did:wba:example.com:agent:bob"},
			wantErr: true,
		},
		{
			name:    "missing agent_did",
			args:    map[string]interface{}{"task_id": "task-123"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := mcp.CallToolRequest{}
			req.Params.Arguments = tt.args

			result, err := server.cancelTaskHandler(context.Background(), req)
			if err != nil {
				t.Fatalf("handler returned error: %v", err)
			}
			if tt.wantErr && !result.IsError {
				t.Error("expected error result")
			}
		})
	}
}

func TestSendTaskInputHandlerValidation(t *testing.T) {
	caller := newHandlerTestCaller()
	server := NewServer(caller, handlerTestLogger())

	tests := []struct {
		name    string
		args    interface{}
		wantErr bool
	}{
		{
			name:    "invalid args format",
			args:    "not a map",
			wantErr: true,
		},
		{
			name:    "missing task_id",
			args:    map[string]interface{}{"agent_did": "did:wba:example.com:agent:bob", "message": `{"text":"yes"}`},
			wantErr: true,
		},
		{
			name:    "missing agent_did",
			args:    map[string]interface{}{"task_id": "task-123", "message": `{"text":"yes"}`},
			wantErr: true,
		},
		{
			name:    "missing message",
			args:    map[string]interface{}{"task_id": "task-123", "agent_did": "did:wba:example.com:agent:bob"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := mcp.CallToolRequest{}
			req.Params.Arguments = tt.args

			result, err := server.sendTaskInputHandler(context.Background(), req)
			if err != nil {
				t.Fatalf("handler returned error: %v", err)
			}
			if tt.wantErr && !result.IsError {
				t.Error("expected error result")
			}
		})
	}
}

func TestListTasksHandler(t *testing.T) {
	caller := newHandlerTestCaller()
	server := NewServer(caller, handlerTestLogger())

	server.trackTask("task-1", "did:wba:example.com:agent:alice")
	server.trackTask("task-2", "did:wba:example.com:agent:bob")

	req := mcp.CallToolRequest{}
	result, err := server.listTasksHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if result.IsError {
		t.Error("should not be an error")
	}

	textContent, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}

	var tasks []*TrackedTask
	if err := json.Unmarshal([]byte(textContent.Text), &tasks); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if len(tasks) != 2 {
		t.Errorf("expected 2 tasks, got %d", len(tasks))
	}
}

func TestTaskTracking(t *testing.T) {
	caller := newHandlerTestCaller()
	server := NewServer(caller, handlerTestLogger())

	server.trackTask("task-1", "did:wba:example.com:agent:alice")
	if len(server.getTrackedTasks()) != 1 {
		t.Errorf("expected 1 task")
	}

	server.updateTaskStatus("task-1", "working")
	server.tasksMu.RLock()
	task := server.tasks["task-1"]
	server.tasksMu.RUnlock()
	if task.Status != "working" {
		t.Errorf("expected status 'working', got '%s'", task.Status)
	}

	server.untrackTask("task-1")
	if len(server.getTrackedTasks()) != 0 {
		t.Errorf("expected 0 tasks after untrack")
	}
}

func TestInbox(t *testing.T) {
	inbox := NewInbox(10)

	id1 := inbox.Add("did:wba:example.com:agent:alice", "chat.message", json.RawMessage(`{"text":"hello"}`))
	id2 := inbox.Add("did:wba:example.com:agent:bob", "chat.message", json.RawMessage(`{"text":"hi"}`))

	if inbox.Count(false) != 2 {
		t.Errorf("expected 2 messages, got %d", inbox.Count(false))
	}

	msg := inbox.Get(id1)
	if msg == nil {
		t.Fatal("should find message by id")
	}
	if msg.From != "did:wba:example.com:agent:alice" {
		t.Errorf("expected from alice, got %s", msg.From)
	}

	unread := inbox.List(true)
	if len(unread) != 2 {
		t.Errorf("expected 2 unread, got %d", len(unread))
	}

	inbox.MarkRead(id1)
	if inbox.Count(true) != 1 {
		t.Errorf("expected 1 unread after marking read, got %d", inbox.Count(true))
	}

	inbox.Delete(id2)
	if inbox.Count(false) != 1 {
		t.Errorf("expected 1 message after delete, got %d", inbox.Count(false))
	}
}

func TestInboxMaxSize(t *testing.T) {
	inbox := NewInbox(3)
	for i := 0; i < 5; i++ {
		inbox.Add("sender", "method", json.RawMessage(`{}`))
	}
	if inbox.Count(false) != 3 {
		t.Errorf("expected 3 messages (max size), got %d", inbox.Count(false))
	}
}

func TestServerInbox(t *testing.T) {
	caller := newHandlerTestCaller()
	server := NewServer(caller, handlerTestLogger())

	inbox := server.Inbox()
	if inbox == nil {
		t.Fatal("inbox should not be nil")
	}

	msgID := server.HandleIncomingMessage("did:wba:example.com:agent:alice", "chat.message", json.RawMessage(`{"text":"hello"}`))
	if msgID == "" {
		t.Error("should return message ID")
	}

	msg := inbox.Get(msgID)
	if msg == nil {
		t.Fatal("should find message in inbox")
	}
}

func TestInboxResourceHandler(t *testing.T) {
	caller := newHandlerTestCaller()
	server := NewServer(caller, handlerTestLogger())

	server.inbox.Add("did:wba:example.com:agent:alice", "chat.message", json.RawMessage(`{"text":"hello"}`))

	req := mcp.ReadResourceRequest{}
	req.Params.URI = "msg2agent://inbox"

	contents, err := server.inboxResourceHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if len(contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(contents))
	}

	textContent, ok := contents[0].(mcp.TextResourceContents)
	if !ok {
		t.Fatal("expected TextResourceContents")
	}

	var messages []*InboxMessage
	if err := json.Unmarshal([]byte(textContent.Text), &messages); err != nil {
		t.Fatalf("failed to parse content: %v", err)
	}
	if len(messages) != 1 {
		t.Errorf("expected 1 message, got %d", len(messages))
	}
}

func TestTasksResourceHandler(t *testing.T) {
	caller := newHandlerTestCaller()
	server := NewServer(caller, handlerTestLogger())

	server.trackTask("task-1", "did:wba:example.com:agent:alice")

	req := mcp.ReadResourceRequest{}
	req.Params.URI = "msg2agent://tasks"

	contents, err := server.tasksResourceHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if len(contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(contents))
	}

	textContent, ok := contents[0].(mcp.TextResourceContents)
	if !ok {
		t.Fatal("expected TextResourceContents")
	}

	var tasks []*TrackedTask
	if err := json.Unmarshal([]byte(textContent.Text), &tasks); err != nil {
		t.Fatalf("failed to parse content: %v", err)
	}
	if len(tasks) != 1 {
		t.Errorf("expected 1 task, got %d", len(tasks))
	}
}

func TestInboxMessageResourceHandler(t *testing.T) {
	caller := newHandlerTestCaller()
	server := NewServer(caller, handlerTestLogger())

	msgID := server.inbox.Add("did:wba:example.com:agent:alice", "chat.message", json.RawMessage(`{"text":"hello"}`))

	req := mcp.ReadResourceRequest{}
	req.Params.URI = "msg2agent://inbox/" + msgID

	contents, err := server.inboxMessageResourceHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if len(contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(contents))
	}

	// Message should be marked as read
	msg := server.inbox.Get(msgID)
	if !msg.Read {
		t.Error("message should be marked as read")
	}

	// Test not found
	req.Params.URI = "msg2agent://inbox/nonexistent"
	_, err = server.inboxMessageResourceHandler(context.Background(), req)
	if err == nil {
		t.Error("expected error for nonexistent message")
	}
}

// --- Inbox tool handler tests ---

func TestListMessagesHandler(t *testing.T) {
	caller := newHandlerTestCaller()
	server := NewServer(caller, handlerTestLogger())

	// Add messages
	server.inbox.Add("did:wba:example.com:agent:alice", "chat.message", json.RawMessage(`{"text":"hello"}`))
	id2 := server.inbox.Add("did:wba:example.com:agent:bob", "chat.message", json.RawMessage(`{"text":"hi"}`))
	server.inbox.MarkRead(id2)

	t.Run("all messages", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{}

		result, err := server.listMessagesHandler(context.Background(), req)
		if err != nil {
			t.Fatalf("handler returned error: %v", err)
		}
		if result.IsError {
			t.Error("result should not be an error")
		}

		textContent := result.Content[0].(mcp.TextContent)
		var messages []*InboxMessage
		json.Unmarshal([]byte(textContent.Text), &messages)
		if len(messages) != 2 {
			t.Errorf("expected 2 messages, got %d", len(messages))
		}
	})

	t.Run("unread only", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{"unread_only": true}

		result, err := server.listMessagesHandler(context.Background(), req)
		if err != nil {
			t.Fatalf("handler returned error: %v", err)
		}

		textContent := result.Content[0].(mcp.TextContent)
		var messages []*InboxMessage
		json.Unmarshal([]byte(textContent.Text), &messages)
		if len(messages) != 1 {
			t.Errorf("expected 1 unread message, got %d", len(messages))
		}
	})
}

func TestReadMessageHandler(t *testing.T) {
	caller := newHandlerTestCaller()
	server := NewServer(caller, handlerTestLogger())

	msgID := server.inbox.Add("did:wba:example.com:agent:alice", "chat.message", json.RawMessage(`{"text":"hello"}`))

	t.Run("found", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{"id": msgID}

		result, err := server.readMessageHandler(context.Background(), req)
		if err != nil {
			t.Fatalf("handler returned error: %v", err)
		}
		if result.IsError {
			t.Error("result should not be an error")
		}

		// Verify marked as read
		msg := server.inbox.Get(msgID)
		if !msg.Read {
			t.Error("message should be marked as read")
		}
	})

	t.Run("not found", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{"id": "nonexistent"}

		result, err := server.readMessageHandler(context.Background(), req)
		if err != nil {
			t.Fatalf("handler returned error: %v", err)
		}
		if !result.IsError {
			t.Error("expected error for nonexistent message")
		}
	})
}

func TestDeleteMessageHandler(t *testing.T) {
	caller := newHandlerTestCaller()
	server := NewServer(caller, handlerTestLogger())

	msgID := server.inbox.Add("did:wba:example.com:agent:alice", "chat.message", json.RawMessage(`{"text":"hello"}`))

	t.Run("found", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{"id": msgID}

		result, err := server.deleteMessageHandler(context.Background(), req)
		if err != nil {
			t.Fatalf("handler returned error: %v", err)
		}
		if result.IsError {
			t.Error("result should not be an error")
		}

		// Verify deleted
		if server.inbox.Get(msgID) != nil {
			t.Error("message should be deleted")
		}
	})

	t.Run("not found", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{"id": "nonexistent"}

		result, err := server.deleteMessageHandler(context.Background(), req)
		if err != nil {
			t.Fatalf("handler returned error: %v", err)
		}
		if !result.IsError {
			t.Error("expected error for nonexistent message")
		}
	})
}

func TestMessageCountHandler(t *testing.T) {
	caller := newHandlerTestCaller()
	server := NewServer(caller, handlerTestLogger())

	id1 := server.inbox.Add("did:wba:example.com:agent:alice", "chat.message", json.RawMessage(`{"text":"hello"}`))
	server.inbox.Add("did:wba:example.com:agent:bob", "chat.message", json.RawMessage(`{"text":"hi"}`))
	server.inbox.MarkRead(id1)

	t.Run("all counts", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{}

		result, err := server.messageCountHandler(context.Background(), req)
		if err != nil {
			t.Fatalf("handler returned error: %v", err)
		}
		if result.IsError {
			t.Error("result should not be an error")
		}

		textContent := result.Content[0].(mcp.TextContent)
		var counts map[string]int
		json.Unmarshal([]byte(textContent.Text), &counts)
		if counts["total"] != 2 {
			t.Errorf("expected total=2, got %d", counts["total"])
		}
		if counts["unread"] != 1 {
			t.Errorf("expected unread=1, got %d", counts["unread"])
		}
	})

	t.Run("unread only", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{"unread_only": true}

		result, err := server.messageCountHandler(context.Background(), req)
		if err != nil {
			t.Fatalf("handler returned error: %v", err)
		}

		textContent := result.Content[0].(mcp.TextContent)
		var counts map[string]int
		json.Unmarshal([]byte(textContent.Text), &counts)
		if counts["count"] != 1 {
			t.Errorf("expected count=1, got %d", counts["count"])
		}
	})
}

func TestExtractIDFromURI(t *testing.T) {
	tests := []struct {
		uri    string
		prefix string
		want   string
	}{
		{"msg2agent://inbox/abc123", "msg2agent://inbox/", "abc123"},
		{"msg2agent://tasks/task-1", "msg2agent://tasks/", "task-1"},
		{"msg2agent://inbox/", "msg2agent://inbox/", ""},
		{"short", "msg2agent://inbox/", ""},
	}

	for _, tt := range tests {
		got := extractIDFromURI(tt.uri, tt.prefix)
		if got != tt.want {
			t.Errorf("extractIDFromURI(%q, %q) = %q, want %q", tt.uri, tt.prefix, got, tt.want)
		}
	}
}
