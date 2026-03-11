package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/gianluca/msg2agent/adapters/a2a"
	"github.com/gianluca/msg2agent/pkg/registry"
)

func (s *Server) listAgentsHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args, ok := request.Params.Arguments.(map[string]any)
	if !ok {
		// Should not happen if params are validated, but safe to handle empty
		args = make(map[string]any)
	}
	capability, _ := args["capability"].(string)

	// Validate capability if provided
	if capability != "" {
		if err := ValidateCapability(capability); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid capability: %v", err)), nil
		}
	}

	s.logger.Info("handling list_agents", "capability", capability)

	// Construct parameters for relay.discover
	params := map[string]string{}
	if capability != "" {
		params["capability"] = capability
	}

	// Use CallRelay to send a raw JSON-RPC request to the relay
	result, err := s.caller.CallRelay(ctx, "relay.discover", params)
	if err != nil {
		s.logger.Error("relay.discover failed", "error", err)
		return mcp.NewToolResultError(fmt.Sprintf("Failed to discover agents: %v", err)), nil
	}

	// Parse result
	var agents []*registry.Agent
	if err := json.Unmarshal(result, &agents); err != nil {
		s.logger.Error("failed to unmarshal agents", "error", err)
		return mcp.NewToolResultError("Failed to parse agent list"), nil
	}

	// Format output
	output, err := json.MarshalIndent(agents, "", "  ")
	if err != nil {
		s.logger.Error("failed to encode agents", "error", err)
		return mcp.NewToolResultError(fmt.Sprintf("%s: %v", ErrEncodingFailed.Error(), err)), nil
	}
	return mcp.NewToolResultText(string(output)), nil
}

func (s *Server) sendMessageHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args, ok := request.Params.Arguments.(map[string]any)
	if !ok {
		return mcp.NewToolResultError(ErrInvalidArguments.Error()), nil
	}

	to, ok := args["to"].(string)
	if !ok {
		return mcp.NewToolResultError("Missing 'to' parameter"), nil
	}

	// Validate DID
	if err := ValidateDID(to); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Invalid 'to' DID: %v", err)), nil
	}

	method, ok := args["method"].(string)
	if !ok {
		return mcp.NewToolResultError("Missing 'method' parameter"), nil
	}

	// Validate method
	if err := ValidateMethod(method); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Invalid method: %v", err)), nil
	}

	paramsStr, ok := args["params"].(string)
	if !ok {
		return mcp.NewToolResultError("Missing 'params' parameter"), nil
	}

	// Validate params length
	if err := ValidateParams(paramsStr); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Invalid params: %v", err)), nil
	}

	var params any
	if err := json.Unmarshal([]byte(paramsStr), &params); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Invalid JSON params: %v", err)), nil
	}

	s.logger.Info("handling send_message", "to", to, "method", method)

	// Use agent.Send to send a standard message
	resp, err := s.caller.Send(ctx, to, method, params)
	if err != nil {
		s.logger.Error("send failed", "error", err)
		return mcp.NewToolResultError(fmt.Sprintf("Send failed: %v", err)), nil
	}

	// Check if response is an error
	if resp.IsError() {
		return mcp.NewToolResultError(fmt.Sprintf("Agent returned error: %s", resp.RawBody())), nil
	}

	// Return the response body
	output, err := json.MarshalIndent(resp.RawBody(), "", "  ")
	if err != nil {
		s.logger.Error("failed to encode response", "error", err)
		return mcp.NewToolResultError(fmt.Sprintf("%s: %v", ErrEncodingFailed.Error(), err)), nil
	}
	return mcp.NewToolResultText(string(output)), nil
}

func (s *Server) getAgentInfoHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args, ok := request.Params.Arguments.(map[string]any)
	if !ok {
		return mcp.NewToolResultError(ErrInvalidArguments.Error()), nil
	}

	did, ok := args["did"].(string)
	if !ok {
		return mcp.NewToolResultError("Missing 'did' parameter"), nil
	}

	// Validate DID
	if err := ValidateDID(did); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Invalid DID: %v", err)), nil
	}

	s.logger.Info("handling get_agent_info", "did", did)

	// Query relay for agent info
	result, err := s.caller.CallRelay(ctx, "relay.lookup", map[string]string{"did": did})
	if err != nil {
		s.logger.Error("relay.lookup failed", "error", err)
		return mcp.NewToolResultError(fmt.Sprintf("Failed to lookup agent: %v", err)), nil
	}

	var agent *registry.Agent
	if err := json.Unmarshal(result, &agent); err != nil {
		s.logger.Error("failed to unmarshal agent", "error", err)
		return mcp.NewToolResultError("Failed to parse agent info"), nil
	}

	if agent == nil {
		return mcp.NewToolResultError(fmt.Sprintf("Agent not found: %s", did)), nil
	}

	// Convert to A2A AgentCard for richer output
	adapter := a2a.NewAdapter()
	card := adapter.ToA2AAgentCard(agent)

	output, err := json.MarshalIndent(card, "", "  ")
	if err != nil {
		s.logger.Error("failed to encode agent card", "error", err)
		return mcp.NewToolResultError(fmt.Sprintf("%s: %v", ErrEncodingFailed.Error(), err)), nil
	}
	return mcp.NewToolResultText(string(output)), nil
}

