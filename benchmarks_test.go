package pgconnector

import (
	"context"
	"testing"
)

var (
	pgBenchErr  error
	pgBenchDial gormDialectorIface
	pgBenchStr  string
)

type gormDialectorIface interface{ Name() string }

func benchmarkPgConfig() *PgConfig {
	return &PgConfig{
		Host:     "10.0.0.1:5432",
		Database: "orders",
		Schema:   "public",
		Username: "app",
		Password: "secret",
		SSLMode:  "require",
	}
}

func BenchmarkNewDialector(b *testing.B) {
	cfg := benchmarkPgConfig()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		pgBenchDial, pgBenchErr = newDialector("10.0.0.1:5432", cfg)
	}
}

func BenchmarkNewDialectorInvalidHost(b *testing.B) {
	cfg := benchmarkPgConfig()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		pgBenchDial, pgBenchErr = newDialector("no-port", cfg)
	}
}

func BenchmarkConnectorValidate(b *testing.B) {
	cfg := benchmarkPgConfig()
	cfg.Host = "10.0.0.1:5432,10.0.0.2:5432,10.0.0.3:5432"
	c := &Connector{cfg: cfg}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		pgBenchErr = c.validate()
	}
}

func BenchmarkExtractRequestIDHit(b *testing.B) {
	ctx := context.WithValue(context.Background(), "X-Request-Id", "a1b2c3d4")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		pgBenchStr = extractRequestID(ctx)
	}
}

func BenchmarkExtractRequestIDMiss(b *testing.B) {
	ctx := context.Background()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		pgBenchStr = extractRequestID(ctx)
	}
}
