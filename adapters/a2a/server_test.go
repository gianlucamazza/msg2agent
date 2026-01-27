package a2a

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/gianluca/msg2agent/pkg/protocol"
)

// TestNewTaskStore tests task store creation.
func TestNewTaskStore(t *testing.T) {
	store := NewTaskStore()
	if store == nil {
		t.Fatal("NewTaskStore returned nil")
	}
	if store.tasks == nil {
		t.Error("tasks map should be initialized")
	}
	if store.sessions == nil {
		t.Error("sessions map should be initialized")
	}
}

// TestTaskStoreCreateTask tests task creation.
func TestTaskStoreCreateTask(t *testing.T) {
	store := NewTaskStore()

	msg := &Message{
		Role:  "user",
		Parts: []Part{{Type: "text", Text: "Hello"}},
	}

	task := store.CreateTask("session-1", msg)

	if task.ID == "" {
		t.Error("task ID should be set")
	}
	if task.SessionID != "session-1" {
		t.Errorf("sessionID = %q, want %q", task.SessionID, "session-1")
	}
	if task.Status.State != TaskStateSubmitted {
		t.Errorf("state = %q, want %q", task.Status.State, TaskStateSubmitted)
	}
	if len(task.History) != 1 {
		t.Errorf("history length = %d, want 1", len(task.History))
	}

	// Verify it's stored
	stored, err := store.GetTask(task.ID)
	if err != nil {
		t.Fatalf("GetTask failed: %v", err)
	}
	if stored.ID != task.ID {
		t.Error("stored task ID mismatch")
	}
}

// TestTaskStoreGetTaskNotFound tests getting non-existent task.
func TestTaskStoreGetTaskNotFound(t *testing.T) {
	store := NewTaskStore()

	_, err := store.GetTask("nonexistent")
	if err != ErrTaskNotFound {
		t.Errorf("err = %v, want ErrTaskNotFound", err)
	}
}

// TestTaskStoreUpdateTaskStatus tests status updates.
func TestTaskStoreUpdateTaskStatus(t *testing.T) {
	store := NewTaskStore()

	msg := &Message{Role: "user", Parts: []Part{{Type: "text", Text: "Hi"}}}
	task := store.CreateTask("", msg)

	response := &Message{Role: "agent", Parts: []Part{{Type: "text", Text: "Hello!"}}}
	err := store.UpdateTaskStatus(task.ID, TaskStateCompleted, response)
	if err != nil {
		t.Fatalf("UpdateTaskStatus failed: %v", err)
	}

	updated, _ := store.GetTask(task.ID)
	if updated.Status.State != TaskStateCompleted {
		t.Errorf("state = %q, want %q", updated.Status.State, TaskStateCompleted)
	}
	if updated.Status.Message == nil {
		t.Error("status message should be set")
	}
}

// TestTaskStoreAddTaskMessage tests adding messages to history.
func TestTaskStoreAddTaskMessage(t *testing.T) {
	store := NewTaskStore()

	msg := &Message{Role: "user", Parts: []Part{{Type: "text", Text: "Hi"}}}
	task := store.CreateTask("", msg)

	err := store.AddTaskMessage(task.ID, TaskMessage{
		Role:  "agent",
		Parts: []Part{{Type: "text", Text: "Hello!"}},
	})
	if err != nil {
		t.Fatalf("AddTaskMessage failed: %v", err)
	}

	updated, _ := store.GetTask(task.ID)
	if len(updated.History) != 2 {
		t.Errorf("history length = %d, want 2", len(updated.History))
	}
}

// TestTaskStoreAddTaskArtifact tests adding artifacts.
func TestTaskStoreAddTaskArtifact(t *testing.T) {
	store := NewTaskStore()

	msg := &Message{Role: "user", Parts: []Part{{Type: "text", Text: "Generate image"}}}
	task := store.CreateTask("", msg)

	artifact := Artifact{
		Name:  "image.png",
		Index: 0,
		Parts: []Part{{Type: "file", File: &FileData{Name: "image.png"}}},
	}

	err := store.AddTaskArtifact(task.ID, artifact)
	if err != nil {
		t.Fatalf("AddTaskArtifact failed: %v", err)
	}

	updated, _ := store.GetTask(task.ID)
	if len(updated.Artifacts) != 1 {
		t.Errorf("artifacts length = %d, want 1", len(updated.Artifacts))
	}
}

// TestTaskStoreCancelTask tests task cancellation.
func TestTaskStoreCancelTask(t *testing.T) {
	store := NewTaskStore()

	msg := &Message{Role: "user", Parts: []Part{{Type: "text", Text: "Long task"}}}
	task := store.CreateTask("", msg)

	// Task starts in submitted state, should be cancelable
	err := store.CancelTask(task.ID)
	if err != nil {
		t.Fatalf("CancelTask failed: %v", err)
	}

	updated, _ := store.GetTask(task.ID)
	if updated.Status.State != TaskStateCanceled {
		t.Errorf("state = %q, want %q", updated.Status.State, TaskStateCanceled)
	}
}

// TestTaskStoreCancelTaskNotCancelable tests canceling completed task.
func TestTaskStoreCancelTaskNotCancelable(t *testing.T) {
	store := NewTaskStore()

	msg := &Message{Role: "user", Parts: []Part{{Type: "text", Text: "Hi"}}}
	task := store.CreateTask("", msg)

	// Complete the task
	store.UpdateTaskStatus(task.ID, TaskStateCompleted, nil)

	// Should not be cancelable
	err := store.CancelTask(task.ID)
	if err != ErrTaskNotCancelable {
		t.Errorf("err = %v, want ErrTaskNotCancelable", err)
	}
}

// TestTaskStoreSession tests session management.
func TestTaskStoreSession(t *testing.T) {
	store := NewTaskStore()

	sessionID := "test-session"
	msg := &Message{Role: "user", Parts: []Part{{Type: "text", Text: "Hi"}}}

	// Create first task in session
	task1 := store.CreateTask(sessionID, msg)

	// Create second task in same session
	task2 := store.CreateTask(sessionID, msg)

	session, err := store.GetSession(sessionID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}

	if len(session.Tasks) != 2 {
		t.Errorf("session tasks = %d, want 2", len(session.Tasks))
	}
	if session.Tasks[0] != task1.ID || session.Tasks[1] != task2.ID {
		t.Error("session task IDs mismatch")
	}
}

