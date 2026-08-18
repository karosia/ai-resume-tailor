package cli

import (
	"context"
	"log/slog"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// otelEnabled reports whether tracing to stdout is turned on. It's off by
// default so normal runs aren't cluttered with span output; set OTEL_ENABLED=1
// to see the OpenTelemetry spans that wrap tailoring.
func otelEnabled() bool {
	return os.Getenv("OTEL_ENABLED") == "1"
}

// setupOTel configures a global OpenTelemetry TracerProvider that writes spans
// to stdout, and returns a shutdown func to flush them. When tracing is
// disabled it returns a no-op shutdown, so callers can always `defer` it.
//
// This owns the OTel SDK wiring; ai-trace-cause only *reads* the active span
// (via its otel hook) and never creates or configures providers itself.
func setupOTel(log *slog.Logger) func(context.Context) {
	if !otelEnabled() {
		return func(context.Context) {} // no-op
	}

	exporter, err := stdouttrace.New(
		stdouttrace.WithPrettyPrint(),
	)
	if err != nil {
		log.Warn("could not start otel stdout exporter; tracing disabled", "error", err)
		return func(context.Context) {}
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
	)
	otel.SetTracerProvider(tp)

	// Return a shutdown that flushes any buffered spans.
	return func(ctx context.Context) {
		if err := tp.Shutdown(ctx); err != nil {
			log.Warn("otel shutdown", "error", err)
		}
	}
}
