<p align="center">
  <h1 align="center">🚀 PostgreSQL-Connector — Cluster-Aware GORM Connector for Go</h1>
</p>

<p align="center">
  <a href="https://pkg.go.dev/github.com/nikon11211/postgresql-connector">
    <img src="https://pkg.go.dev/badge/github.com/nikon11211/postgresql-connector.svg" alt="Go Reference"/>
  </a>
  <a href="https://goreportcard.com/report/github.com/nikon11211/postgresql-connector">
    <img src="https://goreportcard.com/badge/github.com/nikon11211/postgresql-connector" alt="Go Report Card"/>
  </a>
  <a href="https://github.com/nikon11211/postgresql-connector/actions/workflows/test.yaml">
    <img src="https://github.com/nikon11211/postgresql-connector/actions/workflows/test.yaml/badge.svg" alt="Tests"/>
  </a>
  <a href="https://codecov.io/gh/nikon11211/postgresql-connector">
    <img src="https://codecov.io/gh/nikon11211/postgresql-connector/branch/main/graph/badge.svg" alt="Coverage"/>
  </a>
  <a href="https://sonarcloud.io/summary/overall?id=nikon11211_postgresql-connector">
    <img src="https://sonarcloud.io/api/project_badges/measure?project=nikon11211_postgresql-connector&metric=coverage" alt="SonarCloud Coverage"/>
  </a>
  <a href="https://opensource.org/licenses/MIT">
    <img src="https://img.shields.io/badge/License-MIT-yellow.svg" alt="License: MIT"/>
  </a>
  <a href="https://golang.org/">
    <img src="https://img.shields.io/badge/Go-%3E%3D%201.26-blue" alt="Go Version"/>
  </a>
</p>

<p align="center">
  <b>A production-ready PostgreSQL cluster connector for Go</b><br/>
  <i>Master discovery • Read replicas • Connection pooling • Prometheus metrics • OpenTelemetry tracing • Retries</i>
</p>

---

## ✨ Why PostgreSQL-Connector?

