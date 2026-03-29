# ADR: MySQL integration coverage in CI

## Status
Accepted on 2026-03-29.

## Context
The repository already contains MySQL-backed integration coverage in `pkg/rain/mysql_integration_test.go`, and CI already runs separate SQLite and Postgres integration jobs.

That leaves one gap in cross-dialect runtime verification: MySQL behavior is exercised locally when contributors provide a DSN, but it is not enforced as a distinct signal in GitHub Actions.

The workflow already uses separate jobs per backend, so adding MySQL should preserve that structure rather than introducing a larger matrix refactor.

## Decision
Add a dedicated `mysql-integration` job to the existing GitHub Actions workflow.

The new job:

- uses `ubuntu-latest`
- provisions a `mysql:8.4` service container
- creates a fixed `rain_test` database with a `rain` user
- passes `RAIN_MYSQL_DSN` to the test process
- runs `go test -race ./pkg/rain -run '^TestMySQLIntegration'`

Keep the existing SQLite and Postgres jobs unchanged.

## Consequences
### Positive
- CI now exposes MySQL runtime compatibility as an independent signal.
- Backend-specific failures remain isolated because each database keeps its own job.
- The workflow change is incremental and consistent with the current CI layout.

### Negative
- CI runtime increases because another service-backed job must start and execute.
- The workflow now contains some duplicated setup across database jobs.

## Testing approach
- Keep running the existing local validation flow:
  - `make fmt`
  - `make lint`
  - `make test`
- In GitHub Actions, run `go test -race ./pkg/rain -run '^TestMySQLIntegration'` in the dedicated MySQL job.
