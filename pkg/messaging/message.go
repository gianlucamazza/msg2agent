// Package messaging provides message types and routing for agent communication.
package messaging

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Validation limits.
const (
	MaxEmojiLength      = 32  // Max bytes for emoji (covers multi-codepoint emojis)
	MaxStatusTextLength = 256 // Max characters for status text
	MaxMethodLength     = 128 // Max characters for method name
	MaxDIDLength        = 256 // Max characters for DID
)

// Validation errors.
var (
	ErrEmojiTooLong      = errors.New("emoji exceeds maximum length")
	ErrStatusTextTooLong = errors.New("status text exceeds maximum length")
	ErrMethodTooLong     = errors.New("method name exceeds maximum length")
	ErrDIDTooLong        = errors.New("DID exceeds maximum length")
	ErrInvalidReaction   = errors.New("invalid reaction: emoji is required")
)

// MessageType identifies the kind of message.
type MessageType string

// Message types for agent-to-agent communication.
const (
	// RPC types (existing)
	TypeRequest      MessageType = "request"
	TypeResponse     MessageType = "response"
	TypeNotification MessageType = "notification"
	TypeStream       MessageType = "stream"
	TypeError        MessageType = "error"

	// Chat types (new)
	TypeChat     MessageType = "chat"     // Conversational message
	TypeTyping   MessageType = "typing"   // Typing indicator
	TypeReceipt  MessageType = "receipt"  // Delivery/read receipt
	TypePresence MessageType = "presence" // Online status
	TypeReaction MessageType = "reaction" // Emoji reaction
)

// Message represents a message envelope for agent-to-agent communication.
type Message struct {
	ID            uuid.UUID   `json:"id"`
	CorrelationID *uuid.UUID  `json:"correlation_id,omitempty"`
	From          string      `json:"from"`
	To            string      `json:"to"`
	Type          MessageType `json:"type"`
	Method        string      `json:"method,omitempty"`
	Body          []byte      `json:"body"`
	Signature     []byte      `json:"signature,omitempty"`
	Timestamp     time.Time   `json:"timestamp"`
	TraceID       string      `json:"trace_id,omitempty"`
	Encrypted     bool        `json:"encrypted"`

	// Threading fields for conversations
	ThreadID    *uuid.UUID `json:"thread_id,omitempty"`     // Groups messages in a conversation
	ParentID    *uuid.UUID `json:"parent_id,omitempty"`     // For nested replies
	ThreadSeqNo int        `json:"thread_seq_no,omitempty"` // Ordering within thread

	// Async messaging
	RequestAck bool `json:"request_ack,omitempty"` // Request delivery acknowledgment
}

// NewMessage creates a new message with a generated UUID v7.
func NewMessage(from, to string, msgType MessageType, method string) *Message {
	return &Message{
		ID:        uuid.Must(uuid.NewV7()),
		From:      from,
		To:        to,
		Type:      msgType,
		Method:    method,
		Timestamp: time.Now(),
	}
}

// NewRequest creates a new request message.
func NewRequest(from, to, method string, body any) (*Message, error) {
	msg := NewMessage(from, to, TypeRequest, method)
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		msg.Body = data
	}
	return msg, nil
}

// NewResponse creates a response to a request message.
func NewResponse(req *Message, body any) (*Message, error) {
	msg := NewMessage(req.To, req.From, TypeResponse, req.Method)
	msg.CorrelationID = &req.ID
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		msg.Body = data
	}
	return msg, nil
}

// NewNotification creates a new notification message (no response expected).
func NewNotification(from, to, method string, body any) (*Message, error) {
	msg := NewMessage(from, to, TypeNotification, method)
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		msg.Body = data
	}
	return msg, nil
}

// NewErrorResponse creates an error response to a request message.
func NewErrorResponse(req *Message, errCode int, errMsg string) (*Message, error) {
	msg := NewMessage(req.To, req.From, TypeError, req.Method)
	msg.CorrelationID = &req.ID
	errBody := ErrorBody{
		Code:    errCode,
		Message: errMsg,
	}
	data, err := json.Marshal(errBody)
	if err != nil {
		return nil, err
	}
	msg.Body = data
	return msg, nil
}

