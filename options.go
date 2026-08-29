package pgconnector

import (
	"go.opentelemetry.io/otel/trace"
)

type ConnectorOption func(*connectorOptions)

type connectorOptions struct {
	tracerProvider trace.TracerProvider
	logger         Logger
}

func WithTracerProvider(tp trace.TracerProvider) ConnectorOption {
	return func(o *connectorOptions) {
		o.tracerProvider = tp
	}
}

func WithLogger(logger Logger) ConnectorOption {
	return func(o *connectorOptions) {
		o.logger = logger
	}
}
