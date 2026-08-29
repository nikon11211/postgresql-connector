package pgconnector

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type fakeDriver struct {
	isMaster bool
	queryErr error
	closeErr error
	rows     []driver.Value
	queries  int
	conn     *fakeConn
}

type fakeConnector struct {
	driver *fakeDriver
}

func (c *fakeConnector) Connect(context.Context) (driver.Conn, error) {
	c.driver.conn = &fakeConn{driver: c.driver}
	return c.driver.conn, nil
}

func (c *fakeConnector) Driver() driver.Driver { return c.driver }

func (d *fakeDriver) Open(string) (driver.Conn, error) {
	d.conn = &fakeConn{driver: d}
	return d.conn, nil
}

type fakeConn struct {
	driver *fakeDriver
}

func (c *fakeConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("not supported") }
func (c *fakeConn) Close() error                        { return c.driver.closeErr }
func (c *fakeConn) Begin() (driver.Tx, error)           { return nil, errors.New("not supported") }

func (c *fakeConn) QueryContext(_ context.Context, _ string, args []driver.NamedValue) (driver.Rows, error) {
	c.driver.queries++
	if c.driver.queryErr != nil {
		return nil, c.driver.queryErr
	}
	return &fakeRows{isMaster: c.driver.isMaster}, nil
}

func (c *fakeConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	return nil, errors.New("not supported")
}

func (c *fakeConn) Ping(context.Context) error { return nil }

func (c *fakeConn) PrepareContext(context.Context, string) (driver.Stmt, error) {
	return &fakeStmt{driver: c.driver}, nil
}

type fakeStmt struct {
	driver *fakeDriver
}

func (s *fakeStmt) Close() error  { return nil }
func (s *fakeStmt) NumInput() int { return -1 }
func (s *fakeStmt) Exec([]driver.Value) (driver.Result, error) {
	return nil, errors.New("not supported")
}
func (s *fakeStmt) Query([]driver.Value) (driver.Rows, error) {
	return nil, errors.New("not supported")
}
func (s *fakeStmt) ExecContext(context.Context, []driver.NamedValue) (driver.Result, error) {
	return nil, errors.New("not supported")
}
func (s *fakeStmt) QueryContext(_ context.Context, _ []driver.NamedValue) (driver.Rows, error) {
	s.driver.queries++
	if s.driver.queryErr != nil {
		return nil, s.driver.queryErr
	}
	return &fakeRows{isMaster: s.driver.isMaster}, nil
}

type fakeRows struct {
	isMaster bool
	pos      int
}

func (r *fakeRows) Columns() []string { return []string{"value"} }
func (r *fakeRows) Close() error      { return nil }
func (r *fakeRows) Next(dest []driver.Value) error {
	if r.pos > 0 {
		return io.EOF
	}
	dest[0] = r.isMaster
	r.pos++
	return nil
}

func newFakeDB(t *testing.T, d *fakeDriver) *sql.DB {
	t.Helper()
	db := sql.OpenDB(&fakeConnector{driver: d})
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func fakeDialector(t *testing.T, d *fakeDriver) gorm.Dialector {
	t.Helper()
	sqlDB := newFakeDB(t, d)
	return postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true})
}

func testConfig() *PgConfig {
	return &PgConfig{
		Host:              "pg-1:5432,pg-2:5432",
		Database:          "app",
		Schema:            "public",
		Username:          "postgres",
		Password:          "secret",
		SSLMode:           "disable",
		HealthCheckPeriod: 10,
		ConnectionPool: ConnectionPool{
			MaxIdleConns:    8,
			MaxOpenConns:    16,
			ConnMaxLifetime: 1800,
			ConnMaxIdleTime: 900,
		},
		Metrics: Metrics{
			Port:            50005,
			RefreshInterval: 5,
			StartServer:     false,
		},
		Retry: Retry{MaxRetries: 0, RetryDelay: 1},
	}
}

func TestNewSuccess(t *testing.T) {
	master := &fakeDriver{isMaster: true}
	replica := &fakeDriver{isMaster: false}

	newDialectorOrig := newDialector
	defer func() { newDialector = newDialectorOrig }()
	newDialector = func(host string, cfg *PgConfig) (gorm.Dialector, error) {
		switch host {
		case "pg-1:5432":
			return fakeDialector(t, replica), nil
		case "pg-2:5432":
			return fakeDialector(t, master), nil
		default:
			return nil, fmt.Errorf("unexpected host %q", host)
		}
	}

	c, err := New(testConfig(), WithLogger(NoopLogger{}))
	require.NoError(t, err)
	require.NotNil(t, c.DB)
	assert.Equal(t, 2, len(c.cluster))

	sqlDB, err := c.DB.DB()
	require.NoError(t, err)
	assert.Equal(t, 16, sqlDB.Stats().MaxOpenConnections)

	require.NoError(t, c.Close())
}