// ErrorBody represents an error response body.
type ErrorBody struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// SetBody sets the message body from any serializable value.
func (m *Message) SetBody(body any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	m.Body = data
	return nil
}

// ParseBody parses the message body into the provided value.
func (m *Message) ParseBody(v any) error {
	return json.Unmarshal(m.Body, v)
}

// IsRequest returns true if this is a request message.
func (m *Message) IsRequest() bool {
	return m.Type == TypeRequest
}

// IsResponse returns true if this is a response message.
func (m *Message) IsResponse() bool {
	return m.Type == TypeResponse
}

// IsError returns true if this is an error message.
func (m *Message) IsError() bool {
	return m.Type == TypeError
}

// WithTraceID sets the trace ID for distributed tracing.
func (m *Message) WithTraceID(traceID string) *Message {
	m.TraceID = traceID
	return m
}

// Validate checks that the message has valid field lengths.
func (m *Message) Validate() error {
	if len(m.From) > MaxDIDLength {
		return ErrDIDTooLong
	}
	if len(m.To) > MaxDIDLength {
		return ErrDIDTooLong
	}
	if len(m.Method) > MaxMethodLength {
		return ErrMethodTooLong
	}
	return nil
}

// Clone creates a copy of the message.
func (m *Message) Clone() *Message {
	clone := *m
	if m.CorrelationID != nil {
		corrID := *m.CorrelationID
		clone.CorrelationID = &corrID
	}
	if m.ThreadID != nil {
		threadID := *m.ThreadID
		clone.ThreadID = &threadID
	}
	if m.ParentID != nil {
		parentID := *m.ParentID
		clone.ParentID = &parentID
	}
	// Preserve nil vs empty slice distinction for proper JSON serialization
	if m.Body != nil {
		clone.Body = make([]byte, len(m.Body))
		copy(clone.Body, m.Body)
	}
	if m.Signature != nil {
		clone.Signature = make([]byte, len(m.Signature))
		copy(clone.Signature, m.Signature)
	}
	return &clone
}

// NewChatMessage creates a new chat message for conversational communication.
func NewChatMessage(from, to string, body any) (*Message, error) {
	msg := NewMessage(from, to, TypeChat, "chat")
	// First message in a thread: ThreadID = ID (self-referential)
	msg.ThreadID = &msg.ID
	msg.ThreadSeqNo = 1
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		msg.Body = data
	}
	return msg, nil
}

// NewChatReply creates a reply within an existing thread.
func NewChatReply(from, to string, threadID uuid.UUID, parentID *uuid.UUID, seqNo int, body any) (*Message, error) {
	msg := NewMessage(from, to, TypeChat, "chat")
	msg.ThreadID = &threadID
	msg.ParentID = parentID
	msg.ThreadSeqNo = seqNo
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		msg.Body = data
	}
	return msg, nil
}

// NewTypingIndicator creates a typing indicator message.
func NewTypingIndicator(from, to string, threadID *uuid.UUID, typing bool) *Message {
	msg := NewMessage(from, to, TypeTyping, "typing")
	msg.ThreadID = threadID
	body := TypingIndicator{Typing: typing}
	data, _ := json.Marshal(body)
	msg.Body = data
	return msg
}

// TypingIndicator represents the body of a typing indicator message.
type TypingIndicator struct {
	Typing bool `json:"typing"`
}

// NewReceipt creates a delivery or read receipt message.
func NewReceipt(from, to string, messageID uuid.UUID, receiptType ReceiptType) *Message {
	msg := NewMessage(from, to, TypeReceipt, "receipt")
	body := Receipt{
		MessageID:   messageID,
		ReceiptType: receiptType,
		Timestamp:   time.Now(),
	}
	data, _ := json.Marshal(body)
	msg.Body = data
	return msg
}

