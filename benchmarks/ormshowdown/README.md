# ORM Showdown Benchmarks

This harness compares six implementations against one canonical SQLite schema and deterministic seeded data:

- Rain ORM
- Bun
- GORM
- SQLC-style typed query layer
- Ent-style query layer
- raw `database/sql`

## Workloads

Each implementation runs the same workload set:

- `insert_single`
- `lookup_by_pk`
- `filtered_slice_scan`
- `join_scan_posts_users`
- `grouped_aggregate`
- `subquery_join_report`
- `eager_load_nearest_equivalent`
- `prepared_point_lookup`

## Run everything

```bash
go test -run '^$' -bench . -benchmem ./benchmarks/ormshowdown/... > full-run.txt
```

## Run one library

```bash
go test -run '^$' -bench '^BenchmarkORMShowdown/rain/' -benchmem ./benchmarks/ormshowdown/... > rain.txt
go test -run '^$' -bench '^BenchmarkORMShowdown/bun/' -benchmem ./benchmarks/ormshowdown/... > bun.txt
go test -run '^$' -bench '^BenchmarkORMShowdown/gorm/' -benchmem ./benchmarks/ormshowdown/... > gorm.txt
go test -run '^$' -bench '^BenchmarkORMShowdown/sqlc/' -benchmem ./benchmarks/ormshowdown/... > sqlc.txt
go test -run '^$' -bench '^BenchmarkORMShowdown/ent/' -benchmem ./benchmarks/ormshowdown/... > ent.txt
go test -run '^$' -bench '^BenchmarkORMShowdown/raw/' -benchmem ./benchmarks/ormshowdown/... > raw.txt
```

## Benchstat examples

```bash
go test -run '^$' -bench . -benchmem ./benchmarks/ormshowdown/... > before.txt
go test -run '^$' -bench . -benchmem ./benchmarks/ormshowdown/... > after.txt
benchstat before.txt after.txt
```

## Fairness notes

- Schema, indexes, and seed data are created before timers start.
- Dataset setup is deterministic.
- Query shape parity is maintained via shared SQL where ORM APIs differ.
- SQLC/Ent are represented by nearest-equivalent typed layers in this first harness version.
