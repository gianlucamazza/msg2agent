package a2a

import (
	"context"
	"testing"

	"github.com/gianluca/msg2agent/pkg/messaging"
	"github.com/gianluca/msg2agent/pkg/registry"
)

// TestNewAdapter tests adapter creation.
func TestNewAdapter(t *testing.T) {
	adapter := NewAdapter()
	if adapter == nil {
		t.Fatal("NewAdapter returned nil")
	}
}

// TestToA2AAgentCard tests conversion to A2A AgentCard.
func TestToA2AAgentCard(t *testing.T) {
	adapter := NewAdapter()

	agent := registry.NewAgent("did:wba:example.com:agent:test", "Test Agent")
	agent.AddEndpoint(registry.TransportWebSocket, "ws://localhost:8080", 1)
	agent.AddCapability("chat", "Chat capability", []string{"send", "receive"})
	agent.AddCapability("translate", "Translation", []string{"translate"})

	card := adapter.ToA2AAgentCard(agent)

	if card.Name != "Test Agent" {
		t.Errorf("Name = %q, want %q", card.Name, "Test Agent")
	}
	if card.Version != "1.0.0" {
		t.Errorf("Version = %q, want %q", card.Version, "1.0.0")
	}
	if card.URL != "ws://localhost:8080" {
		t.Errorf("URL = %q, want %q", card.URL, "ws://localhost:8080")
	}
	if !card.Capabilities.Streaming {
		t.Error("Streaming should be true")
	}
	if len(card.Skills) != 2 {
		t.Errorf("Skills count = %d, want 2", len(card.Skills))
	}
	if len(card.DefaultInputModes) != 1 || card.DefaultInputModes[0] != "text" {
		t.Error("DefaultInputModes should be [text]")
	}
}

// TestToA2AAgentCardNoEndpoints tests conversion with no endpoints.
func TestToA2AAgentCardNoEndpoints(t *testing.T) {
	adapter := NewAdapter()

	agent := registry.NewAgent("did:wba:example.com:agent:test", "Test")

	card := adapter.ToA2AAgentCard(agent)

	if card.URL != "" {
		t.Errorf("URL = %q, want empty", card.URL)
	}
}

// TestToMessage tests conversion from messaging.Message to A2A Message.
func TestToMessage(t *testing.T) {
	adapter := NewAdapter()

	msg, _ := messaging.NewRequest("did:from", "did:to", "test.method", map[string]string{"key": "value"})

	a2aMsg, err := adapter.ToMessage(msg)
	if err != nil {
		t.Fatalf("ToMessage failed: %v", err)
	}

	if a2aMsg.Role != "agent" {
		t.Errorf("Role = %q, want %q", a2aMsg.Role, "agent")
	}
	if len(a2aMsg.Parts) != 1 {
		t.Errorf("Parts count = %d, want 1", len(a2aMsg.Parts))
	}
	if a2aMsg.Parts[0].Type != "text" {
		t.Errorf("Part type = %q, want %q", a2aMsg.Parts[0].Type, "text")
	}
	if a2aMsg.Metadata["messageId"] != msg.ID.String() {
		t.Error("messageId metadata mismatch")
	}
	if a2aMsg.Metadata["from"] != "did:from" {
		t.Error("from metadata mismatch")
	}
	if a2aMsg.Metadata["method"] != "test.method" {
		t.Error("method metadata mismatch")
	}
}

// TestToMessageInvalidBody tests conversion with invalid JSON body.
func TestToMessageInvalidBody(t *testing.T) {
	adapter := NewAdapter()

	msg := messaging.NewMessage("did:from", "did:to", messaging.TypeRequest, "test")
	msg.Body = []byte("not valid json")

	a2aMsg, err := adapter.ToMessage(msg)
	if err != nil {
		t.Fatalf("ToMessage failed: %v", err)
	}

	// Should fall back to raw body
	if a2aMsg.Parts[0].Text == "" {
		t.Error("should have raw body in text")
	}
}

// TestFromMessage tests conversion from A2A Message to messaging.Message.
func TestFromMessage(t *testing.T) {
	adapter := NewAdapter()

	a2aMsg := &Message{
		Role: "user",
		Parts: []Part{
			{Type: "text", Text: "Hello, agent!"},
			{Type: "text", Text: " How are you?"},
		},
	}

	msg, err := adapter.FromMessage(a2aMsg, "did:from", "did:to", "chat.send")
	if err != nil {
		t.Fatalf("FromMessage failed: %v", err)
	}

	if msg.From != "did:from" {
		t.Errorf("From = %q, want %q", msg.From, "did:from")
	}
	if msg.To != "did:to" {
		t.Errorf("To = %q, want %q", msg.To, "did:to")
	}
	if msg.Method != "chat.send" {
		t.Errorf("Method = %q, want %q", msg.Method, "chat.send")
	}
	// With conversational detection: role="user" and method="chat.*" -> TypeChat
	if msg.Type != messaging.TypeChat {
		t.Errorf("Type = %v, want TypeChat (user role + chat method)", msg.Type)
	}

	// Check body contains concatenated text
	var body map[string]any
	msg.ParseBody(&body)
	if body["content"] != "Hello, agent! How are you?" {
		t.Errorf("content = %q, want combined text", body["content"])
	}
	if body["role"] != "user" {
		t.Errorf("role = %q, want %q", body["role"], "user")
	}
}

