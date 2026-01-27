package a2a

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gianluca/msg2agent/pkg/conversation"
	"github.com/gianluca/msg2agent/pkg/messaging"
	"github.com/gianluca/msg2agent/pkg/registry"
)

// TestNewAgentBridge tests bridge creation.
func TestNewAgentBridge(t *testing.T) {
	store := registry.NewMemoryStore()
	bridge := NewAgentBridge(store)

	if bridge == nil {
		t.Fatal("NewAgentBridge returned nil")
	}
	if bridge.methods == nil {
		t.Error("methods map should be initialized")
	}
	if bridge.adapter == nil {
		t.Error("adapter should be initialized")
	}
}

// TestAgentBridgeRegisterMethod tests method registration.
func TestAgentBridgeRegisterMethod(t *testing.T) {
	store := registry.NewMemoryStore()
	bridge := NewAgentBridge(store)

	called := false
	bridge.RegisterMethod("test", func(ctx context.Context, params json.RawMessage) (any, error) {
		called = true
		return "result", nil
	})

	handler, exists := bridge.methods["test"]
	if !exists {
		t.Fatal("method should be registered")
	}

	_, err := handler(context.Background(), nil)
	if err != nil {
		t.Errorf("handler error: %v", err)
	}
	if !called {
		t.Error("handler should have been called")
	}
}

// TestAgentBridgeTaskHandler tests task handler creation.
func TestAgentBridgeTaskHandler(t *testing.T) {
	store := registry.NewMemoryStore()
	bridge := NewAgentBridge(store)

	bridge.RegisterMethod("default", func(ctx context.Context, params json.RawMessage) (any, error) {
		return "Hello from handler!", nil
	})

	taskHandler := bridge.TaskHandler()
	if taskHandler == nil {
		t.Fatal("TaskHandler returned nil")
	}

	task := &Task{
		ID:       "test-task",
		Metadata: make(map[string]any),
	}
	msg := &Message{
		Role:  "user",
		Parts: []Part{{Type: "text", Text: "Hello"}},
	}

	response, _, err := taskHandler(context.Background(), task, msg)
	if err != nil {
		t.Fatalf("TaskHandler error: %v", err)
	}

	if response == nil {
		t.Fatal("response should not be nil")
	}
	if response.Role != "agent" {
		t.Errorf("response role = %q, want %q", response.Role, "agent")
	}

	text := ExtractTextContent(response)
	if text != "Hello from handler!" {
		t.Errorf("response text = %q, want %q", text, "Hello from handler!")
	}
}

// TestAgentBridgeTaskHandlerMethodSelection tests method selection from metadata.
func TestAgentBridgeTaskHandlerMethodSelection(t *testing.T) {
	store := registry.NewMemoryStore()
	bridge := NewAgentBridge(store)

	bridge.RegisterMethod("custom", func(ctx context.Context, params json.RawMessage) (any, error) {
		return "Custom method called", nil
	})
	bridge.RegisterMethod("default", func(ctx context.Context, params json.RawMessage) (any, error) {
		return "Default method called", nil
	})

	taskHandler := bridge.TaskHandler()

	// Test with custom method
	task := &Task{
		ID:       "test-task",
		Metadata: map[string]any{"method": "custom"},
	}
	msg := &Message{Role: "user", Parts: []Part{{Type: "text", Text: "Hi"}}}

	response, _, _ := taskHandler(context.Background(), task, msg)
	text := ExtractTextContent(response)
	if text != "Custom method called" {
		t.Errorf("expected custom method, got: %s", text)
	}
}

// TestAgentBridgeTaskHandlerNoHandler tests error when no handler exists.
func TestAgentBridgeTaskHandlerNoHandler(t *testing.T) {
	store := registry.NewMemoryStore()
	bridge := NewAgentBridge(store)

	taskHandler := bridge.TaskHandler()

	task := &Task{
		ID:       "test-task",
		Metadata: map[string]any{"method": "unknown"},
	}
	msg := &Message{Role: "user", Parts: []Part{{Type: "text", Text: "Hi"}}}

	_, _, err := taskHandler(context.Background(), task, msg)
	if err == nil {
		t.Error("expected error for unknown method")
	}
}

