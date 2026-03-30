# ORM Showdown Fairness Design

## Decision

Benchmark only independently implemented adapters, and use Bun and GORM idiomatically for every workload even when that changes the exact SQL shape or requires multiple queries.

## Rationale

The benchmark is intended to compare practical ORM overhead fairly. Using raw SQL for Bun and GORM on complex workloads hides builder, mapping, relation-loading, and query-construction costs that Rain pays in normal usage. Placeholder wrappers that only mirror `raw` should not appear as independent contenders because they create misleading near-baseline results.

## Workload Mapping

- `insert_single`: builder/model insert APIs
- `lookup_by_pk`: builder/model lookup APIs
- `filtered_slice_scan`: builder/model filtered select APIs
- `join_scan_posts_users`: select builders with join clauses
- `grouped_aggregate`: select builders with grouping
- `subquery_join_report`: select builders with ORM-built subqueries
- `join_user_posts_flat_rows`: flat joined rows over users and posts with the same logical row shape across adapters
- `preload_posts`: relation loading APIs (`WithRelations` / `Relation` / `Preload`) or equivalent manual graph assembly
- `prepared_point_lookup`: prepare once during adapter setup, execute in loop

## Tradeoff

Generated SQL may differ across ORMs, and relation loading may use multiple queries instead of a single join. The harness now isolates that cost in `preload_posts`, while `join_user_posts_flat_rows` keeps a flatter apples-to-apples comparison for joined row scans.

## Scope

The published showdown currently includes only `raw`, `rain`, `bun`, and `gorm`. `sqlc` and `ent` are excluded until the repo has real independent adapters for them.