// TestTaskStoreGetSessionNotFound tests getting non-existent session.
func TestTaskStoreGetSessionNotFound(t *testing.T) {
	store := NewTaskStore()

	_, err := store.GetSession("nonexistent")
	if err != ErrSessionNotFound {
		t.Errorf("err = %v, want ErrSessionNotFound", err)
	}
}

// TestNewServer tests server creation.
func TestNewServer(t *testing.T) {
	server := NewServer(nil)
	if server == nil {
		t.Fatal("NewServer returned nil")
	}
	if server.store == nil {
		t.Error("store should be initialized")
	}
	if server.adapter == nil {
		t.Error("adapter should be initialized")
	}
}

// TestServerHandleMessageSend tests the message/send handler.
func TestServerHandleMessageSend(t *testing.T) {
	handler := func(ctx context.Context, task *Task, msg *Message) (*Message, []Artifact, error) {
		return &Message{
			Role:  "agent",
			Parts: []Part{{Type: "text", Text: "Hello back!"}},
		}, nil, nil
	}

	server := NewServer(handler)

	params := SendMessageParams{
		Message: Message{
			Role:  "user",
			Parts: []Part{{Type: "text", Text: "Hello"}},
		},
	}

	paramsJSON, _ := json.Marshal(params)
	req, _ := protocol.NewRequest("1", A2AMessageSend, json.RawMessage(paramsJSON))
	reqData, _ := protocol.Encode(req)

	respData, err := server.HandleRequest(context.Background(), reqData)
	if err != nil {
		t.Fatalf("HandleRequest failed: %v", err)
	}

	resp, err := protocol.DecodeResponse(respData)
	if err != nil {
		t.Fatalf("DecodeResponse failed: %v", err)
	}

	if resp.IsError() {
		t.Fatalf("response is error: %v", resp.Error)
	}

	var result SendMessageResult
	if err := resp.ParseResult(&result); err != nil {
		t.Fatalf("ParseResult failed: %v", err)
	}

	if result.ID == "" {
		t.Error("result ID should be set")
	}
	if result.Status.State != TaskStateCompleted {
		t.Errorf("state = %q, want %q", result.Status.State, TaskStateCompleted)
	}
}

// TestServerHandleMessageSendWithSession tests session handling.
func TestServerHandleMessageSendWithSession(t *testing.T) {
	server := NewServer(nil)

	params := SendMessageParams{
		SessionID: "my-session",
		Message: Message{
			Role:  "user",
			Parts: []Part{{Type: "text", Text: "Hello"}},
		},
	}

	paramsJSON, _ := json.Marshal(params)
	req, _ := protocol.NewRequest("1", A2AMessageSend, json.RawMessage(paramsJSON))
	reqData, _ := protocol.Encode(req)

	respData, _ := server.HandleRequest(context.Background(), reqData)
	resp, _ := protocol.DecodeResponse(respData)

	var result SendMessageResult
	resp.ParseResult(&result)

	if result.SessionID != "my-session" {
		t.Errorf("sessionID = %q, want %q", result.SessionID, "my-session")
	}
}

// TestServerHandleMessageSendError tests handler error.
func TestServerHandleMessageSendError(t *testing.T) {
	handler := func(ctx context.Context, task *Task, msg *Message) (*Message, []Artifact, error) {
		return nil, nil, errors.New("processing failed")
	}

	server := NewServer(handler)

	params := SendMessageParams{
		Message: Message{
			Role:  "user",
			Parts: []Part{{Type: "text", Text: "Hello"}},
		},
	}

	paramsJSON, _ := json.Marshal(params)
	req, _ := protocol.NewRequest("1", A2AMessageSend, json.RawMessage(paramsJSON))
	reqData, _ := protocol.Encode(req)

	respData, _ := server.HandleRequest(context.Background(), reqData)
	resp, _ := protocol.DecodeResponse(respData)

	var result SendMessageResult
	resp.ParseResult(&result)

	if result.Status.State != TaskStateFailed {
		t.Errorf("state = %q, want %q", result.Status.State, TaskStateFailed)
	}
}

// TestServerHandleTasksGet tests the tasks/get handler.
func TestServerHandleTasksGet(t *testing.T) {
	server := NewServer(nil)

	// Create a task first
	sendParams := SendMessageParams{
		Message: Message{
			Role:  "user",
			Parts: []Part{{Type: "text", Text: "Hello"}},
		},
	}
	sendJSON, _ := json.Marshal(sendParams)
	sendReq, _ := protocol.NewRequest("1", A2AMessageSend, json.RawMessage(sendJSON))
	sendData, _ := protocol.Encode(sendReq)
	sendResp, _ := server.HandleRequest(context.Background(), sendData)
	resp, _ := protocol.DecodeResponse(sendResp)
	var sendResult SendMessageResult
	resp.ParseResult(&sendResult)

	// Now get the task
	getParams := TaskGetParams{ID: sendResult.ID}
	getJSON, _ := json.Marshal(getParams)
	getReq, _ := protocol.NewRequest("2", A2ATasksGet, json.RawMessage(getJSON))
	getData, _ := protocol.Encode(getReq)

	getResp, err := server.HandleRequest(context.Background(), getData)
	if err != nil {
		t.Fatalf("HandleRequest failed: %v", err)
	}

	resp, _ = protocol.DecodeResponse(getResp)
	if resp.IsError() {
		t.Fatalf("response is error: %v", resp.Error)
	}

	var getResult TaskGetResult
	resp.ParseResult(&getResult)

	if getResult.ID != sendResult.ID {
		t.Errorf("ID = %q, want %q", getResult.ID, sendResult.ID)
	}
	if len(getResult.History) == 0 {
		t.Error("history should not be empty")
	}
}

