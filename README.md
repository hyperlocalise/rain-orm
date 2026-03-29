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
- **Schema-first design** — Define tables and indexes with typed Go schema handles
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
│   └── dialect/main.go    # Database dialect example
├── pkg/
│   ├── rain/
│   │   ├── rain.go        # DB connection, Tx, entry point
│   │   └── query.go       # Query builder (SELECT, INSERT, UPDATE, DELETE)
│   ├── schema/
│   │   └── schema.go      # Table/column definitions, types
│   └── dialect/
│       └── dialect.go     # PostgreSQL, MySQL, SQLite support
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
    ID     int64  `db:"id"`
    Email  string `db:"email"`
    Name   string `db:"name"`
    Active bool   `db:"active"`
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

## Schema Definition

```go
import "github.com/hyperlocalise/rain-orm/pkg/schema"

type UsersTable struct {
    schema.TableModel
    ID        *schema.Column[int64]
    Email     *schema.Column[string]
    Active    *schema.Column[bool]
    CreatedAt *schema.Column[time.Time]
}

var Users = schema.Define("users", func(t *UsersTable) {
    t.ID = t.BigSerial("id").PrimaryKey()
    t.Email = t.VarChar("email", 255).NotNull().Unique()
    t.Active = t.Boolean("active").NotNull().Default(true)
    t.CreatedAt = t.TimestampTZ("created_at").NotNull().DefaultNow()

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

Rain can compile `CREATE TABLE` SQL directly from schema metadata, including dialect-specific type rendering, defaults, foreign keys, and enum `CHECK` constraints.

This is intentionally limited to schema-to-DDL compilation. Rain does not generate migration diffs or manage migration history.

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

# Performance Benchmarks

Rain includes a SQLite-first benchmark suite for measuring end-to-end ORM performance and memory usage across representative CRUD and join workloads.

Run the full suite:

```sh
make bench
```

Run a single workload:

```sh
go test -run '^$' -bench 'BenchmarkSQLiteSelectJoinScan' -benchmem ./pkg/rain
```

Run one workload for one dataset size:

```sh
go test -run '^$' -bench 'BenchmarkSQLiteSelectJoinScan/medium$' -benchmem ./pkg/rain
```

Compare two runs over time by saving the benchmark output and diffing the benchmark lines from the same machine and environment. Use the built-in Go metrics as the primary signals:

- `ns/op` shows the average execution time per benchmark iteration.
- `B/op` shows the average bytes allocated per iteration.
- `allocs/op` shows the average number of heap allocations per iteration.

The suite seeds deterministic `small`, `medium`, and `large` SQLite datasets before measurements start so setup cost does not pollute the reported ORM metrics.

# Makefile Targets

```sh
$> make
bench                          run sqlite benchmark suite with allocation metrics
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