// TestFromMessageEmptyParts tests conversion with no text parts.
func TestFromMessageEmptyParts(t *testing.T) {
	adapter := NewAdapter()

	a2aMsg := &Message{
		Role:  "user",
		Parts: []Part{},
	}

	msg, err := adapter.FromMessage(a2aMsg, "did:from", "did:to", "method")
	if err != nil {
		t.Fatalf("FromMessage failed: %v", err)
	}

	var body map[string]any
	msg.ParseBody(&body)
	if body["content"] != "" {
		t.Errorf("content = %q, want empty", body["content"])
	}
}

// TestToTask tests conversion to A2A Task.
func TestToTask(t *testing.T) {
	adapter := NewAdapter()

	history := []TaskMessage{
		{Role: "user", Parts: []Part{{Type: "text", Text: "Hello"}}},
		{Role: "agent", Parts: []Part{{Type: "text", Text: "Hi there!"}}},
	}

	task := adapter.ToTask("task-123", "completed", history)

	if task.ID != "task-123" {
		t.Errorf("ID = %q, want %q", task.ID, "task-123")
	}
	if task.Status.State != "completed" {
		t.Errorf("State = %q, want %q", task.Status.State, "completed")
	}
	if len(task.History) != 2 {
		t.Errorf("History count = %d, want 2", len(task.History))
	}
}

// TestNewHandler tests A2A handler creation.
func TestNewHandler(t *testing.T) {
	handler := NewHandler()
	if handler == nil {
		t.Fatal("NewHandler returned nil")
	}
	if handler.adapter == nil {
		t.Error("adapter should be initialized")
	}
}

// TestHandleSendMessage tests the SendMessage handler.
func TestHandleSendMessage(t *testing.T) {
	handler := NewHandler()

	params := &SendMessageParams{
		ID:        "custom-id",
		SessionID: "session-1",
		Message: Message{
			Role:  "user",
			Parts: []Part{{Type: "text", Text: "Hello"}},
		},
	}

	result, err := handler.HandleSendMessage(context.Background(), params)
	if err != nil {
		t.Fatalf("HandleSendMessage failed: %v", err)
	}

	if result.ID != "custom-id" {
		t.Errorf("ID = %q, want %q", result.ID, "custom-id")
	}
	if result.SessionID != "session-1" {
		t.Errorf("SessionID = %q, want %q", result.SessionID, "session-1")
	}
	if result.Status.State != "completed" {
		t.Errorf("State = %q, want %q", result.Status.State, "completed")
	}
}

// TestHandleSendMessageGenerateID tests ID generation when not provided.
func TestHandleSendMessageGenerateID(t *testing.T) {
	handler := NewHandler()

	params := &SendMessageParams{
		Message: Message{
			Role:  "user",
			Parts: []Part{{Type: "text", Text: "Hello"}},
		},
	}

	result, err := handler.HandleSendMessage(context.Background(), params)
	if err != nil {
		t.Fatalf("HandleSendMessage failed: %v", err)
	}

	if result.ID == "" {
		t.Error("ID should be generated when not provided")
	}
}

// TestA2AProtocolConstants tests protocol constant values.
func TestA2AProtocolConstants(t *testing.T) {
	if A2AMessageSend != "message/send" {
		t.Errorf("A2AMessageSend = %q, want %q", A2AMessageSend, "message/send")
	}
	if A2AMessageStream != "message/stream" {
		t.Errorf("A2AMessageStream = %q, want %q", A2AMessageStream, "message/stream")
	}
	if A2ATasksGet != "tasks/get" {
		t.Errorf("A2ATasksGet = %q, want %q", A2ATasksGet, "tasks/get")
	}
	if A2ATasksCancel != "tasks/cancel" {
		t.Errorf("A2ATasksCancel = %q, want %q", A2ATasksCancel, "tasks/cancel")
	}
	if A2ATasksResubscribe != "tasks/resubscribe" {
		t.Errorf("A2ATasksResubscribe = %q, want %q", A2ATasksResubscribe, "tasks/resubscribe")
	}
}