// TestServerHandleTasksCancel tests the tasks/cancel handler.
func TestServerHandleTasksCancel(t *testing.T) {
	// Handler that takes time (simulated by doing nothing special)
	server := NewServer(nil)

	// Create a task
	sendParams := SendMessageParams{
		Message: Message{Role: "user", Parts: []Part{{Type: "text", Text: "Hello"}}},
	}
	sendJSON, _ := json.Marshal(sendParams)
	sendReq, _ := protocol.NewRequest("1", A2AMessageSend, json.RawMessage(sendJSON))
	sendData, _ := protocol.Encode(sendReq)
	sendResp, _ := server.HandleRequest(context.Background(), sendData)
	resp, _ := protocol.DecodeResponse(sendResp)
	var sendResult SendMessageResult
	resp.ParseResult(&sendResult)

	// Task is already completed, so it shouldn't be cancelable
	// Let's create a new task in submitted state directly
	store := server.Store()
	task := store.CreateTask("", &Message{Role: "user", Parts: []Part{{Type: "text", Text: "Cancel me"}}})

	// Cancel the task
	cancelParams := TaskCancelParams{ID: task.ID}
	cancelJSON, _ := json.Marshal(cancelParams)
	cancelReq, _ := protocol.NewRequest("2", A2ATasksCancel, json.RawMessage(cancelJSON))
	cancelData, _ := protocol.Encode(cancelReq)

	cancelResp, _ := server.HandleRequest(context.Background(), cancelData)
	resp, _ = protocol.DecodeResponse(cancelResp)

	if resp.IsError() {
		t.Fatalf("response is error: %v", resp.Error)
	}

	var cancelResult TaskCancelResult
	resp.ParseResult(&cancelResult)

	if cancelResult.Status.State != TaskStateCanceled {
		t.Errorf("state = %q, want %q", cancelResult.Status.State, TaskStateCanceled)
	}
}

// TestServerHandleMethodNotFound tests unknown method.
func TestServerHandleMethodNotFound(t *testing.T) {
	server := NewServer(nil)

	req, _ := protocol.NewRequest("1", "unknown/method", nil)
	reqData, _ := protocol.Encode(req)

	respData, _ := server.HandleRequest(context.Background(), reqData)
	resp, _ := protocol.DecodeResponse(respData)

	if !resp.IsError() {
		t.Error("response should be error for unknown method")
	}
	if resp.Error.Code != ErrCodeMethodNotFound {
		t.Errorf("error code = %d, want %d", resp.Error.Code, ErrCodeMethodNotFound)
	}
}

// TestServerHandleInvalidRequest tests invalid JSON-RPC request.
func TestServerHandleInvalidRequest(t *testing.T) {
	server := NewServer(nil)

	respData, _ := server.HandleRequest(context.Background(), []byte("not json"))
	resp, _ := protocol.DecodeResponse(respData)

	if !resp.IsError() {
		t.Error("response should be error for invalid request")
	}
	if resp.Error.Code != ErrCodeInvalidRequest {
		t.Errorf("error code = %d, want %d", resp.Error.Code, ErrCodeInvalidRequest)
	}
}

// TestServerStreaming tests streaming functionality.
func TestServerStreaming(t *testing.T) {
	handler := func(ctx context.Context, task *Task, msg *Message) (*Message, []Artifact, error) {
		return &Message{
				Role:  "agent",
				Parts: []Part{{Type: "text", Text: "Response"}},
			}, []Artifact{
				{Name: "file1.txt", Parts: []Part{{Type: "text", Text: "content1"}}},
				{Name: "file2.txt", Parts: []Part{{Type: "text", Text: "content2"}}},
			}, nil
	}

	server := NewServer(handler)

	params := SendMessageParams{
		Message: Message{
			Role:  "user",
			Parts: []Part{{Type: "text", Text: "Generate files"}},
		},
	}
	paramsJSON, _ := json.Marshal(params)

	var events []*StreamEvent
	sendFn := func(event *StreamEvent) error {
		events = append(events, event)
		return nil
	}

	err := server.HandleStreamRequest(context.Background(), paramsJSON, sendFn)
	if err != nil {
		t.Fatalf("HandleStreamRequest failed: %v", err)
	}

	// Should have: submitted status, working status, 2 artifacts, completed status
	if len(events) < 4 {
		t.Errorf("events count = %d, want >= 4", len(events))
	}

	// Check final event
	final := events[len(events)-1]
	if !final.Final {
		t.Error("last event should be final")
	}
	if final.Task.Status.State != TaskStateCompleted {
		t.Errorf("final state = %q, want %q", final.Task.Status.State, TaskStateCompleted)
	}
}

// TestServerStartCloseStream tests stream lifecycle.
func TestServerStartCloseStream(t *testing.T) {
	server := NewServer(nil)

	taskID := "test-task"
	ch := server.StartStream(taskID)
	if ch == nil {
		t.Fatal("StartStream returned nil channel")
	}

	// Send event
	server.SendStreamEvent(taskID, &StreamEvent{Type: "test"})

	// Should receive event
	select {
	case event := <-ch:
		if event.Type != "test" {
			t.Errorf("event type = %q, want %q", event.Type, "test")
		}
	default:
		t.Error("should have received event")
	}

	// Close stream
	server.CloseStream(taskID)

	// Channel should be closed
	_, ok := <-ch
	if ok {
		t.Error("channel should be closed")
	}
}

// TestServerAccessors tests Store and Adapter accessors.
func TestServerAccessors(t *testing.T) {
	server := NewServer(nil)

	if server.Store() == nil {
		t.Error("Store() should not return nil")
	}
	if server.Adapter() == nil {
		t.Error("Adapter() should not return nil")
	}
}

// TestTaskStateConstants tests task state constants.
func TestTaskStateConstants(t *testing.T) {
	states := []string{
		TaskStateSubmitted,
		TaskStateWorking,
		TaskStateInputRequired,
		TaskStateCompleted,
		TaskStateFailed,
		TaskStateCanceled,
	}

	for _, state := range states {
		if state == "" {
			t.Error("task state constant should not be empty")
		}
	}
}

// TestErrorCodeConstants tests error code constants.
func TestErrorCodeConstants(t *testing.T) {
	if ErrCodeTaskNotFound >= 0 {
		t.Error("error codes should be negative")
	}
	if ErrCodeTaskNotCancelable >= 0 {
		t.Error("error codes should be negative")
	}
}

