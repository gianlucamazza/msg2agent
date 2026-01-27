// Package a2a provides A2A protocol compatibility.
package a2a

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/gianluca/msg2agent/pkg/protocol"
)

// Task states per A2A protocol specification.
const (
	TaskStateSubmitted     = "submitted"
	TaskStateWorking       = "working"
	TaskStateInputRequired = "input-required"
	TaskStateCompleted     = "completed"
	TaskStateFailed        = "failed"
	TaskStateCanceled      = "canceled"
)

// Default configuration values for TaskStore.
const (
	DefaultTaskTTL            = 24 * time.Hour  // Tasks expire after 24 hours
	DefaultCleanupPeriod      = 5 * time.Minute // Cleanup runs every 5 minutes
	DefaultMaxHistoryLen      = 100             // Max messages in task history
	DefaultMaxTasksPerSession = 1000            // Max tasks per session
)

// Error codes for A2A protocol.
const (
	ErrCodeTaskNotFound      = -32001
	ErrCodeTaskNotCancelable = -32002
	ErrCodeInvalidRequest    = -32600
	ErrCodeMethodNotFound    = -32601
	ErrCodeInvalidParams     = -32602
	ErrCodeInternalError     = -32603
)

// Task and session errors.
var (
	ErrTaskNotFound      = errors.New("task not found")
	ErrTaskNotCancelable = errors.New("task cannot be canceled")
	ErrSessionNotFound   = errors.New("session not found")
)

// TaskStoreConfig configures the TaskStore.
type TaskStoreConfig struct {
	TaskTTL            time.Duration // TTL for tasks (default: 24h)
	CleanupPeriod      time.Duration // How often to run cleanup (default: 5m)
	MaxHistoryLen      int           // Max messages in task history (default: 100)
	MaxTasksPerSession int           // Max tasks per session (default: 1000)
}

// DefaultTaskStoreConfig returns the default configuration.
func DefaultTaskStoreConfig() TaskStoreConfig {
	return TaskStoreConfig{
		TaskTTL:            DefaultTaskTTL,
		CleanupPeriod:      DefaultCleanupPeriod,
		MaxHistoryLen:      DefaultMaxHistoryLen,
		MaxTasksPerSession: DefaultMaxTasksPerSession,
	}
}

// TaskStore manages A2A tasks with TTL and automatic cleanup.
type TaskStore struct {
	tasks    map[string]*Task
	sessions map[string]*Session
	expiry   map[string]time.Time // task ID -> expiry time
	mu       sync.RWMutex

	config    TaskStoreConfig
	stopCh    chan struct{}
	stoppedCh chan struct{}
}

// ParticipantPresence tracks presence info for a session participant.
type ParticipantPresence struct {
	DID      string    `json:"did"`
	Status   string    `json:"status"` // "online", "offline", "away", "busy", "dnd"
	LastSeen time.Time `json:"last_seen"`
}

// Session represents a conversation session.
type Session struct {
	ID             string
	Tasks          []string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	LastActivityAt time.Time                       // Last activity timestamp
	Participants   map[string]*ParticipantPresence // DID -> presence
	Metadata       map[string]any
}

// NewTaskStore creates a new task store with default configuration.
func NewTaskStore() *TaskStore {
	return NewTaskStoreWithConfig(DefaultTaskStoreConfig())
}

// NewTaskStoreWithConfig creates a new task store with custom configuration.
func NewTaskStoreWithConfig(cfg TaskStoreConfig) *TaskStore {
	s := &TaskStore{
		tasks:     make(map[string]*Task),
		sessions:  make(map[string]*Session),
		expiry:    make(map[string]time.Time),
		config:    cfg,
		stopCh:    make(chan struct{}),
		stoppedCh: make(chan struct{}),
	}
	go s.cleanupLoop()
	return s
}

// Stop stops the cleanup goroutine and waits for it to finish.
func (s *TaskStore) Stop() {
	close(s.stopCh)
	<-s.stoppedCh
}

