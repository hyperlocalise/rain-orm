// Package migrate provides a minimal database schema migration runner.
package migrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

const (
	// DefaultTableName is the table used to track applied migrations.
	DefaultTableName = "rain_schema_migrations"
)

var (
	// ErrDuplicateMigrationID is returned when the same migration ID appears more than once.
	ErrDuplicateMigrationID = errors.New("migrate: duplicate migration id")
	// ErrEmptyMigrationID is returned when a migration ID is blank.
	ErrEmptyMigrationID = errors.New("migrate: migration id is required")
	// ErrNilMigrationUp is returned when a migration has a nil Up function.
	ErrNilMigrationUp = errors.New("migrate: migration up function is required")
	// ErrInProgressMigration is returned when a prior non-transactional migration attempt did not finalize tracking.
	ErrInProgressMigration = errors.New("migrate: found migration in progress; manual reconciliation required")
)

// Executor executes SQL statements.
type Executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// Operation applies one side of a migration.
type Operation func(context.Context, Executor) error

// Migration defines one ordered schema change.
type Migration struct {
	ID string
	// Up is the forward migration function.
	Up Operation
	// Down is reserved for future rollback support.
	// v1 runner behavior is forward-only.
	Down Operation
	// NonTransactional disables the per-migration transaction wrapper.
	NonTransactional bool
}

// Runner applies migrations and tracks their state in a schema table.
type Runner struct {
	tableName string
}

// ApplyResult summarizes one ApplyPending run.
type ApplyResult struct {
	AppliedIDs []string
}

// NewRunner creates a migration runner.
func NewRunner(tableName string) *Runner {
	if strings.TrimSpace(tableName) == "" {
		tableName = DefaultTableName
	}

	return &Runner{tableName: tableName}
}

// ApplyPending applies migrations that are not yet recorded in the migration table.
func (r *Runner) ApplyPending(ctx context.Context, db *sql.DB, migrations []Migration) (ApplyResult, error) {
	if db == nil {
		return ApplyResult{}, errors.New("migrate: db is required")
	}

	normalized, err := validateAndSort(migrations)
	if err != nil {
		return ApplyResult{}, err
	}

	if err := r.ensureTable(ctx, db); err != nil {
		return ApplyResult{}, err
	}

	appliedSet, err := r.loadApplied(ctx, db)
	if err != nil {
		return ApplyResult{}, err
	}

	result := ApplyResult{AppliedIDs: make([]string, 0, len(normalized))}
	for _, migration := range normalized {
		if _, exists := appliedSet[migration.ID]; exists {
			continue
		}

		if err := r.applyOne(ctx, db, migration); err != nil {
			return result, err
		}

		result.AppliedIDs = append(result.AppliedIDs, migration.ID)
	}

	return result, nil
}

// ApplyPending applies pending migrations using the default migration table.
func ApplyPending(ctx context.Context, db *sql.DB, migrations []Migration) (ApplyResult, error) {
	return NewRunner(DefaultTableName).ApplyPending(ctx, db, migrations)
}

func validateAndSort(migrations []Migration) ([]Migration, error) {
	normalized := make([]Migration, len(migrations))
	copy(normalized, migrations)

	slices.SortFunc(normalized, func(a, b Migration) int {
		return strings.Compare(a.ID, b.ID)
	})

	seen := make(map[string]struct{}, len(normalized))
	for _, migration := range normalized {
		if strings.TrimSpace(migration.ID) == "" {
			return nil, ErrEmptyMigrationID
		}
		if migration.Up == nil {
			return nil, fmt.Errorf("%w: %q", ErrNilMigrationUp, migration.ID)
		}
		if _, exists := seen[migration.ID]; exists {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateMigrationID, migration.ID)
		}
		seen[migration.ID] = struct{}{}
	}

	return normalized, nil
}

