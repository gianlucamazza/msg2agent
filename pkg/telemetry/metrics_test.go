package telemetry

import (
	"testing"
)

func TestNewAgentMetrics(t *testing.T) {
	// Use unique namespace to avoid conflicts between tests
	m := NewAgentMetrics("test_agent_1")

	if m.MessagesSent == nil {
		t.Error("MessagesSent should not be nil")
	}
	if m.MessagesReceived == nil {
		t.Error("MessagesReceived should not be nil")
	}
	if m.HandlerCalls == nil {
		t.Error("HandlerCalls should not be nil")
	}
	if m.HandlerDuration == nil {
		t.Error("HandlerDuration should not be nil")
	}
}

func TestRecordMessageSent(t *testing.T) {
	m := NewAgentMetrics("test_agent_2")

	// Should not panic
	m.RecordMessageSent("ping", "did:wba:example.com:agent:bob")
	m.RecordMessageSent("echo", "did:wba:example.com:agent:charlie")
}

func TestRecordMessageReceived(t *testing.T) {
	m := NewAgentMetrics("test_agent_3")

	// Should not panic
	m.RecordMessageReceived("ping", "did:wba:example.com:agent:alice")
	m.RecordMessageReceived("pong", "did:wba:example.com:agent:alice")
}

func TestRecordMessageError(t *testing.T) {
	m := NewAgentMetrics("test_agent_4")

	// Should not panic
	m.RecordMessageError("signature_invalid")
	m.RecordMessageError("decryption_failed")
	m.RecordMessageError("peer_not_found")
}

func TestRecordPeerConnection(t *testing.T) {
	m := NewAgentMetrics("test_agent_5")

	// Should not panic
	m.RecordPeerConnected()
	m.RecordPeerConnected()
	m.RecordPeerDisconnected()
}

func TestRecordHandler(t *testing.T) {
	m := NewAgentMetrics("test_agent_6")

	// Should not panic
	m.RecordHandlerCall("ping")
	m.RecordHandlerDuration("ping", 0.05)
	m.RecordHandlerError("ping")
}

func TestRecordCrypto(t *testing.T) {
	m := NewAgentMetrics("test_agent_7")

	// Should not panic
	m.RecordEncryption(true)
	m.RecordEncryption(false)
	m.RecordDecryption(true)
	m.RecordDecryption(false)
	m.RecordSignature(true)
	m.RecordSignature(false)
}

func TestRecordDuplicateDropped(t *testing.T) {
	m := NewAgentMetrics("test_agent_8")

	// Should not panic
	m.RecordDuplicateDropped()
	m.RecordDuplicateDropped()
}

func TestRecordTask(t *testing.T) {
	m := NewAgentMetrics("test_agent_9")

	// Should not panic
	m.RecordTaskCreated("pending")
	m.RecordTaskCreated("working")
	m.RecordTaskCompleted("completed")
	m.RecordTaskCompleted("failed")
	m.RecordTaskDuration("completed", 5.5)
}

func TestDefaultAgentMetrics(t *testing.T) {
	// Default metrics should be available
	if DefaultAgentMetrics == nil {
		t.Error("DefaultAgentMetrics should not be nil")
	}

	// Should be usable without panic
	DefaultAgentMetrics.RecordMessageSent("test", "test-did")
}

func TestLabelConstants(t *testing.T) {
	tests := []struct {
		label    string
		expected string
	}{
		{LabelAgentDID, "agent_did"},
		{LabelMethod, "method"},
		{LabelPeerDID, "peer_did"},
		{LabelStatus, "status"},
		{LabelReason, "reason"},
		{LabelTransport, "transport"},
		{LabelTaskState, "task_state"},
	}

	for _, tt := range tests {
		if tt.label != tt.expected {
			t.Errorf("label = %q, want %q", tt.label, tt.expected)
		}
	}
}