// TestServerHandleTasksResubscribe tests the tasks/resubscribe handler.
func TestServerHandleTasksResubscribe(t *testing.T) {
	server := NewServer(nil)

	// Create a task first
	store := server.Store()
	task := store.CreateTask("session-1", &Message{
		Role:  "user",
		Parts: []Part{{Type: "text", Text: "Hello"}},
	})

	// Resubscribe to the task
	params := TaskResubscribeParams{ID: task.ID}
	paramsJSON, _ := json.Marshal(params)
	req, _ := protocol.NewRequest("1", A2ATasksResubscribe, json.RawMessage(paramsJSON))
	reqData, _ := protocol.Encode(req)

	respData, err := server.HandleRequest(context.Background(), reqData)
	if err != nil {
		t.Fatalf("HandleRequest failed: %v", err)
	}

	resp, _ := protocol.DecodeResponse(respData)
	if resp.IsError() {
		t.Fatalf("response is error: %v", resp.Error)
	}

	var result TaskResubscribeResult
	resp.ParseResult(&result)

	if result.ID != task.ID {
		t.Errorf("ID = %q, want %q", result.ID, task.ID)
	}
}

// TestServerHandleTasksResubscribeNotFound tests resubscribe to non-existent task.
func TestServerHandleTasksResubscribeNotFound(t *testing.T) {
	server := NewServer(nil)

	params := TaskResubscribeParams{ID: "nonexistent"}
	paramsJSON, _ := json.Marshal(params)
	req, _ := protocol.NewRequest("1", A2ATasksResubscribe, json.RawMessage(paramsJSON))
	reqData, _ := protocol.Encode(req)

	respData, _ := server.HandleRequest(context.Background(), reqData)
	resp, _ := protocol.DecodeResponse(respData)

	if !resp.IsError() {
		t.Error("response should be error for non-existent task")
	}
}

// TestSendStreamEventNonExistent tests sending event to non-existent stream.
func TestSendStreamEventNonExistent(t *testing.T) {
	server := NewServer(nil)

	// Should not panic
	server.SendStreamEvent("nonexistent", &StreamEvent{Type: "test"})
}

// TestCloseStreamNonExistent tests closing non-existent stream.
func TestCloseStreamNonExistent(t *testing.T) {
	server := NewServer(nil)

	// Should not panic
	server.CloseStream("nonexistent")
}

// TestTaskStoreUpdateNotFound tests updating non-existent task.
func TestTaskStoreUpdateNotFound(t *testing.T) {
	store := NewTaskStore()

	err := store.UpdateTaskStatus("nonexistent", TaskStateCompleted, nil)
	if err != ErrTaskNotFound {
		t.Errorf("err = %v, want ErrTaskNotFound", err)
	}
}

// TestTaskStoreAddMessageNotFound tests adding message to non-existent task.
func TestTaskStoreAddMessageNotFound(t *testing.T) {
	store := NewTaskStore()

	err := store.AddTaskMessage("nonexistent", TaskMessage{})
	if err != ErrTaskNotFound {
		t.Errorf("err = %v, want ErrTaskNotFound", err)
	}
}

// TestTaskStoreAddArtifactNotFound tests adding artifact to non-existent task.
func TestTaskStoreAddArtifactNotFound(t *testing.T) {
	store := NewTaskStore()

	err := store.AddTaskArtifact("nonexistent", Artifact{})
	if err != ErrTaskNotFound {
		t.Errorf("err = %v, want ErrTaskNotFound", err)
	}
}

// TestTaskStoreCancelNotFound tests canceling non-existent task.
func TestTaskStoreCancelNotFound(t *testing.T) {
	store := NewTaskStore()

	err := store.CancelTask("nonexistent")
	if err != ErrTaskNotFound {
		t.Errorf("err = %v, want ErrTaskNotFound", err)
	}
}

// TestTaskStoreCancelableStates tests canceling from different states.
func TestTaskStoreCancelableStates(t *testing.T) {
	states := []string{TaskStateSubmitted, TaskStateWorking, TaskStateInputRequired}

	for _, state := range states {
		t.Run(state, func(t *testing.T) {
			store := NewTaskStore()
			task := store.CreateTask("", &Message{Role: "user"})
			store.UpdateTaskStatus(task.ID, state, nil)

			err := store.CancelTask(task.ID)
			if err != nil {
				t.Errorf("should be able to cancel from state %q: %v", state, err)
			}

			updated, _ := store.GetTask(task.ID)
			if updated.Status.State != TaskStateCanceled {
				t.Errorf("state = %q, want %q", updated.Status.State, TaskStateCanceled)
			}
		})
	}
}

// TestTaskStoreNonCancelableStates tests non-cancelable states.
func TestTaskStoreNonCancelableStates(t *testing.T) {
	states := []string{TaskStateCompleted, TaskStateFailed, TaskStateCanceled}

	for _, state := range states {
		t.Run(state, func(t *testing.T) {
			store := NewTaskStore()
			task := store.CreateTask("", &Message{Role: "user"})
			store.UpdateTaskStatus(task.ID, state, nil)

			err := store.CancelTask(task.ID)
			if err != ErrTaskNotCancelable {
				t.Errorf("should not be able to cancel from state %q", state)
			}
		})
	}
}

// TestServerStreamingWithError tests streaming when handler returns error.
func TestServerStreamingWithError(t *testing.T) {
	handler := func(ctx context.Context, task *Task, msg *Message) (*Message, []Artifact, error) {
		return nil, nil, errors.New("handler error")
	}

	server := NewServer(handler)

	params := SendMessageParams{
		Message: Message{
			Role:  "user",
			Parts: []Part{{Type: "text", Text: "Hello"}},
		},
	}
	paramsJSON, _ := json.Marshal(params)

	var events []*StreamEvent
	sendFn := func(event *StreamEvent) error {
		events = append(events, event)
		return nil
	}

	err := server.HandleStreamRequest(context.Background(), paramsJSON, sendFn)
	if err != nil {
		t.Fatalf("HandleStreamRequest failed: %v", err)
	}

	// Check final event shows failed state
	final := events[len(events)-1]
	if final.Task.Status.State != TaskStateFailed {
		t.Errorf("final state = %q, want %q", final.Task.Status.State, TaskStateFailed)
	}
}

