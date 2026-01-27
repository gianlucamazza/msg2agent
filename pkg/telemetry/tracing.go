// Package telemetry provides observability features including tracing and metrics.
package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// TracerConfig holds configuration for the tracer.
type TracerConfig struct {
	// ServiceName is the name of the service for tracing.
	ServiceName string

	// ServiceVersion is the version of the service.
	ServiceVersion string

	// Environment is the deployment environment (e.g., "production", "staging").
	Environment string

	// OTLPEndpoint is the OTLP collector endpoint (e.g., "http://localhost:4318").
	// If empty, tracing is disabled or uses stdout.
	OTLPEndpoint string

	// UseStdout enables stdout exporter for debugging.
	UseStdout bool

	// SampleRate is the sampling rate (0.0 to 1.0). Default is 1.0 (sample all).
	SampleRate float64

	// Logger for tracing setup messages.
	Logger *slog.Logger
}

// TracerProvider wraps the OpenTelemetry tracer provider with cleanup.
type TracerProvider struct {
	provider *sdktrace.TracerProvider
	logger   *slog.Logger
}

// InitTracer initializes OpenTelemetry tracing and returns a TracerProvider.
// Call Shutdown() when done to flush traces.
func InitTracer(ctx context.Context, cfg TracerConfig) (*TracerProvider, error) {
	if cfg.ServiceName == "" {
		cfg.ServiceName = "msg2agent"
	}
	if cfg.ServiceVersion == "" {
		cfg.ServiceVersion = "1.0.0"
	}
	if cfg.Environment == "" {
		cfg.Environment = "development"
	}
	if cfg.SampleRate == 0 {
		cfg.SampleRate = 1.0
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	// Build resource with service information
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(cfg.ServiceVersion),
			attribute.String("environment", cfg.Environment),
		),
		resource.WithHost(),
		resource.WithOS(),
		resource.WithProcess(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Create exporter based on configuration
	var exporter sdktrace.SpanExporter

	switch {
	case cfg.OTLPEndpoint != "":
		// OTLP HTTP exporter
		otlpExporter, err := otlptracehttp.New(ctx,
			otlptracehttp.WithEndpoint(cfg.OTLPEndpoint),
			otlptracehttp.WithInsecure(), // Use WithTLSClientConfig for TLS
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create OTLP exporter: %w", err)
		}
		exporter = otlpExporter
		cfg.Logger.Info("tracing enabled with OTLP exporter", "endpoint", cfg.OTLPEndpoint)
	case cfg.UseStdout:
		// Stdout exporter for debugging
		stdoutExporter, err := stdouttrace.New(
			stdouttrace.WithPrettyPrint(),
			stdouttrace.WithWriter(os.Stdout),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create stdout exporter: %w", err)
		}
		exporter = stdoutExporter
		cfg.Logger.Info("tracing enabled with stdout exporter")
	default:
		// No exporter configured, return no-op provider
		cfg.Logger.Debug("tracing disabled (no exporter configured)")
		return &TracerProvider{logger: cfg.Logger}, nil
	}

	// Create sampler
	var sampler sdktrace.Sampler
	if cfg.SampleRate >= 1.0 {
		sampler = sdktrace.AlwaysSample()
	} else if cfg.SampleRate <= 0 {
		sampler = sdktrace.NeverSample()
	} else {
		sampler = sdktrace.TraceIDRatioBased(cfg.SampleRate)
	}

	// Create tracer provider
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)

	// Set global tracer provider
	otel.SetTracerProvider(provider)

	// Set global propagator for context propagation
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return &TracerProvider{
		provider: provider,
		logger:   cfg.Logger,
	}, nil
}

// Shutdown flushes any remaining traces and shuts down the provider.
func (tp *TracerProvider) Shutdown(ctx context.Context) error {
	if tp.provider == nil {
		return nil
	}
	tp.logger.Debug("shutting down tracer provider")
	return tp.provider.Shutdown(ctx)
}

// Tracer returns a tracer for the given name.
func Tracer(name string) trace.Tracer {
	return otel.Tracer(name)
}

// Common attribute keys for msg2agent tracing.
var (
	AttrAgentDID    = attribute.Key("agent.did")
	AttrAgentName   = attribute.Key("agent.name")
	AttrPeerDID     = attribute.Key("peer.did")
	AttrMethod      = attribute.Key("rpc.method")
	AttrMessageID   = attribute.Key("message.id")
	AttrMessageType = attribute.Key("message.type")
	AttrEncrypted   = attribute.Key("message.encrypted")
	AttrRelayAddr   = attribute.Key("relay.addr")
	AttrTaskID      = attribute.Key("task.id")
	AttrTaskState   = attribute.Key("task.state")
)

// SpanKind constants for convenience.
const (
	SpanKindClient   = trace.SpanKindClient
	SpanKindServer   = trace.SpanKindServer
	SpanKindProducer = trace.SpanKindProducer
	SpanKindConsumer = trace.SpanKindConsumer
	SpanKindInternal = trace.SpanKindInternal
)

// StartSpan starts a new span with the given name and options.
// Returns the context with the span and the span itself.
func StartSpan(ctx context.Context, tracerName, spanName string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return Tracer(tracerName).Start(ctx, spanName, opts...)
}

// AddEvent adds an event to the current span in the context.
func AddEvent(ctx context.Context, name string, attrs ...attribute.KeyValue) {
	span := trace.SpanFromContext(ctx)
	span.AddEvent(name, trace.WithAttributes(attrs...))
}

// SetAttributes sets attributes on the current span in the context.
func SetAttributes(ctx context.Context, attrs ...attribute.KeyValue) {
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attrs...)
}

// RecordError records an error on the current span.
func RecordError(ctx context.Context, err error, opts ...trace.EventOption) {
	span := trace.SpanFromContext(ctx)
	span.RecordError(err, opts...)
}

// SpanFromContext returns the span from the context.
func SpanFromContext(ctx context.Context) trace.Span {
	return trace.SpanFromContext(ctx)
}
