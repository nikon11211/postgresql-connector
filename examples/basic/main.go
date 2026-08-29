package main

import (
	"context"
	"fmt"

	pgconnector "github.com/nikon11211/postgresql-connector"
	"gorm.io/gorm/logger"
)

type User struct {
	ID   int64
	Name string
}

func main() {
	cfg := &pgconnector.PgConfig{
		Host:              "localhost:6432,localhost:6433",
		Database:          "app",
		Schema:            "public",
		Username:          "postgres",
		Password:          "secret",
		SSLMode:           "require",
		HealthCheckPeriod: 10,
		ConnectionPool: pgconnector.ConnectionPool{
			MaxIdleConns:    8,
			MaxOpenConns:    16,
			ConnMaxLifetime: 1800,
			ConnMaxIdleTime: 900,
		},
		Metrics: pgconnector.Metrics{
			Port:            50005,
			RefreshInterval: 5,
			StartServer:     true,
		},
		Retry: pgconnector.Retry{
			MaxRetries: 3,
			RetryDelay: 1,
		},
	}

	conn, err := pgconnector.New(cfg, pgconnector.WithLogger(gormLogger{}))
	if err != nil {
		panic(err)
	}
	defer func() { _ = conn.Close() }()

	var users []User
	if err := conn.DB.WithContext(context.Background()).Find(&users).Error; err != nil {
		panic(err)
	}
	fmt.Printf("loaded %d users\n", len(users))
}

type gormLogger struct{}

func (gormLogger) Debug(msg string) { logger.Default.Info(context.Background(), msg) }
func (gormLogger) Info(msg string)  { logger.Default.Info(context.Background(), msg) }
func (gormLogger) Warn(msg string)  { logger.Default.Warn(context.Background(), msg) }
func (gormLogger) Error(msg string) { logger.Default.Error(context.Background(), msg) }
