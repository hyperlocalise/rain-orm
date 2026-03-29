# ADR: Relation loading and scan performance follow-up

## Status
Accepted on 2026-03-29.

## Context
Rain's first relation-loading implementation favored simplicity. It loaded one level of relations with a single follow-up `IN (...)` query per relation and rebuilt scan setup on every row. That kept the code small, but it left four gaps:

- schema indexes existed in metadata but had no DDL helper
- slice scans did not support `[]*T`
- relation loading did not support nested relation paths
- bulk scans paid avoidable reflection and allocation costs per row

These gaps showed up in a parity review against Drizzle ORM and Bun. Drizzle emphasizes nested relational queries. Bun's Go-specific implementation avoids repeated row-scan setup and supports pointer-heavy model shapes.

## Decision
Make focused, additive changes instead of rewriting the query engine around join-based eager loading.

### Scan path

- Add a reusable row-scan plan that caches column-to-field mapping per result set.
- Reuse the destination and finalizer slices across rows.
- Support slice destinations whose element type is either `T` or `*T`.

### Relation loading

- Accept nested relation paths such as `"posts.author"`.
- Build a relation tree once, validate it before scanning, and load children recursively.
- Batch relation fetches with fixed-size `IN (...)` chunks to avoid unbounded parameter growth.
- Allow `has_many` relation fields to use either `[]T` or `[]*T`.

### DDL

- Keep `CreateTableSQL` single-purpose and single-statement.
- Add `CreateIndexesSQL` as the explicit companion for schema index metadata.

## Consequences
### Positive

- Bulk scans do less work per row.
- Rain supports more Go-native model shapes.
- Nested relations and large parent result sets behave predictably.
- Index metadata now has a concrete DDL path.

### Negative

- Relation loading still uses follow-up queries rather than Bun-style join hydration.
- Chunked relation loading may execute multiple child queries for large result sets.
- The relation loader is more complex than the original v1 implementation.

## Deferred work

- Join-based eager loading for selected relation shapes
- Relation-level filters, ordering, and limits
- Configurable relation batch sizes
- Benchmarks that compare Rain's scan path against raw `database/sql` and Bun
