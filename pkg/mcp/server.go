package mcp

import (
	"log/slog"
	"sync"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/gianluca/msg2agent/pkg/agent"
)

// TrackedTask represents a task being tracked locally.
type TrackedTask struct {
	ID       string `json:"id"`
	AgentDID string `json:"agent_did"`
	Status   string `json:"status,omitempty"`
}

// Server is the MCP server implementation
type Server struct {
	agent  *agent.Agent
	mcp    *server.MCPServer
	logger *slog.Logger

	// Task tracking
	tasks   map[string]*TrackedTask
	tasksMu sync.RWMutex

	// Inbox for incoming messages
	inbox *Inbox
}

// NewServer creates a new MCP server
func NewServer(a *agent.Agent, logger *slog.Logger) *Server {
	s := &Server{
		agent:  a,
		mcp:    server.NewMCPServer("msg2agent-mcp", "0.1.0"),
		logger: logger,
		tasks:  make(map[string]*TrackedTask),
		inbox:  NewInbox(1000), // Buffer up to 1000 messages
	}

	s.registerTools()
	s.registerResources()
	return s
}

// trackTask adds a task to local tracking.
func (s *Server) trackTask(taskID, agentDID string) {
	s.tasksMu.Lock()
	defer s.tasksMu.Unlock()
	s.tasks[taskID] = &TrackedTask{
		ID:       taskID,
		AgentDID: agentDID,
		Status:   "submitted",
	}
}

// untrackTask removes a task from local tracking.
func (s *Server) untrackTask(taskID string) {
	s.tasksMu.Lock()
	defer s.tasksMu.Unlock()
	delete(s.tasks, taskID)
}

// updateTaskStatus updates the status of a tracked task.
func (s *Server) updateTaskStatus(taskID, status string) {
	s.tasksMu.Lock()
	defer s.tasksMu.Unlock()
	if task, ok := s.tasks[taskID]; ok {
		task.Status = status
	}
}

// getTrackedTasks returns all tracked tasks.
func (s *Server) getTrackedTasks() []*TrackedTask {
	s.tasksMu.RLock()
	defer s.tasksMu.RUnlock()
	tasks := make([]*TrackedTask, 0, len(s.tasks))
	for _, task := range s.tasks {
		tasks = append(tasks, task)
	}
	return tasks
}

// Inbox returns the server's inbox for incoming messages.
func (s *Server) Inbox() *Inbox {
	return s.inbox
}

// ServeStdio starts the server on stdio
func (s *Server) ServeStdio() error {
	return server.ServeStdio(s.mcp)
}

func (s *Server) registerTools() {
	// List Agents
	s.mcp.AddTool(mcp.NewTool("list_agents",
		mcp.WithDescription("List available agents on the network. Usage: Returns a list of agents with their DIDs and names."),
		mcp.WithString("capability", mcp.Description("Optional capability to filter by")),
	), s.listAgentsHandler)

	// Send Message
	s.mcp.AddTool(mcp.NewTool("send_message",
		mcp.WithDescription("Send a message to another agent. Usage: Provide the recipient DID, method name, and parameters."),
		mcp.WithString("to", mcp.Required(), mcp.Description("DID of the recipient agent")),
		mcp.WithString("method", mcp.Required(), mcp.Description("Method to call")),
		mcp.WithString("params", mcp.Required(), mcp.Description("JSON string of parameters")),
	), s.sendMessageHandler)

	// Get Agent Info
	s.mcp.AddTool(mcp.NewTool("get_agent_info",
		mcp.WithDescription("Get detailed information about a specific agent including capabilities and endpoints."),
		mcp.WithString("did", mcp.Required(), mcp.Description("DID of the agent to query")),
	), s.getAgentInfoHandler)

	// Get Task Status
	s.mcp.AddTool(mcp.NewTool("get_task_status",
		mcp.WithDescription("Get the status of an A2A task by its ID."),
		mcp.WithString("task_id", mcp.Required(), mcp.Description("ID of the task")),
		mcp.WithString("agent_did", mcp.Required(), mcp.Description("DID of the agent that owns the task")),
	), s.getTaskStatusHandler)

	// Query Capabilities
	s.mcp.AddTool(mcp.NewTool("query_capabilities",
		mcp.WithDescription("Find agents that support specific capabilities."),
		mcp.WithString("capabilities", mcp.Required(), mcp.Description("Comma-separated list of required capabilities")),
	), s.queryCapabilitiesHandler)

	// Get Self Info
	s.mcp.AddTool(mcp.NewTool("get_self_info",
		mcp.WithDescription("Get information about this agent (self)."),
	), s.getSelfInfoHandler)

	// Submit Task - Create A2A task with session tracking
	s.mcp.AddTool(mcp.NewTool("submit_task",
		mcp.WithDescription("Submit a new A2A task to an agent. Returns the task with ID and initial status."),
		mcp.WithString("agent_did", mcp.Required(), mcp.Description("DID of the target agent")),
		mcp.WithString("message", mcp.Required(), mcp.Description("JSON message to send to the agent")),
		mcp.WithString("session_id", mcp.Description("Optional session ID to continue an existing conversation")),
	), s.submitTaskHandler)

	// Cancel Task - Cancel a task in progress
	s.mcp.AddTool(mcp.NewTool("cancel_task",
		mcp.WithDescription("Cancel an A2A task in progress."),
		mcp.WithString("task_id", mcp.Required(), mcp.Description("ID of the task to cancel")),
		mcp.WithString("agent_did", mcp.Required(), mcp.Description("DID of the agent that owns the task")),
	), s.cancelTaskHandler)

	// Send Task Input - Send input to a task in input_required state
	s.mcp.AddTool(mcp.NewTool("send_task_input",
		mcp.WithDescription("Send input to an A2A task that is waiting for user input."),
		mcp.WithString("task_id", mcp.Required(), mcp.Description("ID of the task")),
		mcp.WithString("agent_did", mcp.Required(), mcp.Description("DID of the agent that owns the task")),
		mcp.WithString("message", mcp.Required(), mcp.Description("JSON message with the user input")),
	), s.sendTaskInputHandler)

	// List Tasks - List locally tracked tasks
	s.mcp.AddTool(mcp.NewTool("list_tasks",
		mcp.WithDescription("List all locally tracked A2A tasks."),
	), s.listTasksHandler)
}
