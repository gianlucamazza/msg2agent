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
