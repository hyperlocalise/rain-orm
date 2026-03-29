# ADR: SQLite-first ORM performance benchmark suite

## Status
Accepted on 2026-03-29.

## Context
Rain had integration coverage for SQLite-backed ORM flows, but no benchmark suite for measuring end-to-end latency and allocation behavior. That left the project without a repeatable way to inspect how query construction, SQL compilation, execution, and scan paths behave under realistic ORM workloads.

The first benchmark suite needs to be practical for engineers running locally, deterministic enough to compare runs over time, and narrow enough to ship without introducing multi-dialect infrastructure or profiling workflows that are not yet required.

## Decision
Add a SQLite-first benchmark suite in `pkg/rain` using Go's native benchmark runner.

### Scope

- Measure end-to-end ORM execution rather than builder-only compilation.
- Focus on developer diagnostics instead of pass/fail CI regression thresholds.
- Use Go benchmark metrics as the memory signal: `ns/op`, `B/op`, and `allocs/op`.

### Workloads

- Single-row insert via `.Model(...)`
- Single-row insert via `.Set(...)`
- Point lookup select and struct scan
- Filtered select into a slice
- Bulk scan into a slice
- Join scan across aliased `users` and `posts` tables

### Dataset defaults

- `small`: 100 users / 1,000 posts
- `medium`: 1,000 users / 10,000 posts
- `large`: 10,000 users / 100,000 posts

### Harness rules

- Each benchmark dataset runs against an isolated SQLite database.
- Schema creation and deterministic data seeding happen before `b.ResetTimer()`.
- Seeded row counts are validated before measurements begin.
- Benchmarks stay on the public ORM API and reuse the same table definitions as integration tests.

## Deferred work

- Raw `database/sql` baseline comparisons
- `pprof` heap and CPU profile capture
- Postgres and MySQL benchmark backends
- CI thresholds for time or allocation regressions

## Consequences
### Positive

- Gives Rain a stable local benchmark entrypoint with realistic ORM workloads.
- Makes allocation behavior visible without adding extra tooling.
- Keeps extension paths open for future dialect backends and baseline comparisons.

### Negative

- Benchmark coverage is limited to SQLite in v1.
- Insert benchmarks still include normal table growth during the measured run.
- Results are intended for trend analysis, not absolute cross-machine comparisons.
