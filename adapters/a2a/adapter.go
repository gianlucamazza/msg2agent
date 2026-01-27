// Package a2a provides A2A protocol compatibility.
package a2a

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/gianluca/msg2agent/pkg/messaging"
	"github.com/gianluca/msg2agent/pkg/registry"
)

// A2A protocol message types
const (
	A2AMessageSend          = "message/send"
	A2AMessageStream        = "message/stream"
	A2ATasksGet             = "tasks/get"
	A2ATasksList            = "tasks/list"
	A2ATasksCancel          = "tasks/cancel"
	A2ATasksResubscribe     = "tasks/resubscribe"
	A2AGetExtendedAgentCard = "agent/getExtendedAgentCard"
)

// AgentCard represents an A2A Agent Card.
type AgentCard struct {
	AgentID            string                    `json:"agentId"`
	Name               string                    `json:"name"`
	Description        string                    `json:"description,omitempty"`
	URL                string                    `json:"url"`
	Provider           *Provider                 `json:"provider,omitempty"`
	Version            string                    `json:"version"`
	ProtocolVersions   []string                  `json:"protocolVersions"`
	DocumentationURL   string                    `json:"documentationUrl,omitempty"`
	Capabilities       Capabilities              `json:"capabilities"`
	SecuritySchemes    map[string]SecurityScheme `json:"securitySchemes,omitempty"`
	Security           []map[string][]string     `json:"security,omitempty"`
	Authentication     *AuthConfig               `json:"authentication,omitempty"`
	DefaultInputModes  []string                  `json:"defaultInputModes,omitempty"`
	DefaultOutputModes []string                  `json:"defaultOutputModes,omitempty"`
	Skills             []Skill                   `json:"skills,omitempty"`
}

// Provider describes the agent provider.
type Provider struct {
	Organization string `json:"organization"`
	URL          string `json:"url,omitempty"`
}

// Capabilities describes agent capabilities.
type Capabilities struct {
	Streaming              bool `json:"streaming,omitempty"`
	PushNotifications      bool `json:"pushNotifications,omitempty"`
	StateTransitionHistory bool `json:"stateTransitionHistory,omitempty"`
}

// AuthConfig describes authentication configuration.
type AuthConfig struct {
	Schemes []string `json:"schemes"`
}

// SecurityScheme describes a security scheme per OpenAPI 3.0 spec.
type SecurityScheme struct {
	Type             string      `json:"type"` // oauth2, apiKey, http
	Description      string      `json:"description,omitempty"`
	Scheme           string      `json:"scheme,omitempty"`           // bearer (for type=http)
	BearerFormat     string      `json:"bearerFormat,omitempty"`     // JWT
	In               string      `json:"in,omitempty"`               // header, query (for apiKey)
	Name             string      `json:"name,omitempty"`             // header/query param name (for apiKey)
	Flows            *OAuthFlows `json:"flows,omitempty"`            // OAuth2 flows
	OpenIDConnectURL string      `json:"openIdConnectUrl,omitempty"` // OpenID Connect URL
}

// OAuthFlows describes OAuth2 flows.
type OAuthFlows struct {
	AuthorizationCode *OAuthFlow `json:"authorizationCode,omitempty"`
	Implicit          *OAuthFlow `json:"implicit,omitempty"`
	ClientCredentials *OAuthFlow `json:"clientCredentials,omitempty"`
	Password          *OAuthFlow `json:"password,omitempty"`
}

// OAuthFlow describes a single OAuth2 flow.
type OAuthFlow struct {
	AuthorizationURL string            `json:"authorizationUrl,omitempty"`
	TokenURL         string            `json:"tokenUrl,omitempty"`
	RefreshURL       string            `json:"refreshUrl,omitempty"`
	Scopes           map[string]string `json:"scopes"`
}

// Skill describes an agent skill.
type Skill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Examples    []string `json:"examples,omitempty"`
	InputModes  []string `json:"inputModes,omitempty"`
	OutputModes []string `json:"outputModes,omitempty"`
}