// cleanupLoop periodically removes expired tasks.
func (s *TaskStore) cleanupLoop() {
	defer close(s.stoppedCh)

	ticker := time.NewTicker(s.config.CleanupPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.cleanExpired()
		case <-s.stopCh:
			return
		}
	}
}

// cleanExpired removes tasks that have exceeded their TTL.
func (s *TaskStore) cleanExpired() {
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	for id, expiryTime := range s.expiry {
		if now.After(expiryTime) {
			// Remove task
			if task, ok := s.tasks[id]; ok {
				// Remove from session
				if session, ok := s.sessions[task.SessionID]; ok {
					session.Tasks = removeString(session.Tasks, id)
				}
				delete(s.tasks, id)
			}
			delete(s.expiry, id)
		}
	}

	// Also clean up empty sessions
	for id, session := range s.sessions {
		if len(session.Tasks) == 0 {
			delete(s.sessions, id)
		}
	}
}

// removeString removes a string from a slice.
func removeString(slice []string, s string) []string {
	for i, v := range slice {
		if v == s {
			return append(slice[:i], slice[i+1:]...)
		}
	}
	return slice
}

// TaskCount returns the number of tasks in the store.
func (s *TaskStore) TaskCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.tasks)
}

// SessionCount returns the number of sessions in the store.
func (s *TaskStore) SessionCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.sessions)
}

// CreateTask creates a new task.
func (s *TaskStore) CreateTask(sessionID string, message *Message) *Task {
	s.mu.Lock()
	defer s.mu.Unlock()

	taskID := uuid.New().String()
	now := time.Now()

	task := &Task{
		ID:        taskID,
		SessionID: sessionID,
		Status: TaskStatus{
			State:     TaskStateSubmitted,
			Timestamp: &now,
		},
		History: []TaskMessage{
			{
				Role:  message.Role,
				Parts: message.Parts,
			},
		},
		Metadata: make(map[string]any),
	}

	s.tasks[taskID] = task
	s.expiry[taskID] = now.Add(s.config.TaskTTL)

	// Update or create session
	if sessionID != "" {
		if session, ok := s.sessions[sessionID]; ok {
			// Enforce max tasks per session
			if len(session.Tasks) >= s.config.MaxTasksPerSession {
				// Remove oldest task
				if len(session.Tasks) > 0 {
					oldestID := session.Tasks[0]
					delete(s.tasks, oldestID)
					delete(s.expiry, oldestID)
					session.Tasks = session.Tasks[1:]
				}
			}
			session.Tasks = append(session.Tasks, taskID)
			session.UpdatedAt = now
			session.LastActivityAt = now
		} else {
			s.sessions[sessionID] = &Session{
				ID:             sessionID,
				Tasks:          []string{taskID},
				CreatedAt:      now,
				UpdatedAt:      now,
				LastActivityAt: now,
				Participants:   make(map[string]*ParticipantPresence),
				Metadata:       make(map[string]any),
			}
		}
	}

	return task
}

// GetTask retrieves a task by ID.
func (s *TaskStore) GetTask(id string) (*Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	task, ok := s.tasks[id]
	if !ok {
		return nil, ErrTaskNotFound
	}
	return task, nil
}

// TaskListFilter defines filters for listing tasks.
type TaskListFilter struct {
	SessionID string   // Filter by session ID (contextId in A2A spec)
	Status    []string // Filter by status (state)
}

// TaskListResult contains the result of listing tasks.
type TaskListResult struct {
	Tasks      []*Task `json:"tasks"`
	NextCursor string  `json:"nextCursor,omitempty"`
	Total      int     `json:"total"`
}