// TestServerStreamingSendError tests streaming when send fails.
func TestServerStreamingSendError(t *testing.T) {
	server := NewServer(nil)

	params := SendMessageParams{
		Message: Message{
			Role:  "user",
			Parts: []Part{{Type: "text", Text: "Hello"}},
		},
	}
	paramsJSON, _ := json.Marshal(params)

	sendFn := func(event *StreamEvent) error {
		return errors.New("send failed")
	}

	err := server.HandleStreamRequest(context.Background(), paramsJSON, sendFn)
	if err == nil {
		t.Error("expected error when send fails")
	}
}

// TestTaskStoreCreateTaskWithoutSession tests creating task without session.
func TestTaskStoreCreateTaskWithoutSession(t *testing.T) {
	store := NewTaskStore()

	msg := &Message{Role: "user", Parts: []Part{{Type: "text", Text: "Hi"}}}
	task := store.CreateTask("", msg)

	if task.SessionID != "" {
		t.Error("session ID should be empty")
	}

	// Session should not be created
	_, err := store.GetSession("")
	if err == nil {
		t.Error("should not find empty session")
	}
}

// TestServerHandleMessageSendWithArtifacts tests message send with artifacts.
func TestServerHandleMessageSendWithArtifacts(t *testing.T) {
	handler := func(ctx context.Context, task *Task, msg *Message) (*Message, []Artifact, error) {
		return &Message{
				Role:  "agent",
				Parts: []Part{{Type: "text", Text: "Here's your file"}},
			}, []Artifact{
				{Name: "result.txt", Parts: []Part{{Type: "text", Text: "file content"}}},
			}, nil
	}

	server := NewServer(handler)

	params := SendMessageParams{
		Message: Message{
			Role:  "user",
			Parts: []Part{{Type: "text", Text: "Generate file"}},
		},
	}

	paramsJSON, _ := json.Marshal(params)
	req, _ := protocol.NewRequest("1", A2AMessageSend, json.RawMessage(paramsJSON))
	reqData, _ := protocol.Encode(req)

	respData, _ := server.HandleRequest(context.Background(), reqData)
	resp, _ := protocol.DecodeResponse(respData)

	var result SendMessageResult
	resp.ParseResult(&result)

	if len(result.Artifacts) != 1 {
		t.Errorf("artifacts count = %d, want 1", len(result.Artifacts))
	}
}

// TestServerHandleInvalidParams tests handlers with invalid JSON params.
func TestServerHandleInvalidParams(t *testing.T) {
	server := NewServer(nil)

	tests := []struct {
		method string
		params string
	}{
		{A2AMessageSend, "not json"},
		{A2ATasksGet, "not json"},
		{A2ATasksCancel, "not json"},
		{A2ATasksResubscribe, "not json"},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			req := &protocol.JSONRPCRequest{
				JSONRPC: "2.0",
				ID:      "1",
				Method:  tt.method,
				Params:  json.RawMessage(tt.params),
			}
			reqData, _ := protocol.Encode(req)

			respData, _ := server.HandleRequest(context.Background(), reqData)
			resp, _ := protocol.DecodeResponse(respData)

			if !resp.IsError() {
				t.Error("expected error for invalid params")
			}
		})
	}
}

// --- Resource Management Tests ---

// TestTaskStoreWithConfig tests creating a task store with custom config.
func TestTaskStoreWithConfig(t *testing.T) {
	cfg := TaskStoreConfig{
		TaskTTL:            1 * time.Hour,
		CleanupPeriod:      10 * time.Second,
		MaxHistoryLen:      50,
		MaxTasksPerSession: 100,
	}

	store := NewTaskStoreWithConfig(cfg)
	defer store.Stop()

	if store.config.TaskTTL != 1*time.Hour {
		t.Errorf("TaskTTL = %v, want 1h", store.config.TaskTTL)
	}
	if store.config.MaxHistoryLen != 50 {
		t.Errorf("MaxHistoryLen = %d, want 50", store.config.MaxHistoryLen)
	}
}

// TestTaskStoreExpiry tests that expired tasks are cleaned up.
func TestTaskStoreExpiry(t *testing.T) {
	cfg := TaskStoreConfig{
		TaskTTL:            50 * time.Millisecond,
		CleanupPeriod:      10 * time.Millisecond,
		MaxHistoryLen:      100,
		MaxTasksPerSession: 1000,
	}

	store := NewTaskStoreWithConfig(cfg)
	defer store.Stop()

	msg := &Message{Role: "user", Parts: []Part{{Type: "text", Text: "test"}}}
	task := store.CreateTask("session-1", msg)
	taskID := task.ID

	// Task should exist
	if store.TaskCount() != 1 {
		t.Errorf("TaskCount = %d, want 1", store.TaskCount())
	}

	// Wait for TTL + cleanup interval
	time.Sleep(100 * time.Millisecond)

	// Task should be expired
	if store.TaskCount() != 0 {
		t.Errorf("TaskCount after expiry = %d, want 0", store.TaskCount())
	}

	// Should not be able to get the task
	_, err := store.GetTask(taskID)
	if err != ErrTaskNotFound {
		t.Errorf("GetTask after expiry error = %v, want ErrTaskNotFound", err)
	}
}

// TestTaskStoreMaxHistory tests history length limit.
func TestTaskStoreMaxHistory(t *testing.T) {
	cfg := TaskStoreConfig{
		TaskTTL:            1 * time.Hour,
		CleanupPeriod:      1 * time.Hour,
		MaxHistoryLen:      5,
		MaxTasksPerSession: 1000,
	}

	store := NewTaskStoreWithConfig(cfg)
	defer store.Stop()

	msg := &Message{Role: "user", Parts: []Part{{Type: "text", Text: "initial"}}}
	task := store.CreateTask("", msg)

	// Add more messages than MaxHistoryLen
	for i := 0; i < 10; i++ {
		store.AddTaskMessage(task.ID, TaskMessage{
			Role:  "user",
			Parts: []Part{{Type: "text", Text: "message"}},
		})
	}

	// Get task and check history length
	task, _ = store.GetTask(task.ID)
	if len(task.History) > cfg.MaxHistoryLen {
		t.Errorf("History length = %d, want <= %d", len(task.History), cfg.MaxHistoryLen)
	}
}

