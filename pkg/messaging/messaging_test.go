package messaging

import (
	"testing"

	"github.com/google/uuid"
)

func TestNewMessage(t *testing.T) {
	msg := NewMessage("did:wba:from", "did:wba:to", TypeRequest, "test.method")

	if msg.From != "did:wba:from" {
		t.Errorf("unexpected from: %s", msg.From)
	}
	if msg.To != "did:wba:to" {
		t.Errorf("unexpected to: %s", msg.To)
	}
	if msg.Type != TypeRequest {
		t.Errorf("unexpected type: %s", msg.Type)
	}
	if msg.Method != "test.method" {
		t.Errorf("unexpected method: %s", msg.Method)
	}
}

func TestNewRequest(t *testing.T) {
	params := map[string]string{"key": "value"}
	msg, err := NewRequest("from", "to", "method", params)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	if msg.Type != TypeRequest {
		t.Errorf("expected request type, got %s", msg.Type)
	}

	var parsed map[string]string
	if err := msg.ParseBody(&parsed); err != nil {
		t.Fatalf("failed to parse body: %v", err)
	}

	if parsed["key"] != "value" {
		t.Errorf("expected value, got %s", parsed["key"])
	}
}

func TestNewResponse(t *testing.T) {
	req, _ := NewRequest("from", "to", "method", nil)
	result := map[string]int{"count": 42}
	resp, err := NewResponse(req, result)
	if err != nil {
		t.Fatalf("failed to create response: %v", err)
	}

	if resp.Type != TypeResponse {
		t.Errorf("expected response type, got %s", resp.Type)
	}
	if resp.CorrelationID == nil || *resp.CorrelationID != req.ID {
		t.Error("correlation ID should match request ID")
	}
	if resp.From != req.To || resp.To != req.From {
		t.Error("response should swap from/to")
	}
}

func TestNewNotification(t *testing.T) {
	msg, err := NewNotification("from", "to", "event", map[string]bool{"active": true})
	if err != nil {
		t.Fatalf("failed to create notification: %v", err)
	}

	if msg.Type != TypeNotification {
		t.Errorf("expected notification type, got %s", msg.Type)
	}
}

func TestNewErrorResponse(t *testing.T) {
	req, _ := NewRequest("from", "to", "method", nil)
	errResp, err := NewErrorResponse(req, 500, "internal error")
	if err != nil {
		t.Fatalf("failed to create error response: %v", err)
	}

	if errResp.Type != TypeError {
		t.Errorf("expected error type, got %s", errResp.Type)
	}

	var errBody ErrorBody
	if err := errResp.ParseBody(&errBody); err != nil {
		t.Fatalf("failed to parse error body: %v", err)
	}

	if errBody.Code != 500 {
		t.Errorf("expected code 500, got %d", errBody.Code)
	}
	if errBody.Message != "internal error" {
		t.Errorf("expected 'internal error', got %s", errBody.Message)
	}
}

func TestMessageClone(t *testing.T) {
	msg, _ := NewRequest("from", "to", "method", "body")
	msg.Signature = []byte("signature")

	clone := msg.Clone()

	if clone.ID != msg.ID {
		t.Error("clone ID should match")
	}
	if &clone.Body == &msg.Body {
		t.Error("clone body should be a different slice")
	}
	if &clone.Signature == &msg.Signature {
		t.Error("clone signature should be a different slice")
	}
}

func TestMessageHelpers(t *testing.T) {
	req, _ := NewRequest("from", "to", "method", nil)
	resp, _ := NewResponse(req, nil)
	errResp, _ := NewErrorResponse(req, 500, "error")

	if !req.IsRequest() {
		t.Error("request should be identified as request")
	}
	if !resp.IsResponse() {
		t.Error("response should be identified as response")
	}
	if !errResp.IsError() {
		t.Error("error response should be identified as error")
	}
}

func TestNewChatMessage(t *testing.T) {
	body := map[string]string{"text": "Hello, World!"}
	msg, err := NewChatMessage("did:wba:alice", "did:wba:bob", body)
	if err != nil {
		t.Fatalf("failed to create chat message: %v", err)
	}

	if msg.Type != TypeChat {
		t.Errorf("expected chat type, got %s", msg.Type)
	}
	if msg.ThreadID == nil {
		t.Error("chat message should have ThreadID")
	}
	if *msg.ThreadID != msg.ID {
		t.Error("first message ThreadID should equal its ID (self-referential)")
	}
	if msg.ThreadSeqNo != 1 {
		t.Errorf("first message ThreadSeqNo should be 1, got %d", msg.ThreadSeqNo)
	}
	if !msg.IsChat() {
		t.Error("IsChat should return true")
	}
	if !msg.InThread() {
		t.Error("InThread should return true")
	}
	if !msg.IsThreadStart() {
		t.Error("IsThreadStart should return true for first message")
	}
}