// TestAgentCardStructure tests AgentCard JSON structure.
func TestAgentCardStructure(t *testing.T) {
	card := AgentCard{
		Name:        "Test",
		Description: "Test agent",
		URL:         "https://example.com",
		Version:     "1.0.0",
		Provider: &Provider{
			Organization: "Test Org",
			URL:          "https://test.org",
		},
		Capabilities: Capabilities{
			Streaming:              true,
			PushNotifications:      true,
			StateTransitionHistory: true,
		},
		Authentication: &AuthConfig{
			Schemes: []string{"bearer"},
		},
		DefaultInputModes:  []string{"text", "audio"},
		DefaultOutputModes: []string{"text"},
		Skills: []Skill{
			{
				ID:          "chat",
				Name:        "Chat",
				Description: "Chat skill",
				Tags:        []string{"conversation"},
				Examples:    []string{"Hello"},
				InputModes:  []string{"text"},
				OutputModes: []string{"text"},
			},
		},
	}

	if card.Name != "Test" {
		t.Error("Name mismatch")
	}
	if card.Provider.Organization != "Test Org" {
		t.Error("Provider organization mismatch")
	}
	if !card.Capabilities.Streaming {
		t.Error("Streaming should be true")
	}
	if len(card.Authentication.Schemes) != 1 {
		t.Error("Auth schemes mismatch")
	}
	if len(card.Skills) != 1 {
		t.Error("Skills mismatch")
	}
}

// TestTaskStructure tests Task JSON structure.
func TestTaskStructure(t *testing.T) {
	task := Task{
		ID:        "task-1",
		SessionID: "session-1",
		Status: TaskStatus{
			State: "completed",
		},
		History: []TaskMessage{
			{
				Role: "user",
				Parts: []Part{
					{Type: "text", Text: "Hello"},
				},
			},
		},
		Artifacts: []Artifact{
			{
				Name:        "output.txt",
				Description: "Output file",
				Index:       0,
				Parts: []Part{
					{Type: "text", Text: "Result"},
				},
				LastChunk: true,
			},
		},
		Metadata: map[string]any{
			"key": "value",
		},
	}

	if task.ID != "task-1" {
		t.Error("ID mismatch")
	}
	if task.Status.State != "completed" {
		t.Error("Status state mismatch")
	}
	if len(task.History) != 1 {
		t.Error("History mismatch")
	}
	if len(task.Artifacts) != 1 {
		t.Error("Artifacts mismatch")
	}
	if task.Artifacts[0].Name != "output.txt" {
		t.Error("Artifact name mismatch")
	}
}

// TestPartTypes tests different part types.
func TestPartTypes(t *testing.T) {
	// Text part
	textPart := Part{Type: "text", Text: "Hello"}
	if textPart.Type != "text" || textPart.Text != "Hello" {
		t.Error("Text part mismatch")
	}

	// File part
	filePart := Part{
		Type: "file",
		File: &FileData{
			Name:     "test.txt",
			MimeType: "text/plain",
			Bytes:    []byte("content"),
			URI:      "file:///test.txt",
		},
	}
	if filePart.Type != "file" || filePart.File.Name != "test.txt" {
		t.Error("File part mismatch")
	}

	// Data part
	dataPart := Part{
		Type: "data",
		Data: map[string]any{"key": "value"},
	}
	if dataPart.Type != "data" || dataPart.Data["key"] != "value" {
		t.Error("Data part mismatch")
	}
}

// TestSendMessageParams tests SendMessageParams structure.
func TestSendMessageParams(t *testing.T) {
	params := SendMessageParams{
		ID:        "req-1",
		SessionID: "session-1",
		Message: Message{
			Role:  "user",
			Parts: []Part{{Type: "text", Text: "Hello"}},
		},
		AcceptedModes: []string{"text", "markdown"},
		Metadata: map[string]any{
			"client": "test",
		},
	}

	if params.ID != "req-1" {
		t.Error("ID mismatch")
	}
	if params.SessionID != "session-1" {
		t.Error("SessionID mismatch")
	}
	if len(params.AcceptedModes) != 2 {
		t.Error("AcceptedModes mismatch")
	}
	if params.Metadata["client"] != "test" {
		t.Error("Metadata mismatch")
	}
}

// TestSendMessageResult tests SendMessageResult structure.
func TestSendMessageResult(t *testing.T) {
	result := SendMessageResult{
		ID:        "task-1",
		SessionID: "session-1",
		Status: TaskStatus{
			State: "working",
		},
		Artifacts: []Artifact{},
		Metadata: map[string]any{
			"processed": true,
		},
	}

	if result.ID != "task-1" {
		t.Error("ID mismatch")
	}
	if result.Status.State != "working" {
		t.Error("Status state mismatch")
	}
}