// TestResultToMessageString tests converting string result.
func TestResultToMessageString(t *testing.T) {
	store := registry.NewMemoryStore()
	bridge := NewAgentBridge(store)

	msg, artifacts, err := bridge.resultToMessage("Hello")
	if err != nil {
		t.Fatalf("resultToMessage error: %v", err)
	}
	if artifacts != nil {
		t.Error("artifacts should be nil for string result")
	}
	if msg.Role != "agent" {
		t.Errorf("role = %q, want %q", msg.Role, "agent")
	}
	if ExtractTextContent(msg) != "Hello" {
		t.Errorf("text = %q, want %q", ExtractTextContent(msg), "Hello")
	}
}

// TestResultToMessageMessage tests converting Message result.
func TestResultToMessageMessage(t *testing.T) {
	store := registry.NewMemoryStore()
	bridge := NewAgentBridge(store)

	input := &Message{Role: "custom", Parts: []Part{{Type: "text", Text: "Custom"}}}
	msg, _, err := bridge.resultToMessage(input)
	if err != nil {
		t.Fatalf("resultToMessage error: %v", err)
	}
	if msg.Role != "custom" {
		t.Errorf("role = %q, want %q", msg.Role, "custom")
	}
}

// TestResultToMessageMap tests converting map result.
func TestResultToMessageMap(t *testing.T) {
	store := registry.NewMemoryStore()
	bridge := NewAgentBridge(store)

	input := map[string]any{"key": "value", "number": 42}
	msg, _, err := bridge.resultToMessage(input)
	if err != nil {
		t.Fatalf("resultToMessage error: %v", err)
	}
	if msg.Role != "agent" {
		t.Errorf("role = %q, want %q", msg.Role, "agent")
	}
	// Text should be JSON
	text := ExtractTextContent(msg)
	if text == "" {
		t.Error("text should not be empty")
	}
}

// TestCreateA2AServer tests server creation from bridge.
func TestCreateA2AServer(t *testing.T) {
	store := registry.NewMemoryStore()
	bridge := NewAgentBridge(store)

	bridge.RegisterMethod("default", func(ctx context.Context, params json.RawMessage) (any, error) {
		return "OK", nil
	})

	server := bridge.CreateA2AServer()
	if server == nil {
		t.Fatal("CreateA2AServer returned nil")
	}
}

// TestTextResponse tests text response helper.
func TestTextResponse(t *testing.T) {
	msg := TextResponse("Hello, world!")

	if msg.Role != "agent" {
		t.Errorf("role = %q, want %q", msg.Role, "agent")
	}
	if len(msg.Parts) != 1 {
		t.Fatalf("parts count = %d, want 1", len(msg.Parts))
	}
	if msg.Parts[0].Type != "text" {
		t.Errorf("part type = %q, want %q", msg.Parts[0].Type, "text")
	}
	if msg.Parts[0].Text != "Hello, world!" {
		t.Errorf("text = %q, want %q", msg.Parts[0].Text, "Hello, world!")
	}
}

// TestTextArtifact tests text artifact helper.
func TestTextArtifact(t *testing.T) {
	artifact := TextArtifact("output.txt", "File content")

	if artifact.Name != "output.txt" {
		t.Errorf("name = %q, want %q", artifact.Name, "output.txt")
	}
	if len(artifact.Parts) != 1 {
		t.Fatalf("parts count = %d, want 1", len(artifact.Parts))
	}
	if artifact.Parts[0].Type != "text" {
		t.Errorf("part type = %q, want %q", artifact.Parts[0].Type, "text")
	}
	if artifact.Parts[0].Text != "File content" {
		t.Errorf("text = %q, want %q", artifact.Parts[0].Text, "File content")
	}
}

// TestFileArtifact tests file artifact helper.
func TestFileArtifact(t *testing.T) {
	artifact := FileArtifact("image.png", "image/png", []byte{0x89, 0x50, 0x4E, 0x47})

	if artifact.Name != "image.png" {
		t.Errorf("name = %q, want %q", artifact.Name, "image.png")
	}
	if len(artifact.Parts) != 1 {
		t.Fatalf("parts count = %d, want 1", len(artifact.Parts))
	}
	if artifact.Parts[0].Type != "file" {
		t.Errorf("part type = %q, want %q", artifact.Parts[0].Type, "file")
	}
	if artifact.Parts[0].File == nil {
		t.Fatal("file should not be nil")
	}
	if artifact.Parts[0].File.MimeType != "image/png" {
		t.Errorf("mimeType = %q, want %q", artifact.Parts[0].File.MimeType, "image/png")
	}
}

