// Package tracing traces bot-lambda with OpenTelemetry. It uses the global tracer provider, so spans are only recorded
// if the application configures one (otel.SetTracerProvider); otherwise tracing is a no-op.
package tracing

import (
	"context"
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const instrumentationName = "github.com/elliotwms/bot-lambda"

// Start starts a span. End it with End.
func Start(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return otel.Tracer(instrumentationName).Start(ctx, name, opts...)
}

// End ends the span, recording err if it isn't nil
func End(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
}

// Client returns a copy of c which traces its requests. c isn't modified, as it may be shared, e.g. http.DefaultClient.
func Client(c *http.Client) *http.Client {
	if c == nil {
		c = http.DefaultClient
	}

	traced := *c
	traced.Transport = otelhttp.NewTransport(c.Transport)

	return &traced
}