// ReceiptType indicates the type of receipt.
type ReceiptType string

// Receipt types.
const (
	ReceiptDelivered ReceiptType = "delivered"
	ReceiptRead      ReceiptType = "read"
)

// Receipt represents the body of a receipt message.
type Receipt struct {
	MessageID   uuid.UUID   `json:"message_id"`
	ReceiptType ReceiptType `json:"receipt_type"`
	Timestamp   time.Time   `json:"timestamp"`
}

// NewReaction creates a reaction message (emoji reaction to a message).
func NewReaction(from, to string, messageID uuid.UUID, emoji string) *Message {
	msg := NewMessage(from, to, TypeReaction, "reaction")
	body := Reaction{
		MessageID: messageID,
		Emoji:     emoji,
	}
	data, _ := json.Marshal(body)
	msg.Body = data
	return msg
}

// Reaction represents the body of a reaction message.
type Reaction struct {
	MessageID uuid.UUID `json:"message_id"`
	Emoji     string    `json:"emoji"`
}

// Validate checks that the reaction has valid values.
func (r *Reaction) Validate() error {
	if r.Emoji == "" {
		return ErrInvalidReaction
	}
	if len(r.Emoji) > MaxEmojiLength {
		return ErrEmojiTooLong
	}
	return nil
}

// PresenceStatus represents an agent's presence status.
type PresenceStatus string

// Presence statuses.
const (
	PresenceOnline  PresenceStatus = "online"
	PresenceOffline PresenceStatus = "offline"
	PresenceBusy    PresenceStatus = "busy"
	PresenceAway    PresenceStatus = "away"
	PresenceDND     PresenceStatus = "dnd"
)

// PresenceUpdate represents the body of a presence message.
type PresenceUpdate struct {
	Status       PresenceStatus `json:"status"`
	StatusText   string         `json:"status_text,omitempty"` // e.g., "In a meeting"
	LastActivity *time.Time     `json:"last_activity,omitempty"`
}

// Validate checks that the presence update has valid values.
func (p *PresenceUpdate) Validate() error {
	if len(p.StatusText) > MaxStatusTextLength {
		return ErrStatusTextTooLong
	}
	return nil
}

// NewPresenceMessage creates a presence update message.
func NewPresenceMessage(from, to string, status PresenceStatus, statusText string) *Message {
	msg := NewMessage(from, to, TypePresence, "")
	body := PresenceUpdate{
		Status:     status,
		StatusText: statusText,
	}
	data, _ := json.Marshal(body)
	msg.Body = data
	return msg
}

// IsChat returns true if this is a chat message.
func (m *Message) IsChat() bool {
	return m.Type == TypeChat
}

// IsTyping returns true if this is a typing indicator.
func (m *Message) IsTyping() bool {
	return m.Type == TypeTyping
}

// IsReceipt returns true if this is a receipt message.
func (m *Message) IsReceipt() bool {
	return m.Type == TypeReceipt
}

// IsPresence returns true if this is a presence message.
func (m *Message) IsPresence() bool {
	return m.Type == TypePresence
}

// IsReaction returns true if this is a reaction message.
func (m *Message) IsReaction() bool {
	return m.Type == TypeReaction
}

// InThread returns true if this message belongs to a thread.
func (m *Message) InThread() bool {
	return m.ThreadID != nil
}

// IsThreadStart returns true if this message starts a new thread.
func (m *Message) IsThreadStart() bool {
	return m.ThreadID != nil && *m.ThreadID == m.ID
}

// WithThread sets the thread context for the message.
func (m *Message) WithThread(threadID uuid.UUID, seqNo int) *Message {
	m.ThreadID = &threadID
	m.ThreadSeqNo = seqNo
	return m
}

// WithParent sets the parent message for nested replies.
func (m *Message) WithParent(parentID uuid.UUID) *Message {
	m.ParentID = &parentID
	return m
}

// WithRequestAck marks the message to request delivery acknowledgment.
func (m *Message) WithRequestAck() *Message {
	m.RequestAck = true
	return m
}