// TestExtractTextContent tests text extraction.
func TestExtractTextContent(t *testing.T) {
	msg := &Message{
		Role: "user",
		Parts: []Part{
			{Type: "text", Text: "Hello, "},
			{Type: "file", File: &FileData{Name: "test.txt"}},
			{Type: "text", Text: "world!"},
		},
	}

	text := ExtractTextContent(msg)
	if text != "Hello, world!" {
		t.Errorf("text = %q, want %q", text, "Hello, world!")
	}
}

// TestExtractTextContentEmpty tests extraction from message with no text.
func TestExtractTextContentEmpty(t *testing.T) {
	msg := &Message{
		Role:  "user",
		Parts: []Part{{Type: "file", File: &FileData{Name: "test.txt"}}},
	}

	text := ExtractTextContent(msg)
	if text != "" {
		t.Errorf("text = %q, want empty", text)
	}
}

// TestExtractFiles tests file extraction.
func TestExtractFiles(t *testing.T) {
	msg := &Message{
		Role: "user",
		Parts: []Part{
			{Type: "text", Text: "See files:"},
			{Type: "file", File: &FileData{Name: "file1.txt"}},
			{Type: "file", File: &FileData{Name: "file2.txt"}},
		},
	}

	files := ExtractFiles(msg)
	if len(files) != 2 {
		t.Fatalf("files count = %d, want 2", len(files))
	}
	if files[0].Name != "file1.txt" {
		t.Errorf("file[0].Name = %q, want %q", files[0].Name, "file1.txt")
	}
	if files[1].Name != "file2.txt" {
		t.Errorf("file[1].Name = %q, want %q", files[1].Name, "file2.txt")
	}
}

// TestExtractFilesNil tests extraction when file part has nil File.
func TestExtractFilesNil(t *testing.T) {
	msg := &Message{
		Role: "user",
		Parts: []Part{
			{Type: "file", File: nil}, // Nil file
			{Type: "file", File: &FileData{Name: "valid.txt"}},
		},
	}

	files := ExtractFiles(msg)
	if len(files) != 1 {
		t.Fatalf("files count = %d, want 1", len(files))
	}
}

// TestNewTextMessage tests text message creation.
func TestNewTextMessage(t *testing.T) {
	msg := NewTextMessage("user", "Hello agent!")

	if msg.Role != "user" {
		t.Errorf("role = %q, want %q", msg.Role, "user")
	}
	if len(msg.Parts) != 1 {
		t.Fatalf("parts count = %d, want 1", len(msg.Parts))
	}
	if msg.Parts[0].Type != "text" {
		t.Errorf("type = %q, want %q", msg.Parts[0].Type, "text")
	}
	if msg.Parts[0].Text != "Hello agent!" {
		t.Errorf("text = %q, want %q", msg.Parts[0].Text, "Hello agent!")
	}
}

// TestNewFileMessage tests file message creation.
func TestNewFileMessage(t *testing.T) {
	file := &FileData{Name: "doc.pdf", MimeType: "application/pdf"}
	msg := NewFileMessage("user", file)

	if msg.Role != "user" {
		t.Errorf("role = %q, want %q", msg.Role, "user")
	}
	if len(msg.Parts) != 1 {
		t.Fatalf("parts count = %d, want 1", len(msg.Parts))
	}
	if msg.Parts[0].Type != "file" {
		t.Errorf("type = %q, want %q", msg.Parts[0].Type, "file")
	}
	if msg.Parts[0].File.Name != "doc.pdf" {
		t.Errorf("file.Name = %q, want %q", msg.Parts[0].File.Name, "doc.pdf")
	}
}

