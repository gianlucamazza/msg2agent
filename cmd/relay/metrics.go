package main

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics for the relay hub.
var (
	// Connection metrics
	connectionsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "relay_connections_total",
		Help: "Total number of WebSocket connections accepted",
	})

	connectionsCurrent = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "relay_connections_current",
		Help: "Current number of active connections",
	})

	connectionsRejected = promauto.NewCounter(prometheus.CounterOpts{
		Name: "relay_connections_rejected_total",
		Help: "Total number of connections rejected due to limit",
	})

	// Message metrics
	messagesRouted = promauto.NewCounter(prometheus.CounterOpts{
		Name: "relay_messages_routed_total",
		Help: "Total number of messages successfully routed",
	})

	messagesDropped = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "relay_messages_dropped_total",
		Help: "Total number of messages dropped",
	}, []string{"reason"})

	// Queue metrics
	messagesQueued = promauto.NewCounter(prometheus.CounterOpts{
		Name: "relay_messages_queued_total",
		Help: "Total number of messages queued for offline delivery",
	})

	messagesDeliveredFromQueue = promauto.NewCounter(prometheus.CounterOpts{
		Name: "relay_messages_delivered_from_queue_total",
		Help: "Total number of queued messages delivered when recipient came online",
	})

	// Rate limit metrics
	rateLimitHits = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "relay_rate_limit_hits_total",
		Help: "Total number of rate limit hits",
	}, []string{"type"})

	// Registration metrics
	registrationsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "relay_registrations_total",
		Help: "Total number of agent registrations",
	})

	// Discovery metrics
	discoveriesTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "relay_discoveries_total",
		Help: "Total number of discovery requests",
	})

	// Error metrics
	errorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "relay_errors_total",
		Help: "Total number of errors",
	}, []string{"type"})
)

// recordConnectionAccepted records a new connection.
func recordConnectionAccepted() {
	connectionsTotal.Inc()
	connectionsCurrent.Inc()
}

// recordConnectionClosed records a closed connection.
func recordConnectionClosed() {
	connectionsCurrent.Dec()
}

// recordConnectionRejected records a rejected connection.
func recordConnectionRejected() {
	connectionsRejected.Inc()
}

// recordMessageRouted records a successfully routed message.
func recordMessageRouted() {
	messagesRouted.Inc()
}

// recordMessageDropped records a dropped message.
func recordMessageDropped(reason string) {
	messagesDropped.WithLabelValues(reason).Inc()
}

// recordMessageQueued records a message queued for offline delivery.
func recordMessageQueued() {
	messagesQueued.Inc()
}

// recordMessageDeliveredFromQueue records a queued message delivered.
func recordMessageDeliveredFromQueue() {
	messagesDeliveredFromQueue.Inc()
}

// recordRateLimitHit records a rate limit hit.
func recordRateLimitHit(limitType string) {
	rateLimitHits.WithLabelValues(limitType).Inc()
}

// recordRegistration records an agent registration.
func recordRegistration() {
	registrationsTotal.Inc()
}

// recordDiscovery records a discovery request.
func recordDiscovery() {
	discoveriesTotal.Inc()
}

// recordError records an error.
func recordError(errorType string) {
	errorsTotal.WithLabelValues(errorType).Inc()
}
