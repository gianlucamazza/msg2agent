package a2a

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/gianluca/msg2agent/pkg/conversation"
	"github.com/gianluca/msg2agent/pkg/registry"
)

// AgentBridge bridges an A2A server with the agent's internal handlers.
type AgentBridge struct {
	methods map[string]MethodHandler
	store   registry.Store
	adapter *Adapter
}

// MethodHandler is a function that handles an A2A method call.
type MethodHandler func(ctx context.Context, params json.RawMessage) (any, error)

// NewAgentBridge creates a new bridge for integrating A2A with an agent.
func NewAgentBridge(store registry.Store) *AgentBridge {
	return &AgentBridge{
		methods: make(map[string]MethodHandler),
		store:   store,
		adapter: NewAdapter(),
	}
}

// RegisterMethod registers a method handler.
func (b *AgentBridge) RegisterMethod(name string, handler MethodHandler) {
	b.methods[name] = handler
}

// TaskHandler returns a TaskHandler suitable for use with A2A Server.
func (b *AgentBridge) TaskHandler() TaskHandler {
	return func(ctx context.Context, task *Task, message *Message) (*Message, []Artifact, error) {
		// Extract method from metadata or use default
		method := "default"
		if task.Metadata != nil {
			if m, ok := task.Metadata["method"].(string); ok {
				method = m
			}
		}

		// Find handler
		handler, exists := b.methods[method]
		if !exists {
			handler, exists = b.methods["default"]
		}
		if !exists {
			return nil, nil, fmt.Errorf("no handler for method: %s", method)
		}

		// Convert A2A message to params
		params, _ := json.Marshal(map[string]any{
			"message": message,
			"task":    task,
		})

		// Call handler
		result, err := handler(ctx, params)
		if err != nil {
			return nil, nil, err
		}

		// Convert result to A2A response
		return b.resultToMessage(result)
	}
}

// resultToMessage converts a handler result to an A2A Message and optional Artifacts.
func (b *AgentBridge) resultToMessage(result any) (*Message, []Artifact, error) {
	switch v := result.(type) {
	case *Message:
		return v, nil, nil
	case Message:
		return &v, nil, nil
	case string:
		return &Message{
			Role:  "agent",
			Parts: []Part{{Type: "text", Text: v}},
		}, nil, nil
	case map[string]any:
		// Check for special response format
		if msg, ok := v["message"].(*Message); ok {
			var artifacts []Artifact
			if a, ok := v["artifacts"].([]Artifact); ok {
				artifacts = a
			}
			return msg, artifacts, nil
		}
		// Convert to JSON text
		text, _ := json.Marshal(v)
		return &Message{
			Role:  "agent",
			Parts: []Part{{Type: "text", Text: string(text)}},
		}, nil, nil
	default:
		text, _ := json.Marshal(result)
		return &Message{
			Role:  "agent",
			Parts: []Part{{Type: "text", Text: string(text)}},
		}, nil, nil
	}
}

// CreateA2AServer creates a fully configured A2A server using the bridge.
func (b *AgentBridge) CreateA2AServer() *Server {
	return NewServer(b.TaskHandler())
}

// TextResponse creates a simple text response message.
func TextResponse(text string) *Message {
	return &Message{
		Role:  "agent",
		Parts: []Part{{Type: "text", Text: text}},
	}
}

// TextArtifact creates a text artifact.
func TextArtifact(name, content string) Artifact {
	return Artifact{
		Name: name,
		Parts: []Part{
			{Type: "text", Text: content},
		},
	}
}

// FileArtifact creates a file artifact.
func FileArtifact(name, mimeType string, data []byte) Artifact {
	return Artifact{
		Name: name,
		Parts: []Part{
			{Type: "file", File: &FileData{
				Name:     name,
				MimeType: mimeType,
				Bytes:    data,
			}},
		},
	}
}

// ExtractTextContent extracts all text content from an A2A message.
func ExtractTextContent(msg *Message) string {
	var text string
	for _, part := range msg.Parts {
		if part.Type == "text" {
			text += part.Text
		}
	}
	return text
}

// ExtractFiles extracts all files from an A2A message.
func ExtractFiles(msg *Message) []*FileData {
	var files []*FileData
	for _, part := range msg.Parts {
		if part.Type == "file" && part.File != nil {
			files = append(files, part.File)
		}
	}
	return files
}

// NewTextMessage creates a message with text content.
func NewTextMessage(role, text string) *Message {
	return &Message{
		Role:  role,
		Parts: []Part{{Type: "text", Text: text}},
	}
}

// NewFileMessage creates a message with file content.
func NewFileMessage(role string, file *FileData) *Message {
	return &Message{
		Role:  role,
		Parts: []Part{{Type: "file", File: file}},
	}
}

// WithArtifacts is a helper to return a message with artifacts.
func WithArtifacts(msg *Message, artifacts ...Artifact) (*Message, []Artifact, error) {
	return msg, artifacts, nil
}

// AgentRouter wraps an MessageSender to provide A2A TaskHandler functionality.
// It can route messages to remote agents via the agent system while also
// supporting local method handlers.
type AgentRouter struct {
	agent      MessageSender
	bridge     *AgentBridge
	adapter    *Adapter
	a2aHandler *Handler
}

// AgentRouterOption configures an AgentRouter.
type AgentRouterOption func(*AgentRouter)