// ListTasks returns tasks matching the filter with pagination.
func (s *TaskStore) ListTasks(filter TaskListFilter, cursor string, limit int) (*TaskListResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}

	// Collect matching tasks
	var matching []*Task
	for _, task := range s.tasks {
		// Apply session filter
		if filter.SessionID != "" && task.SessionID != filter.SessionID {
			continue
		}

		// Apply status filter
		if len(filter.Status) > 0 {
			found := false
			for _, status := range filter.Status {
				if task.Status.State == status {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		matching = append(matching, task)
	}

	// Sort by creation (task ID contains timestamp from UUIDv7)
	// For simplicity, sort by ID string which gives chronological order for UUIDs
	sortTasksByID(matching)

	// Apply cursor-based pagination
	startIdx := 0
	if cursor != "" {
		for i, task := range matching {
			if task.ID == cursor {
				startIdx = i + 1
				break
			}
		}
	}

	// Slice results
	endIdx := startIdx + limit
	if endIdx > len(matching) {
		endIdx = len(matching)
	}

	result := &TaskListResult{
		Tasks: matching[startIdx:endIdx],
		Total: len(matching),
	}

	// Set next cursor if there are more results
	if endIdx < len(matching) {
		result.NextCursor = matching[endIdx-1].ID
	}

	return result, nil
}

// sortTasksByID sorts tasks by ID (chronological for UUIDs).
func sortTasksByID(tasks []*Task) {
	for i := 1; i < len(tasks); i++ {
		for j := i; j > 0 && tasks[j].ID < tasks[j-1].ID; j-- {
			tasks[j], tasks[j-1] = tasks[j-1], tasks[j]
		}
	}
}

// UpdateTaskStatus updates a task's status.
// Refreshes the task TTL on each update.
func (s *TaskStore) UpdateTaskStatus(id string, state string, message *Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.tasks[id]
	if !ok {
		return ErrTaskNotFound
	}

	now := time.Now()
	task.Status = TaskStatus{
		State:     state,
		Message:   message,
		Timestamp: &now,
	}

	// Refresh TTL on status update
	s.expiry[id] = now.Add(s.config.TaskTTL)

	return nil
}

// AddTaskMessage adds a message to task history.
// If history exceeds MaxHistoryLen, oldest messages are removed.
func (s *TaskStore) AddTaskMessage(id string, msg TaskMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.tasks[id]
	if !ok {
		return ErrTaskNotFound
	}

	task.History = append(task.History, msg)

	// Enforce history limit
	if len(task.History) > s.config.MaxHistoryLen {
		// Keep the last MaxHistoryLen messages
		task.History = task.History[len(task.History)-s.config.MaxHistoryLen:]
	}

	return nil
}

// AddTaskArtifact adds an artifact to a task.
func (s *TaskStore) AddTaskArtifact(id string, artifact Artifact) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.tasks[id]
	if !ok {
		return ErrTaskNotFound
	}

	task.Artifacts = append(task.Artifacts, artifact)
	return nil
}

// CancelTask cancels a task if it's in a cancelable state.
func (s *TaskStore) CancelTask(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.tasks[id]
	if !ok {
		return ErrTaskNotFound
	}

	// Only certain states can be canceled
	switch task.Status.State {
	case TaskStateSubmitted, TaskStateWorking, TaskStateInputRequired:
		now := time.Now()
		task.Status = TaskStatus{
			State:     TaskStateCanceled,
			Timestamp: &now,
		}
		return nil
	default:
		return ErrTaskNotCancelable
	}
}

// GetSession retrieves a session by ID.
func (s *TaskStore) GetSession(id string) (*Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	session, ok := s.sessions[id]
	if !ok {
		return nil, ErrSessionNotFound
	}
	return session, nil
}

// UpdateParticipantPresence updates presence for a participant in a session.
func (s *TaskStore) UpdateParticipantPresence(sessionID, did, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.sessions[sessionID]
	if !ok {
		return ErrSessionNotFound
	}

	now := time.Now()
	session.Participants[did] = &ParticipantPresence{
		DID:      did,
		Status:   status,
		LastSeen: now,
	}
	session.LastActivityAt = now
	return nil
}

// GetParticipantPresence gets presence info for a participant in a session.
func (s *TaskStore) GetParticipantPresence(sessionID, did string) (*ParticipantPresence, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	session, ok := s.sessions[sessionID]
	if !ok {
		return nil, ErrSessionNotFound
	}

	presence, ok := session.Participants[did]
	if !ok {
		return nil, nil // Not found, but not an error
	}
	return presence, nil
}

// GetSessionParticipants returns all participants and their presence in a session.
func (s *TaskStore) GetSessionParticipants(sessionID string) (map[string]*ParticipantPresence, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	session, ok := s.sessions[sessionID]
	if !ok {
		return nil, ErrSessionNotFound
	}

	// Return a copy to avoid data races
	result := make(map[string]*ParticipantPresence, len(session.Participants))
	for k, v := range session.Participants {
		copied := *v
		result[k] = &copied
	}
	return result, nil
}

// TouchSession updates the LastActivityAt for a session.
func (s *TaskStore) TouchSession(sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.sessions[sessionID]
	if !ok {
		return ErrSessionNotFound
	}

	session.LastActivityAt = time.Now()
	return nil
}

// Server is an A2A-compatible JSON-RPC server.
type Server struct {
	store     *TaskStore
	adapter   *Adapter
	handler   TaskHandler
	streams   map[string]chan *StreamEvent
	streamsMu sync.RWMutex
}

// TaskHandler processes task messages and returns results.
type TaskHandler func(ctx context.Context, task *Task, message *Message) (*Message, []Artifact, error)

// StreamEvent represents an event in a streaming response.
type StreamEvent struct {
	Type     string    `json:"type"` // "status", "artifact", "message"
	Task     *Task     `json:"task,omitempty"`
	Artifact *Artifact `json:"artifact,omitempty"`
	Message  *Message  `json:"message,omitempty"`
	Final    bool      `json:"final,omitempty"`
}

// NewServer creates a new A2A server.
func NewServer(handler TaskHandler) *Server {
	return &Server{
		store:   NewTaskStore(),
		adapter: NewAdapter(),
		handler: handler,
		streams: make(map[string]chan *StreamEvent),
	}
}

// HandleRequest processes an A2A JSON-RPC request.
func (s *Server) HandleRequest(ctx context.Context, data []byte) ([]byte, error) {
	req, err := protocol.DecodeRequest(data)
	if err != nil {
		return s.errorResponse(nil, ErrCodeInvalidRequest, "invalid request")
	}

	var result any
	var handleErr error

	switch req.Method {
	case A2AMessageSend:
		result, handleErr = s.handleMessageSend(ctx, req.Params)
	case A2ATasksGet:
		result, handleErr = s.handleTasksGet(ctx, req.Params)
	case A2ATasksList:
		result, handleErr = s.handleTasksList(ctx, req.Params)
	case A2ATasksCancel:
		result, handleErr = s.handleTasksCancel(ctx, req.Params)
	case A2ATasksResubscribe:
		result, handleErr = s.handleTasksResubscribe(ctx, req.Params)
	case A2AGetExtendedAgentCard:
		result, handleErr = s.handleGetExtendedAgentCard(ctx, req.Params)
	default:
		return s.errorResponse(req.ID, ErrCodeMethodNotFound, "method not found: "+req.Method)
	}

	if handleErr != nil {
		return s.errorResponse(req.ID, ErrCodeInternalError, handleErr.Error())
	}

	resp, _ := protocol.NewResponse(req.ID, result)
	return protocol.Encode(resp)
}

// handleMessageSend processes a message/send request.
func (s *Server) handleMessageSend(ctx context.Context, params json.RawMessage) (*SendMessageResult, error) {
	var p SendMessageParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}

	// Create or get session ID
	sessionID := p.SessionID
	if sessionID == "" {
		sessionID = uuid.New().String()
	}

	// Create task
	task := s.store.CreateTask(sessionID, &p.Message)

	// Copy params metadata to task metadata
	if p.Metadata != nil {
		maps.Copy(task.Metadata, p.Metadata)
	}

	// Propagate thread_id from message metadata if present
	if p.Message.Metadata != nil {
		if threadID, ok := p.Message.Metadata["thread_id"].(string); ok {
			task.Metadata["thread_id"] = threadID
		}
	}

	// Update to working state
	_ = s.store.UpdateTaskStatus(task.ID, TaskStateWorking, nil) // Best effort

	// Process the message
	if s.handler != nil {
		response, artifacts, err := s.handler(ctx, task, &p.Message)
		if err != nil {
			_ = s.CompleteTask(task.ID, TaskStateFailed, &Message{
				Role:  "agent",
				Parts: []Part{{Type: "text", Text: err.Error()}},
			})
			return s.buildResult(task)
		}

		// Add response to history
		if response != nil {
			_ = s.store.AddTaskMessage(task.ID, TaskMessage{
				Role:  response.Role,
				Parts: response.Parts,
			})
		}

		// Add artifacts
		for _, artifact := range artifacts {
			_ = s.store.AddTaskArtifact(task.ID, artifact)
		}

		// Mark as completed (auto-closes stream)
		_ = s.CompleteTask(task.ID, TaskStateCompleted, response)
	} else {
		// No handler, just mark as completed
		_ = s.CompleteTask(task.ID, TaskStateCompleted, nil)
	}

	return s.buildResult(task)
}

