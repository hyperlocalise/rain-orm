# ADR: SQLite integration coverage in CI

## Status
Accepted on 2026-03-29.

## Context
The repository already contains SQLite-backed integration tests in `pkg/rain/sqlite_integration_test.go`, but CI only runs the generic precommit flow.

That means runtime verification against a real database-backed code path is present in the repo but not surfaced as a distinct CI signal.

Postgres and MySQL integration tests do not exist yet, so provisioning those services in CI today would add setup cost without adding verification value.

## Decision
Add a separate `integration` job to the existing GitHub Actions workflow.

The new job:

- uses `ubuntu-latest`
- installs Go 1.26
- runs the existing SQLite integration tests in `pkg/rain`

Keep the current `precommit` job unchanged.

Do not add Postgres or MySQL services yet. Track those as follow-up work once matching integration tests exist.

## Consequences
### Positive
- CI now exposes a dedicated integration signal instead of hiding SQLite runtime verification inside the broader test suite.
- The workflow remains simple because SQLite does not require service containers.
- Follow-up database expansion can be added incrementally once Postgres and MySQL test coverage exists.

### Negative
- Cross-dialect runtime verification remains incomplete until Postgres and MySQL integration tests are implemented.
- Some overlap remains between `precommit` and the separate integration job because both exercise code in `pkg/rain`.

## Testing approach
- Keep running the existing local validation flow:
  - `make fmt`
  - `make lint`
  - `make test`
- In GitHub Actions, run `go test -race ./pkg/rain -run '^TestSQLiteIntegration'` in the dedicated integration job.