func TestNewNilConfig(t *testing.T) {
	_, err := New(nil)
	require.ErrorIs(t, err, ErrNilConfig)
}

func TestNewNoMasterFound(t *testing.T) {
	replica := &fakeDriver{isMaster: false}

	newDialectorOrig := newDialector
	defer func() { newDialector = newDialectorOrig }()
	newDialector = func(host string, cfg *PgConfig) (gorm.Dialector, error) {
		return fakeDialector(t, replica), nil
	}

	_, err := New(testConfig(), WithLogger(NoopLogger{}))
	require.ErrorIs(t, err, ErrNoMasterFound)
}

func TestNewProbeError(t *testing.T) {
	driver := &fakeDriver{isMaster: true, queryErr: errors.New("boom")}

	newDialectorOrig := newDialector
	defer func() { newDialector = newDialectorOrig }()
	newDialector = func(host string, cfg *PgConfig) (gorm.Dialector, error) {
		return fakeDialector(t, driver), nil
	}

	_, err := New(testConfig(), WithLogger(NoopLogger{}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "master probe failed")
}

func TestNewInvalidHost(t *testing.T) {
	cfg := testConfig()
	cfg.Host = "no-port-here"
	_, err := New(cfg, WithLogger(NoopLogger{}))
	require.ErrorIs(t, err, ErrInvalidConfig)
	assert.Contains(t, err.Error(), "expected host:port")
}

func TestNewEmptyHost(t *testing.T) {
	cfg := testConfig()
	cfg.Host = ""
	_, err := New(cfg, WithLogger(NoopLogger{}))
	require.ErrorIs(t, err, ErrInvalidConfig)
}

func TestNewOpenFailure(t *testing.T) {
	newDialectorOrig := newDialector
	openOrig := openGORM
	defer func() {
		newDialector = newDialectorOrig
		openGORM = openOrig
	}()
	newDialector = func(host string, cfg *PgConfig) (gorm.Dialector, error) {
		return fakeDialector(t, &fakeDriver{isMaster: true}), nil
	}
	openGORM = func(dialector gorm.Dialector, cfg *gorm.Config) (*gorm.DB, error) {
		return nil, errors.New("cannot open")
	}

	_, err := New(testConfig(), WithLogger(NoopLogger{}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to open gorm.DB")
}

func TestNewSQLDBError(t *testing.T) {
	newDialectorOrig := newDialector
	getterOrig := sqlDBGetter
	defer func() {
		newDialector = newDialectorOrig
		sqlDBGetter = getterOrig
	}()
	newDialector = func(host string, cfg *PgConfig) (gorm.Dialector, error) {
		return fakeDialector(t, &fakeDriver{isMaster: true}), nil
	}
	sqlDBGetter = func(db *gorm.DB) (*sql.DB, error) {
		return nil, errors.New("no sql.DB")
	}

	_, err := New(testConfig(), WithLogger(NoopLogger{}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get sql.DB")
}

func TestRetryOnFailure(t *testing.T) {
	master := &fakeDriver{isMaster: true}

	newDialectorOrig := newDialector
	openOrig := openGORM
	sleepOrig := sleepHook
	defer func() {
		newDialector = newDialectorOrig
		openGORM = openOrig
		sleepHook = sleepOrig
	}()
	newDialector = func(host string, cfg *PgConfig) (gorm.Dialector, error) {
		return fakeDialector(t, master), nil
	}

	attempts := 0
	openGORM = func(dialector gorm.Dialector, cfg *gorm.Config) (*gorm.DB, error) {
		attempts++
		if attempts == 1 {
			return nil, errors.New("transient")
		}
		return gorm.Open(dialector, cfg)
	}
	slept := 0
	sleepHook = func(d time.Duration) { slept++ }

	cfg := testConfig()
	cfg.Retry = Retry{MaxRetries: 2, RetryDelay: 1}

	c, err := New(cfg, WithLogger(NoopLogger{}))
	require.NoError(t, err)
	assert.Equal(t, 2, attempts)
	assert.Equal(t, 1, slept)
	require.NoError(t, c.Close())
}

func TestResolveReconnect(t *testing.T) {
	master := &fakeDriver{isMaster: true}

	newDialectorOrig := newDialector
	defer func() { newDialector = newDialectorOrig }()
	newDialector = func(host string, cfg *PgConfig) (gorm.Dialector, error) {
		return fakeDialector(t, master), nil
	}

	c, err := New(testConfig(), WithLogger(NoopLogger{}))
	require.NoError(t, err)
	defer c.Close()

	require.NoError(t, c.Resolve())
	assert.NotNil(t, c.DB)
}

func TestCloseNotConnected(t *testing.T) {
	c := &Connector{logger: NoopLogger{}, cfg: testConfig()}
	err := c.Close()
	require.ErrorIs(t, err, ErrNotConnected)
}

func TestCloseError(t *testing.T) {
	master := &fakeDriver{isMaster: true, closeErr: errors.New("close failed")}

	newDialectorOrig := newDialector
	defer func() { newDialector = newDialectorOrig }()
	newDialector = func(host string, cfg *PgConfig) (gorm.Dialector, error) {
		return fakeDialector(t, master), nil
	}

	c, err := New(testConfig(), WithLogger(NoopLogger{}))
	require.NoError(t, err)
	err = c.Close()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "close failed")
}

func TestNewEmptyHostsAfterTrim(t *testing.T) {
	cfg := testConfig()
	cfg.Host = " , , "
	_, err := New(cfg, WithLogger(NoopLogger{}))
	require.ErrorIs(t, err, ErrInvalidConfig)
	assert.Contains(t, err.Error(), "no usable hosts")
}

func TestSetupReplicasError(t *testing.T) {
	newDialectorOrig := newDialector
	replicaOrig := useReplicaResolver
	defer func() {
		newDialector = newDialectorOrig
		useReplicaResolver = replicaOrig
	}()
	newDialector = func(host string, cfg *PgConfig) (gorm.Dialector, error) {
		return fakeDialector(t, &fakeDriver{isMaster: true}), nil
	}
	useReplicaResolver = func(db *gorm.DB, cluster []gorm.Dialector, pool ConnectionPool) error {
		return errors.New("replica resolver failed")
	}

	_, err := New(testConfig(), WithLogger(NoopLogger{}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "replica resolver failed")
}

func TestSetupMetricsError(t *testing.T) {
	newDialectorOrig := newDialector
	metricsOrig := useMetricsPlugin
	defer func() {
		newDialector = newDialectorOrig
		useMetricsPlugin = metricsOrig
	}()
	newDialector = func(host string, cfg *PgConfig) (gorm.Dialector, error) {
		return fakeDialector(t, &fakeDriver{isMaster: true}), nil
	}
	useMetricsPlugin = func(db *gorm.DB, cfg *PgConfig) error {
		return errors.New("metrics plugin failed")
	}

	_, err := New(testConfig(), WithLogger(NoopLogger{}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "metrics plugin failed")
}

func TestNegativeMaxRetries(t *testing.T) {
	newDialectorOrig := newDialector
	defer func() { newDialector = newDialectorOrig }()
	newDialector = func(host string, cfg *PgConfig) (gorm.Dialector, error) {
		return fakeDialector(t, &fakeDriver{isMaster: true}), nil
	}

	cfg := testConfig()
	cfg.Retry = Retry{MaxRetries: -3, RetryDelay: 1}
	c, err := New(cfg, WithLogger(NoopLogger{}))
	require.NoError(t, err)
	require.NoError(t, c.Close())
}

func TestCloseSQLDBGetterError(t *testing.T) {
	c := testConnector(t)
	defer c.Close()

	getterOrig := sqlDBGetter
	defer func() { sqlDBGetter = getterOrig }()
	sqlDBGetter = func(db *gorm.DB) (*sql.DB, error) {
		return nil, errors.New("no sql.DB")
	}

	err := c.Close()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no sql.DB")
}

func TestResolveAllRetriesFailed(t *testing.T) {
	openOrig := openGORM
	defer func() { openGORM = openOrig }()
	openGORM = func(dialector gorm.Dialector, cfg *gorm.Config) (*gorm.DB, error) {
		return nil, errors.New("always fails")
	}

	cfg := testConfig()
	cfg.Retry = Retry{MaxRetries: 1, RetryDelay: 1}
	_, err := New(cfg, WithLogger(NoopLogger{}))
	require.Error(t, err)
}

func TestGORMQueryRoundTrip(t *testing.T) {
	master := &fakeDriver{isMaster: true, rows: []driver.Value{true}}

	newDialectorOrig := newDialector
	defer func() { newDialector = newDialectorOrig }()
	newDialector = func(host string, cfg *PgConfig) (gorm.Dialector, error) {
		return fakeDialector(t, master), nil
	}

	c, err := New(testConfig(), WithLogger(NoopLogger{}))
	require.NoError(t, err)
	defer c.Close()

	var isMaster bool
	err = c.DB.Raw("SELECT 1").Scan(&isMaster).Error
	require.NoError(t, err)
	assert.True(t, isMaster)
}