// handleTasksGet processes a tasks/get request.
func (s *Server) handleTasksGet(_ context.Context, params json.RawMessage) (*TaskGetResult, error) {
	var p TaskGetParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}

	task, err := s.store.GetTask(p.ID)
	if err != nil {
		return nil, err
	}

	return &TaskGetResult{
		ID:        task.ID,
		SessionID: task.SessionID,
		Status:    task.Status,
		History:   task.History,
		Artifacts: task.Artifacts,
		Metadata:  task.Metadata,
	}, nil
}

// handleTasksCancel processes a tasks/cancel request.
func (s *Server) handleTasksCancel(_ context.Context, params json.RawMessage) (*TaskCancelResult, error) {
	var p TaskCancelParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}

	if err := s.store.CancelTask(p.ID); err != nil {
		return nil, err
	}

	// Auto-close stream on cancel
	s.CloseStream(p.ID)

	task, _ := s.store.GetTask(p.ID)
	return &TaskCancelResult{
		ID:     task.ID,
		Status: task.Status,
	}, nil
}

// TasksListParams are the params for tasks/list.
type TasksListParams struct {
	ContextID string   `json:"contextId,omitempty"` // A2A uses contextId (maps to sessionId)
	SessionID string   `json:"sessionId,omitempty"` // Alternative name
	Status    []string `json:"status,omitempty"`    // Filter by status
	Cursor    string   `json:"cursor,omitempty"`    // Pagination cursor
	Limit     int      `json:"limit,omitempty"`     // Max results (default 10, max 100)
}

