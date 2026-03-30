# rain-orm

A type-safe, SQL-like ORM for Go inspired by DrizzleORM — lightweight, fast, and idiomatic.

[![Go Report Card](https://goreportcard.com/badge/github.com/quiet-circles/rain-orm)](https://goreportcard.com/report/github.com/quiet-circles/rain-orm)
[![GoDoc](https://pkg.go.dev/badge/github.com/quiet-circles/rain-orm.svg)](https://pkg.go.dev/github.com/quiet-circles/rain-orm)

# Table of Contents
   * [rain-orm](#rain-orm)
   * [Features](#features)
   * [Project Layout](#project-layout)
   * [Quick Start](#quick-start)
   * [Examples](#examples)
   * [Performance Benchmarks](#performance-benchmarks)
   * [Makefile Targets](#makefile-targets)
   * [Contribute](#contribute)

# Features

- **Type-safe query builder** — Chain methods with compile-time safety
- **Schema-first design** — Define tables, constraints, and indexes with typed Go schema handles
- **Multiple dialect support** — PostgreSQL, MySQL, SQLite (extensible)
- **Fluent API** — DrizzleORM-inspired SQL-like syntax
- **Transaction support** — First-class transaction handling
- **Lightweight** — Minimal dependencies, maximum performance
- [golangci-lint](https://golangci-lint.run/) for linting and formatting
- [Makefile](Makefile) - with various useful targets and documentation (see Makefile Targets)
- [`go.mod`](go.mod) `tool` directives — tracks CLI tooling versions in a Go 1.24+ compatible way

# Project Layout

This is a **library** scaffold. The structure follows Go best practices for reusable packages:

```
rain-orm/
├── README.md              # Updated with rain-orm content
├── go.mod                 # Module: github.com/quiet-circles/rain-orm
├── Makefile               # Library-focused targets
├── .gitignore
├── examples/
│   ├── basic/main.go      # CRUD, queries, transactions example
│   ├── schema/main.go     # Schema definition example
│   └── schema/registry    # Importable schema registry for CLI usage
│   └── dialect/main.go    # Database dialect example
├── pkg/
│   ├── rain/
│   │   ├── rain.go        # DB connection, Tx, entry point
│   │   └── query.go       # Query builder (SELECT, INSERT, UPDATE, DELETE)
│   ├── schema/
│   │   └── schema.go      # Table/column definitions, types
│   ├── migrator/
│   │   └── ...            # Schema snapshots, diffs, and SQL migration helpers
│   └── dialect/
│       └── dialect.go     # PostgreSQL, MySQL, SQLite support
├── cmd/
│   └── rain/main.go       # CLI entry point
└── internal/  
```

# Quick Start

```go
package main

import (
    "time"

    "github.com/hyperlocalise/rain-orm/pkg/rain"
    "github.com/hyperlocalise/rain-orm/pkg/schema"
)

type UsersTable struct {
    schema.TableModel
    ID        *schema.Column[int64]
    Email     *schema.Column[string]
    Name      *schema.Column[string]
    Active    *schema.Column[bool]
    CreatedAt *schema.Column[time.Time]
}

type User struct {
    ID     int64
    Email  string
    Name   string
    Active bool
}

var Users = schema.Define("users", func(t *UsersTable) {
    t.ID = t.BigSerial("id").PrimaryKey()
    t.Email = t.VarChar("email", 255).NotNull().Unique()
    t.Name = t.Text("name").NotNull()
    t.Active = t.Boolean("active").NotNull().Default(true)
    t.CreatedAt = t.TimestampTZ("created_at").NotNull().DefaultNow()
})

func main() {
    db, err := rain.Open("postgres", "postgres://user:pass@localhost/mydb")
    if err != nil {
        panic(err)
    }
    defer db.Close()

    insertSQL, _, _ := db.Insert().
        Table(Users).
        Model(&User{Email: "alice@example.com", Name: "Alice", Active: true}).
        Returning(Users.ID).
        ToSQL()

    selectSQL, _, _ := db.Select().
        Table(Users).
        Column(Users.ID, Users.Email, Users.Name).
        Where(Users.Active.Eq(true)).
        OrderBy(Users.CreatedAt.Desc()).
        Limit(10).
        ToSQL()

    _, _, _ = insertSQL, selectSQL, db.Update().
        Table(Users).
        Set(Users.Name, "Alice Smith").
        Where(Users.ID.Eq(int64(1))).
        ToSQL()
}
```

# Examples

## Basic CRUD

```go
u := schema.Alias(Users, "u")
p := schema.Alias(Posts, "p")

db.Select().
    Table(p).
    Column(p.ID, p.Title, u.Email).
    Join(u, p.UserID.EqCol(u.ID)).
    Where(u.Active.Eq(true)).
    OrderBy(p.ID.Desc()).
    Limit(10)

db.Insert().
    Table(Users).
    Model(&user).
    Returning(Users.ID)

db.Update().
    Table(Users).
    Set(Users.Name, "Alice Smith").
    Where(Users.ID.Eq(int64(1)))
```

## Transactions

```go
err := db.RunInTx(ctx, func(tx *rain.Tx) error {
    _, err := tx.Insert().
        Table(Posts).
        Set(Posts.UserID, user.ID).
        Set(Posts.Title, "Hello").
        Exec(ctx)
    if err != nil {
        return err
    }

    return tx.RunInTx(ctx, func(nested *rain.Tx) error {
        _, nestedErr := nested.Update().
            Table(Users).
            Set(Users.Active, true).
            Where(Users.ID.Eq(user.ID)).
            Exec(ctx)
        return nestedErr
    })
})
if err != nil {
    return err
}
```

`RunInTx` commits when the callback returns `nil` and rolls back when it returns an error. Nested `RunInTx` calls use savepoints on dialects that support them.
Inside a nested callback, call patterns should return errors instead of calling `Commit`/`Rollback` directly.

## Read Replicas

Rain can route builder-based reads to replicas while keeping writes, raw SQL, and transactions on primary:

```go
primaryDB, err := rain.Open("postgres", "postgres://user:pass@localhost/primary")
if err != nil {
    panic(err)
}

read1, err := rain.Open("postgres", "postgres://user:pass@localhost/read1")
if err != nil {
    panic(err)
}

read2, err := rain.Open("postgres", "postgres://user:pass@localhost/read2")
if err != nil {
    panic(err)
}

db, err := rain.WithReplicas(primaryDB, []*rain.DB{read1, read2}, nil)
if err != nil {
    panic(err)
}
defer func() { _ = db.Close() }()

var replicaRows []User
if err := db.Select().
    Table(Users).
    Where(Users.Active.Eq(true)).
    Scan(context.Background(), &replicaRows); err != nil {
    panic(err)
}

var primaryRows []User
if err := db.Primary().Select().
    Table(Users).
    Where(Users.Active.Eq(true)).
    Scan(context.Background(), &primaryRows); err != nil {
    panic(err)
}

// Writes stay on primary.
if _, err := db.Insert().
    Table(Users).
    Model(&User{Email: "replica-aware@example.com", Name: "Replica Aware"}).
    Exec(context.Background()); err != nil {
    panic(err)
}
```

Notes:
- `Select()` uses a replica by default.
- `Primary().Select()` forces reads to the primary database.
- `Insert`, `Update`, `Delete`, `Exec`, `Query`, `QueryRow`, `Begin`, and `RunInTx` always use primary.
- v1 does not hide replica lag automatically; use `Primary()` when you need read-after-write consistency.

## Opt-in Query Cache (v1)

Rain supports opt-in caching for `SELECT` helpers (`Scan`, `Count`, and `Exists`). Caching is disabled unless you set a cache backend on `DB`.

```go
cache := rain.NewMemoryQueryCache()
db.WithQueryCache(cache)

var users []User
err := db.Select().
    Table(Users).
    Where(Users.Active.Eq(true)).
    Cache(rain.QueryCacheOptions{
        TTL:  2 * time.Minute,
        Tags: []string{"users", "lookup"},
    }).
    Scan(ctx, &users)
```

### Invalidation (manual in v1)

Use cache tags and invalidate them explicitly:

```go
if err := db.InvalidateQueryCache(ctx, "users"); err != nil {
    return err
}
```

Notes:
- Cache is opt-in per query.
- TTL is required (zero or negative TTL disables caching for that query).
- Tags are optional, but recommended for manual invalidation.
- Cache is a convenience for read-mostly workloads, not a substitute for fixing inefficient query shapes.

## Relation Loading

`WithRelations` supports top-level and nested relation paths:

```go
var rows []UserWithPosts
err := db.Select().
    Table(Users).
    WithRelations("posts", "posts.author").
    Scan(ctx, &rows)
```

Relation loading batches follow-up `IN (...)` queries automatically for larger parent result sets.

## Schema Definition

```go
import "github.com/hyperlocalise/rain-orm/pkg/schema"

type UsersTable struct {
    schema.TableModel
    ID        *schema.Column[int64]
    Email     *schema.Column[string]
    OrgID     *schema.Column[int64]
    Active    *schema.Column[bool]
    CreatedAt *schema.Column[time.Time]
}

var Users = schema.Define("users", func(t *UsersTable) {
    t.ID = t.BigSerial("id").PrimaryKey()
    t.Email = t.VarChar("email", 255).NotNull().Unique()
    t.OrgID = t.BigInt("org_id").NotNull()
    t.Active = t.Boolean("active").NotNull().Default(true)
    t.CreatedAt = t.TimestampTZ("created_at").NotNull().DefaultNow()

    t.Unique("users_org_email_key").On(t.OrgID, t.Email)
    t.Check("users_active_email_check", schema.Or(t.Active.Eq(true), t.Email.IsNotNull()))
    t.UniqueIndex("users_email_key").On(t.Email)
    t.Index("users_active_created_idx").On(t.Active, t.CreatedAt.Desc())
})
```

## Create Table SQL

```go
ddl, err := rain.OpenDialect("sqlite")
if err != nil {
    panic(err)
}

sql, err := ddl.CreateTableSQL(Users)
if err != nil {
    panic(err)
}
```

Rain can compile `CREATE TABLE` SQL directly from schema metadata, including dialect-specific type rendering, defaults, primary and unique constraints, foreign keys, and enum or custom `CHECK` constraints.

Standalone indexes compile separately from table constraints:

```go
indexSQL, err := ddl.CreateIndexesSQL(Users)
if err != nil {
    panic(err)
}
```

Rain also includes a first-party CLI for code-first SQL migration generation and application. The v1 CLI supports additive snapshot diffs with `generate`, migration application with `migrate`, and project validation with `check`. It intentionally does not include Studio, direct schema push, or database introspection commands.

## CLI Migrations

See [cmd/rain/README.md](cmd/rain/README.md) for the CLI design, migration folder format, config examples, and command workflow.

## Migrations (forward-only v1)

Rain includes a small `pkg/migrate` runner for applying ordered migrations in apps and tests.

```go
import (
    "context"
    "database/sql"

    "github.com/hyperlocalise/rain-orm/pkg/migrate"
)

func applyMigrations(ctx context.Context, db *sql.DB) error {
    migrations := []migrate.Migration{
        {
            ID: "202603010900_create_users",
            Up: func(ctx context.Context, exec migrate.Executor) error {
                _, err := exec.ExecContext(ctx, `CREATE TABLE users (id INTEGER PRIMARY KEY AUTOINCREMENT, email TEXT NOT NULL)`)
                return err
            },
        },
    }

    _, err := migrate.ApplyPending(ctx, db, migrations)
    return err
}
```

`ApplyPending` creates a migration tracking table (`rain_schema_migrations`) if needed, applies pending migrations in deterministic ID order, records applied IDs and timing metadata, and skips already-applied migrations. In v1, migration execution is forward-only: `Down` callbacks are accepted for future compatibility but are not run by the runner.

## Database Dialects

```go
import "github.com/hyperlocalise/rain-orm/pkg/dialect"
import "github.com/hyperlocalise/rain-orm/pkg/schema"

// Get dialect by name
d, err := dialect.GetDialect("postgres")
if err != nil {
    panic(err)
}

// Use dialect-specific features
quoted := d.QuoteIdentifier("users")  // "users"
placeholder := d.Placeholder(1)       // $1
sqlType := d.DataType(schema.ColumnType{DataType: "string", Size: 255})  // VARCHAR
```

See the [examples/](examples/) directory for complete, runnable examples.

# Integration Tests

SQLite integration coverage runs in CI and can be executed locally with:

```sh
go test -race ./pkg/rain -run "^TestSQLiteIntegration"
```

Postgres integration coverage uses environment-driven connection settings and skips automatically when configuration is missing.

```sh
RAIN_POSTGRES_DSN="postgres://user:pass@localhost:5432/rain_test?sslmode=disable" \
  go test -race ./pkg/rain -run "^TestPostgresIntegration"
```

You can also set `RAIN_POSTGRES_HOST`, `RAIN_POSTGRES_PORT` (default `5432`), `RAIN_POSTGRES_USER`, `RAIN_POSTGRES_PASSWORD` (optional), `RAIN_POSTGRES_DB`, and `RAIN_POSTGRES_SSLMODE` (default `disable`) instead of a DSN.

MySQL integration coverage also uses environment-driven connection settings and skips automatically when configuration is missing.

```sh
RAIN_MYSQL_DSN="user:pass@tcp(localhost:3306)/rain_test" \
  go test -race ./pkg/rain -run "^TestMySQLIntegration"
```

You can also set `RAIN_MYSQL_HOST`, `RAIN_MYSQL_PORT` (default `3306`), `RAIN_MYSQL_USER`, `RAIN_MYSQL_PASSWORD` (optional), and `RAIN_MYSQL_DB` instead of a DSN.

For a repo-managed local workflow that starts Postgres and MySQL, runs both integration suites, and tears the containers down afterward:

```sh
make test-integration-db
```

# Performance Benchmarks

Rain includes a SQLite-first benchmark suite for measuring end-to-end ORM performance and memory usage across representative CRUD and join workloads.

Run the full suite:

```sh
make bench
```

Save an annotated report with environment details and a metric legend:

```sh
make bench-report
```

Run the ORM showdown suite per library and print a `benchstat` comparison against the `raw` baseline:

```sh
make benchstats
```

Run a single workload:

```sh
go test -run '^$' -bench 'BenchmarkSQLiteSelectJoinScan' -benchmem ./pkg/rain
```

Run one workload for one dataset size:

```sh
go test -run '^$' -bench 'BenchmarkSQLiteSelectJoinScan/medium$' -benchmem ./pkg/rain
```

Save a filtered annotated report:

```sh
BENCH_FILTER='BenchmarkSQLiteRichSelectWithNestedRelations/medium$' make bench-report
```

Filter the ORM showdown report to one dataset or workload suffix:

```sh
BENCH_FILTER='small/prepared_point_lookup$$' make benchstats
```

Compare two runs over time by saving the benchmark output and diffing the benchmark lines from the same machine and environment. Use the built-in Go metrics as the primary signals:

- `ns/op` shows the average execution time per benchmark iteration.
- `B/op` shows the average bytes allocated per iteration.
- `allocs/op` shows the average number of heap allocations per iteration.

`make bench-report` writes each run under `artifacts/bench/sqlite/<timestamp>/`, with benchmark output in a plain `.txt` file and environment details in `manifest.txt`.

The suite uses two SQLite fixtures:

- the baseline `users`/`posts` schema for fast, broad CRUD and relation benchmarks
- a richer `rich_users`/`rich_categories`/`rich_posts` schema for grouped-reporting, subquery, nested-relation, upsert, and wider-row benchmarks

The suite seeds deterministic `small`, `medium`, and `large` SQLite datasets before measurements start so setup cost does not pollute the reported ORM metrics. Local same-machine comparisons remain the source of truth for performance changes; CI benchmark artifacts are best treated as informational snapshots rather than precise regression gates.

# Makefile Targets

```sh
$> make
bench                          run sqlite benchmark suite with allocation metrics
bench-ormshowdown              run ORM showdown benchmark suite with allocation metrics
benchstats                     run ORM showdown per-library benchmarks and compare them with benchstat
bootstrap                      download tool and module dependencies
build                          build the library (verifies compilation)
clean                          clean up test artifacts
example-basic                  run basic usage example (placeholder)
example-dialect                run dialect example (placeholder)
example-schema                 run schema definition example (placeholder)
fmt                            format go files
help                           list makefile targets
lint                           lint go files
precommit                      run local CI validation flow
staticcheck                    run staticcheck directly
test                           run tests with coverage
test-json                      run tests with JSON output (for CI)
```

# Contribute

If you find issues in that setup or have some nice features / improvements, I would welcome an issue or a PR :)

## Adding New Features

1. **New Query Methods**: Add to `pkg/rain/query.go`
2. **New Column Types**: Add to `pkg/schema/schema.go`
3. **New Dialect Support**: Implement the `Dialect` interface in `pkg/dialect/dialect.go`
4. **Examples**: Add new examples to `examples/<name>/main.go`

## Design Principles

- **Type safety first** — Leverage Go's type system
- **SQL-like API** — Familiar syntax for SQL developers
- **Zero magic** — Explicit is better than implicit
- **Composable** — Build complex queries from simple parts

## Contributing Guide

### Getting Started
1. Fork the repository
2. Clone your fork: `git clone https://github.com/YOUR_USERNAME/rain-orm.git`
3. Install dependencies: `make bootstrap`
4. Run tests: `make test`

### Development Workflow
1. Create a feature branch: `git checkout -b feature/my-feature`
2. Make your changes
3. Run precommit checks: `make precommit`
4. Commit with clear messages
5. Push and open a Pull Request

### Pull Request Requirements
- All tests pass (`make test`)
- Code is formatted (`make fmt`)
- No linting errors (`make lint`)
- Changes are documented in README if public API changes

### Code Style
- Follow standard Go conventions
- Use `gofumpt` for formatting (stricter than gofmt)
- Import grouping via `gci`
- All public functions must have documentation comments
- Keep functions focused and small

### Testing
- Write table-driven tests
- Aim for >70% coverage on new code
- Include example tests for public APIs
- Run `make test` before committing

### Reporting Issues
When reporting bugs, include:
- Go version (`go version`)
- Steps to reproduce
- Expected vs actual behavior
- Minimal code example if applicable
