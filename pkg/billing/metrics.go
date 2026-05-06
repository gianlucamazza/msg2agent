package billing

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	billingQuotaExceeded = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "billing_quota_exceeded_total",
		Help: "Total quota-exceeded rejections per tenant and event type",
	}, []string{"tenant_id", "event"})

	billingRateLimited = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "billing_rate_limited_total",
		Help: "Total tenant rate-limit rejections per tenant",
	}, []string{"tenant_id"})

	billingAuditDropped = promauto.NewCounter(prometheus.CounterOpts{
		Name: "billing_audit_dropped_total",
		Help: "Total audit events dropped due to full channel",
	})

	billingUsageEvents = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "billing_usage_events_total",
		Help: "Total billable events recorded per tenant and event type",
	}, []string{"tenant_id", "event"})

	billingQuotaRatio = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "billing_quota_usage_ratio",
		Help: "Current quota usage ratio (0.0–1.0) per tenant and event type",
	}, []string{"tenant_id", "event"})

	billingAuditChainTampered = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "billing_audit_chain_tampered_total",
		Help: "Audit chain divergences detected by VerifyAuditChain",
	}, []string{"tenant_id"})

	billingWebhookDelivery = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "billing_webhook_delivery_total",
		Help: "Webhook delivery outcomes",
	}, []string{"status"})

	billingWebhookDropped = promauto.NewCounter(prometheus.CounterOpts{
		Name: "billing_webhook_dropped_total",
		Help: "Webhook events dropped after all retries exhausted",
	})
)

// RecordQuotaExceeded increments the quota-exceeded counter for external callers
// (e.g. relay handleRegister enforcing MaxAgentDIDs).
func RecordQuotaExceeded(tenantID, event string) {
	billingQuotaExceeded.WithLabelValues(tenantID, event).Inc()
}

// RecordRateLimited increments the rate-limited counter for external callers
// (e.g. relay handleMessage enforcing tenant rate limit).
func RecordRateLimited(tenantID string) {
	billingRateLimited.WithLabelValues(tenantID).Inc()
}

// RecordAuditChainTampered increments the tamper counter for a tenant.
func RecordAuditChainTampered(tenantID string) {
	billingAuditChainTampered.WithLabelValues(tenantID).Inc()
}

// RecordWebhookDelivery increments the webhook delivery counter with the given status ("success" or "failure").
func RecordWebhookDelivery(status string) {
	billingWebhookDelivery.WithLabelValues(status).Inc()
}

// RecordWebhookDropped increments the dropped counter when all retries are exhausted.
func RecordWebhookDropped() {
	billingWebhookDropped.Inc()
}