// TestIsConversational tests conversational message detection.
func TestIsConversational(t *testing.T) {
	tests := []struct {
		name     string
		msg      *Message
		method   string
		expected bool
	}{
		{
			name: "explicit chat type in metadata",
			msg: &Message{
				Role:     "agent",
				Metadata: map[string]any{"type": "chat"},
			},
			method:   "custom.method",
			expected: true,
		},
		{
			name:     "chat method prefix",
			msg:      &Message{Role: "agent"},
			method:   "chat.send",
			expected: true,
		},
		{
			name:     "user role implies conversation",
			msg:      &Message{Role: "user"},
			method:   "some.method",
			expected: true,
		},
		{
			name:     "agent role non-chat method",
			msg:      &Message{Role: "agent"},
			method:   "rpc.call",
			expected: false,
		},
		{
			name:     "nil metadata agent role",
			msg:      &Message{Role: "agent", Metadata: nil},
			method:   "api.request",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isConversational(tt.msg, tt.method)
			if result != tt.expected {
				t.Errorf("isConversational() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// TestFromMessageWithOptions tests conversion with session ID.
func TestFromMessageWithOptions(t *testing.T) {
	adapter := NewAdapter()

	a2aMsg := &Message{
		Role:  "user",
		Parts: []Part{{Type: "text", Text: "Hello"}},
	}

	opts := FromMessageOptions{SessionID: "session-123"}
	msg, err := adapter.FromMessageWithOptions(a2aMsg, "did:from", "did:to", "chat.send", opts)
	if err != nil {
		t.Fatalf("FromMessageWithOptions failed: %v", err)
	}

	// Should be TypeChat because method starts with "chat."
	if msg.Type != messaging.TypeChat {
		t.Errorf("Type = %v, want TypeChat", msg.Type)
	}

	// ThreadID should be set from SessionID
	if msg.ThreadID == nil {
		t.Fatal("ThreadID should be set")
	}
}

// TestFromMessageWithOptionsUUIDSession tests session ID as UUID.
func TestFromMessageWithOptionsUUIDSession(t *testing.T) {
	adapter := NewAdapter()

	a2aMsg := &Message{
		Role:  "user",
		Parts: []Part{{Type: "text", Text: "Hello"}},
	}

	sessionID := "550e8400-e29b-41d4-a716-446655440000"
	opts := FromMessageOptions{SessionID: sessionID}
	msg, err := adapter.FromMessageWithOptions(a2aMsg, "did:from", "did:to", "method", opts)
	if err != nil {
		t.Fatalf("FromMessageWithOptions failed: %v", err)
	}

	// ThreadID should match the session UUID
	if msg.ThreadID == nil {
		t.Fatal("ThreadID should be set")
	}
	if msg.ThreadID.String() != sessionID {
		t.Errorf("ThreadID = %s, want %s", msg.ThreadID.String(), sessionID)
	}
}

// TestToMessagePreservesThreadID tests that ThreadID is preserved in metadata.
func TestToMessagePreservesThreadID(t *testing.T) {
	adapter := NewAdapter()

	// Create a message with ThreadID
	msg := messaging.NewMessage("did:from", "did:to", messaging.TypeChat, "chat")
	threadID := msg.ID
	msg.ThreadID = &threadID
	msg.ThreadSeqNo = 5
	msg.SetBody(map[string]string{"text": "Hello"})

	a2aMsg, err := adapter.ToMessage(msg)
	if err != nil {
		t.Fatalf("ToMessage failed: %v", err)
	}

	// Check metadata contains thread info
	if a2aMsg.Metadata["thread_id"] != threadID.String() {
		t.Errorf("thread_id = %v, want %s", a2aMsg.Metadata["thread_id"], threadID.String())
	}
	if a2aMsg.Metadata["thread_seq_no"] != 5 {
		t.Errorf("thread_seq_no = %v, want 5", a2aMsg.Metadata["thread_seq_no"])
	}
	if a2aMsg.Metadata["type"] != "chat" {
		t.Errorf("type = %v, want chat", a2aMsg.Metadata["type"])
	}
}

// TestSessionToThreadID tests session ID to thread ID conversion.
func TestSessionToThreadID(t *testing.T) {
	// Valid UUID should be returned as-is
	validUUID := "550e8400-e29b-41d4-a716-446655440000"
	result := sessionToThreadID(validUUID)
	if result.String() != validUUID {
		t.Errorf("UUID session: got %s, want %s", result.String(), validUUID)
	}

	// Non-UUID should generate deterministic UUID
	nonUUID := "my-session-123"
	result1 := sessionToThreadID(nonUUID)
	result2 := sessionToThreadID(nonUUID)
	if result1 != result2 {
		t.Error("Non-UUID sessions should produce same UUID")
	}
	// Should not be the same as the input
	if result1.String() == nonUUID {
		t.Error("Generated UUID should differ from input")
	}
}