`postgresql-connector` connects a Go application to a PostgreSQL cluster via
[GORM](https://gorm.io). Given a list of `host:port` nodes it:

- **discovers the master** node (`SELECT NOT pg_is_in_recovery()`) and routes
  all writes to it;
- **registers every other node as a read replica** with
  [dbresolver](https://gorm.io/plugin/dbresolver/) round-robin load balancing;
- **configures the connection pool** (max idle/open connections, connection
  lifetime and idle time) on master and replicas alike;
- **exposes Prometheus metrics** through the GORM metrics plugin;
- **optionally enables OpenTelemetry tracing** for every GORM query, including
  request-ID propagation;
- **retries** transient connection failures during startup.

The `Connector` embeds `*gorm.DB`, so all standard GORM APIs are available
directly on the connector.

## 📦 Installation

```bash
go get github.com/nikon11211/postgresql-connector
```

## 🚀 Quick Start

```go
package main

import (
	"context"
	"fmt"

	"github.com/nikon11211/postgresql-connector"
)

func main() {
	cfg := &pgconnector.PgConfig{
		Host:              "pg-1:5432,pg-2:5432",
		Database:          "app",
		Schema:            "public",
		Username:          "postgres",
		Password:          "secret",
		SSLMode:           "disable",
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
			StartServer:     false,
		},
	}

	conn, err := pgconnector.New(cfg)
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	var users []User
	if err := conn.WithContext(context.Background()).Find(&users).Error; err != nil {
		panic(err)
	}
	fmt.Printf("loaded %d users\n", len(users))
}
```

A complete runnable example lives in [`examples/basic`](examples/basic/main.go).

### Example YAML config

```yaml
postgres:
  host: "pg-1:5432,pg-2:5432"
  database: "app"
  schema: "public"
  username: "postgres"
  password: "secret"
  sslmode: "disable"
  health_check_period: 10
  connection_pool:
    max_idle_conns: 8
    max_open_conns: 16
    max_life_time: 1800
    max_idle_time: 900
  metrics:
    port: 50005
    refresh_interval: 5
    start_server: false
  retry:
    max_retries: 3
    retry_delay: 1
```

All fields carry `mapstructure` tags, so `PgConfig` can be populated directly
from YAML/JSON with your favourite configuration library.

## 🏗️ Architecture

```
┌───────────────────────────────────────────────────────────────┐
│                        Your Application                       │
├───────────────────────────────────────────────────────────────┤
│                        pgconnector.Connector                  │
│                         (embeds *gorm.DB)                     │
│                                                               │
│  ┌─────────────┐   ┌─────────────┐   ┌──────────────────────┐ │
│  │  Master DB  │   │  Replicas   │   │   Pool + Metrics     │ │
│  │  (writes)   │   │ (reads, RR) │   │  + Tracing (OTEL)    │ │
│  └─────────────┘   └─────────────┘   └──────────────────────┘ │
│        │                 │                    │               │
│        └─────────────────┴────────────────────┘               │
│                         GORM / pgx                            │
└───────────────────────────────────────────────────────────────┘
```

## ⚙️ Configuration

| Field               | Description                                              |
|---------------------|----------------------------------------------------------|
| `Host`              | Comma-separated cluster nodes, `host:port,host:port`     |
| `Database`          | Database name                                            |
| `Schema`            | Schema used as GORM table prefix (`schema.table`)        |
| `Username`/`Password`| Database credentials                                     |
| `SSLMode`           | TLS mode: `disable`, `require`, `verify-full`, ...       |
| `HealthCheckPeriod` | Ping timeout during validation, seconds                  |
| `ConnectionPool`    | Pool limits: `MaxIdleConns`, `MaxOpenConns`, `ConnMaxLifetime`, `ConnMaxIdleTime` |
| `Metrics`           | Prometheus: `Port`, `RefreshInterval`, `StartServer`     |
| `Retry`             | Startup retries: `MaxRetries`, `RetryDelay` (seconds)    |

## 🧩 Options

| Option                 | Description                                          |
|------------------------|------------------------------------------------------|
| `WithTracerProvider(tp)` | Configure an OpenTelemetry tracer provider         |
| `WithLogger(l)`          | Replace the default slog `Logger` with your own    |

## ❌ Errors

All failures wrap sentinel errors so `errors.Is` works:

| Sentinel error      | Raised when                                        |
|---------------------|----------------------------------------------------|
| `ErrNilConfig`      | `New(nil, ...)` is called                          |
| `ErrInvalidConfig`  | Config validation fails                            |
| `ErrNoMasterFound`  | No master node found in the cluster                |
| `ErrNotConnected`   | `Close()` without an established connection        |

## 🚀 Features

### Master / replica routing

The connector probes each node in order with `SELECT NOT pg_is_in_recovery()`.
The first node that answers as master becomes the write target; all nodes are
registered as replicas behind the round-robin read balancer. Writes go to the
master, reads are distributed across the cluster.

### Reconnection on failover

Call `conn.Resolve()` again to re-discover the master after a failover; the
whole cluster configuration (resolver, pool, metrics) is rebuilt on a fresh
set of connections.

### OpenTelemetry tracing

```go
conn, _ := pgconnector.New(cfg, pgconnector.WithTracerProvider(provider))
if err := conn.EnableTracing(); err != nil {
	// handle
}
```

Every GORM query is traced; spans are decorated with `db.system`, `db.name`,
`db.user`, `db.host`, `db.cluster_size`, `sql.rows_affected`, `sql.table`,
`sql.dry_run` and the `X-Request-Id` taken from the request context.

### Logging

Implement the 4-method `Logger` interface (`Debug`/`Info`/`Warn`/`Error`) and
pass it with `WithLogger`. A structured JSON `slog` logger is used by default;
`NoopLogger` is provided for quiet setups.

## 🧪 Testing & Benchmarks

The library ships with **100.0% statement coverage** (excluding `examples/`).
All tests run against a fake `database/sql` driver — no PostgreSQL instance is
required:

```bash
go test -race -coverprofile=coverage.txt -covermode=atomic $(go list ./... | grep -v /examples)
```

Run benchmarks:

```bash
go test -bench=. -benchmem -run '^$' .
```

| Benchmark                          | What it measures                        |
|------------------------------------|-----------------------------------------|
| `BenchmarkNewDialector`            | GORM dialector + DSN construction       |
| `BenchmarkNewDialectorInvalidHost` | Dialector rejection path                |
| `BenchmarkConnectorValidate`       | Cluster host parsing & validation       |
| `BenchmarkExtractRequestID*`       | Request-ID extraction from context      |

CI enforces the 100% gate, runs `go vet`, benchmarks and
[golangci-lint](https://golangci-lint.run) with a strict configuration, and
publishes coverage to Codecov and SonarCloud.

## 🤝 Contributing

We welcome contributions! Here's how you can help:

1. **Fork** the repository
2. **Create** a feature branch (`git checkout -b feature/amazing-feature`)
3. **Commit** your changes (`git commit -m 'Add amazing feature'`)
4. **Push** to the branch (`git push origin feature/amazing-feature`)
5. **Open** a Pull Request

## 📄 License

MIT License — see [LICENSE](LICENSE) for details.

## 🙏 Acknowledgments

- [GORM](https://gorm.io) — The fantastic ORM for Go
- [dbresolver](https://gorm.io/plugin/dbresolver/) — Read/write clustering plugin
- [uptrace/opentelemetry-go-extra](https://github.com/uptrace/opentelemetry-go-extra) — GORM tracing plugin
- [Prometheus](https://prometheus.io/) — Metrics and monitoring

---

<p align="center">
  <b>Made with ❤️ for the Go community</b><br/>
  <sub>Built for production, designed for microservices</sub>
</p>