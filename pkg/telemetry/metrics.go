package telemetry

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Common labels used across metrics.
const (
	LabelAgentDID  = "agent_did"
	LabelMethod    = "method"
	LabelPeerDID   = "peer_did"
	LabelStatus    = "status"
	LabelReason    = "reason"
	LabelTransport = "transport"
	LabelTaskState = "task_state"
)

// AgentMetrics holds Prometheus metrics for an agent.
type AgentMetrics struct {
	// Message metrics
	MessagesSent     *prometheus.CounterVec
	MessagesReceived *prometheus.CounterVec
	MessageErrors    *prometheus.CounterVec

	// Connection metrics
	PeersConnected prometheus.Gauge
	PeersTotal     prometheus.Counter

	// Handler metrics
	HandlerCalls    *prometheus.CounterVec
	HandlerDuration *prometheus.HistogramVec
	HandlerErrors   *prometheus.CounterVec

	// Crypto metrics
	EncryptionOps *prometheus.CounterVec
	DecryptionOps *prometheus.CounterVec
	SignatureOps  *prometheus.CounterVec

	// Dedup metrics
	DuplicatesDropped prometheus.Counter

	// A2A task metrics (if using A2A adapter)
	TasksCreated   *prometheus.CounterVec
	TasksCompleted *prometheus.CounterVec
	TaskDuration   *prometheus.HistogramVec
}

// NewAgentMetrics creates a new set of agent metrics with the given namespace.
// The namespace should identify the agent (e.g., "agent_alice").
func NewAgentMetrics(namespace string) *AgentMetrics {
	return &AgentMetrics{
		MessagesSent: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "messages_sent_total",
			Help:      "Total number of messages sent",
		}, []string{LabelMethod, LabelPeerDID}),

		MessagesReceived: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "messages_received_total",
			Help:      "Total number of messages received",
		}, []string{LabelMethod, LabelPeerDID}),

		MessageErrors: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "message_errors_total",
			Help:      "Total number of message errors",
		}, []string{LabelReason}),

		PeersConnected: promauto.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "peers_connected",
			Help:      "Current number of connected peers",
		}),

		PeersTotal: promauto.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "peers_total",
			Help:      "Total number of peer connections (lifetime)",
		}),

		HandlerCalls: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "handler_calls_total",
			Help:      "Total number of handler calls",
		}, []string{LabelMethod}),

		HandlerDuration: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "handler_duration_seconds",
			Help:      "Handler execution duration in seconds",
			Buckets:   prometheus.DefBuckets,
		}, []string{LabelMethod}),

		HandlerErrors: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "handler_errors_total",
			Help:      "Total number of handler errors",
		}, []string{LabelMethod}),

		EncryptionOps: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "encryption_ops_total",
			Help:      "Total number of encryption operations",
		}, []string{LabelStatus}),

		DecryptionOps: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "decryption_ops_total",
			Help:      "Total number of decryption operations",
		}, []string{LabelStatus}),

		SignatureOps: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "signature_ops_total",
			Help:      "Total number of signature operations",
		}, []string{LabelStatus}),

		DuplicatesDropped: promauto.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "duplicates_dropped_total",
			Help:      "Total number of duplicate messages dropped",
		}),

		TasksCreated: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "tasks_created_total",
			Help:      "Total number of A2A tasks created",
		}, []string{LabelTaskState}),

		TasksCompleted: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "tasks_completed_total",
			Help:      "Total number of A2A tasks completed",
		}, []string{LabelTaskState}),

		TaskDuration: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "task_duration_seconds",
			Help:      "A2A task duration in seconds",
			Buckets:   []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60, 120, 300},
		}, []string{LabelTaskState}),
	}
}

// RecordMessageSent records a sent message.
func (m *AgentMetrics) RecordMessageSent(method, peerDID string) {
	m.MessagesSent.WithLabelValues(method, peerDID).Inc()
}

// RecordMessageReceived records a received message.
func (m *AgentMetrics) RecordMessageReceived(method, peerDID string) {
	m.MessagesReceived.WithLabelValues(method, peerDID).Inc()
}

// RecordMessageError records a message error.
func (m *AgentMetrics) RecordMessageError(reason string) {
	m.MessageErrors.WithLabelValues(reason).Inc()
}

// RecordPeerConnected increments connected peers count.
func (m *AgentMetrics) RecordPeerConnected() {
	m.PeersConnected.Inc()
	m.PeersTotal.Inc()
}

// RecordPeerDisconnected decrements connected peers count.
func (m *AgentMetrics) RecordPeerDisconnected() {
	m.PeersConnected.Dec()
}

// RecordHandlerCall records a handler invocation.
func (m *AgentMetrics) RecordHandlerCall(method string) {
	m.HandlerCalls.WithLabelValues(method).Inc()
}

// RecordHandlerDuration records handler execution time.
func (m *AgentMetrics) RecordHandlerDuration(method string, seconds float64) {
	m.HandlerDuration.WithLabelValues(method).Observe(seconds)
}

// RecordHandlerError records a handler error.
func (m *AgentMetrics) RecordHandlerError(method string) {
	m.HandlerErrors.WithLabelValues(method).Inc()
}

// RecordEncryption records an encryption operation.
func (m *AgentMetrics) RecordEncryption(success bool) {
	status := "success"
	if !success {
		status = "failure"
	}
	m.EncryptionOps.WithLabelValues(status).Inc()
}

// RecordDecryption records a decryption operation.
func (m *AgentMetrics) RecordDecryption(success bool) {
	status := "success"
	if !success {
		status = "failure"
	}
	m.DecryptionOps.WithLabelValues(status).Inc()
}

// RecordSignature records a signature operation.
func (m *AgentMetrics) RecordSignature(success bool) {
	status := "success"
	if !success {
		status = "failure"
	}
	m.SignatureOps.WithLabelValues(status).Inc()
}

// RecordDuplicateDropped records a dropped duplicate message.
func (m *AgentMetrics) RecordDuplicateDropped() {
	m.DuplicatesDropped.Inc()
}

// RecordTaskCreated records a new A2A task.
func (m *AgentMetrics) RecordTaskCreated(state string) {
	m.TasksCreated.WithLabelValues(state).Inc()
}

// RecordTaskCompleted records a completed A2A task.
func (m *AgentMetrics) RecordTaskCompleted(state string) {
	m.TasksCompleted.WithLabelValues(state).Inc()
}

// RecordTaskDuration records the duration of an A2A task.
func (m *AgentMetrics) RecordTaskDuration(state string, seconds float64) {
	m.TaskDuration.WithLabelValues(state).Observe(seconds)
}