// TestWithArtifacts tests the helper for returning message with artifacts.
func TestWithArtifacts(t *testing.T) {
	msg := TextResponse("Here are your files")
	art1 := TextArtifact("file1.txt", "Content 1")
	art2 := TextArtifact("file2.txt", "Content 2")

	result, artifacts, err := WithArtifacts(msg, art1, art2)

	if err != nil {
		t.Fatalf("WithArtifacts error: %v", err)
	}
	if result != msg {
		t.Error("message should be the same")
	}
	if len(artifacts) != 2 {
		t.Fatalf("artifacts count = %d, want 2", len(artifacts))
	}
	if artifacts[0].Name != "file1.txt" {
		t.Error("artifact[0] name mismatch")
	}
}

// mockAgent implements AgentCaller for testing.
type mockAgent struct {
	did       string
	responses map[string]*mockResponse
}

type mockResponse struct {
	body map[string]any
	err  error
}

func newMockAgent(did string) *mockAgent {
	return &mockAgent{
		did:       did,
		responses: make(map[string]*mockResponse),
	}
}

func (m *mockAgent) DID() string {
	return m.did
}

func (m *mockAgent) Send(ctx context.Context, to, method string, params any) (*messaging.Message, error) {
	key := to + ":" + method
	if resp, ok := m.responses[key]; ok {
		if resp.err != nil {
			return nil, resp.err
		}
		msg := messaging.NewMessage(to, m.did, messaging.TypeResponse, method)
		msg.SetBody(resp.body)
		return msg, nil
	}
	// Default response
	msg := messaging.NewMessage(to, m.did, messaging.TypeResponse, method)
	msg.SetBody(map[string]any{"status": "ok", "echo": params})
	return msg, nil
}

func (m *mockAgent) setResponse(to, method string, body map[string]any, err error) {
	key := to + ":" + method
	m.responses[key] = &mockResponse{body: body, err: err}
}

// TestAgentRouterCreation tests AgentRouter creation.
func TestAgentRouterCreation(t *testing.T) {
	agent := newMockAgent("did:wba:example.com:agent:test")
	router := NewAgentRouter(agent)

	if router == nil {
		t.Fatal("NewAgentRouter returned nil")
	}
	if router.agent != agent {
		t.Error("agent not set correctly")
	}
	if router.adapter == nil {
		t.Error("adapter should be initialized")
	}
	if router.a2aHandler == nil {
		t.Error("a2aHandler should be initialized")
	}
}

// TestAgentRouterWithLocalHandlers tests AgentRouter with local handlers.
func TestAgentRouterWithLocalHandlers(t *testing.T) {
	agent := newMockAgent("did:wba:example.com:agent:test")
	store := registry.NewMemoryStore()
	bridge := NewAgentBridge(store)

	bridge.RegisterMethod("default", func(ctx context.Context, params json.RawMessage) (any, error) {
		return "Local handler response", nil
	})

	router := NewAgentRouter(agent, WithRouterLocalHandlers(bridge))

	if router.bridge != bridge {
		t.Error("bridge not set correctly")
	}

	// Test that local handler is called when no recipient
	taskHandler := router.TaskHandler()
	task := &Task{
		ID:       "test-task",
		Metadata: make(map[string]any), // No recipient
	}
	msg := &Message{Role: "user", Parts: []Part{{Type: "text", Text: "Hello"}}}

	response, _, err := taskHandler(context.Background(), task, msg)
	if err != nil {
		t.Fatalf("TaskHandler error: %v", err)
	}

	text := ExtractTextContent(response)
	if text != "Local handler response" {
		t.Errorf("expected local handler response, got: %s", text)
	}
}

// TestAgentRouterRemoteRouting tests routing to remote agents.
func TestAgentRouterRemoteRouting(t *testing.T) {
	agent := newMockAgent("did:wba:example.com:agent:alice")
	agent.setResponse("did:wba:example.com:agent:bob", "a2a.message", map[string]any{
		"response": "Hello from Bob!",
	}, nil)

	router := NewAgentRouter(agent)
	taskHandler := router.TaskHandler()

	task := &Task{
		ID:        "test-task",
		SessionID: "session-1",
		Metadata: map[string]any{
			"recipient": "did:wba:example.com:agent:bob",
		},
	}
	msg := &Message{Role: "user", Parts: []Part{{Type: "text", Text: "Hello Bob!"}}}

	response, _, err := taskHandler(context.Background(), task, msg)
	if err != nil {
		t.Fatalf("TaskHandler error: %v", err)
	}

	if response == nil {
		t.Fatal("response should not be nil")
	}

	text := ExtractTextContent(response)
	if text == "" {
		t.Error("response text should not be empty")
	}
}

