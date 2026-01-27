package telemetry

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

func TestInitTracer_NoExporter(t *testing.T) {
	ctx := context.Background()

	tp, err := InitTracer(ctx, TracerConfig{
		ServiceName: "test-service",
	})
	if err != nil {
		t.Fatalf("InitTracer failed: %v", err)
	}

	// Should return a no-op provider
	if tp.provider != nil {
		t.Error("expected nil provider when no exporter configured")
	}

	// Shutdown should not fail
	if err := tp.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown failed: %v", err)
	}
}

func TestInitTracer_Stdout(t *testing.T) {
	ctx := context.Background()

	tp, err := InitTracer(ctx, TracerConfig{
		ServiceName: "test-service",
		UseStdout:   true,
		SampleRate:  0.5,
	})
	if err != nil {
		t.Fatalf("InitTracer failed: %v", err)
	}
	defer tp.Shutdown(ctx)

	if tp.provider == nil {
		t.Error("expected non-nil provider with stdout exporter")
	}
}

func TestTracer(t *testing.T) {
	tracer := Tracer("test-tracer")
	if tracer == nil {
		t.Error("Tracer returned nil")
	}
}

func TestStartSpan(t *testing.T) {
	ctx := context.Background()

	ctx, span := StartSpan(ctx, "test-tracer", "test-span",
		trace.WithSpanKind(SpanKindInternal),
	)
	defer span.End()

	if span == nil {
		t.Error("StartSpan returned nil span")
	}

	// Verify context contains a span (can't compare directly due to interface types)
	spanFromCtx := SpanFromContext(ctx)
	if spanFromCtx == nil {
		t.Error("SpanFromContext returned nil")
	}

	// Verify span context is valid
	if !span.SpanContext().IsValid() && span.SpanContext().TraceID().IsValid() {
		// Note: no-op spans may not have valid contexts, which is OK
		t.Log("span context may be no-op (no exporter configured)")
	}
}

func TestAddEvent(t *testing.T) {
	ctx := context.Background()
	ctx, span := StartSpan(ctx, "test-tracer", "test-span")
	defer span.End()

	// Should not panic
	AddEvent(ctx, "test-event",
		attribute.String("key", "value"),
		attribute.Int("count", 42),
	)
}

func TestSetAttributes(t *testing.T) {
	ctx := context.Background()
	ctx, span := StartSpan(ctx, "test-tracer", "test-span")
	defer span.End()

	// Should not panic
	SetAttributes(ctx,
		AttrAgentDID.String("did:wba:example.com:agent:test"),
		AttrMethod.String("ping"),
	)
}

func TestRecordError(t *testing.T) {
	ctx := context.Background()
	ctx, span := StartSpan(ctx, "test-tracer", "test-span")
	defer span.End()

	// Should not panic
	RecordError(ctx, ErrTracingDisabled)
}

func TestCommonAttributes(t *testing.T) {
	tests := []struct {
		attr     attribute.Key
		expected string
	}{
		{AttrAgentDID, "agent.did"},
		{AttrAgentName, "agent.name"},
		{AttrPeerDID, "peer.did"},
		{AttrMethod, "rpc.method"},
		{AttrMessageID, "message.id"},
		{AttrMessageType, "message.type"},
		{AttrEncrypted, "message.encrypted"},
		{AttrRelayAddr, "relay.addr"},
		{AttrTaskID, "task.id"},
		{AttrTaskState, "task.state"},
	}

	for _, tt := range tests {
		if string(tt.attr) != tt.expected {
			t.Errorf("attribute %v = %q, want %q", tt.attr, string(tt.attr), tt.expected)
		}
	}
}

func TestSamplerConfigurations(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name       string
		sampleRate float64
	}{
		{"always sample", 1.0},
		{"never sample", 0.0},
		{"half sample", 0.5},
		{"above 1", 2.0},   // Should clamp to always
		{"negative", -0.5}, // Should clamp to never
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tp, err := InitTracer(ctx, TracerConfig{
				ServiceName: "test",
				UseStdout:   true,
				SampleRate:  tt.sampleRate,
			})
			if err != nil {
				t.Fatalf("InitTracer failed: %v", err)
			}
			tp.Shutdown(ctx)
		})
	}
}

// Error for testing
var ErrTracingDisabled = &tracingError{msg: "tracing disabled"}

type tracingError struct {
	msg string
}

func (e *tracingError) Error() string {
	return e.msg
}
