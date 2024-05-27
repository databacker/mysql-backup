package util

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

type contextKey string

const (
	tracerKey contextKey = "mysql-backup-tracer-name"
)

// ContextWithTracer adds a tracer to the context, using a key known only internally to this package.
func ContextWithTracer(ctx context.Context, tracer trace.Tracer) context.Context {
	return context.WithValue(ctx, tracerKey, tracer)
}

// GetTracerFromContext retrieves a tracer from the context, or returns a default tracer if none is found.
func GetTracerFromContext(ctx context.Context) trace.Tracer {
	tracerAny := ctx.Value(tracerKey)
	if tracerAny == nil {
		return otel.Tracer("default")
	}
	tracer, ok := tracerAny.(trace.Tracer)
	if !ok {
		return otel.Tracer("default")
	}

	return tracer
}
