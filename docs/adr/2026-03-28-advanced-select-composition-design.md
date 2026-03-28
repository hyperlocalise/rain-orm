# ADR: Advanced SELECT composition in `SelectQuery`

## Status
Accepted on 2026-03-28.

## Context
`SelectQuery` currently supports table selection, column selection, joins, filtering, ordering, limits, and offsets. It does not support common read-query patterns such as CTEs, grouped queries, HAVING filters, or subqueries as table sources.

This gap prevents expressive reporting and aggregation queries.

## Decision
Extend `SelectQuery` with explicit builder state and methods for:

- `Distinct()`
- `GroupBy(...schema.Expression)`
- `Having(schema.Predicate)`
- `With(name string, query *SelectQuery)`
- `TableSubquery(query *SelectQuery, alias string)`
- `JoinSubquery(query *SelectQuery, alias string, on schema.Predicate)`
- `LeftJoinSubquery(query *SelectQuery, alias string, on schema.Predicate)`

Add internal query-source abstractions so `FROM` and `JOIN` can render either a normal table or a subquery.

Render SQL in canonical clause order:

1. `WITH`
2. `SELECT [DISTINCT]`
3. `FROM`
4. `JOIN`
5. `WHERE`
6. `GROUP BY`
7. `HAVING`
8. `ORDER BY`
9. `LIMIT/OFFSET`

Compile nested CTE and subquery SQL into the same compile context so placeholders remain globally ordered.

## Constraints and guardrails
- Preserve existing `Table`, `Join`, and `LeftJoin` APIs.
- Keep behavior deterministic by preserving insertion order for CTEs and grouping expressions.
- Require subquery aliases and return a clear error when missing.
- Gate CTE support behind dialect feature checks.
- Keep aggregate helpers (`Count`, `Exists` internals) conservative; they now reject unsupported advanced clauses.

## Consequences
### Positive
- Supports common advanced select shapes without breaking existing query code.
- Preserves existing placeholder numbering behavior across nested query fragments.
- Keeps the API explicit and readable.

### Negative
- Introduces a new internal source abstraction in the select compiler.
- `Count` helper now rejects advanced clauses instead of trying to infer intent.

## Testing approach
Add table-driven tests for:

- DISTINCT
- GROUP BY with and without HAVING
- Single and multiple CTEs
- Subquery in `FROM` and `JOIN`
- Placeholder numbering across nested queries
- Invalid usage (missing subquery alias)
- CTE dialect guard error on unsupported dialect

Run full test suite across packages.
