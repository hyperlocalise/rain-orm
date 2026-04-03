// Package migrate provides a minimal database schema migration runner.
package migrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
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
	// Checksum identifies the exact migration contents when provided.
	Checksum string
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
	tableName   string
	dialectName string
}

type appliedMigration struct {
	ID       string
	Checksum string
	State    string
}

// ApplyResult summarizes one ApplyPending run.
type ApplyResult struct {
	AppliedIDs []string
}

// NewRunner creates a migration runner.
func NewRunner(tableName string) *Runner {
	return NewRunnerForDialect(tableName, "")
}

// NewRunnerForDialect creates a migration runner configured for one SQL dialect.
func NewRunnerForDialect(tableName, dialectName string) *Runner {
	if strings.TrimSpace(tableName) == "" {
		tableName = DefaultTableName
	}

	return &Runner{tableName: tableName, dialectName: normalizeDialectName(dialectName)}
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
		if applied, exists := appliedSet[migration.ID]; exists {
			if migration.Checksum != "" && applied.Checksum != "" && migration.Checksum != applied.Checksum {
				return result, fmt.Errorf(
					"migrate: applied migration %q checksum mismatch: db=%s local=%s",
					migration.ID,
					applied.Checksum,
					migration.Checksum,
				)
			}
			switch applied.State {
			case "applied":
				continue
			case "failed":
				return result, fmt.Errorf("migrate: migration %q is marked failed and must be recovered before retry", migration.ID)
			case "in_progress":
				return result, fmt.Errorf("migrate: migration %q is already in_progress and requires operator recovery", migration.ID)
			default:
				return result, fmt.Errorf("migrate: migration %q is in unsupported state %q", migration.ID, applied.State)
			}
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
  checksum TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL DEFAULT 'applied',
  started_at TIMESTAMP NULL,
  applied_at TIMESTAMP NULL,
  failed_at TIMESTAMP NULL,
  runtime_ms INTEGER NOT NULL,
  error_message TEXT NOT NULL DEFAULT '',
  tool_version TEXT NOT NULL DEFAULT '',
  notes TEXT NOT NULL DEFAULT ''
);`, quoteIdentifierForDialect(r.dialectName, r.tableName))

	if _, err := db.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("migrate: create migration table %q: %w", r.tableName, err)
	}

	for _, statement := range []string{
		fmt.Sprintf(
			`ALTER TABLE %s ADD COLUMN checksum TEXT NOT NULL DEFAULT ''`,
			quoteIdentifierForDialect(r.dialectName, r.tableName),
		),
		fmt.Sprintf(
			`ALTER TABLE %s ADD COLUMN tool_version TEXT NOT NULL DEFAULT ''`,
			quoteIdentifierForDialect(r.dialectName, r.tableName),
		),
		fmt.Sprintf(
			`ALTER TABLE %s ADD COLUMN state TEXT NOT NULL DEFAULT 'applied'`,
			quoteIdentifierForDialect(r.dialectName, r.tableName),
		),
		fmt.Sprintf(
			`ALTER TABLE %s ADD COLUMN started_at TIMESTAMP NULL`,
			quoteIdentifierForDialect(r.dialectName, r.tableName),
		),
		fmt.Sprintf(
			`ALTER TABLE %s ADD COLUMN failed_at TIMESTAMP NULL`,
			quoteIdentifierForDialect(r.dialectName, r.tableName),
		),
		fmt.Sprintf(
			`ALTER TABLE %s ADD COLUMN error_message TEXT NOT NULL DEFAULT ''`,
			quoteIdentifierForDialect(r.dialectName, r.tableName),
		),
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil && !isDuplicateColumnError(err) {
			return fmt.Errorf("migrate: evolve migration table %q: %w", r.tableName, err)
		}
	}
	if _, err := db.ExecContext(
		ctx,
		fmt.Sprintf(
			`UPDATE %s SET state = 'applied' WHERE state = '' AND applied_at IS NOT NULL`,
			quoteIdentifierForDialect(r.dialectName, r.tableName),
		),
	); err != nil {
		return fmt.Errorf("migrate: backfill migration state for %q: %w", r.tableName, err)
	}

	return nil
}

func (r *Runner) loadApplied(ctx context.Context, db *sql.DB) (map[string]appliedMigration, error) {
	query := fmt.Sprintf("SELECT id, checksum, state FROM %s", quoteIdentifierForDialect(r.dialectName, r.tableName))

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("migrate: query applied migrations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	applied := make(map[string]appliedMigration)
	for rows.Next() {
		var migration appliedMigration
		if scanErr := rows.Scan(&migration.ID, &migration.Checksum, &migration.State); scanErr != nil {
			return nil, fmt.Errorf("migrate: scan applied migration id: %w", scanErr)
		}
		applied[migration.ID] = migration
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("migrate: read applied migrations: %w", rowsErr)
	}

	return applied, nil
}

func (r *Runner) applyOne(ctx context.Context, db *sql.DB, migration Migration) error {
	started := time.Now().UTC()
	if normalizeDialectName(r.dialectName) == "mysql" {
		return r.applyOneMySQLSafe(ctx, db, migration, started)
	}

	execute := func(exec Executor) error {
		if err := migration.Up(ctx, exec); err != nil {
			return fmt.Errorf("migrate: run migration %q: %w", migration.ID, err)
		}
		runtimeMS := time.Since(started).Milliseconds()
		insertQuery := fmt.Sprintf(
			"INSERT INTO %s (id, checksum, state, started_at, applied_at, runtime_ms, error_message, tool_version, notes) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
			quoteIdentifierForDialect(r.dialectName, r.tableName),
		)
		if _, err := execWithPlaceholdersResult(
			ctx,
			exec,
			r.dialectName,
			insertQuery,
			migration.ID,
			migration.Checksum,
			"applied",
			started,
			started,
			runtimeMS,
			"",
			"",
			"",
		); err != nil {
			return fmt.Errorf("migrate: record migration %q: %w", migration.ID, err)
		}
		return nil
	}

	if migration.NonTransactional {
		if err := execute(db); err != nil {
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

func (r *Runner) applyOneMySQLSafe(ctx context.Context, db *sql.DB, migration Migration, started time.Time) error {
	insertQuery := fmt.Sprintf(
		"INSERT INTO %s (id, checksum, state, started_at, runtime_ms, error_message, tool_version, notes) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		quoteIdentifierForDialect(r.dialectName, r.tableName),
	)
	if _, err := execWithPlaceholdersResult(
		ctx,
		db,
		r.dialectName,
		insertQuery,
		migration.ID,
		migration.Checksum,
		"in_progress",
		started,
		int64(0),
		"",
		"",
		"",
	); err != nil {
		return fmt.Errorf("migrate: mark migration %q in_progress: %w", migration.ID, err)
	}

	if err := migration.Up(ctx, db); err != nil {
		runtimeMS := time.Since(started).Milliseconds()
		updateFailure := fmt.Sprintf(
			"UPDATE %s SET state = ?, failed_at = ?, runtime_ms = ?, error_message = ? WHERE id = ? AND state = ?",
			quoteIdentifierForDialect(r.dialectName, r.tableName),
		)
		trackCtx, trackCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer trackCancel()
		result, markErr := execWithPlaceholdersResult(
			trackCtx,
			db,
			r.dialectName,
			updateFailure,
			"failed",
			time.Now().UTC(),
			runtimeMS,
			truncateMigrationError(err.Error()),
			migration.ID,
			"in_progress",
		)
		if markErr != nil {
			return errors.Join(fmt.Errorf("migrate: run migration %q: %w", migration.ID, err), fmt.Errorf("migrate: mark migration %q failed: %w", migration.ID, markErr))
		}
		if rowsAffected, rowsErr := result.RowsAffected(); rowsErr == nil && rowsAffected == 0 {
			return errors.Join(
				fmt.Errorf("migrate: run migration %q: %w", migration.ID, err),
				fmt.Errorf("migrate: mark migration %q failed: state row not found or already changed", migration.ID),
			)
		}
		return fmt.Errorf("migrate: run migration %q: %w", migration.ID, err)
	}

	runtimeMS := time.Since(started).Milliseconds()
	updateSuccess := fmt.Sprintf(
		"UPDATE %s SET state = ?, applied_at = ?, runtime_ms = ?, failed_at = NULL, error_message = '' WHERE id = ? AND state = ?",
		quoteIdentifierForDialect(r.dialectName, r.tableName),
	)
	trackCtx, trackCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer trackCancel()
	result, err := execWithPlaceholdersResult(
		trackCtx,
		db,
		r.dialectName,
		updateSuccess,
		"applied",
		time.Now().UTC(),
		runtimeMS,
		migration.ID,
		"in_progress",
	)
	if err != nil {
		return fmt.Errorf("migrate: mark migration %q applied: %w", migration.ID, err)
	}
	if rowsAffected, rowsErr := result.RowsAffected(); rowsErr == nil && rowsAffected == 0 {
		return fmt.Errorf("migrate: mark migration %q applied: state row not found or already changed", migration.ID)
	}
	return nil
}

func quoteIdentifierForDialect(dialectName, name string) string {
	if normalizeDialectName(dialectName) == "mysql" {
		return "`" + strings.ReplaceAll(name, "`", "``") + "`"
	}
	escaped := strings.ReplaceAll(name, `"`, `""`)
	return `"` + escaped + `"`
}

