# ORM Showdown Benchmarks

This harness compares four implementations against one canonical SQLite schema and deterministic seeded data:

- Rain ORM
- Bun
- GORM
- raw `database/sql`

## Workloads

Each implementation runs the same workload set:

- `insert_single`
- `lookup_by_pk`
- `filtered_slice_scan`
- `join_scan_posts_users`
- `grouped_aggregate`
- `subquery_join_report`
- `join_user_posts_flat_rows`
- `preload_posts`
- `prepared_point_lookup`

## Run everything

```bash
go test -run '^$' -bench . -benchmem ./benchmarks/ormshowdown/... > full-run.txt
```

Or use the repo target to run each library separately and print a `benchstat` comparison with `raw` as the baseline:

```bash
make benchstats
```

## Run one library

```bash
go test -run '^$' -bench '^BenchmarkORMShowdown/rain/' -benchmem ./benchmarks/ormshowdown/... > rain.txt
go test -run '^$' -bench '^BenchmarkORMShowdown/bun/' -benchmem ./benchmarks/ormshowdown/... > bun.txt
go test -run '^$' -bench '^BenchmarkORMShowdown/gorm/' -benchmem ./benchmarks/ormshowdown/... > gorm.txt
go test -run '^$' -bench '^BenchmarkORMShowdown/raw/' -benchmem ./benchmarks/ormshowdown/... > raw.txt
```

## Benchstat examples

```bash
go test -run '^$' -bench . -benchmem ./benchmarks/ormshowdown/... > before.txt
go test -run '^$' -bench . -benchmem ./benchmarks/ormshowdown/... > after.txt
benchstat before.txt after.txt
```

The Makefile target writes one output file per library under `artifacts/bench/ormshowdown/<timestamp>/` and then runs `benchstat` across `raw`, `rain`, `bun`, and `gorm`.

## Fairness notes

- Schema, indexes, and seed data are created before timers start.
- Dataset setup is deterministic.
- Query shape parity is maintained for flat-row workloads, including the dedicated `join_user_posts_flat_rows` benchmark.
- `preload_posts` intentionally measures relation loading and graph assembly instead of a flat SQL join.
- SQLC/Ent are intentionally excluded until this repo has real independent adapters for them.