// TestAgentRouterRemoteRoutingWithMethod tests routing with custom method.
func TestAgentRouterRemoteRoutingWithMethod(t *testing.T) {
	agent := newMockAgent("did:wba:example.com:agent:alice")
	agent.setResponse("did:wba:example.com:agent:bob", "custom.method", map[string]any{
		"custom": "response",
	}, nil)

	router := NewAgentRouter(agent)
	taskHandler := router.TaskHandler()

	task := &Task{
		ID:        "test-task",
		SessionID: "session-1",
		Metadata: map[string]any{
			"recipient": "did:wba:example.com:agent:bob",
			"method":    "custom.method",
		},
	}
	msg := &Message{Role: "user", Parts: []Part{{Type: "text", Text: "Custom request"}}}

	response, _, err := taskHandler(context.Background(), task, msg)
	if err != nil {
		t.Fatalf("TaskHandler error: %v", err)
	}

	if response == nil {
		t.Fatal("response should not be nil")
	}
}

// TestAgentRouterRemoteError tests error handling for remote calls.
func TestAgentRouterRemoteError(t *testing.T) {
	agent := newMockAgent("did:wba:example.com:agent:alice")
	agent.setResponse("did:wba:example.com:agent:bob", "a2a.message", nil,
		errors.New("connection refused"))

	router := NewAgentRouter(agent)
	taskHandler := router.TaskHandler()

	task := &Task{
		ID:       "test-task",
		Metadata: map[string]any{"recipient": "did:wba:example.com:agent:bob"},
	}
	msg := &Message{Role: "user", Parts: []Part{{Type: "text", Text: "Hello"}}}

	_, _, err := taskHandler(context.Background(), task, msg)
	if err == nil {
		t.Error("expected error for failed remote call")
	}
	if !strings.Contains(err.Error(), "remote call failed") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestAgentRouterNoRecipientNoLocalHandlers tests error when no recipient and no handlers.
func TestAgentRouterNoRecipientNoLocalHandlers(t *testing.T) {
	agent := newMockAgent("did:wba:example.com:agent:test")
	router := NewAgentRouter(agent) // No local handlers

	taskHandler := router.TaskHandler()
	task := &Task{
		ID:       "test-task",
		Metadata: make(map[string]any), // No recipient
	}
	msg := &Message{Role: "user", Parts: []Part{{Type: "text", Text: "Hello"}}}

	_, _, err := taskHandler(context.Background(), task, msg)
	if err == nil {
		t.Error("expected error when no recipient and no local handlers")
	}
}

// TestAgentRouterCreateServer tests server creation from router.
func TestAgentRouterCreateServer(t *testing.T) {
	agent := newMockAgent("did:wba:example.com:agent:test")
	router := NewAgentRouter(agent)

	server := router.CreateServer()
	if server == nil {
		t.Fatal("CreateServer returned nil")
	}
}

// TestAgentRouterHandler tests Handler() method.
func TestAgentRouterHandler(t *testing.T) {
	agent := newMockAgent("did:wba:example.com:agent:test")
	router := NewAgentRouter(agent)

	handler := router.Handler()
	if handler == nil {
		t.Fatal("Handler returned nil")
	}
	if handler.agent != agent {
		t.Error("handler should have same agent")
	}
}

// TestHandlerWithAgent tests Handler with agent integration.
func TestHandlerWithAgent(t *testing.T) {
	agent := newMockAgent("did:wba:example.com:agent:alice")
	agent.setResponse("did:wba:example.com:agent:bob", "a2a.message", map[string]any{
		"message": "Hello from Bob!",
	}, nil)

	handler := NewHandler(WithAgent(agent))

	params := &SendMessageParams{
		Message: Message{
			Role:  "user",
			Parts: []Part{{Type: "text", Text: "Hello Bob!"}},
		},
		Metadata: map[string]any{
			"recipient": "did:wba:example.com:agent:bob",
		},
	}

	result, err := handler.HandleSendMessage(context.Background(), params)
	if err != nil {
		t.Fatalf("HandleSendMessage error: %v", err)
	}

	if result.Status.State != TaskStateCompleted {
		t.Errorf("expected completed state, got: %s", result.Status.State)
	}
}

// TestHandlerWithAgentError tests Handler error handling.
func TestHandlerWithAgentError(t *testing.T) {
	agent := newMockAgent("did:wba:example.com:agent:alice")
	agent.setResponse("did:wba:example.com:agent:bob", "a2a.message", nil,
		errors.New("remote agent unavailable"))

	handler := NewHandler(WithAgent(agent))

	params := &SendMessageParams{
		Message: Message{
			Role:  "user",
			Parts: []Part{{Type: "text", Text: "Hello"}},
		},
		Metadata: map[string]any{
			"recipient": "did:wba:example.com:agent:bob",
		},
	}

	result, err := handler.HandleSendMessage(context.Background(), params)
	if err != nil {
		t.Fatalf("HandleSendMessage should not return error: %v", err)
	}

	// Should return failed status, not error
	if result.Status.State != TaskStateFailed {
		t.Errorf("expected failed state, got: %s", result.Status.State)
	}
}

// TestHandlerNoAgentNoRecipient tests Handler without agent and no recipient.
func TestHandlerNoAgentNoRecipient(t *testing.T) {
	handler := NewHandler() // No agent

	params := &SendMessageParams{
		Message: Message{
			Role:  "user",
			Parts: []Part{{Type: "text", Text: "Hello"}},
		},
	}

	result, err := handler.HandleSendMessage(context.Background(), params)
	if err != nil {
		t.Fatalf("HandleSendMessage error: %v", err)
	}

	// Should return completed (stub behavior)
	if result.Status.State != TaskStateCompleted {
		t.Errorf("expected completed state, got: %s", result.Status.State)
	}
}

// TestAgentRouterResolveRecipientTo tests resolving recipient from 'to' field.
func TestAgentRouterResolveRecipientTo(t *testing.T) {
	agent := newMockAgent("did:wba:example.com:agent:alice")
	router := NewAgentRouter(agent)

	task := &Task{
		Metadata: map[string]any{
			"to": "did:wba:example.com:agent:bob",
		},
	}

	recipient := router.resolveRecipient(task)
	if recipient != "did:wba:example.com:agent:bob" {
		t.Errorf("expected bob, got: %s", recipient)
	}
}

// TestAgentRouterResolveRecipientEmpty tests resolving with no recipient.
func TestAgentRouterResolveRecipientEmpty(t *testing.T) {
	agent := newMockAgent("did:wba:example.com:agent:alice")
	router := NewAgentRouter(agent)

	task := &Task{
		Metadata: nil,
	}

	recipient := router.resolveRecipient(task)
	if recipient != "" {
		t.Errorf("expected empty, got: %s", recipient)
	}
}

// TestA2AServerWithAgentRouter tests full E2E flow with A2A Server.
func TestA2AServerWithAgentRouter(t *testing.T) {
	agent := newMockAgent("did:wba:example.com:agent:alice")
	store := registry.NewMemoryStore()
	bridge := NewAgentBridge(store)

	// Register local handler
	bridge.RegisterMethod("echo", func(ctx context.Context, params json.RawMessage) (any, error) {
		var p map[string]any
		json.Unmarshal(params, &p)
		if msg, ok := p["message"].(*Message); ok {
			return "Echo: " + ExtractTextContent(msg), nil
		}
		return "Echo: received", nil
	})

	router := NewAgentRouter(agent, WithRouterLocalHandlers(bridge))
	server := router.CreateServer()

	// Test via server handleMessageSend
	reqData, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  "message/send",
		"id":      1,
		"params": map[string]any{
			"message": map[string]any{
				"role":  "user",
				"parts": []map[string]any{{"type": "text", "text": "Hello server!"}},
			},
			"metadata": map[string]any{
				"method": "echo",
			},
		},
	})

	respData, err := server.HandleRequest(context.Background(), reqData)
	if err != nil {
		t.Fatalf("HandleRequest error: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal(respData, &resp); err != nil {
		t.Fatalf("unmarshal response error: %v", err)
	}

	if resp["error"] != nil {
		t.Errorf("unexpected error in response: %v", resp["error"])
	}

	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("result not found in response")
	}

	status, ok := result["status"].(map[string]any)
	if !ok {
		t.Fatalf("status not found in result")
	}

	if status["state"] != "completed" {
		t.Errorf("expected completed state, got: %v", status["state"])
	}
}