// TestTaskStoreMaxTasksPerSession tests max tasks per session limit.
func TestTaskStoreMaxTasksPerSession(t *testing.T) {
	cfg := TaskStoreConfig{
		TaskTTL:            1 * time.Hour,
		CleanupPeriod:      1 * time.Hour,
		MaxHistoryLen:      100,
		MaxTasksPerSession: 3,
	}

	store := NewTaskStoreWithConfig(cfg)
	defer store.Stop()

	msg := &Message{Role: "user", Parts: []Part{{Type: "text", Text: "test"}}}
	sessionID := "limited-session"

	// Create more tasks than the limit
	var taskIDs []string
	for i := 0; i < 5; i++ {
		task := store.CreateTask(sessionID, msg)
		taskIDs = append(taskIDs, task.ID)
	}

	// Session should only have MaxTasksPerSession tasks
	session, _ := store.GetSession(sessionID)
	if len(session.Tasks) > cfg.MaxTasksPerSession {
		t.Errorf("Session tasks = %d, want <= %d", len(session.Tasks), cfg.MaxTasksPerSession)
	}

	// Oldest tasks should have been removed
	_, err := store.GetTask(taskIDs[0])
	if err != ErrTaskNotFound {
		t.Errorf("Oldest task should have been removed, got err = %v", err)
	}

	// Newest task should exist
	_, err = store.GetTask(taskIDs[4])
	if err != nil {
		t.Errorf("Newest task should exist, got err = %v", err)
	}
}

// TestTaskStoreCounters tests TaskCount and SessionCount methods.
func TestTaskStoreCounters(t *testing.T) {
	store := NewTaskStore()
	defer store.Stop()

	if store.TaskCount() != 0 {
		t.Errorf("Initial TaskCount = %d, want 0", store.TaskCount())
	}
	if store.SessionCount() != 0 {
		t.Errorf("Initial SessionCount = %d, want 0", store.SessionCount())
	}

	msg := &Message{Role: "user", Parts: []Part{{Type: "text", Text: "test"}}}
	store.CreateTask("session-1", msg)
	store.CreateTask("session-2", msg)
	store.CreateTask("session-1", msg) // Same session

	if store.TaskCount() != 3 {
		t.Errorf("TaskCount = %d, want 3", store.TaskCount())
	}
	if store.SessionCount() != 2 {
		t.Errorf("SessionCount = %d, want 2", store.SessionCount())
	}
}

// TestIsTerminalState tests terminal state detection.
func TestIsTerminalState(t *testing.T) {
	tests := []struct {
		state    string
		terminal bool
	}{
		{TaskStateSubmitted, false},
		{TaskStateWorking, false},
		{TaskStateInputRequired, false},
		{TaskStateCompleted, true},
		{TaskStateFailed, true},
		{TaskStateCanceled, true},
	}

	for _, tt := range tests {
		t.Run(tt.state, func(t *testing.T) {
			if got := IsTerminalState(tt.state); got != tt.terminal {
				t.Errorf("IsTerminalState(%q) = %v, want %v", tt.state, got, tt.terminal)
			}
		})
	}
}

// TestCompleteTaskClosesStream tests that CompleteTask auto-closes streams.
func TestCompleteTaskClosesStream(t *testing.T) {
	server := NewServer(nil)

	msg := &Message{Role: "user", Parts: []Part{{Type: "text", Text: "test"}}}
	task := server.store.CreateTask("", msg)

	// Start a stream
	streamCh := server.StartStream(task.ID)

	// Verify stream is active
	server.streamsMu.RLock()
	_, exists := server.streams[task.ID]
	server.streamsMu.RUnlock()
	if !exists {
		t.Error("Stream should exist after StartStream")
	}

	// Complete the task
	err := server.CompleteTask(task.ID, TaskStateCompleted, nil)
	if err != nil {
		t.Errorf("CompleteTask error = %v", err)
	}

	// Stream should be closed
	server.streamsMu.RLock()
	_, exists = server.streams[task.ID]
	server.streamsMu.RUnlock()
	if exists {
		t.Error("Stream should be closed after CompleteTask")
	}

	// Channel should be closed (reading returns immediately)
	select {
	case _, ok := <-streamCh:
		if ok {
			t.Error("Stream channel should be closed")
		}
	default:
		t.Error("Stream channel should be readable (closed)")
	}
}

// TestCancelTaskClosesStream tests that cancel auto-closes streams.
func TestCancelTaskClosesStream(t *testing.T) {
	server := NewServer(nil)

	msg := &Message{Role: "user", Parts: []Part{{Type: "text", Text: "test"}}}
	task := server.store.CreateTask("", msg)

	// Start a stream
	server.StartStream(task.ID)

	// Cancel via JSON-RPC
	params := TaskCancelParams{ID: task.ID}
	paramsData, _ := json.Marshal(params)

	req := &protocol.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      "1",
		Method:  A2ATasksCancel,
		Params:  paramsData,
	}
	reqData, _ := protocol.Encode(req)

	server.HandleRequest(context.Background(), reqData)

	// Stream should be closed
	server.streamsMu.RLock()
	_, exists := server.streams[task.ID]
	server.streamsMu.RUnlock()
	if exists {
		t.Error("Stream should be closed after cancel")
	}
}

// --- Presence Tracking Tests ---

// TestUpdateParticipantPresence tests presence update.
func TestUpdateParticipantPresence(t *testing.T) {
	store := NewTaskStore()
	defer store.Stop()

	// Create a session first
	msg := &Message{Role: "user", Parts: []Part{{Type: "text", Text: "test"}}}
	store.CreateTask("session-1", msg)

	// Update presence
	err := store.UpdateParticipantPresence("session-1", "did:alice", "online")
	if err != nil {
		t.Fatalf("UpdateParticipantPresence failed: %v", err)
	}

	// Verify presence
	presence, err := store.GetParticipantPresence("session-1", "did:alice")
	if err != nil {
		t.Fatalf("GetParticipantPresence failed: %v", err)
	}
	if presence == nil {
		t.Fatal("presence should not be nil")
	}
	if presence.Status != "online" {
		t.Errorf("Status = %q, want %q", presence.Status, "online")
	}
	if presence.DID != "did:alice" {
		t.Errorf("DID = %q, want %q", presence.DID, "did:alice")
	}
}