// TasksListResponse is the result of tasks/list.
type TasksListResponse struct {
	Tasks      []TaskSummary `json:"tasks"`
	NextCursor string        `json:"nextCursor,omitempty"`
	Total      int           `json:"total"`
}

// TaskSummary is a brief summary of a task for listing.
type TaskSummary struct {
	ID        string     `json:"id"`
	SessionID string     `json:"sessionId,omitempty"`
	Status    TaskStatus `json:"status"`
}

// handleTasksList processes a tasks/list request.
func (s *Server) handleTasksList(_ context.Context, params json.RawMessage) (*TasksListResponse, error) {
	var p TasksListParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}

	// Use contextId if provided, fallback to sessionId
	sessionID := p.ContextID
	if sessionID == "" {
		sessionID = p.SessionID
	}

	filter := TaskListFilter{
		SessionID: sessionID,
		Status:    p.Status,
	}

	result, err := s.store.ListTasks(filter, p.Cursor, p.Limit)
	if err != nil {
		return nil, err
	}

	// Convert to response format with summaries
	response := &TasksListResponse{
		Tasks:      make([]TaskSummary, len(result.Tasks)),
		NextCursor: result.NextCursor,
		Total:      result.Total,
	}

	for i, task := range result.Tasks {
		response.Tasks[i] = TaskSummary{
			ID:        task.ID,
			SessionID: task.SessionID,
			Status:    task.Status,
		}
	}

	return response, nil
}

