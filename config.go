package pgconnector

type PgConfig struct {
	Host              string         `mapstructure:"host" validate:"required"`
	Database          string         `mapstructure:"database" validate:"required"`
	Schema            string         `mapstructure:"schema" validate:"required"`
	Username          string         `mapstructure:"username" validate:"required"`
	Password          string         `mapstructure:"password" validate:"required"`
	SSLMode           string         `mapstructure:"sslmode" validate:"required"`
	HealthCheckPeriod int            `mapstructure:"health_check_period" validate:"required"`
	ConnectionPool    ConnectionPool `mapstructure:"connection_pool" validate:"required"`
	Metrics           Metrics        `mapstructure:"metrics" validate:"required"`
	Retry             Retry          `mapstructure:"retry"`
}

type ConnectionPool struct {
	MaxIdleConns    int `mapstructure:"max_idle_conns" validate:"required"`
	MaxOpenConns    int `mapstructure:"max_open_conns" validate:"required"`
	ConnMaxLifetime int `mapstructure:"max_life_time" validate:"required"`
	ConnMaxIdleTime int `mapstructure:"max_idle_time" validate:"required"`
}

type Metrics struct {
	Port            uint32 `mapstructure:"port" validate:"required"`
	RefreshInterval uint32 `mapstructure:"refresh_interval" validate:"required"`
	StartServer     bool   `mapstructure:"start_server" validate:"required"`
}

type Retry struct {
	MaxRetries int `mapstructure:"max_retries"`
	RetryDelay int `mapstructure:"retry_delay"`
}
