package pgconnector

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"gorm.io/gorm"
)

func testConnector(t *testing.T) *Connector {
	t.Helper()
	master := &fakeDriver{isMaster: true}

	newDialectorOrig := newDialector
	t.Cleanup(func() { newDialector = newDialectorOrig })
	newDialector = func(host string, cfg *PgConfig) (gorm.Dialector, error) {
		return fakeDialector(t, master), nil
	}

	c, err := New(testConfig(), WithLogger(NoopLogger{}))
	require.NoError(t, err)
	return c
}

func TestEnableTracing(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tp := trace.NewTracerProvider(trace.WithSpanProcessor(recorder))

	c := testConnector(t)
	defer c.Close()
	c.tracerProvider = tp

	require.NoError(t, c.EnableTracing())
	assert.True(t, c.tracingEnabled)

	require.NoError(t, c.EnableTracing())

	ctx := context.WithValue(context.Background(), "X-Request-Id", "req-42")
	var isMaster bool
	require.NoError(t, c.DB.WithContext(ctx).Raw("SELECT 1").Scan(&isMaster).Error)
}

func TestEnableTracingNoProvider(t *testing.T) {
	c := testConnector(t)
	defer c.Close()
	c.tracerProvider = nil
	require.NoError(t, c.EnableTracing())
	assert.False(t, c.tracingEnabled)
}

func TestEnableTracingPluginError(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tp := trace.NewTracerProvider(trace.WithSpanProcessor(recorder))

	c := testConnector(t)
	defer c.Close()
	c.tracerProvider = tp

	c.DB.Plugins["otelgorm"] = nil
	require.Error(t, c.EnableTracing())
	assert.False(t, c.tracingEnabled)
}

func TestEnableTracingCallbackError(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tp := trace.NewTracerProvider(trace.WithSpanProcessor(recorder))

	c := testConnector(t)
	defer c.Close()
	c.tracerProvider = tp

	registerOrig := registerQueryCallback
	defer func() { registerQueryCallback = registerOrig }()
	registerQueryCallback = func(db *gorm.DB, name string, fn func(*gorm.DB)) error {
		return errors.New("callback registration failed")
	}

	err := c.EnableTracing()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to register query callback")
}

func TestDefaultLogger(t *testing.T) {
	master := &fakeDriver{isMaster: true}

	newDialectorOrig := newDialector
	defer func() { newDialector = newDialectorOrig }()
	newDialector = func(host string, cfg *PgConfig) (gorm.Dialector, error) {
		return fakeDialector(t, master), nil
	}

	c, err := New(testConfig())
	require.NoError(t, err)
	defer c.Close()
	require.NoError(t, c.EnableTracing())

	bad := testConfig()
	bad.Host = "nope"
	newDialector = func(host string, cfg *PgConfig) (gorm.Dialector, error) {
		if host == "nope" {
			return nil, errors.New("invalid host")
		}
		return fakeDialector(t, master), nil
	}
	_, err = New(bad)
	require.Error(t, err)

	retryCfg := testConfig()
	retryCfg.Retry = Retry{MaxRetries: 1, RetryDelay: 1}
	failOpen := &fakeDriver{isMaster: true, queryErr: errors.New("probe fail")}
	newDialectorOrig2 := newDialector
	defer func() { newDialector = newDialectorOrig2 }()
	newDialector = func(host string, cfg *PgConfig) (gorm.Dialector, error) {
		return fakeDialector(t, failOpen), nil
	}
	_, err = New(retryCfg)
	require.Error(t, err)

	_, err = New(testConfig(), WithLogger(nil))
	require.Error(t, err)
}

func TestExtractRequestID(t *testing.T) {
	ctx := context.WithValue(context.Background(), "X-Request-Id", "abc")
	assert.Equal(t, "abc", extractRequestID(ctx))

	ctxBad := context.WithValue(context.Background(), "X-Request-Id", 42)
	assert.Equal(t, "", extractRequestID(ctxBad))

	assert.Equal(t, "", extractRequestID(context.Background()))
}

func TestTraceQueryCallback(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tp := trace.NewTracerProvider(trace.WithSpanProcessor(recorder))

	ctx, span := tp.Tracer("test").Start(context.Background(), "query")
	ctx = context.WithValue(ctx, "X-Request-Id", "req-42")

	db, err := gorm.Open(testDialector(t), &gorm.Config{})
	require.NoError(t, err)
	db = db.WithContext(ctx).Table("users")
	db.RowsAffected = 7

	traceQueryCallback(db)
	span.End()

	spans := recorder.Ended()
	require.Len(t, spans, 1)
	attrs := spans[0].Attributes()
	values := make(map[string]string)
	for _, kv := range attrs {
		values[string(kv.Key)] = kv.Value.AsString()
	}
	assert.Equal(t, "7", values["sql.rows_affected"])
	assert.Equal(t, "users", values["sql.table"])
	assert.Equal(t, "req-42", values["X-Request-Id"])
}

func TestTraceQueryCallbackNilContext(t *testing.T) {
	db, err := gorm.Open(testDialector(t), &gorm.Config{})
	require.NoError(t, err)
	db.Statement.Context = nil
	traceQueryCallback(db)
}

func TestTraceQueryCallbackNilSpan(t *testing.T) {
	db, err := gorm.Open(testDialector(t), &gorm.Config{})
	require.NoError(t, err)
	db = db.WithContext(context.Background())
	traceQueryCallback(db)
}

func testDialector(t *testing.T) gorm.Dialector {
	t.Helper()
	return fakeDialector(t, &fakeDriver{isMaster: true})
}

func TestOptions(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tp := trace.NewTracerProvider(trace.WithSpanProcessor(recorder))

	o := &connectorOptions{}
	WithTracerProvider(tp)(o)
	assert.NotNil(t, o.tracerProvider)

	WithLogger(NoopLogger{})(o)
	assert.NotNil(t, o.logger)
}

func TestPoolSettingsApplied(t *testing.T) {
	c := testConnector(t)
	defer c.Close()

	sqlDB, err := c.DB.DB()
	require.NoError(t, err)
	stats := sqlDB.Stats()
	assert.Equal(t, 16, stats.MaxOpenConnections)
}