// ExtendedAgentCardParams are the params for agent/getExtendedAgentCard.
type ExtendedAgentCardParams struct {
	// No params needed for now
}

// ExtendedAgentCardResponse is the result of agent/getExtendedAgentCard.
type ExtendedAgentCardResponse struct {
	AgentCard
	AuthenticationRequired bool `json:"authenticationRequired"`
}

// handleGetExtendedAgentCard processes agent/getExtendedAgentCard request.
func (s *Server) handleGetExtendedAgentCard(_ context.Context, _ json.RawMessage) (*ExtendedAgentCardResponse, error) {
	// Build extended agent card
	// Note: In production, this would get the actual agent info from config
	card := &ExtendedAgentCardResponse{
		AgentCard: AgentCard{
			Name:             "A2A Agent",
			Version:          "1.0.0",
			ProtocolVersions: []string{"1.0"},
			Capabilities: Capabilities{
				Streaming:              true,
				PushNotifications:      false,
				StateTransitionHistory: true,
			},
			DefaultInputModes:  []string{"text"},
			DefaultOutputModes: []string{"text"},
		},
		AuthenticationRequired: true,
	}

	return card, nil
}

// StartStream starts a streaming response for a task.
func (s *Server) StartStream(taskID string) <-chan *StreamEvent {
	s.streamsMu.Lock()
	defer s.streamsMu.Unlock()

	ch := make(chan *StreamEvent, 100)
	s.streams[taskID] = ch
	return ch
}

// SendStreamEvent sends an event to a task's stream.
func (s *Server) SendStreamEvent(taskID string, event *StreamEvent) {
	s.streamsMu.RLock()
	ch, ok := s.streams[taskID]
	s.streamsMu.RUnlock()

	if ok {
		select {
		case ch <- event:
		default:
			// Drop if channel is full
		}
	}
}

// CloseStream closes a task's stream.
func (s *Server) CloseStream(taskID string) {
	s.streamsMu.Lock()
	defer s.streamsMu.Unlock()

	if ch, ok := s.streams[taskID]; ok {
		close(ch)
		delete(s.streams, taskID)
	}
}

// IsTerminalState returns true if the task state is terminal (no further updates expected).
func IsTerminalState(state string) bool {
	switch state {
	case TaskStateCompleted, TaskStateFailed, TaskStateCanceled:
		return true
	default:
		return false
	}
}

// CompleteTask updates task status and auto-closes any associated stream.
// Use this instead of UpdateTaskStatus for terminal states.
func (s *Server) CompleteTask(taskID string, state string, message *Message) error {
	if err := s.store.UpdateTaskStatus(taskID, state, message); err != nil {
		return err
	}

	// Auto-close stream if task reached terminal state
	if IsTerminalState(state) {
		s.CloseStream(taskID)
	}

	return nil
}

