# ADR: Migration lock and state hardening

## Status
Accepted on 2026-04-02.

## Context
Rain's first CLI migration flow supported ordered forward-only SQL application backed by a migration table. That was enough for a narrow single-runner workflow, but it left gaps for normal deployment operations:

- concurrent `rain migrate` runs could execute the same migration body before one process recorded the applied row
- the migration table tracked only IDs, so edited migration files could not be detected
- deploys could silently run against a database that was already ahead of the local migration artifacts

The project needs a safer baseline without expanding into a full migration management product.

## Decision
Harden the existing migration flow with two additions:

1. Expand migration state to record a checksum for each applied migration.
2. Acquire a migration lock for the whole `rain migrate` run before validating state or applying SQL.

### State model
- Keep `rain_schema_migrations` as the default table.
- Record:
  - `id`
  - `checksum`
  - `applied_at`
  - `runtime_ms`
  - `tool_version`
  - `notes`
- Compute the checksum from the exact `migration.sql` bytes on disk.
- Fail `migrate` when:
  - the database has applied IDs that do not exist locally
  - an applied migration checksum differs from the local artifact
  - a pending local migration sorts before the last applied migration

### Lock model
- Use a repository-managed migration lock table.
- Acquire the lock before state validation and hold it until the run finishes.
- Store:
  - `lock_name`
  - `owner`
  - `expires_at`
- Treat the lock as a short lease and refresh it in the background while a migration run is active.
- Release the lock on normal exit.

This keeps the implementation cross-dialect and testable with SQLite while still covering the main deployment race.

## Consequences
### Positive
- Concurrent migration runners no longer execute the same pending migration set at the same time.
- Migration artifact edits become detectable.
- Deploying older artifacts against newer databases fails fast instead of silently doing nothing.

### Negative
- The migration table schema now evolves over time and must remain backward compatible.
- A crashed process can hold the lock until its lease expires.
- The lock is a pragmatic cross-dialect lease, not a database-native advisory lock.

## Follow-up
- Add a `rain status` command for operator-friendly state inspection.
- Consider native advisory locks for Postgres and MySQL in a later revision.
- Add explicit policy for dialects with non-transactional DDL semantics.