// TestTaskStateToThreadState tests task to thread state mapping.
func TestTaskStateToThreadState(t *testing.T) {
	tests := []struct {
		taskState   string
		threadState conversation.ThreadState
	}{
		{TaskStateSubmitted, conversation.ThreadStateActive},
		{TaskStateWorking, conversation.ThreadStateActive},
		{TaskStateInputRequired, conversation.ThreadStateAwaitingInput},
		{TaskStateCompleted, conversation.ThreadStateCompleted},
		{TaskStateFailed, conversation.ThreadStateFailed},
		{TaskStateCanceled, conversation.ThreadStateFailed},
		{"unknown", conversation.ThreadStateActive},
	}

	for _, tt := range tests {
		t.Run(tt.taskState, func(t *testing.T) {
			result := TaskStateToThreadState(tt.taskState)
			if result != tt.threadState {
				t.Errorf("TaskStateToThreadState(%q) = %q, want %q", tt.taskState, result, tt.threadState)
			}
		})
	}
}

// TestThreadStateToTaskState tests thread to task state mapping.
func TestThreadStateToTaskState(t *testing.T) {
	tests := []struct {
		threadState conversation.ThreadState
		taskState   string
	}{
		{conversation.ThreadStateActive, TaskStateWorking},
		{conversation.ThreadStateAwaitingInput, TaskStateInputRequired},
		{conversation.ThreadStateCompleted, TaskStateCompleted},
		{conversation.ThreadStateFailed, TaskStateFailed},
		{"unknown", TaskStateWorking},
	}

	for _, tt := range tests {
		t.Run(string(tt.threadState), func(t *testing.T) {
			result := ThreadStateToTaskState(tt.threadState)
			if result != tt.taskState {
				t.Errorf("ThreadStateToTaskState(%q) = %q, want %q", tt.threadState, result, tt.taskState)
			}
		})
	}
}