// HandleStreamRequest processes a message/stream request.
func (s *Server) HandleStreamRequest(ctx context.Context, params json.RawMessage, send func(*StreamEvent) error) error {
	var p SendMessageParams
	if err := json.Unmarshal(params, &p); err != nil {
		return err
	}

	// Create or get session ID
	sessionID := p.SessionID
	if sessionID == "" {
		sessionID = uuid.New().String()
	}

	// Create task
	task := s.store.CreateTask(sessionID, &p.Message)

	// Send initial status
	if err := send(&StreamEvent{
		Type: "status",
		Task: task,
	}); err != nil {
		return err
	}

	// Update to working state
	_ = s.store.UpdateTaskStatus(task.ID, TaskStateWorking, nil) // Best effort
	task, _ = s.store.GetTask(task.ID)
	if err := send(&StreamEvent{
		Type: "status",
		Task: task,
	}); err != nil {
		return err
	}

	// Process the message
	if s.handler != nil {
		response, artifacts, err := s.handler(ctx, task, &p.Message)
		if err != nil {
			_ = s.store.UpdateTaskStatus(task.ID, TaskStateFailed, &Message{
				Role:  "agent",
				Parts: []Part{{Type: "text", Text: err.Error()}},
			})
			task, _ = s.store.GetTask(task.ID)
			return send(&StreamEvent{
				Type:  "status",
				Task:  task,
				Final: true,
			})
		}

		// Send artifacts
		for i, artifact := range artifacts {
			artifact.Index = i
			artifact.LastChunk = i == len(artifacts)-1
			_ = s.store.AddTaskArtifact(task.ID, artifact) // Best effort
			if err := send(&StreamEvent{
				Type:     "artifact",
				Artifact: &artifact,
			}); err != nil {
				return err
			}
		}

		// Add response to history and update status
		if response != nil {
			_ = s.store.AddTaskMessage(task.ID, TaskMessage{
				Role:  response.Role,
				Parts: response.Parts,
			})
		}
		_ = s.store.UpdateTaskStatus(task.ID, TaskStateCompleted, response)
	} else {
		_ = s.store.UpdateTaskStatus(task.ID, TaskStateCompleted, nil)
	}

	// Send final status
	task, _ = s.store.GetTask(task.ID)
	return send(&StreamEvent{
		Type:  "status",
		Task:  task,
		Final: true,
	})
}

// buildResult builds a SendMessageResult from a task.
func (s *Server) buildResult(task *Task) (*SendMessageResult, error) {
	task, err := s.store.GetTask(task.ID)
	if err != nil {
		return nil, err
	}

	return &SendMessageResult{
		ID:        task.ID,
		SessionID: task.SessionID,
		Status:    task.Status,
		Artifacts: task.Artifacts,
		Metadata:  task.Metadata,
	}, nil
}

// errorResponse creates a JSON-RPC error response.
func (s *Server) errorResponse(id any, code int, message string) ([]byte, error) {
	resp := protocol.NewErrorResponse(id, code, message, nil)
	return protocol.Encode(resp)
}

// TaskGetParams are the params for tasks/get.
type TaskGetParams struct {
	ID            string `json:"id"`
	HistoryLength int    `json:"historyLength,omitempty"`
}

// TaskGetResult is the result of tasks/get.
type TaskGetResult struct {
	ID        string         `json:"id"`
	SessionID string         `json:"sessionId,omitempty"`
	Status    TaskStatus     `json:"status"`
	History   []TaskMessage  `json:"history,omitempty"`
	Artifacts []Artifact     `json:"artifacts,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// TaskCancelParams are the params for tasks/cancel.
type TaskCancelParams struct {
	ID string `json:"id"`
}

// TaskCancelResult is the result of tasks/cancel.
type TaskCancelResult struct {
	ID     string     `json:"id"`
	Status TaskStatus `json:"status"`
}

// TaskResubscribeParams are the params for tasks/resubscribe.
type TaskResubscribeParams struct {
	ID string `json:"id"`
}

// TaskResubscribeResult is the result of tasks/resubscribe.
type TaskResubscribeResult struct {
	ID     string     `json:"id"`
	Status TaskStatus `json:"status"`
}

// handleTasksResubscribe processes a tasks/resubscribe request.
// This allows a client to re-subscribe to updates for an existing task.
func (s *Server) handleTasksResubscribe(_ context.Context, params json.RawMessage) (*TaskResubscribeResult, error) {
	var p TaskResubscribeParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}

	task, err := s.store.GetTask(p.ID)
	if err != nil {
		return nil, err
	}

	return &TaskResubscribeResult{
		ID:     task.ID,
		Status: task.Status,
	}, nil
}

// Store returns the server's task store.
func (s *Server) Store() *TaskStore {
	return s.store
}

// Adapter returns the server's adapter.
func (s *Server) Adapter() *Adapter {
	return s.adapter
}
