package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
)

// InboxMessage represents a message stored in the inbox.
type InboxMessage struct {
	ID        string          `json:"id"`
	From      string          `json:"from"`
	Method    string          `json:"method"`
	Body      json.RawMessage `json:"body,omitempty"`
	Timestamp time.Time       `json:"timestamp"`
	Read      bool            `json:"read"`
}

// Inbox stores incoming messages for the MCP agent.
type Inbox struct {
	messages []*InboxMessage
	mu       sync.RWMutex
	maxSize  int
}

// NewInbox creates a new inbox with the specified maximum size.
func NewInbox(maxSize int) *Inbox {
	return &Inbox{
		messages: make([]*InboxMessage, 0),
		maxSize:  maxSize,
	}
}

// Add adds a message to the inbox.
func (i *Inbox) Add(from, method string, body json.RawMessage) string {
	i.mu.Lock()
	defer i.mu.Unlock()

	msg := &InboxMessage{
		ID:        uuid.New().String(),
		From:      from,
		Method:    method,
		Body:      body,
		Timestamp: time.Now(),
		Read:      false,
	}

	// Prepend (most recent first)
	i.messages = append([]*InboxMessage{msg}, i.messages...)

	// Trim if over max size
	if len(i.messages) > i.maxSize {
		i.messages = i.messages[:i.maxSize]
	}

	return msg.ID
}

// Get returns a message by ID.
func (i *Inbox) Get(id string) *InboxMessage {
	i.mu.RLock()
	defer i.mu.RUnlock()

	for _, msg := range i.messages {
		if msg.ID == id {
			return msg
		}
	}
	return nil
}

// MarkRead marks a message as read.
func (i *Inbox) MarkRead(id string) bool {
	i.mu.Lock()
	defer i.mu.Unlock()

	for _, msg := range i.messages {
		if msg.ID == id {
			msg.Read = true
			return true
		}
	}
	return false
}

// List returns all messages, optionally filtered by unread only.
func (i *Inbox) List(unreadOnly bool) []*InboxMessage {
	i.mu.RLock()
	defer i.mu.RUnlock()

	if !unreadOnly {
		result := make([]*InboxMessage, len(i.messages))
		copy(result, i.messages)
		return result
	}

	result := make([]*InboxMessage, 0)
	for _, msg := range i.messages {
		if !msg.Read {
			result = append(result, msg)
		}
	}
	return result
}

// Delete removes a message by ID.
func (i *Inbox) Delete(id string) bool {
	i.mu.Lock()
	defer i.mu.Unlock()

	for idx, msg := range i.messages {
		if msg.ID == id {
			i.messages = append(i.messages[:idx], i.messages[idx+1:]...)
			return true
		}
	}
	return false
}

// Count returns the number of messages, optionally filtering by unread.
func (i *Inbox) Count(unreadOnly bool) int {
	i.mu.RLock()
	defer i.mu.RUnlock()

	if !unreadOnly {
		return len(i.messages)
	}

	count := 0
	for _, msg := range i.messages {
		if !msg.Read {
			count++
		}
	}
	return count
}

// registerResources registers MCP resources for inbox and tasks.
func (s *Server) registerResources() {
	// Inbox resource - list all messages
	s.mcp.AddResource(mcp.NewResource(
		"msg2agent://inbox",
		"Inbox messages",
		mcp.WithResourceDescription("List of incoming messages from other agents"),
		mcp.WithMIMEType("application/json"),
	), s.inboxResourceHandler)

	// Tasks resource - list tracked tasks
	s.mcp.AddResource(mcp.NewResource(
		"msg2agent://tasks",
		"Tracked tasks",
		mcp.WithResourceDescription("List of A2A tasks being tracked"),
		mcp.WithMIMEType("application/json"),
	), s.tasksResourceHandler)

	// Resource templates for individual items
	s.mcp.AddResourceTemplate(mcp.NewResourceTemplate(
		"msg2agent://inbox/{id}",
		"Inbox message",
		mcp.WithTemplateDescription("Get a specific inbox message by ID"),
		mcp.WithTemplateMIMEType("application/json"),
	), s.inboxMessageResourceHandler)

	s.mcp.AddResourceTemplate(mcp.NewResourceTemplate(
		"msg2agent://tasks/{id}",
		"Task details",
		mcp.WithTemplateDescription("Get details of a specific tracked task"),
		mcp.WithTemplateMIMEType("application/json"),
	), s.taskResourceHandler)
}

// inboxResourceHandler handles the inbox resource request.
func (s *Server) inboxResourceHandler(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
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

// tasksResourceHandler handles the tasks resource request.
func (s *Server) tasksResourceHandler(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	tasks := s.getTrackedTasks()

	data, err := json.MarshalIndent(tasks, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal tasks: %w", err)
	}

	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      "msg2agent://tasks",
			MIMEType: "application/json",
			Text:     string(data),
		},
	}, nil
}

// inboxMessageResourceHandler handles individual inbox message resource requests.
func (s *Server) inboxMessageResourceHandler(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	// Extract ID from URI: msg2agent://inbox/{id}
	id := extractIDFromURI(request.Params.URI, "msg2agent://inbox/")
	if id == "" {
		return nil, fmt.Errorf("invalid message URI")
	}

	msg := s.inbox.Get(id)
	if msg == nil {
		return nil, fmt.Errorf("message not found: %s", id)
	}

	// Mark as read when accessed
	s.inbox.MarkRead(id)

	data, err := json.MarshalIndent(msg, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal message: %w", err)
	}

	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      request.Params.URI,
			MIMEType: "application/json",
			Text:     string(data),
		},
	}, nil
}

// taskResourceHandler handles individual task resource requests.
func (s *Server) taskResourceHandler(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	// Extract ID from URI: msg2agent://tasks/{id}
	id := extractIDFromURI(request.Params.URI, "msg2agent://tasks/")
	if id == "" {
		return nil, fmt.Errorf("invalid task URI")
	}

	s.tasksMu.RLock()
	task, ok := s.tasks[id]
	s.tasksMu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("task not found: %s", id)
	}

	data, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal task: %w", err)
	}

	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      request.Params.URI,
			MIMEType: "application/json",
			Text:     string(data),
		},
	}, nil
}

// extractIDFromURI extracts the ID from a resource URI with the given prefix.
func extractIDFromURI(uri, prefix string) string {
	if len(uri) <= len(prefix) {
		return ""
	}
	return uri[len(prefix):]
}
