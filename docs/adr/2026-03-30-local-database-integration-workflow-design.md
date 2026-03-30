# ADR: Local database integration workflow

## Status
Accepted on 2026-03-30.

## Context
The repository already has SQLite integration coverage that runs without external services, plus MySQL and Postgres integration tests that accept environment-driven connection settings.

That is enough for CI and manual runs, but it leaves a local workflow gap: contributors must provision both databases themselves, wait for readiness, remember the correct DSNs, and clean the services up afterward.

The project needs a repeatable local path that keeps database-backed integration testing close to the repository and does not require hand-written shell commands every time.

## Decision
Add a repository-owned local database integration workflow built from:

- a root `compose.yaml` file that provisions Postgres and MySQL with fixed local credentials
- a `scripts/test-integration-databases.sh` helper that starts the services, waits for health checks, exports `RAIN_POSTGRES_DSN` and `RAIN_MYSQL_DSN`, runs the database integration test groups, and tears everything down on exit
- a `make test-integration-db` target that invokes the script

Keep the existing environment-variable based tests unchanged so CI and custom local setups continue to work.

## Consequences
### Positive
- Contributors get a single local command for service-backed integration coverage.
- Database lifecycle and DSN wiring stay in versioned repo files instead of ad hoc shell history.
- Cleanup runs even on test failure because the helper traps process exit.

### Negative
- The repository now owns local container configuration and must keep image tags and health checks up to date.
- The helper depends on Docker Compose being installed locally.

## Testing approach
- Run `make test-integration-db` for the local Postgres and MySQL workflow.
- Keep the standard repository validation flow:
  - `make fmt`
  - `make lint`
  - `make test`