// WithRouterLocalHandlers adds local method handlers via an AgentBridge.
func WithRouterLocalHandlers(bridge *AgentBridge) AgentRouterOption {
	return func(r *AgentRouter) {
		r.bridge = bridge
	}
}

// NewAgentRouter creates a new AgentRouter that routes A2A messages
// through the agent system.
func NewAgentRouter(agent MessageSender, opts ...AgentRouterOption) *AgentRouter {
	r := &AgentRouter{
		agent:      agent,
		adapter:    NewAdapter(),
		a2aHandler: NewHandler(WithAgent(agent)),
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// TaskHandler returns a TaskHandler suitable for use with A2A Server.
// It routes messages to remote agents when a recipient is specified,
// or falls back to local handlers otherwise.
func (r *AgentRouter) TaskHandler() TaskHandler {
	return func(ctx context.Context, task *Task, message *Message) (*Message, []Artifact, error) {
		// Check for recipient in task metadata
		recipient := r.resolveRecipient(task)

		if recipient != "" {
			// Route to remote agent
			return r.handleRemote(ctx, task, message, recipient)
		}

		// Fall back to local handlers if configured
		if r.bridge != nil {
			localHandler := r.bridge.TaskHandler()
			return localHandler(ctx, task, message)
		}

		// No recipient and no local handlers - return error
		return nil, nil, fmt.Errorf("no recipient specified and no local handlers configured")
	}
}

// handleRemote routes a message to a remote agent.
func (r *AgentRouter) handleRemote(ctx context.Context, task *Task, message *Message, recipient string) (*Message, []Artifact, error) {
	// Determine method
	method := "a2a.message"
	if task.Metadata != nil {
		if m, ok := task.Metadata["method"].(string); ok && m != "" {
			method = m
		}
	}

	// Build params for the remote call
	params := map[string]any{
		"task_id":    task.ID,
		"session_id": task.SessionID,
		"message":    message,
		"metadata":   task.Metadata,
	}

	// Extract text content for simpler remote handling
	if text := ExtractTextContent(message); text != "" {
		params["content"] = text
	}

	// Send via agent
	response, err := r.agent.Send(ctx, recipient, method, params)
	if err != nil {
		return nil, nil, fmt.Errorf("remote call failed: %w", err)
	}

	// Convert response to A2A format
	a2aMsg, err := r.adapter.ToMessage(response)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to convert response: %w", err)
	}

	return a2aMsg, nil, nil
}

// resolveRecipient extracts the recipient from task metadata.
func (r *AgentRouter) resolveRecipient(task *Task) string {
	if task.Metadata == nil {
		return ""
	}

	if recipient, ok := task.Metadata["recipient"].(string); ok && recipient != "" {
		return recipient
	}
	if to, ok := task.Metadata["to"].(string); ok && to != "" {
		return to
	}
	return ""
}

// CreateServer creates an A2A Server configured with this router.
func (r *AgentRouter) CreateServer() *Server {
	return NewServer(r.TaskHandler())
}

// Handler returns the underlying Handler for direct message/send handling.
func (r *AgentRouter) Handler() *Handler {
	return r.a2aHandler
}

// State mapping between A2A Task states and conversation Thread states.

// TaskStateToThreadState converts an A2A task state to a thread state.
func TaskStateToThreadState(taskState string) conversation.ThreadState {
	switch taskState {
	case TaskStateSubmitted, TaskStateWorking:
		return conversation.ThreadStateActive
	case TaskStateInputRequired:
		return conversation.ThreadStateAwaitingInput
	case TaskStateCompleted:
		return conversation.ThreadStateCompleted
	case TaskStateFailed, TaskStateCanceled:
		return conversation.ThreadStateFailed
	default:
		return conversation.ThreadStateActive
	}
}

// ThreadStateToTaskState converts a thread state to an A2A task state.
func ThreadStateToTaskState(threadState conversation.ThreadState) string {
	switch threadState {
	case conversation.ThreadStateActive:
		return TaskStateWorking
	case conversation.ThreadStateAwaitingInput:
		return TaskStateInputRequired
	case conversation.ThreadStateCompleted:
		return TaskStateCompleted
	case conversation.ThreadStateFailed:
		return TaskStateFailed
	default:
		return TaskStateWorking
	}
}

// SyncThreadWithTask updates a thread's state based on an A2A task.
func SyncThreadWithTask(thread *conversation.Thread, task *Task) {
	// Update thread state
	thread.State = TaskStateToThreadState(task.Status.State)
	thread.UpdatedAt = *task.Status.Timestamp

	// Track task ID if not already present
	if task.ID != "" && !slices.Contains(thread.TaskIDs, task.ID) {
		thread.TaskIDs = append(thread.TaskIDs, task.ID)
	}

	// Track session ID if not already present
	if task.SessionID != "" && !slices.Contains(thread.SessionIDs, task.SessionID) {
		thread.SessionIDs = append(thread.SessionIDs, task.SessionID)
	}
}

// SyncTaskWithThread updates an A2A task's state based on a thread.
func SyncTaskWithThread(task *Task, thread *conversation.Thread) {
	// Update task state from thread
	newState := ThreadStateToTaskState(thread.State)
	if task.Status.State != newState {
		task.Status.State = newState
		now := thread.UpdatedAt
		task.Status.Timestamp = &now
	}

	// Add thread metadata to task
	if task.Metadata == nil {
		task.Metadata = make(map[string]any)
	}
	task.Metadata["thread_id"] = thread.ID.String()
}
