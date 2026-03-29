# ADR: Typed relation definitions and relation loading

## Context

Rain tracks foreign keys but lacked first-class relation metadata and a relation loading API for select queries.

## Decision

Add relation metadata to schema table definitions and explicit relation loading to `SelectQuery`.

### Schema metadata

- `schema.RelationDef` captures:
  - relation name
  - relation type (`belongs_to`, `has_many`)
  - source column
  - target table/column
- `TableModel.BelongsTo` and `TableModel.HasMany` register typed relations.
- `TableDef.RelationByName` resolves metadata by relation name.

### Query API

- `SelectQuery.WithRelations("name", ...)` opt-in relation loading.
- Relation loading runs after base rows are scanned.
- Unknown relations return errors.
- Relation loading requires a concrete table source (`Table(...)`), not a subquery source.

### Scan mapping rules (v1)

- Relation fields are explicit via struct tag: `rain:"relation:<name>"`.
- `belongs_to` relation fields must be a struct or `*struct`.
- `has_many` relation fields must be `[]struct`.
- Column mapping still uses `db:"column_name"` tags.

## Consequences

- API remains explicit and understandable.
- v1 implementation is intentionally simple and may issue additional queries when loading relations.
- Design leaves room for future optimization (e.g., join-based eager loading, batched `IN` loading, nested relations).