func (s *Server) getTaskStatusHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args, ok := request.Params.Arguments.(map[string]any)
	if !ok {
		return mcp.NewToolResultError(ErrInvalidArguments.Error()), nil
	}

	taskID, ok := args["task_id"].(string)
	if !ok {
		return mcp.NewToolResultError("Missing 'task_id' parameter"), nil
	}

	// Validate task ID
	if err := ValidateTaskID(taskID); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Invalid task_id: %v", err)), nil
	}

	agentDID, ok := args["agent_did"].(string)
	if !ok {
		return mcp.NewToolResultError("Missing 'agent_did' parameter"), nil
	}

	// Validate agent DID
	if err := ValidateDID(agentDID); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Invalid agent_did: %v", err)), nil
	}

	s.logger.Info("handling get_task_status", "task_id", taskID, "agent_did", agentDID)

	// Call the target agent's tasks/get method
	resp, err := s.caller.Send(ctx, agentDID, "tasks/get", map[string]string{"id": taskID})
	if err != nil {
		s.logger.Error("tasks/get failed", "error", err)
		return mcp.NewToolResultError(fmt.Sprintf("Failed to get task status: %v", err)), nil
	}

	if resp.IsError() {
		return mcp.NewToolResultError(fmt.Sprintf("Agent returned error: %s", resp.RawBody())), nil
	}

	output, err := json.MarshalIndent(resp.RawBody(), "", "  ")
	if err != nil {
		s.logger.Error("failed to encode task status", "error", err)
		return mcp.NewToolResultError(fmt.Sprintf("%s: %v", ErrEncodingFailed.Error(), err)), nil
	}
	return mcp.NewToolResultText(string(output)), nil
}

func (s *Server) queryCapabilitiesHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args, ok := request.Params.Arguments.(map[string]any)
	if !ok {
		return mcp.NewToolResultError(ErrInvalidArguments.Error()), nil
	}

	capsStr, ok := args["capabilities"].(string)
	if !ok {
		return mcp.NewToolResultError("Missing 'capabilities' parameter"), nil
	}

	// Validate and parse capabilities
	capabilities, err := ValidateCapabilities(capsStr)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Invalid capabilities: %v", err)), nil
	}

	s.logger.Info("handling query_capabilities", "capabilities", capabilities)

	// Discover all agents first
	result, err := s.caller.CallRelay(ctx, "relay.discover", map[string]string{})
	if err != nil {
		s.logger.Error("relay.discover failed", "error", err)
		return mcp.NewToolResultError(fmt.Sprintf("Failed to discover agents: %v", err)), nil
	}

	var agents []*registry.Agent
	if err := json.Unmarshal(result, &agents); err != nil {
		s.logger.Error("failed to unmarshal agents", "error", err)
		return mcp.NewToolResultError("Failed to parse agent list"), nil
	}

	// Filter agents by required capabilities
	var matching []*registry.Agent
	for _, agent := range agents {
		if hasAllCapabilities(agent, capabilities) {
			matching = append(matching, agent)
		}
	}

	output, err := json.MarshalIndent(matching, "", "  ")
	if err != nil {
		s.logger.Error("failed to encode matching agents", "error", err)
		return mcp.NewToolResultError(fmt.Sprintf("%s: %v", ErrEncodingFailed.Error(), err)), nil
	}
	return mcp.NewToolResultText(string(output)), nil
}

func hasAllCapabilities(agent *registry.Agent, required []string) bool {
	caps := make(map[string]bool)
	for _, c := range agent.Capabilities {
		caps[c.Name] = true
	}
	for _, req := range required {
		if !caps[req] {
			return false
		}
	}
	return true
}

func (s *Server) getSelfInfoHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	s.logger.Info("handling get_self_info")

	info := map[string]any{
		"did":          s.caller.DID(),
		"display_name": s.caller.Record().DisplayName,
		"endpoints":    s.caller.Record().Endpoints,
		"capabilities": s.caller.Record().Capabilities,
		"status":       s.caller.Record().Status,
	}

	output, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		s.logger.Error("failed to encode self info", "error", err)
		return mcp.NewToolResultError(fmt.Sprintf("%s: %v", ErrEncodingFailed.Error(), err)), nil
	}
	return mcp.NewToolResultText(string(output)), nil
}

