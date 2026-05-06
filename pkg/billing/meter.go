package billing

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"go.opentelemetry.io/otel/attribute"

	"github.com/gianlucamazza/msg2agent/pkg/telemetry"
)

// UsageEvent identifies a billable action.
type UsageEvent string

const (
	EventToolCall   UsageEvent = "tool_call"
	EventMessage    UsageEvent = "message"
	EventTaskSubmit UsageEvent = "task_submit"
)

// periodKey produces a YYYY-MM string for monthly bucketing (UTC).
func periodKey(t time.Time) string {
	return fmt.Sprintf("%04d-%02d", t.Year(), t.Month())
}

type counterKey struct {
	tenantID string
	period   string
	event    UsageEvent
}

type auditEvent struct {
	tenantID  string
	event     string
	toolName  string
	requestID string
}

// UsageMeter counts billable events per tenant per calendar month.
// Call WithStore to enable async audit persistence to an EventStore.
type UsageMeter struct {
	mu       sync.RWMutex
	counters map[counterKey]*atomic.Int64

	// optional async persistence
	eventCh chan auditEvent
	logger  *slog.Logger

	// optional quota-threshold notifications
	notifier Notifier
	notifySt *notifierState
}

// NewUsageMeter creates an in-memory meter with no persistence.
func NewUsageMeter() *UsageMeter {
	return &UsageMeter{counters: make(map[counterKey]*atomic.Int64)}
}

// WithNotifier registers a Notifier that fires when quota crosses the warn
// (default 80%, configurable via BILLING_QUOTA_WARN_RATIO) or exceeded (100%) threshold.
// Idempotent per counterKey+period: each threshold is fired at most once per period.
func (m *UsageMeter) WithNotifier(n Notifier) {
	m.notifier = n
	m.notifySt = newNotifierState()
}

// WithStore starts the background audit writer that persists events to store.
// It also flushes aggregate snapshots every flushInterval.
// Must be called before recording events. Goroutine exits when ctx is done.
func (m *UsageMeter) WithStore(ctx context.Context, store EventStore, logger *slog.Logger) {
	const bufSize = 1000
	const flushInterval = 5 * time.Second
	const batchSize = 100

	m.eventCh = make(chan auditEvent, bufSize)
	m.logger = logger

	go func() {
		ticker := time.NewTicker(flushInterval)
		defer ticker.Stop()

		batch := make([]auditEvent, 0, batchSize)

		flush := func() {
			if len(batch) == 0 {
				return
			}
			for _, ev := range batch {
				if err := store.RecordEvent(ev.tenantID, ev.event, ev.toolName, ev.requestID); err != nil {
					if logger != nil {
						logger.Warn("billing: failed to persist event", "error", err)
					}
				}
			}
			batch = batch[:0]
			// Flush aggregates snapshot.
			if err := store.FlushAggregates(m.Snapshot()); err != nil {
				if logger != nil {
					logger.Warn("billing: failed to flush aggregates", "error", err)
				}
			}
		}

		for {
			select {
			case ev, ok := <-m.eventCh:
				if !ok {
					flush()
					return
				}
				batch = append(batch, ev)
				if len(batch) >= batchSize {
					flush()
				}
			case <-ticker.C:
				flush()
			case <-ctx.Done():
				flush()
				return
			}
		}
	}()
}

// getOrCreateCounter returns the atomic counter for (tenantID, current month, event),
// creating it if necessary. Safe for concurrent use.
func (m *UsageMeter) getOrCreateCounter(tenantID string, event UsageEvent) *atomic.Int64 {
	k := counterKey{tenantID: tenantID, period: periodKey(time.Now().UTC()), event: event}
	m.mu.RLock()
	c, ok := m.counters[k]
	m.mu.RUnlock()
	if !ok {
		m.mu.Lock()
		if c, ok = m.counters[k]; !ok {
			c = &atomic.Int64{}
			m.counters[k] = c
		}
		m.mu.Unlock()
	}
	return c
}

// Record increments the counter for (tenantID, current month, event) by delta.
func (m *UsageMeter) Record(tenantID string, event UsageEvent, delta int64) {
	m.getOrCreateCounter(tenantID, event).Add(delta)
}

