package cmd

import (
	"fmt"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

const (
	appName = "mysql-backup"
)

// getTracer get a global tracer for the application, which incorporates both the name of the application
// and the command that is being run.
func getTracer(cmd string) trace.Tracer {
	return getTracerProvider().Tracer(fmt.Sprintf("%s/%s", appName, cmd))
}

func getTracerProvider() *sdktrace.TracerProvider {
	tp, ok := otel.GetTracerProvider().(*sdktrace.TracerProvider)
	if !ok {
		return nil
	}
	return tp
}