// TestSyncThreadWithTask tests syncing thread state from task.
func TestSyncThreadWithTask(t *testing.T) {
	now := time.Now()
	thread := conversation.NewThread("did:alice", "did:bob")
	task := &Task{
		ID:        "task-123",
		SessionID: "session-456",
		Status: TaskStatus{
			State:     TaskStateCompleted,
			Timestamp: &now,
		},
	}

	SyncThreadWithTask(thread, task)

	if thread.State != conversation.ThreadStateCompleted {
		t.Errorf("State = %q, want %q", thread.State, conversation.ThreadStateCompleted)
	}
	if len(thread.TaskIDs) != 1 || thread.TaskIDs[0] != "task-123" {
		t.Errorf("TaskIDs = %v, want [task-123]", thread.TaskIDs)
	}
	if len(thread.SessionIDs) != 1 || thread.SessionIDs[0] != "session-456" {
		t.Errorf("SessionIDs = %v, want [session-456]", thread.SessionIDs)
	}

	// Sync again - should not duplicate IDs
	SyncThreadWithTask(thread, task)
	if len(thread.TaskIDs) != 1 {
		t.Error("Should not duplicate task ID")
	}
}

// TestSyncTaskWithThread tests syncing task state from thread.
func TestSyncTaskWithThread(t *testing.T) {
	thread := conversation.NewThread("did:alice")
	thread.State = conversation.ThreadStateAwaitingInput

	task := &Task{
		ID: "task-123",
		Status: TaskStatus{
			State: TaskStateWorking,
		},
	}

	SyncTaskWithThread(task, thread)

	if task.Status.State != TaskStateInputRequired {
		t.Errorf("State = %q, want %q", task.Status.State, TaskStateInputRequired)
	}
	if task.Metadata["thread_id"] != thread.ID.String() {
		t.Errorf("thread_id = %v, want %s", task.Metadata["thread_id"], thread.ID.String())
	}
}