// TryConsume atomically increments the counter by delta and returns ErrQuotaExceeded
// if the new value exceeds limit. On failure the increment is rolled back.
// Pass limit ≤ 0 to skip the quota check. Updates Prometheus gauges/counters.
func (m *UsageMeter) TryConsume(tenantID string, event UsageEvent, limit, delta int64) error {
	_, span := telemetry.StartSpan(context.Background(), "billing", "billing.TryConsume")
	defer span.End()
	span.SetAttributes(
		attribute.String("billing.tenant_id", tenantID),
		attribute.String("billing.event", string(event)),
		attribute.Int64("billing.quota.limit", limit),
	)

	c := m.getOrCreateCounter(tenantID, event)
	newVal := c.Add(delta)
	if limit > 0 && newVal > limit {
		c.Add(-delta)
		billingQuotaRatio.WithLabelValues(tenantID, string(event)).Set(1.0)
		billingQuotaExceeded.WithLabelValues(tenantID, string(event)).Inc()
		m.maybeNotify(tenantID, event, limit, 1.0)
		err := fmt.Errorf("%w: %s limit %d reached for tenant %s",
			ErrQuotaExceeded, event, limit, tenantID)
		span.RecordError(err)
		span.SetAttributes(attribute.Int64("billing.quota.current", newVal))
		return err
	}
	if limit > 0 {
		ratio := float64(newVal) / float64(limit)
		billingQuotaRatio.WithLabelValues(tenantID, string(event)).Set(ratio)
		m.maybeNotify(tenantID, event, limit, ratio)
		span.SetAttributes(attribute.Int64("billing.quota.current", newVal))
	}
	return nil
}

// ReleaseQuota decrements the counter by delta — used to roll back a successful TryConsume
// when the downstream handler fails and the event should not be billed.
func (m *UsageMeter) ReleaseQuota(tenantID string, event UsageEvent, delta int64) {
	m.getOrCreateCounter(tenantID, event).Add(-delta)
}

// maybeNotify fires the registered Notifier when ratio crosses a threshold for the first
// time in this period. No-op when no notifier is configured.
func (m *UsageMeter) maybeNotify(tenantID string, event UsageEvent, limit int64, ratio float64) {
	if m.notifier == nil {
		return
	}
	k := counterKey{tenantID: tenantID, period: periodKey(time.Now().UTC()), event: event}
	evType, ok := m.notifySt.shouldNotify(k, ratio)
	if !ok {
		return
	}
	current := int64(ratio * float64(limit))
	ev := NotifyEvent{
		TenantID:  tenantID,
		Period:    k.period,
		Event:     event,
		EventType: evType,
		Current:   current,
		Limit:     limit,
		Ratio:     ratio,
		Timestamp: time.Now().UTC(),
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := m.notifier.Notify(ctx, ev); err != nil && m.logger != nil {
			m.logger.Warn("billing: quota notification failed", "tenant", tenantID, "type", evType, "error", err)
		}
	}()
}

// queueAudit sends an audit event to the persistence channel (best-effort) and
// increments the Prometheus usage counter. Does NOT touch the in-memory counter.
func (m *UsageMeter) queueAudit(tenantID string, event UsageEvent, toolName, requestID string) {
	billingUsageEvents.WithLabelValues(tenantID, string(event)).Add(1)
	if m.eventCh != nil {
		select {
		case m.eventCh <- auditEvent{tenantID: tenantID, event: string(event), toolName: toolName, requestID: requestID}:
		default:
			billingAuditDropped.Inc()
			if m.logger != nil {
				m.logger.Warn("billing: audit event channel full, event dropped", "tenant", tenantID, "event", event)
			}
		}
	}
}

// RecordAudit increments the counter AND queues an audit event for persistence.
func (m *UsageMeter) RecordAudit(tenantID string, event UsageEvent, toolName, requestID string, delta int64) {
	m.Record(tenantID, event, delta)
	m.queueAudit(tenantID, event, toolName, requestID)
}

// RestoreFromAggregates repopulates in-memory counters from the EventStore on startup.
func (m *UsageMeter) RestoreFromAggregates(store EventStore) error {
	snapshots, err := store.LoadAggregates()
	if err != nil {
		return err
	}
	for _, snap := range snapshots {
		k := counterKey{tenantID: snap.TenantID, period: snap.Period, event: snap.Event}
		m.mu.Lock()
		if _, ok := m.counters[k]; !ok {
			c := &atomic.Int64{}
			c.Store(snap.Count)
			m.counters[k] = c
		}
		m.mu.Unlock()
	}
	return nil
}

