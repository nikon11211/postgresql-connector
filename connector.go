package pgconnector

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel/trace"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
	"gorm.io/plugin/dbresolver"
	"gorm.io/plugin/prometheus"
)

const (
	defaultLocation = "Europe/Moscow"
	masterQuery     = "SELECT NOT pg_is_in_recovery()"
)

var (
	newDialector = func(host string, cfg *PgConfig) (gorm.Dialector, error) {
		parts := strings.Split(host, ":")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return nil, fmt.Errorf("invalid host %q, expected host:port", host)
		}
		dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=%s TimeZone=%s",
			parts[0], cfg.Username, cfg.Password, cfg.Database, parts[1], cfg.SSLMode, defaultLocation)
		return postgres.New(postgres.Config{
			DSN:                  dsn,
			PreferSimpleProtocol: true,
		}), nil
	}
	openGORM    = func(dialector gorm.Dialector, cfg *gorm.Config) (*gorm.DB, error) { return gorm.Open(dialector, cfg) }
	sqlDBGetter = func(db *gorm.DB) (*sql.DB, error) { return db.DB() }
	sleepHook   = time.Sleep
)

type Connector struct {
	cfg    *PgConfig
	logger Logger
	*gorm.DB
	cluster        []gorm.Dialector
	tracerProvider trace.TracerProvider
	tracingEnabled bool
}

func New(config *PgConfig, opts ...ConnectorOption) (*Connector, error) {
	const op = "pgconnector.New"

	if config == nil {
		return nil, fmt.Errorf("%s: %w", op, ErrNilConfig)
	}

	o := &connectorOptions{logger: newSlogLogger()}
	for _, opt := range opts {
		opt(o)
	}
	if o.logger == nil {
		o.logger = NoopLogger{}
	}

	c := &Connector{
		cfg:            config,
		logger:         o.logger,
		tracerProvider: o.tracerProvider,
	}

	c.logger.Info(fmt.Sprintf("%s: connecting to cluster %s database=%s", op, config.Host, config.Database))
	if err := c.Resolve(); err != nil {
		c.logger.Error(fmt.Sprintf("%s: %v", op, err))
		return nil, err
	}

	c.logger.Info(fmt.Sprintf("%s: connected to cluster, nodes=%d", op, len(c.cluster)))
	return c, nil
}

func (c *Connector) Resolve() error {
	const op = "pgconnector.Resolve"

	if err := c.validate(); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	attempts := max(c.cfg.Retry.MaxRetries+1, 1)

	var lastErr error
	for attempt := range attempts {
		if attempt > 0 {
			c.logger.Warn(fmt.Sprintf("%s: retry %d/%d after failure: %v", op, attempt, c.cfg.Retry.MaxRetries, lastErr))
			sleepHook(time.Duration(c.cfg.Retry.RetryDelay) * time.Second)
		}
		lastErr = c.connect()
		if lastErr == nil {
			return nil
		}
	}
	return fmt.Errorf("%s: %w", op, lastErr)
}

func (c *Connector) connect() error {
	const op = "pgconnector.connect"

	config := &gorm.Config{
		PrepareStmt:    true,
		Logger:         logger.Discard,
		NamingStrategy: schema.NamingStrategy{TablePrefix: c.cfg.Schema + "."},
	}

	var masterDB *gorm.DB
	for _, machine := range c.cluster {
		gormDB, err := openGORM(machine, config)
		if err != nil {
			c.logger.Error(fmt.Sprintf("%s: failed to open gorm.DB on %s: %v", op, machine.Name(), err))
			return fmt.Errorf("%s: failed to open gorm.DB: %w", op, err)
		}

		sqlDB, err := sqlDBGetter(gormDB)
		if err != nil {
			return fmt.Errorf("%s: failed to get sql.DB: %w", op, err)
		}
		applyPoolSettings(sqlDB, c.cfg.ConnectionPool)

		isMaster, err := c.isMaster(gormDB)
		if err != nil {
			c.logger.Error(fmt.Sprintf("%s: master probe failed on %s: %v", op, machine.Name(), err))
			return fmt.Errorf("%s: master probe failed: %w", op, err)
		}

		if isMaster {
			masterDB = gormDB
			c.DB = gormDB
			c.logger.Info(fmt.Sprintf("%s: master node found: %s", op, machine.Name()))
			break
		}
	}

	if masterDB == nil {
		return fmt.Errorf("%w", ErrNoMasterFound)
	}

	if err := useReplicaResolver(masterDB, c.cluster, c.cfg.ConnectionPool); err != nil {
		return err
	}
	if err := useMetricsPlugin(masterDB, c.cfg); err != nil {
		return err
	}

	c.logger.Info(fmt.Sprintf("%s: cluster configuration complete, nodes=%d", op, len(c.cluster)))
	return nil
}

var (
	useReplicaResolver = func(db *gorm.DB, cluster []gorm.Dialector, pool ConnectionPool) error {
		return db.Use(dbresolver.Register(dbresolver.Config{
			Replicas: cluster,
			Policy:   dbresolver.RoundRobinPolicy(),
		}).
			SetConnMaxIdleTime(time.Duration(pool.ConnMaxIdleTime) * time.Second).
			SetConnMaxLifetime(time.Duration(pool.ConnMaxLifetime) * time.Second).
			SetMaxIdleConns(pool.MaxIdleConns).
			SetMaxOpenConns(pool.MaxOpenConns))
	}
	useMetricsPlugin = func(db *gorm.DB, cfg *PgConfig) error {
		return db.Use(prometheus.New(prometheus.Config{
			DBName:          cfg.Database,
			RefreshInterval: cfg.Metrics.RefreshInterval,
			StartServer:     cfg.Metrics.StartServer,
			HTTPServerPort:  cfg.Metrics.Port,
		}))
	}
)

func (c *Connector) isMaster(db *gorm.DB) (bool, error) {
	var isMaster bool
	err := db.WithContext(context.Background()).Raw(masterQuery).Scan(&isMaster).Error
	return isMaster, err
}

func applyPoolSettings(db *sql.DB, pool ConnectionPool) {
	db.SetConnMaxIdleTime(time.Duration(pool.ConnMaxIdleTime) * time.Second)
	db.SetConnMaxLifetime(time.Duration(pool.ConnMaxLifetime) * time.Second)
	db.SetMaxIdleConns(pool.MaxIdleConns)
	db.SetMaxOpenConns(pool.MaxOpenConns)
}

func (c *Connector) validate() error {
	hosts := strings.Split(c.cfg.Host, ",")

	c.cluster = nil
	for _, host := range hosts {
		host = strings.TrimSpace(host)
		if host == "" {
			continue
		}
		dialector, err := newDialector(host, c.cfg)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidConfig, err)
		}
		c.cluster = append(c.cluster, dialector)
	}

	if len(c.cluster) == 0 {
		return fmt.Errorf("%w: no usable hosts", ErrInvalidConfig)
	}
	return nil
}

func (c *Connector) Close() error {
	const op = "pgconnector.Close"

	if c.DB == nil {
		return fmt.Errorf("%s: %w", op, ErrNotConnected)
	}
	sqlDB, err := sqlDBGetter(c.DB)
	if err != nil {
		c.logger.Error(fmt.Sprintf("%s: failed to get sql.DB: %v", op, err))
		return fmt.Errorf("%s: %w", op, err)
	}
	if err := sqlDB.Close(); err != nil {
		c.logger.Error(fmt.Sprintf("%s: failed to close connection: %v", op, err))
		return fmt.Errorf("%s: %w", op, err)
	}
	c.logger.Info(op + ": connection closed")
	return nil
}