var placeholderPattern = regexp.MustCompile(`\?`)

func execWithPlaceholdersResult(
	ctx context.Context,
	exec Executor,
	dialectName string,
	query string,
	args ...any,
) (sql.Result, error) {
	preparedQuery := replacePlaceholdersForDialect(normalizeDialectName(dialectName), query)
	result, err := exec.ExecContext(ctx, preparedQuery, args...)
	if err == nil {
		return result, nil
	}

	if preparedQuery != query || !shouldRetryWithDollarPlaceholders(query, err) {
		return nil, err
	}

	fallbackQuery := replaceWithDollarPlaceholders(query)
	return exec.ExecContext(ctx, fallbackQuery, args...)
}

func replacePlaceholdersForDialect(dialectName, query string) string {
	if dialectName == "postgres" || dialectName == "postgresql" {
		return replaceWithDollarPlaceholders(query)
	}
	return query
}

func normalizeDialectName(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "postgres", "postgresql":
		return "postgres"
	case "sqlite", "sqlite3":
		return "sqlite"
	case "mysql":
		return "mysql"
	default:
		return strings.ToLower(strings.TrimSpace(name))
	}
}

func shouldRetryWithDollarPlaceholders(query string, err error) bool {
	if !strings.Contains(query, "?") {
		return false
	}

	lowerErr := strings.ToLower(err.Error())

	placeholderHints := []string{
		`near "?"`,
		"at or near \"?\"",
		"syntax error",
		"expected",
	}

	for _, hint := range placeholderHints {
		if strings.Contains(lowerErr, hint) {
			return true
		}
	}

	return false
}

func isDuplicateColumnError(err error) bool {
	lowerErr := strings.ToLower(err.Error())
	return strings.Contains(lowerErr, "duplicate column") ||
		(strings.Contains(lowerErr, "column") && strings.Contains(lowerErr, "already exists")) ||
		strings.Contains(lowerErr, "duplicate column name") ||
		strings.Contains(lowerErr, "sqlstate 42701")
}

func truncateMigrationError(message string) string {
	const maxLen = 1024
	if len(message) <= maxLen {
		return message
	}
	for idx := maxLen; idx > 0; idx-- {
		if utf8.RuneStart(message[idx]) {
			return message[:idx]
		}
	}
	return ""
}

func replaceWithDollarPlaceholders(query string) string {
	counter := 0

	return placeholderPattern.ReplaceAllStringFunc(query, func(_ string) string {
		counter++
		return fmt.Sprintf("$%d", counter)
	})
}