func (r *Runner) ensureTable(ctx context.Context, db *sql.DB) error {
	query := fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS %s (
  id TEXT PRIMARY KEY,
  applied_at TIMESTAMP NOT NULL,
  runtime_ms INTEGER NOT NULL,
  notes TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL DEFAULT 'applied'
);`, quoteIdentifier(r.tableName))

	if _, err := db.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("migrate: create migration table %q: %w", r.tableName, err)
	}
	addStateQuery := fmt.Sprintf(
		"ALTER TABLE %s ADD COLUMN state TEXT NOT NULL DEFAULT 'applied'",
		quoteIdentifier(r.tableName),
	)
	_, _ = db.ExecContext(ctx, addStateQuery)

	return nil
}

func (r *Runner) loadApplied(ctx context.Context, db *sql.DB) (map[string]struct{}, error) {
	query := fmt.Sprintf("SELECT id, state FROM %s", quoteIdentifier(r.tableName))

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("migrate: query applied migrations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	applied := make(map[string]struct{})
	for rows.Next() {
		var (
			id    string
			state string
		)
		if scanErr := rows.Scan(&id, &state); scanErr != nil {
			return nil, fmt.Errorf("migrate: scan applied migration id: %w", scanErr)
		}
		if state != "applied" {
			return nil, fmt.Errorf("%w: id=%q state=%q", ErrInProgressMigration, id, state)
		}
		applied[id] = struct{}{}
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("migrate: read applied migrations: %w", rowsErr)
	}

	return applied, nil
}

func (r *Runner) applyOne(ctx context.Context, db *sql.DB, migration Migration) error {
	started := time.Now().UTC()

	execute := func(exec Executor) error {
		if err := migration.Up(ctx, exec); err != nil {
			return fmt.Errorf("migrate: run migration %q: %w", migration.ID, err)
		}
		runtimeMS := time.Since(started).Milliseconds()
		insertQuery := fmt.Sprintf(
			"INSERT INTO %s (id, applied_at, runtime_ms, notes, state) VALUES ({{p1}}, {{p2}}, {{p3}}, {{p4}}, {{p5}})",
			quoteIdentifier(r.tableName),
		)
		if err := execWithPlaceholderFallback(
			ctx,
			exec,
			insertQuery,
			migration.ID,
			started,
			runtimeMS,
			"",
			"applied",
		); err != nil {
			return fmt.Errorf("migrate: record migration %q: %w", migration.ID, err)
		}
		return nil
	}

	if migration.NonTransactional {
		if err := r.markInProgress(ctx, db, migration.ID, started); err != nil {
			return err
		}
		if err := migration.Up(ctx, db); err != nil {
			return r.clearInProgressOnFailure(ctx, db, migration.ID, err)
		}
		if err := r.finalizeInProgress(ctx, db, migration.ID, started); err != nil {
			return err
		}
		return nil
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("migrate: begin transaction for %q: %w", migration.ID, err)
	}

	if err := execute(tx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return errors.Join(err, fmt.Errorf("migrate: rollback transaction for %q: %w", migration.ID, rbErr))
		}
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("migrate: commit transaction for %q: %w", migration.ID, err)
	}

	return nil
}

func quoteIdentifier(name string) string {
	escaped := strings.ReplaceAll(name, `"`, `""`)
	return `"` + escaped + `"`
}

func execWithPlaceholderFallback(ctx context.Context, exec Executor, query string, args ...any) error {
	_, err := execWithPlaceholderFallbackResult(ctx, exec, query, args...)
	return err
}

func execWithPlaceholderFallbackResult(ctx context.Context, exec Executor, query string, args ...any) (sql.Result, error) {
	dollarQuery := bindNumbered(query)
	if result, err := exec.ExecContext(ctx, dollarQuery, args...); err == nil {
		return result, nil
	}

	questionQuery := bindQuestion(query)
	result, err := exec.ExecContext(ctx, questionQuery, args...)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func bindNumbered(template string) string {
	result := template
	for i := 1; strings.Contains(result, "{{p"); i++ {
		marker := fmt.Sprintf("{{p%d}}", i)
		if !strings.Contains(result, marker) {
			break
		}
		result = strings.ReplaceAll(result, marker, fmt.Sprintf("$%d", i))
	}
	return result
}

func bindQuestion(template string) string {
	result := template
	for i := 1; strings.Contains(result, "{{p"); i++ {
		marker := fmt.Sprintf("{{p%d}}", i)
		if !strings.Contains(result, marker) {
			break
		}
		result = strings.ReplaceAll(result, marker, "?")
	}
	return result
}

func (r *Runner) markInProgress(ctx context.Context, db *sql.DB, id string, started time.Time) error {
	query := fmt.Sprintf(
		"INSERT INTO %s (id, applied_at, runtime_ms, notes, state) VALUES ({{p1}}, {{p2}}, {{p3}}, {{p4}}, {{p5}})",
		quoteIdentifier(r.tableName),
	)
	if err := execWithPlaceholderFallback(ctx, db, query, id, started, int64(0), "non_transactional", "in_progress"); err != nil {
		return fmt.Errorf("migrate: mark migration %q in progress: %w", id, err)
	}
	return nil
}

func (r *Runner) clearInProgressOnFailure(ctx context.Context, db *sql.DB, id string, originalErr error) error {
	query := fmt.Sprintf("DELETE FROM %s WHERE id = {{p1}} AND state = {{p2}}", quoteIdentifier(r.tableName))
	if err := execWithPlaceholderFallback(ctx, db, query, id, "in_progress"); err != nil {
		return errors.Join(originalErr, fmt.Errorf("migrate: clear in-progress marker for %q: %w", id, err))
	}
	return fmt.Errorf("migrate: run migration %q: %w", id, originalErr)
}

func (r *Runner) finalizeInProgress(ctx context.Context, db *sql.DB, id string, started time.Time) error {
	query := fmt.Sprintf(
		"UPDATE %s SET runtime_ms = {{p1}}, notes = {{p2}}, state = {{p3}} WHERE id = {{p4}} AND state = {{p5}}",
		quoteIdentifier(r.tableName),
	)
	result, err := execWithPlaceholderFallbackResult(
		ctx,
		db,
		query,
		time.Since(started).Milliseconds(),
		"",
		"applied",
		id,
		"in_progress",
	)
	if err != nil {
		return fmt.Errorf("migrate: finalize migration %q tracking: %w", id, err)
	}
	rowsAffected, rowsErr := result.RowsAffected()
	if rowsErr != nil {
		return fmt.Errorf("migrate: read finalize rows affected for %q: %w", id, rowsErr)
	}
	if rowsAffected != 1 {
		return fmt.Errorf("migrate: finalize migration %q tracking: expected 1 row, got %d", id, rowsAffected)
	}
	return nil
}