// Task represents an A2A task.
type Task struct {
	ID        string         `json:"id"`
	SessionID string         `json:"sessionId,omitempty"`
	Status    TaskStatus     `json:"status"`
	History   []TaskMessage  `json:"history,omitempty"`
	Artifacts []Artifact     `json:"artifacts,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// TaskStatus represents the status of a task.
type TaskStatus struct {
	State     string     `json:"state"`
	Message   *Message   `json:"message,omitempty"`
	Timestamp *time.Time `json:"timestamp,omitempty"`
}

// TaskMessage represents a message in task history.
type TaskMessage struct {
	Role     string         `json:"role"`
	Parts    []Part         `json:"parts"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// Message is the A2A message format.
type Message struct {
	Role     string         `json:"role"`
	Parts    []Part         `json:"parts"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// Part is a part of a message.
type Part struct {
	Type string         `json:"type"`
	Text string         `json:"text,omitempty"`
	File *FileData      `json:"file,omitempty"`
	Data map[string]any `json:"data,omitempty"`
}

// FileData represents file content.
type FileData struct {
	Name     string `json:"name,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	Bytes    []byte `json:"bytes,omitempty"`
	URI      string `json:"uri,omitempty"`
}

// Artifact represents a task artifact.
type Artifact struct {
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Index       int    `json:"index"`
	Parts       []Part `json:"parts"`
	Append      bool   `json:"append,omitempty"`
	LastChunk   bool   `json:"lastChunk,omitempty"`
}

// Adapter converts between msg2agent and A2A formats.
type Adapter struct{}

// isConversational determines if an A2A message should be treated as chat.
// Detection criteria:
// 1. metadata["type"] == "chat"
// 2. method prefix "chat."
// 3. role == "user" implies conversation
func isConversational(m *Message, method string) bool {
	// Check explicit type in metadata
	if m.Metadata != nil {
		if t, ok := m.Metadata["type"].(string); ok && t == "chat" {
			return true
		}
	}
	// Check method prefix
	if strings.HasPrefix(method, "chat.") {
		return true
	}
	// User role implies conversation
	if m.Role == "user" {
		return true
	}
	return false
}

// NewAdapter creates a new A2A adapter.
func NewAdapter() *Adapter {
	return &Adapter{}
}

// AgentCardConfig provides configuration for generating an AgentCard.
type AgentCardConfig struct {
	// BaseURL is the public URL where the agent is accessible
	BaseURL string

	// OAuth2 configuration (optional)
	OAuth2Enabled  bool
	OAuth2AuthURL  string
	OAuth2TokenURL string
	OAuth2Scopes   map[string]string

	// Provider information (optional)
	ProviderOrganization string
	ProviderURL          string

	// Documentation URL (optional)
	DocumentationURL string
}

// DefaultAgentCardConfig returns a config with Google OAuth2 defaults for Gemini Enterprise.
func DefaultAgentCardConfig() AgentCardConfig {
	return AgentCardConfig{
		OAuth2Enabled:  true,
		OAuth2AuthURL:  "https://accounts.google.com/o/oauth2/auth",
		OAuth2TokenURL: "https://oauth2.googleapis.com/token",
		OAuth2Scopes: map[string]string{
			"openid":  "OpenID Connect",
			"email":   "Email address",
			"profile": "User profile",
		},
	}
}

// ToA2AAgentCard converts a registry.Agent to an A2A AgentCard.
func (a *Adapter) ToA2AAgentCard(agent *registry.Agent) *AgentCard {
	return a.ToA2AAgentCardWithConfig(agent, AgentCardConfig{})
}

// ToA2AAgentCardWithConfig converts a registry.Agent to an A2A AgentCard with custom config.
func (a *Adapter) ToA2AAgentCardWithConfig(agent *registry.Agent, cfg AgentCardConfig) *AgentCard {
	card := &AgentCard{
		AgentID:          agent.DID,
		Name:             agent.DisplayName,
		Version:          "1.0.0",
		ProtocolVersions: []string{"1.0"},
		Capabilities: Capabilities{
			Streaming:              true,
			PushNotifications:      false,
			StateTransitionHistory: true,
		},
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
	}

	// Set URL from config or endpoint
	if cfg.BaseURL != "" {
		card.URL = cfg.BaseURL
	} else if len(agent.Endpoints) > 0 {
		card.URL = agent.Endpoints[0].URL
	}

	// Set documentation URL
	if cfg.DocumentationURL != "" {
		card.DocumentationURL = cfg.DocumentationURL
	}

	// Set provider if configured
	if cfg.ProviderOrganization != "" {
		card.Provider = &Provider{
			Organization: cfg.ProviderOrganization,
			URL:          cfg.ProviderURL,
		}
	}

	// Configure OAuth2 security scheme if enabled
	if cfg.OAuth2Enabled {
		card.SecuritySchemes = map[string]SecurityScheme{
			"oauth2": {
				Type:        "oauth2",
				Description: "OAuth 2.0 authorization code flow",
				Flows: &OAuthFlows{
					AuthorizationCode: &OAuthFlow{
						AuthorizationURL: cfg.OAuth2AuthURL,
						TokenURL:         cfg.OAuth2TokenURL,
						Scopes:           cfg.OAuth2Scopes,
					},
				},
			},
		}
		// Reference the security scheme
		card.Security = []map[string][]string{
			{"oauth2": {"openid"}},
		}
	}

	// Convert capabilities to skills
	for _, cap := range agent.Capabilities {
		skill := Skill{
			ID:          cap.Name,
			Name:        cap.Name,
			Description: cap.Description,
			InputModes:  []string{"text"},
			OutputModes: []string{"text"},
		}
		card.Skills = append(card.Skills, skill)
	}

	return card
}

// ToMessage converts a messaging.Message to an A2A Message.
func (a *Adapter) ToMessage(msg *messaging.Message) (*Message, error) {
	var body map[string]any
	if err := msg.ParseBody(&body); err != nil {
		body = map[string]any{"raw": string(msg.Body)}
	}

	// Convert body to text part
	text, _ := json.Marshal(body)

	metadata := map[string]any{
		"messageId": msg.ID.String(),
		"from":      msg.From,
		"method":    msg.Method,
	}

	// Preserve ThreadID in metadata for bidirectional mapping
	if msg.ThreadID != nil {
		metadata["thread_id"] = msg.ThreadID.String()
	}
	if msg.ParentID != nil {
		metadata["parent_id"] = msg.ParentID.String()
	}
	if msg.ThreadSeqNo > 0 {
		metadata["thread_seq_no"] = msg.ThreadSeqNo
	}

	// Include message type for downstream processing
	metadata["type"] = string(msg.Type)

	return &Message{
		Role: "agent",
		Parts: []Part{
			{Type: "text", Text: string(text)},
		},
		Metadata: metadata,
	}, nil
}

// FromMessageOptions contains optional parameters for FromMessage.
type FromMessageOptions struct {
	SessionID string // A2A session ID to map to ThreadID
}

// FromMessage converts an A2A Message to a messaging.Message.
// It automatically detects conversational messages and sets TypeChat.
func (a *Adapter) FromMessage(m *Message, from, to, method string) (*messaging.Message, error) {
	return a.FromMessageWithOptions(m, from, to, method, FromMessageOptions{})
}

// FromMessageWithOptions converts an A2A Message with additional options.
func (a *Adapter) FromMessageWithOptions(m *Message, from, to, method string, opts FromMessageOptions) (*messaging.Message, error) {
	// Determine message type based on content
	msgType := messaging.TypeRequest
	if isConversational(m, method) {
		msgType = messaging.TypeChat
	}

	msg := messaging.NewMessage(from, to, msgType, method)

	// Map SessionID to ThreadID if provided
	if opts.SessionID != "" {
		threadID := sessionToThreadID(opts.SessionID)
		msg.ThreadID = &threadID
	}

	// Extract text content
	var sb strings.Builder
	for _, part := range m.Parts {
		if part.Type == "text" {
			sb.WriteString(part.Text)
		}
	}
	text := sb.String()

	if err := msg.SetBody(map[string]any{
		"content": text,
		"role":    m.Role,
	}); err != nil {
		return nil, fmt.Errorf("failed to set message body: %w", err)
	}

	return msg, nil
}

// sessionToThreadID converts an A2A session ID to a UUID for ThreadID.
// If the sessionID is a valid UUID, it's used directly; otherwise, a
// deterministic UUID is generated from the session string.
func sessionToThreadID(sessionID string) uuid.UUID {
	// Try to parse as UUID first
	if id, err := uuid.Parse(sessionID); err == nil {
		return id
	}
	// Generate deterministic UUID from session string
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(sessionID))
}

