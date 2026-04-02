# rain CLI

The `rain` CLI provides Rain's code-first migration workflow.
It supports:

- `generate` to diff the current schema registry against the latest persisted snapshot
- `migrate` to apply generated SQL migrations
- `check` to verify that the current schema still matches the latest migration snapshot

This design is intentionally similar to Drizzle Kit's code-first `generate` flow, but adapted to Go and Rain's schema model.

## Design

`generate` works as a migration-chain operation:

1. Load the configured schema registry and build a JSON snapshot from Rain table metadata.
2. Read prior migration folders from the configured `out` directory in timestamp order.
3. Compare the current snapshot to the newest snapshot in that chain.
4. Write a new timestamped migration folder containing:
   - `migration.sql`
   - `snapshot.json`

Example layout:

```text
rain/migrations/
  20260330121500_init/
    migration.sql
    snapshot.json
  20260330123010_add_posts/
    migration.sql
    snapshot.json
```

The migration folders are the source of truth. There is no mutable `latest.snapshot.json`.
If any migration folder is missing `migration.sql` or `snapshot.json`, both `generate` and `check` fail until the chain is repaired.
Rain v1 is forward-only. It does not generate or apply down migrations.

## Registry

Rain uses an explicit schema registry package instead of file globs.
The registry must export a function that returns the managed tables:

```go
package registry

import "github.com/hyperlocalise/rain-orm/pkg/schema"

func ManagedTables() []schema.TableReference {
	return []schema.TableReference{Users, Posts, Memberships}
}
```

## Config

Development config:

```yml
dialect: sqlite
schema_package: ./examples/schema/registry
schema_function: ManagedTables
out: rain/migrations
migration_table: rain_schema_migrations
dsn: app.sqlite
```

Deploy-only config for `migrate`:

```yml
dialect: sqlite
out: rain/migrations
migration_table: rain_schema_migrations
dsn: app.sqlite
```

`migrate` does not require schema source settings because it runs from generated artifacts only.

## Commands

Generate a migration:

```bash
go run ./cmd/rain generate --name init
```

Apply pending migrations:

```bash
go run ./cmd/rain migrate
```

`migrate` acquires a migration lock for the duration of the run and verifies that already-applied migration checksums still match the local `migration.sql` artifacts before executing pending SQL.
It also fails fast when the configured migration directory is missing instead of treating that as an empty chain.
Today `migrate` is supported for `sqlite` and `postgres`. MySQL migration apply is intentionally blocked until locking and DDL-failure semantics are hardened further.

Check that the live schema registry matches the newest migration snapshot:

```bash
go run ./cmd/rain check
```

## Current Scope

This v1 CLI is additive-first.
It supports create-table, add-column, add-index, and safe additive constraint flows where the dialect supports them.
It does not include:

- Studio
- direct schema push
- database introspection / pull
- rename prompts
- automatic destructive diffs