// TestUpdateParticipantPresenceSessionNotFound tests presence update on non-existent session.
func TestUpdateParticipantPresenceSessionNotFound(t *testing.T) {
	store := NewTaskStore()
	defer store.Stop()

	err := store.UpdateParticipantPresence("nonexistent", "did:alice", "online")
	if err != ErrSessionNotFound {
		t.Errorf("err = %v, want ErrSessionNotFound", err)
	}
}

// TestGetParticipantPresenceNotFound tests getting presence for non-existent participant.
func TestGetParticipantPresenceNotFound(t *testing.T) {
	store := NewTaskStore()
	defer store.Stop()

	// Create session
	msg := &Message{Role: "user", Parts: []Part{{Type: "text", Text: "test"}}}
	store.CreateTask("session-1", msg)

	// Get non-existent participant
	presence, err := store.GetParticipantPresence("session-1", "did:nobody")
	if err != nil {
		t.Fatalf("GetParticipantPresence error: %v", err)
	}
	if presence != nil {
		t.Error("presence should be nil for non-existent participant")
	}
}

// TestGetSessionParticipants tests getting all participants.
func TestGetSessionParticipants(t *testing.T) {
	store := NewTaskStore()
	defer store.Stop()

	// Create session
	msg := &Message{Role: "user", Parts: []Part{{Type: "text", Text: "test"}}}
	store.CreateTask("session-1", msg)

	// Add participants
	store.UpdateParticipantPresence("session-1", "did:alice", "online")
	store.UpdateParticipantPresence("session-1", "did:bob", "away")

	// Get all participants
	participants, err := store.GetSessionParticipants("session-1")
	if err != nil {
		t.Fatalf("GetSessionParticipants failed: %v", err)
	}
	if len(participants) != 2 {
		t.Errorf("participants count = %d, want 2", len(participants))
	}
	if participants["did:alice"].Status != "online" {
		t.Errorf("alice status = %q, want %q", participants["did:alice"].Status, "online")
	}
	if participants["did:bob"].Status != "away" {
		t.Errorf("bob status = %q, want %q", participants["did:bob"].Status, "away")
	}
}

// TestGetSessionParticipantsNotFound tests getting participants from non-existent session.
func TestGetSessionParticipantsNotFound(t *testing.T) {
	store := NewTaskStore()
	defer store.Stop()

	_, err := store.GetSessionParticipants("nonexistent")
	if err != ErrSessionNotFound {
		t.Errorf("err = %v, want ErrSessionNotFound", err)
	}
}

// TestTouchSession tests session activity update.
func TestTouchSession(t *testing.T) {
	store := NewTaskStore()
	defer store.Stop()

	// Create session
	msg := &Message{Role: "user", Parts: []Part{{Type: "text", Text: "test"}}}
	store.CreateTask("session-1", msg)

	// Get initial activity time
	session, _ := store.GetSession("session-1")
	initialActivity := session.LastActivityAt

	// Wait and touch
	time.Sleep(10 * time.Millisecond)
	err := store.TouchSession("session-1")
	if err != nil {
		t.Fatalf("TouchSession failed: %v", err)
	}

	// Verify activity was updated
	session, _ = store.GetSession("session-1")
	if !session.LastActivityAt.After(initialActivity) {
		t.Error("LastActivityAt should be updated")
	}
}

// TestTouchSessionNotFound tests touching non-existent session.
func TestTouchSessionNotFound(t *testing.T) {
	store := NewTaskStore()
	defer store.Stop()

	err := store.TouchSession("nonexistent")
	if err != ErrSessionNotFound {
		t.Errorf("err = %v, want ErrSessionNotFound", err)
	}
}

// TestSessionLastActivityUpdatedOnTask tests that LastActivityAt is updated on task creation.
func TestSessionLastActivityUpdatedOnTask(t *testing.T) {
	store := NewTaskStore()
	defer store.Stop()

	msg := &Message{Role: "user", Parts: []Part{{Type: "text", Text: "test"}}}
	store.CreateTask("session-1", msg)

	session, _ := store.GetSession("session-1")
	if session.LastActivityAt.IsZero() {
		t.Error("LastActivityAt should be set on session creation")
	}

	// Create another task in the same session
	initialActivity := session.LastActivityAt
	time.Sleep(10 * time.Millisecond)
	store.CreateTask("session-1", msg)

	session, _ = store.GetSession("session-1")
	if !session.LastActivityAt.After(initialActivity) {
		t.Error("LastActivityAt should be updated on new task")
	}
}

// --- tasks/list Tests ---

// TestTaskStoreListTasks tests basic listing of tasks.
func TestTaskStoreListTasks(t *testing.T) {
	store := NewTaskStore()
	defer store.Stop()

	msg := &Message{Role: "user", Parts: []Part{{Type: "text", Text: "test"}}}

	// Create some tasks
	store.CreateTask("session-1", msg)
	store.CreateTask("session-1", msg)
	store.CreateTask("session-2", msg)

	// List all tasks
	result, err := store.ListTasks(TaskListFilter{}, "", 10)
	if err != nil {
		t.Fatalf("ListTasks failed: %v", err)
	}

	if result.Total != 3 {
		t.Errorf("Total = %d, want 3", result.Total)
	}
	if len(result.Tasks) != 3 {
		t.Errorf("Tasks count = %d, want 3", len(result.Tasks))
	}
}

// TestTaskStoreListTasksBySession tests filtering by session.
func TestTaskStoreListTasksBySession(t *testing.T) {
	store := NewTaskStore()
	defer store.Stop()

	msg := &Message{Role: "user", Parts: []Part{{Type: "text", Text: "test"}}}

	// Create tasks in different sessions
	store.CreateTask("session-1", msg)
	store.CreateTask("session-1", msg)
	store.CreateTask("session-2", msg)

	// List tasks for session-1 only
	result, err := store.ListTasks(TaskListFilter{SessionID: "session-1"}, "", 10)
	if err != nil {
		t.Fatalf("ListTasks failed: %v", err)
	}

	if result.Total != 2 {
		t.Errorf("Total = %d, want 2", result.Total)
	}
	for _, task := range result.Tasks {
		if task.SessionID != "session-1" {
			t.Errorf("Task sessionID = %q, want session-1", task.SessionID)
		}
	}
}