// ToTask converts internal state to an A2A Task.
func (a *Adapter) ToTask(id string, state string, history []TaskMessage) *Task {
	return &Task{
		ID:      id,
		Status:  TaskStatus{State: state},
		History: history,
	}
}

// SendMessageParams are the params for message/send.
type SendMessageParams struct {
	ID            string         `json:"id"`
	SessionID     string         `json:"sessionId,omitempty"`
	Message       Message        `json:"message"`
	AcceptedModes []string       `json:"acceptedOutputModes,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

// SendMessageResult is the result of message/send.
type SendMessageResult struct {
	ID        string         `json:"id"`
	SessionID string         `json:"sessionId,omitempty"`
	Status    TaskStatus     `json:"status"`
	Artifacts []Artifact     `json:"artifacts,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// AgentCaller is an interface for calling remote agents.
// This abstracts the agent.Agent dependency for testing.
type AgentCaller interface {
	// DID returns the agent's DID.
	DID() string
	// Send sends a message to another agent and waits for response.
	Send(ctx context.Context, to, method string, params any) (*messaging.Message, error)
}

// Handler wraps an A2A-compatible handler.
type Handler struct {
	adapter *Adapter
	agent   AgentCaller
}

// HandlerOption configures a Handler.
type HandlerOption func(*Handler)

// WithAgent sets the agent for remote message routing.
func WithAgent(agent AgentCaller) HandlerOption {
	return func(h *Handler) {
		h.agent = agent
	}
}

// NewHandler creates a new A2A handler.
func NewHandler(opts ...HandlerOption) *Handler {
	h := &Handler{
		adapter: NewAdapter(),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// HandleSendMessage handles the message/send method.
// If an agent is configured and a recipient is specified, the message is
// forwarded to the recipient agent. Otherwise, returns a stub response.
func (h *Handler) HandleSendMessage(ctx context.Context, params *SendMessageParams) (*SendMessageResult, error) {
	// Generate task ID if not provided
	taskID := params.ID
	if taskID == "" {
		taskID = uuid.New().String()
	}

	// Generate session ID if not provided
	sessionID := params.SessionID
	if sessionID == "" {
		sessionID = uuid.New().String()
	}

	// Check if we need to route to a remote agent
	recipient := h.resolveRecipient(params)
	if recipient != "" && h.agent != nil {
		return h.handleRemoteMessage(ctx, taskID, sessionID, recipient, params)
	}

	// No remote routing - return basic acknowledgment
	// This is the stub behavior when no agent is configured
	return &SendMessageResult{
		ID:        taskID,
		SessionID: sessionID,
		Status: TaskStatus{
			State: TaskStateCompleted,
		},
	}, nil
}

// handleRemoteMessage routes a message to a remote agent.
func (h *Handler) handleRemoteMessage(ctx context.Context, taskID, sessionID, recipient string, params *SendMessageParams) (*SendMessageResult, error) {
	// Determine the method to call
	method := h.resolveMethod(params)

	// Convert A2A message to internal format with session context
	opts := FromMessageOptions{SessionID: sessionID}
	internalMsg, err := h.adapter.FromMessageWithOptions(&params.Message, h.agent.DID(), recipient, method, opts)
	if err != nil {
		return h.errorResult(taskID, sessionID, "failed to convert message: "+err.Error())
	}

	// Add A2A metadata to the message body
	var body map[string]any
	if err := internalMsg.ParseBody(&body); err != nil {
		body = make(map[string]any)
	}
	body["a2a_task_id"] = taskID
	body["a2a_session_id"] = sessionID
	if params.Metadata != nil {
		body["a2a_metadata"] = params.Metadata
	}
	if err := internalMsg.SetBody(body); err != nil {
		return h.errorResult(taskID, sessionID, "failed to set message body: "+err.Error())
	}

	// Send via agent
	response, err := h.agent.Send(ctx, recipient, method, body)
	if err != nil {
		return h.errorResult(taskID, sessionID, "failed to send message: "+err.Error())
	}

	// Convert response to A2A format
	return h.buildSuccessResult(taskID, sessionID, response)
}

// resolveRecipient determines the recipient agent from the params.
func (h *Handler) resolveRecipient(params *SendMessageParams) string {
	// Check metadata for explicit recipient
	if params.Metadata != nil {
		if recipient, ok := params.Metadata["recipient"].(string); ok && recipient != "" {
			return recipient
		}
		if to, ok := params.Metadata["to"].(string); ok && to != "" {
			return to
		}
	}
	return ""
}

// resolveMethod determines the method to call from the params.
func (h *Handler) resolveMethod(params *SendMessageParams) string {
	// Check metadata for explicit method
	if params.Metadata != nil {
		if method, ok := params.Metadata["method"].(string); ok && method != "" {
			return method
		}
	}
	// Default method for A2A messages
	return "a2a.message"
}

// errorResult creates an error result.
func (h *Handler) errorResult(taskID, sessionID, errMsg string) (*SendMessageResult, error) {
	return &SendMessageResult{
		ID:        taskID,
		SessionID: sessionID,
		Status: TaskStatus{
			State: TaskStateFailed,
			Message: &Message{
				Role:  "agent",
				Parts: []Part{{Type: "text", Text: errMsg}},
			},
		},
	}, nil
}

// buildSuccessResult converts an internal message response to A2A format.
func (h *Handler) buildSuccessResult(taskID, sessionID string, response *messaging.Message) (*SendMessageResult, error) {
	// Convert internal message to A2A format
	a2aMsg, err := h.adapter.ToMessage(response)
	if err != nil {
		return h.errorResult(taskID, sessionID, "failed to convert response: "+err.Error())
	}

	return &SendMessageResult{
		ID:        taskID,
		SessionID: sessionID,
		Status: TaskStatus{
			State:   TaskStateCompleted,
			Message: a2aMsg,
		},
	}, nil
}
