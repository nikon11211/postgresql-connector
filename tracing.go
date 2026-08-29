package pgconnector

import (
	"context"
	"fmt"
	"strconv"

	"github.com/uptrace/opentelemetry-go-extra/otelgorm"
	"go.opentelemetry.io/otel/attribute"
	ottrace "go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"
)

var registerQueryCallback = func(db *gorm.DB, name string, fn func(*gorm.DB)) error {
	return db.Callback().Query().After("gorm:query").Register(name, fn)
}

func (c *Connector) EnableTracing() error {
	const op = "pgconnector.EnableTracing"

	if c.tracerProvider == nil {
		c.logger.Debug(op + ": no tracer provider configured, skipping")
		return nil
	}
	if c.tracingEnabled {
		return nil
	}

	if err := c.Use(otelgorm.NewPlugin(
		otelgorm.WithTracerProvider(c.tracerProvider),
		otelgorm.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.name", c.cfg.Database),
			attribute.String("db.user", c.cfg.Username),
			attribute.String("db.host", c.cfg.Host),
			attribute.Int("db.cluster_size", len(c.cluster)),
		),
	)); err != nil {
		c.logger.Error(fmt.Sprintf("%s: failed to register tracing plugin: %v", op, err))
		return fmt.Errorf("%s: failed to register tracing plugin: %w", op, err)
	}

	err := registerQueryCallback(c.DB, "otel:trace_query", traceQueryCallback)
	if err != nil {
		c.logger.Error(fmt.Sprintf("%s: failed to register query callback: %v", op, err))
		return fmt.Errorf("%s: failed to register query callback: %w", op, err)
	}

	c.tracingEnabled = true
	c.logger.Info(op + ": OpenTelemetry tracing enabled for GORM")
	return nil
}

var traceQueryCallback = func(db *gorm.DB) {
	if db.Statement.Context != nil {
		span := ottrace.SpanFromContext(db.Statement.Context)
		if span != nil {
			span.SetAttributes(
				attribute.String("sql.rows_affected", strconv.FormatInt(db.RowsAffected, 10)),
				attribute.String("sql.table", db.Statement.Table),
				attribute.Bool("sql.dry_run", db.DryRun),
				attribute.String("X-Request-Id", extractRequestID(db.Statement.Context)),
			)
		}
	}
}

func extractRequestID(ctx context.Context) string {
	if reqID := ctx.Value("X-Request-Id"); reqID != nil {
		if s, ok := reqID.(string); ok {
			return s
		}
	}
	return ""
}