// TestTaskStoreListTasksByStatus tests filtering by status.
func TestTaskStoreListTasksByStatus(t *testing.T) {
	store := NewTaskStore()
	defer store.Stop()

	msg := &Message{Role: "user", Parts: []Part{{Type: "text", Text: "test"}}}

	// Create tasks with different statuses
	task1 := store.CreateTask("session-1", msg)
	task2 := store.CreateTask("session-1", msg)
	_ = store.CreateTask("session-1", msg) // stays in submitted state

	store.UpdateTaskStatus(task1.ID, TaskStateCompleted, nil)
	store.UpdateTaskStatus(task2.ID, TaskStateFailed, nil)

	// List only completed tasks
	result, err := store.ListTasks(TaskListFilter{Status: []string{TaskStateCompleted}}, "", 10)
	if err != nil {
		t.Fatalf("ListTasks failed: %v", err)
	}

	if result.Total != 1 {
		t.Errorf("Total = %d, want 1", result.Total)
	}

	// List completed and failed tasks
	result, err = store.ListTasks(TaskListFilter{Status: []string{TaskStateCompleted, TaskStateFailed}}, "", 10)
	if err != nil {
		t.Fatalf("ListTasks failed: %v", err)
	}

	if result.Total != 2 {
		t.Errorf("Total = %d, want 2", result.Total)
	}
}

// TestTaskStoreListTasksPagination tests pagination.
func TestTaskStoreListTasksPagination(t *testing.T) {
	store := NewTaskStore()
	defer store.Stop()

	msg := &Message{Role: "user", Parts: []Part{{Type: "text", Text: "test"}}}

	// Create 5 tasks
	for i := 0; i < 5; i++ {
		store.CreateTask("session-1", msg)
	}

	// Get first page (limit 2)
	result1, err := store.ListTasks(TaskListFilter{}, "", 2)
	if err != nil {
		t.Fatalf("ListTasks page 1 failed: %v", err)
	}

	if len(result1.Tasks) != 2 {
		t.Errorf("Page 1 tasks = %d, want 2", len(result1.Tasks))
	}
	if result1.Total != 5 {
		t.Errorf("Total = %d, want 5", result1.Total)
	}
	if result1.NextCursor == "" {
		t.Error("NextCursor should be set for pagination")
	}

	// Get second page using cursor
	result2, err := store.ListTasks(TaskListFilter{}, result1.NextCursor, 2)
	if err != nil {
		t.Fatalf("ListTasks page 2 failed: %v", err)
	}

	if len(result2.Tasks) != 2 {
		t.Errorf("Page 2 tasks = %d, want 2", len(result2.Tasks))
	}

	// Tasks should be different from page 1
	for _, t1 := range result1.Tasks {
		for _, t2 := range result2.Tasks {
			if t1.ID == t2.ID {
				t.Errorf("Task %s appears in both pages", t1.ID)
			}
		}
	}
}

// TestServerHandleTasksList tests the tasks/list JSON-RPC method.
func TestServerHandleTasksList(t *testing.T) {
	server := NewServer(nil)

	msg := &Message{Role: "user", Parts: []Part{{Type: "text", Text: "test"}}}
	server.store.CreateTask("session-1", msg)
	server.store.CreateTask("session-1", msg)

	params := TasksListParams{SessionID: "session-1", Limit: 10}
	paramsJSON, _ := json.Marshal(params)
	req, _ := protocol.NewRequest("1", A2ATasksList, json.RawMessage(paramsJSON))
	reqData, _ := protocol.Encode(req)

	respData, err := server.HandleRequest(context.Background(), reqData)
	if err != nil {
		t.Fatalf("HandleRequest failed: %v", err)
	}

	resp, _ := protocol.DecodeResponse(respData)
	if resp.IsError() {
		t.Fatalf("response is error: %v", resp.Error)
	}

	var result TasksListResponse
	resp.ParseResult(&result)

	if result.Total != 2 {
		t.Errorf("Total = %d, want 2", result.Total)
	}
	if len(result.Tasks) != 2 {
		t.Errorf("Tasks count = %d, want 2", len(result.Tasks))
	}
}

// TestServerHandleTasksListWithContextID tests tasks/list with contextId parameter.
func TestServerHandleTasksListWithContextID(t *testing.T) {
	server := NewServer(nil)

	msg := &Message{Role: "user", Parts: []Part{{Type: "text", Text: "test"}}}
	server.store.CreateTask("ctx-123", msg)
	server.store.CreateTask("ctx-456", msg)

	// Use contextId instead of sessionId
	params := TasksListParams{ContextID: "ctx-123", Limit: 10}
	paramsJSON, _ := json.Marshal(params)
	req, _ := protocol.NewRequest("1", A2ATasksList, json.RawMessage(paramsJSON))
	reqData, _ := protocol.Encode(req)

	respData, _ := server.HandleRequest(context.Background(), reqData)
	resp, _ := protocol.DecodeResponse(respData)

	var result TasksListResponse
	resp.ParseResult(&result)

	if result.Total != 1 {
		t.Errorf("Total = %d, want 1 (filtered by contextId)", result.Total)
	}
}

// TestServerHandleGetExtendedAgentCard tests agent/getExtendedAgentCard method.
func TestServerHandleGetExtendedAgentCard(t *testing.T) {
	server := NewServer(nil)

	req, _ := protocol.NewRequest("1", A2AGetExtendedAgentCard, json.RawMessage("{}"))
	reqData, _ := protocol.Encode(req)

	respData, err := server.HandleRequest(context.Background(), reqData)
	if err != nil {
		t.Fatalf("HandleRequest failed: %v", err)
	}

	resp, _ := protocol.DecodeResponse(respData)
	if resp.IsError() {
		t.Fatalf("response is error: %v", resp.Error)
	}

	var result ExtendedAgentCardResponse
	resp.ParseResult(&result)

	if result.Name == "" {
		t.Error("Name should be set")
	}
	if !result.AuthenticationRequired {
		t.Error("AuthenticationRequired should be true")
	}
	if len(result.ProtocolVersions) == 0 {
		t.Error("ProtocolVersions should be set")
	}
}