func TestNewChatReply(t *testing.T) {
	// Create initial message
	initial, _ := NewChatMessage("did:wba:alice", "did:wba:bob", "Hello")
	threadID := *initial.ThreadID

	// Create reply
	reply, err := NewChatReply("did:wba:bob", "did:wba:alice", threadID, &initial.ID, 2, "Hi back!")
	if err != nil {
		t.Fatalf("failed to create chat reply: %v", err)
	}

	if reply.Type != TypeChat {
		t.Errorf("expected chat type, got %s", reply.Type)
	}
	if reply.ThreadID == nil || *reply.ThreadID != threadID {
		t.Error("reply ThreadID should match original thread")
	}
	if reply.ParentID == nil || *reply.ParentID != initial.ID {
		t.Error("reply ParentID should match parent message")
	}
	if reply.ThreadSeqNo != 2 {
		t.Errorf("reply ThreadSeqNo should be 2, got %d", reply.ThreadSeqNo)
	}
	if reply.IsThreadStart() {
		t.Error("reply should not be thread start")
	}
}

func TestTypingIndicator(t *testing.T) {
	msg := NewTypingIndicator("did:wba:alice", "did:wba:bob", nil, true)

	if msg.Type != TypeTyping {
		t.Errorf("expected typing type, got %s", msg.Type)
	}
	if !msg.IsTyping() {
		t.Error("IsTyping should return true")
	}

	var indicator TypingIndicator
	if err := msg.ParseBody(&indicator); err != nil {
		t.Fatalf("failed to parse typing indicator: %v", err)
	}
	if !indicator.Typing {
		t.Error("typing indicator should be true")
	}
}

func TestReceipt(t *testing.T) {
	msgID := msg2agentUUID()
	receipt := NewReceipt("did:wba:bob", "did:wba:alice", msgID, ReceiptRead)

	if receipt.Type != TypeReceipt {
		t.Errorf("expected receipt type, got %s", receipt.Type)
	}
	if !receipt.IsReceipt() {
		t.Error("IsReceipt should return true")
	}

	var body Receipt
	if err := receipt.ParseBody(&body); err != nil {
		t.Fatalf("failed to parse receipt: %v", err)
	}
	if body.MessageID != msgID {
		t.Error("receipt MessageID should match")
	}
	if body.ReceiptType != ReceiptRead {
		t.Errorf("expected read receipt, got %s", body.ReceiptType)
	}
}

func TestReaction(t *testing.T) {
	msgID := msg2agentUUID()
	reaction := NewReaction("did:wba:bob", "did:wba:alice", msgID, "👍")

	if reaction.Type != TypeReaction {
		t.Errorf("expected reaction type, got %s", reaction.Type)
	}
	if !reaction.IsReaction() {
		t.Error("IsReaction should return true")
	}

	var body Reaction
	if err := reaction.ParseBody(&body); err != nil {
		t.Fatalf("failed to parse reaction: %v", err)
	}
	if body.MessageID != msgID {
		t.Error("reaction MessageID should match")
	}
	if body.Emoji != "👍" {
		t.Errorf("expected 👍, got %s", body.Emoji)
	}
}

func TestMessageCloneWithThreading(t *testing.T) {
	msg, _ := NewChatMessage("from", "to", "body")
	msg.ParentID = msg.ThreadID // Set parent for test

	clone := msg.Clone()

	if clone.ThreadID == nil {
		t.Error("clone should have ThreadID")
	}
	if clone.ThreadID == msg.ThreadID {
		t.Error("clone ThreadID should be a different pointer")
	}
	if *clone.ThreadID != *msg.ThreadID {
		t.Error("clone ThreadID value should match original")
	}
	if clone.ParentID == msg.ParentID {
		t.Error("clone ParentID should be a different pointer")
	}
}

func TestMessageWithMethods(t *testing.T) {
	msg := NewMessage("from", "to", TypeChat, "")
	threadID := msg2agentUUID()
	parentID := msg2agentUUID()

	msg.WithThread(threadID, 5)
	if msg.ThreadID == nil || *msg.ThreadID != threadID {
		t.Error("WithThread should set ThreadID")
	}
	if msg.ThreadSeqNo != 5 {
		t.Error("WithThread should set ThreadSeqNo")
	}

	msg.WithParent(parentID)
	if msg.ParentID == nil || *msg.ParentID != parentID {
		t.Error("WithParent should set ParentID")
	}

	msg.WithRequestAck()
	if !msg.RequestAck {
		t.Error("WithRequestAck should set RequestAck to true")
	}
}

func TestChatMessageTypes(t *testing.T) {
	tests := []struct {
		msgType MessageType
		check   func(*Message) bool
	}{
		{TypeChat, (*Message).IsChat},
		{TypeTyping, (*Message).IsTyping},
		{TypeReceipt, (*Message).IsReceipt},
		{TypePresence, (*Message).IsPresence},
		{TypeReaction, (*Message).IsReaction},
	}

	for _, tc := range tests {
		msg := NewMessage("from", "to", tc.msgType, "")
		if !tc.check(msg) {
			t.Errorf("Is* method should return true for %s type", tc.msgType)
		}
	}
}

func msg2agentUUID() uuid.UUID {
	return uuid.Must(uuid.NewV7())
}