func (s *Server) submitTaskHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args, ok := request.Params.Arguments.(map[string]any)
	if !ok {
		return mcp.NewToolResultError(ErrInvalidArguments.Error()), nil
	}

	agentDID, ok := args["agent_did"].(string)
	if !ok {
		return mcp.NewToolResultError("Missing 'agent_did' parameter"), nil
	}

	if err := ValidateDID(agentDID); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Invalid agent_did: %v", err)), nil
	}

	messageStr, ok := args["message"].(string)
	if !ok {
		return mcp.NewToolResultError("Missing 'message' parameter"), nil
	}

	if err := ValidateParams(messageStr); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Invalid message: %v", err)), nil
	}

	var message any
	if err := json.Unmarshal([]byte(messageStr), &message); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Invalid JSON message: %v", err)), nil
	}

	// Optional session_id for continuing an existing session
	sessionID, _ := args["session_id"].(string)

	s.logger.Info("handling submit_task", "agent_did", agentDID, "session_id", sessionID)

	// Build the task/send params
	taskParams := map[string]any{
		"message": map[string]any{
			"role": "user",
			"parts": []map[string]any{
				{"text": message},
			},
		},
	}
	if sessionID != "" {
		taskParams["sessionId"] = sessionID
	}

	// Call the target agent's message/send method (A2A protocol)
	resp, err := s.caller.Send(ctx, agentDID, "message/send", taskParams)
	if err != nil {
		s.logger.Error("message/send failed", "error", err)
		return mcp.NewToolResultError(fmt.Sprintf("Failed to submit task: %v", err)), nil
	}

	if resp.IsError() {
		return mcp.NewToolResultError(fmt.Sprintf("Agent returned error: %s", resp.RawBody())), nil
	}

	// Store task locally for tracking
	var taskResp map[string]any
	if err := json.Unmarshal(resp.RawBody(), &taskResp); err == nil {
		if taskID, ok := taskResp["id"].(string); ok {
			s.trackTask(taskID, agentDID)
		}
	}

	output, err := json.MarshalIndent(resp.RawBody(), "", "  ")
	if err != nil {
		s.logger.Error("failed to encode task response", "error", err)
		return mcp.NewToolResultError(fmt.Sprintf("%s: %v", ErrEncodingFailed.Error(), err)), nil
	}
	return mcp.NewToolResultText(string(output)), nil
}

func (s *Server) cancelTaskHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args, ok := request.Params.Arguments.(map[string]any)
	if !ok {
		return mcp.NewToolResultError(ErrInvalidArguments.Error()), nil
	}

	taskID, ok := args["task_id"].(string)
	if !ok {
		return mcp.NewToolResultError("Missing 'task_id' parameter"), nil
	}

	if err := ValidateTaskID(taskID); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Invalid task_id: %v", err)), nil
	}

	agentDID, ok := args["agent_did"].(string)
	if !ok {
		return mcp.NewToolResultError("Missing 'agent_did' parameter"), nil
	}

	if err := ValidateDID(agentDID); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Invalid agent_did: %v", err)), nil
	}

	s.logger.Info("handling cancel_task", "task_id", taskID, "agent_did", agentDID)

	// Call the target agent's tasks/cancel method
	resp, err := s.caller.Send(ctx, agentDID, "tasks/cancel", map[string]string{"id": taskID})
	if err != nil {
		s.logger.Error("tasks/cancel failed", "error", err)
		return mcp.NewToolResultError(fmt.Sprintf("Failed to cancel task: %v", err)), nil
	}

	if resp.IsError() {
		return mcp.NewToolResultError(fmt.Sprintf("Agent returned error: %s", resp.RawBody())), nil
	}

	// Remove from local tracking
	s.untrackTask(taskID)

	output, err := json.MarshalIndent(resp.RawBody(), "", "  ")
	if err != nil {
		s.logger.Error("failed to encode cancel response", "error", err)
		return mcp.NewToolResultError(fmt.Sprintf("%s: %v", ErrEncodingFailed.Error(), err)), nil
	}
	return mcp.NewToolResultText(string(output)), nil
}

