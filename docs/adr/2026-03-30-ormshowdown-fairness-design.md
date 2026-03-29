# ORM Showdown Fairness Design

## Decision

Benchmark Bun and GORM using their idiomatic high-level APIs for every workload, even when that changes the exact SQL shape or requires multiple queries.

## Rationale

The benchmark is intended to compare practical ORM overhead fairly. Using raw SQL for Bun and GORM on complex workloads hides builder, mapping, relation-loading, and query-construction costs that Rain pays in normal usage.

## Workload Mapping

- `insert_single`: builder/model insert APIs
- `lookup_by_pk`: builder/model lookup APIs
- `filtered_slice_scan`: builder/model filtered select APIs
- `join_scan_posts_users`: select builders with join clauses
- `grouped_aggregate`: select builders with grouping
- `subquery_join_report`: select builders with ORM-built subqueries
- `eager_load_nearest_equivalent`: relation loading APIs (`Relation` / `Preload`)
- `prepared_point_lookup`: prepare once during adapter setup, execute in loop

## Tradeoff

Generated SQL may differ across ORMs, and eager loading may use multiple queries instead of a single join. This is acceptable because the goal is to measure idiomatic ORM usage rather than identical handwritten SQL.
