package mcp

import (
	"encoding/json"
	"time"
)

// NotificationType represents the type of MCP notification.
type NotificationType string

const (
	// NotificationMessage is sent when a new message arrives.
	NotificationMessage NotificationType = "notifications/message"
	// NotificationTaskUpdate is sent when a task status changes.
	NotificationTaskUpdate NotificationType = "notifications/task/update"
	// NotificationResourceUpdated is sent when a resource changes.
	NotificationResourceUpdated NotificationType = "notifications/resources/updated"
)

// MessageNotification represents a notification about an incoming message.
type MessageNotification struct {
	ID        string          `json:"id"`
	From      string          `json:"from"`
	Method    string          `json:"method"`
	Body      json.RawMessage `json:"body,omitempty"`
	Timestamp time.Time       `json:"timestamp"`
}

// TaskUpdateNotification represents a notification about a task status change.
type TaskUpdateNotification struct {
	TaskID   string `json:"task_id"`
	AgentDID string `json:"agent_did"`
	Status   string `json:"status"`
	Message  string `json:"message,omitempty"`
}

// ResourceUpdatedNotification represents a notification that a resource changed.
type ResourceUpdatedNotification struct {
	URI string `json:"uri"`
}

// NotifyMessageReceived sends a notification about a new incoming message.
func (s *Server) NotifyMessageReceived(id, from, method string, body json.RawMessage) {
	notification := map[string]any{
		"id":        id,
		"from":      from,
		"method":    method,
		"body":      body,
		"timestamp": time.Now(),
	}

	s.mcp.SendNotificationToAllClients(string(NotificationMessage), notification)

	// Also notify that inbox resource was updated
	s.notifyResourceUpdated("msg2agent://inbox")
}

// NotifyTaskUpdate sends a notification about a task status change.
func (s *Server) NotifyTaskUpdate(taskID, agentDID, status, message string) {
	notification := map[string]any{
		"task_id":   taskID,
		"agent_did": agentDID,
		"status":    status,
		"message":   message,
	}

	s.mcp.SendNotificationToAllClients(string(NotificationTaskUpdate), notification)

	// Update local task status
	s.updateTaskStatus(taskID, status)

	// Also notify that tasks resource was updated
	s.notifyResourceUpdated("msg2agent://tasks")
}

// notifyResourceUpdated sends a notification that a resource was updated.
func (s *Server) notifyResourceUpdated(uri string) {
	notification := map[string]any{
		"uri": uri,
	}

	s.mcp.SendNotificationToAllClients(string(NotificationResourceUpdated), notification)
}

// HandleIncomingMessage processes an incoming message and stores it in the inbox.
// This should be called by the agent's message handler.
func (s *Server) HandleIncomingMessage(from, method string, body json.RawMessage) string {
	// Add to inbox
	msgID := s.inbox.Add(from, method, body)

	// Send notification
	s.NotifyMessageReceived(msgID, from, method, body)

	return msgID
}

// HandleTaskStatusUpdate processes a task status update notification.
// This should be called when receiving task status updates from other agents.
func (s *Server) HandleTaskStatusUpdate(taskID, status, message string) {
	s.tasksMu.RLock()
	task, ok := s.tasks[taskID]
	s.tasksMu.RUnlock()

	if !ok {
		return // Not tracking this task
	}

	s.NotifyTaskUpdate(taskID, task.AgentDID, status, message)
}