// Current returns the count for a tenant/event in the current calendar month.
func (m *UsageMeter) Current(tenantID string, event UsageEvent) int64 {
	k := counterKey{tenantID: tenantID, period: periodKey(time.Now().UTC()), event: event}
	m.mu.RLock()
	c, ok := m.counters[k]
	m.mu.RUnlock()
	if !ok {
		return 0
	}
	return c.Load()
}

// CheckQuota returns ErrQuotaExceeded if the tenant has reached the monthly limit.
// Pass limit ≤ 0 to skip the check. Updates the quota_usage_ratio gauge on every call.
func (m *UsageMeter) CheckQuota(tenantID string, event UsageEvent, limit int64) error {
	if limit <= 0 {
		return nil
	}
	current := m.Current(tenantID, event)
	billingQuotaRatio.WithLabelValues(tenantID, string(event)).Set(float64(current) / float64(limit))
	if current >= limit {
		billingQuotaExceeded.WithLabelValues(tenantID, string(event)).Inc()
		return fmt.Errorf("%w: %s limit %d reached for tenant %s",
			ErrQuotaExceeded, event, limit, tenantID)
	}
	return nil
}

// UsageSnapshot holds a point-in-time view of all counters.
type UsageSnapshot struct {
	TenantID string
	Period   string
	Event    UsageEvent
	Count    int64
}

// Snapshot returns all non-zero counters.
func (m *UsageMeter) Snapshot() []UsageSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]UsageSnapshot, 0, len(m.counters))
	for k, c := range m.counters {
		if v := c.Load(); v > 0 {
			out = append(out, UsageSnapshot{
				TenantID: k.tenantID,
				Period:   k.period,
				Event:    k.event,
				Count:    v,
			})
		}
	}
	return out
}

// ExportCSV writes raw usage events for the given period (YYYY-MM) to w.
// If period is empty, all events are exported.
// Format: tenant_id,event,tool_name,request_id,ts (CSV with header).
func ExportCSV(w io.Writer, period string, store EventStore) error {
	snaps, err := store.LoadAggregates()
	if err != nil {
		return fmt.Errorf("billing: load aggregates: %w", err)
	}
	fmt.Fprintln(w, "tenant_id,period,event,count")
	for _, s := range snaps {
		if period != "" && s.Period != period {
			continue
		}
		fmt.Fprintf(w, "%s,%s,%s,%d\n", s.TenantID, s.Period, string(s.Event), s.Count)
	}
	return nil
}

// messageTools are the MCP tool names counted as EventMessage (relay traffic).
var messageTools = map[string]bool{
	"send_message":    true,
	"submit_task":     true,
	"send_task_input": true,
}

// MCPToolMeterMiddleware returns a mcp-go ToolHandlerMiddleware that records
// per-tenant billing events. messageTools count as EventMessage, all others
// as EventToolCall. Quota is checked pre-call; exceeded → tool error (no panic).
func MCPToolMeterMiddleware(meter *UsageMeter) server.ToolHandlerMiddleware {
	return func(next server.ToolHandlerFunc) server.ToolHandlerFunc {
		return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			tenant := TenantFromContext(ctx)
			if tenant == nil || meter == nil {
				return next(ctx, req)
			}

			event := EventToolCall
			if messageTools[req.Params.Name] {
				event = EventMessage
			}

			limit := tenant.Quota.MaxToolCallsPerMonth
			if event == EventMessage {
				limit = tenant.Quota.MaxMessagesPerMonth
			}
			if err := meter.TryConsume(tenant.ID, event, limit, 1); err != nil {
				current := meter.Current(tenant.ID, event)
				data := FormatQuotaErrorData(string(tenant.Plan), tenant.ID, current, limit)
				return NewQuotaErrorToolResult(event, data), nil
			}

			result, err := next(ctx, req)
			if err != nil {
				meter.ReleaseQuota(tenant.ID, event, 1)
				return result, err
			}
			meter.queueAudit(tenant.ID, event, req.Params.Name, "")
			return result, err
		}
	}
}