func (s *Server) sendTaskInputHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args, ok := request.Params.Arguments.(map[string]any)
	if !ok {
		return mcp.NewToolResultError(ErrInvalidArguments.Error()), nil
	}

	taskID, ok := args["task_id"].(string)
	if !ok {
		return mcp.NewToolResultError("Missing 'task_id' parameter"), nil
	}

	if err := ValidateTaskID(taskID); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Invalid task_id: %v", err)), nil
	}

	agentDID, ok := args["agent_did"].(string)
	if !ok {
		return mcp.NewToolResultError("Missing 'agent_did' parameter"), nil
	}

	if err := ValidateDID(agentDID); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Invalid agent_did: %v", err)), nil
	}

	messageStr, ok := args["message"].(string)
	if !ok {
		return mcp.NewToolResultError("Missing 'message' parameter"), nil
	}

	if err := ValidateParams(messageStr); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Invalid message: %v", err)), nil
	}

	var message any
	if err := json.Unmarshal([]byte(messageStr), &message); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Invalid JSON message: %v", err)), nil
	}

	s.logger.Info("handling send_task_input", "task_id", taskID, "agent_did", agentDID)

	// Build input params per A2A spec
	inputParams := map[string]any{
		"id": taskID,
		"message": map[string]any{
			"role": "user",
			"parts": []map[string]any{
				{"text": message},
			},
		},
	}

	// Call the target agent's message/send method with task ID for continuation
	resp, err := s.caller.Send(ctx, agentDID, "message/send", inputParams)
	if err != nil {
		s.logger.Error("message/send (input) failed", "error", err)
		return mcp.NewToolResultError(fmt.Sprintf("Failed to send task input: %v", err)), nil
	}

	if resp.IsError() {
		return mcp.NewToolResultError(fmt.Sprintf("Agent returned error: %s", resp.RawBody())), nil
	}

	output, err := json.MarshalIndent(resp.RawBody(), "", "  ")
	if err != nil {
		s.logger.Error("failed to encode input response", "error", err)
		return mcp.NewToolResultError(fmt.Sprintf("%s: %v", ErrEncodingFailed.Error(), err)), nil
	}
	return mcp.NewToolResultText(string(output)), nil
}

func (s *Server) listTasksHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	s.logger.Info("handling list_tasks")

	tasks := s.getTrackedTasks()

	output, err := json.MarshalIndent(tasks, "", "  ")
	if err != nil {
		s.logger.Error("failed to encode tasks list", "error", err)
		return mcp.NewToolResultError(fmt.Sprintf("%s: %v", ErrEncodingFailed.Error(), err)), nil
	}
	return mcp.NewToolResultText(string(output)), nil
}

func (s *Server) listMessagesHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args, _ := request.Params.Arguments.(map[string]any)
	unreadOnly, _ := args["unread_only"].(bool)

	s.logger.Info("handling list_messages", "unread_only", unreadOnly)

	messages := s.inbox.List(unreadOnly)

	output, err := json.MarshalIndent(messages, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("%s: %v", ErrEncodingFailed.Error(), err)), nil
	}
	return mcp.NewToolResultText(string(output)), nil
}

func (s *Server) readMessageHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args, ok := request.Params.Arguments.(map[string]any)
	if !ok {
		return mcp.NewToolResultError(ErrInvalidArguments.Error()), nil
	}

	id, ok := args["id"].(string)
	if !ok || id == "" {
		return mcp.NewToolResultError("Missing 'id' parameter"), nil
	}

	s.logger.Info("handling read_message", "id", id)

	msg := s.inbox.Get(id)
	if msg == nil {
		return mcp.NewToolResultError(fmt.Sprintf("Message not found: %s", id)), nil
	}

	s.inbox.MarkRead(id)

	output, err := json.MarshalIndent(msg, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("%s: %v", ErrEncodingFailed.Error(), err)), nil
	}
	return mcp.NewToolResultText(string(output)), nil
}

func (s *Server) deleteMessageHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args, ok := request.Params.Arguments.(map[string]any)
	if !ok {
		return mcp.NewToolResultError(ErrInvalidArguments.Error()), nil
	}

	id, ok := args["id"].(string)
	if !ok || id == "" {
		return mcp.NewToolResultError("Missing 'id' parameter"), nil
	}

	s.logger.Info("handling delete_message", "id", id)

	if !s.inbox.Delete(id) {
		return mcp.NewToolResultError(fmt.Sprintf("Message not found: %s", id)), nil
	}

	output, _ := json.Marshal(map[string]string{"status": "deleted"})
	return mcp.NewToolResultText(string(output)), nil
}

func (s *Server) messageCountHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args, _ := request.Params.Arguments.(map[string]any)
	unreadOnly, _ := args["unread_only"].(bool)

	s.logger.Info("handling message_count", "unread_only", unreadOnly)

	total := s.inbox.Count(false)
	unread := s.inbox.Count(true)

	result := map[string]int{"total": total, "unread": unread}
	if unreadOnly {
		result = map[string]int{"count": unread}
	}

	output, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("%s: %v", ErrEncodingFailed.Error(), err)), nil
	}
	return mcp.NewToolResultText(string(output)), nil
}
