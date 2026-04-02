package migrate

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

type stubResult struct{}

func (stubResult) LastInsertId() (int64, error) { return 0, nil }
func (stubResult) RowsAffected() (int64, error) { return 0, nil }

type scriptedExecutor struct {
	calls []execCall
}

type execCall struct {
	query  string
	result sql.Result
	err    error
}

func (s *scriptedExecutor) ExecContext(_ context.Context, query string, _ ...any) (sql.Result, error) {
	if len(s.calls) == 0 {
		return nil, errors.New("unexpected ExecContext call")
	}

	call := s.calls[0]
	s.calls = s.calls[1:]
	if query != call.query {
		return nil, errors.New("unexpected query: " + query)
	}

	return call.result, call.err
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "migrate.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	return db
}

func migrationCount(t *testing.T, ctx context.Context, db *sql.DB, table string) int {
	t.Helper()

	query := `SELECT COUNT(*) FROM ` + quoteIdentifier(table)
	row := db.QueryRowContext(ctx, query)

	var count int
	if err := row.Scan(&count); err != nil {
		t.Fatalf("scan migration count: %v", err)
	}

	return count
}

func userColumns(t *testing.T, ctx context.Context, db *sql.DB) []string {
	t.Helper()

	rows, err := db.QueryContext(ctx, `PRAGMA table_info(users)`)
	if err != nil {
		t.Fatalf("query pragma table_info(users): %v", err)
	}
	defer func() { _ = rows.Close() }()

	columns := make([]string, 0, 4)
	for rows.Next() {
		var (
			cid      int
			name     string
			colType  string
			notNull  int
			defaultV any
			pk       int
		)
		if scanErr := rows.Scan(&cid, &name, &colType, &notNull, &defaultV, &pk); scanErr != nil {
			t.Fatalf("scan table_info row: %v", scanErr)
		}
		columns = append(columns, name)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		t.Fatalf("iterate table_info rows: %v", rowsErr)
	}

	return columns
}

func TestApplyPendingZeroPendingMigrations(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTestDB(t)

	result, err := ApplyPending(ctx, db, nil)
	if err != nil {
		t.Fatalf("ApplyPending returned error: %v", err)
	}
	if len(result.AppliedIDs) != 0 {
		t.Fatalf("expected no applied ids, got %v", result.AppliedIDs)
	}
	if got := migrationCount(t, ctx, db, DefaultTableName); got != 0 {
		t.Fatalf("expected migration table to remain empty, got %d", got)
	}
}

func TestApplyPendingAppliesInDeterministicOrder(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTestDB(t)

	migrations := []Migration{
		{
			ID: "202602010930_create_users",
			Up: func(ctx context.Context, exec Executor) error {
				_, err := exec.ExecContext(ctx, `CREATE TABLE users (id INTEGER PRIMARY KEY AUTOINCREMENT)`)
				return err
			},
		},
		{
			ID: "202602010935_add_email",
			Up: func(ctx context.Context, exec Executor) error {
				_, err := exec.ExecContext(ctx, `ALTER TABLE users ADD COLUMN email TEXT NOT NULL DEFAULT ''`)
				return err
			},
		},
		{
			ID: "202602010940_add_name",
			Up: func(ctx context.Context, exec Executor) error {
				_, err := exec.ExecContext(ctx, `ALTER TABLE users ADD COLUMN name TEXT NOT NULL DEFAULT ''`)
				return err
			},
		},
	}

	unsorted := []Migration{migrations[2], migrations[0], migrations[1]}
	result, err := ApplyPending(ctx, db, unsorted)
	if err != nil {
		t.Fatalf("ApplyPending returned error: %v", err)
	}

	expectedIDs := []string{
		"202602010930_create_users",
		"202602010935_add_email",
		"202602010940_add_name",
	}
	if !reflect.DeepEqual(result.AppliedIDs, expectedIDs) {
		t.Fatalf("expected applied ids %v, got %v", expectedIDs, result.AppliedIDs)
	}

	expectedColumns := []string{"id", "email", "name"}
	if got := userColumns(t, ctx, db); !reflect.DeepEqual(got, expectedColumns) {
		t.Fatalf("expected users columns %v, got %v", expectedColumns, got)
	}
}

func TestApplyPendingStopsOnFailedMigration(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTestDB(t)

	failErr := errors.New("simulated failure")
	migrations := []Migration{
		{
			ID: "202602011000_create_users",
			Up: func(ctx context.Context, exec Executor) error {
				_, err := exec.ExecContext(ctx, `CREATE TABLE users (id INTEGER PRIMARY KEY AUTOINCREMENT)`)
				return err
			},
		},
		{
			ID: "202602011005_fail",
			Up: func(context.Context, Executor) error {
				return failErr
			},
		},
		{
			ID: "202602011010_never_runs",
			Up: func(ctx context.Context, exec Executor) error {
				_, err := exec.ExecContext(ctx, `ALTER TABLE users ADD COLUMN email TEXT`)
				return err
			},
		},
	}

	result, err := ApplyPending(ctx, db, migrations)
	if err == nil {
		t.Fatalf("expected migration failure")
	}
	if !errors.Is(err, failErr) {
		t.Fatalf("expected wrapped failErr, got %v", err)
	}

	if len(result.AppliedIDs) != 1 || result.AppliedIDs[0] != "202602011000_create_users" {
		t.Fatalf("expected first migration to be the only applied id, got %v", result.AppliedIDs)
	}
	if got := migrationCount(t, ctx, db, DefaultTableName); got != 1 {
		t.Fatalf("expected one recorded migration after failure, got %d", got)
	}
	if got := userColumns(t, ctx, db); !reflect.DeepEqual(got, []string{"id"}) {
		t.Fatalf("expected only id column after failed run, got %v", got)
	}
}

func TestApplyPendingSkipsAlreadyAppliedMigrations(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTestDB(t)
	upCalls := 0

	migrations := []Migration{
		{
			ID: "202602011100_create_users",
			Up: func(ctx context.Context, exec Executor) error {
				upCalls++
				_, err := exec.ExecContext(ctx, `CREATE TABLE users (id INTEGER PRIMARY KEY AUTOINCREMENT)`)
				return err
			},
		},
	}

	firstResult, firstErr := ApplyPending(ctx, db, migrations)
	if firstErr != nil {
		t.Fatalf("first ApplyPending returned error: %v", firstErr)
	}
	if len(firstResult.AppliedIDs) != 1 {
		t.Fatalf("expected first run to apply one migration, got %v", firstResult.AppliedIDs)
	}

	secondResult, secondErr := ApplyPending(ctx, db, migrations)
	if secondErr != nil {
		t.Fatalf("second ApplyPending returned error: %v", secondErr)
	}
	if len(secondResult.AppliedIDs) != 0 {
		t.Fatalf("expected second run to apply no migrations, got %v", secondResult.AppliedIDs)
	}
	if upCalls != 1 {
		t.Fatalf("expected migration Up to execute once, got %d", upCalls)
	}
}

func TestApplyPendingRollsBackFailedMigrationTransaction(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTestDB(t)

	migrations := []Migration{
		{
			ID: "202602011200_create_users",
			Up: func(ctx context.Context, exec Executor) error {
				if _, err := exec.ExecContext(ctx, `CREATE TABLE users (id INTEGER PRIMARY KEY AUTOINCREMENT)`); err != nil {
					return err
				}
				if _, err := exec.ExecContext(ctx, `ALTER TABLE users ADD COLUMN email TEXT`); err != nil {
					return err
				}
				return errors.New("fail after DDL")
			},
		},
	}

	_, err := ApplyPending(ctx, db, migrations)
	if err == nil {
		t.Fatalf("expected migration failure")
	}

	rows, queryErr := db.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name='users'`)
	if queryErr != nil {
		t.Fatalf("query sqlite_master: %v", queryErr)
	}
	defer func() { _ = rows.Close() }()
	if rows.Next() {
		t.Fatalf("expected failed migration transaction to roll back users table")
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		t.Fatalf("iterate sqlite_master rows: %v", rowsErr)
	}

	if got := migrationCount(t, ctx, db, DefaultTableName); got != 0 {
		t.Fatalf("expected no recorded migrations after rollback, got %d", got)
	}
}

func TestApplyPendingFailsFastOnDuplicateIdentifiers(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTestDB(t)

	_, err := ApplyPending(ctx, db, []Migration{
		{ID: "202602011300_same", Up: func(context.Context, Executor) error { return nil }},
		{ID: "202602011300_same", Up: func(context.Context, Executor) error { return nil }},
	})
	if err == nil {
		t.Fatalf("expected duplicate migration id error")
	}
	if !errors.Is(err, ErrDuplicateMigrationID) {
		t.Fatalf("expected ErrDuplicateMigrationID, got %v", err)
	}
	if !strings.Contains(err.Error(), "202602011300_same") {
		t.Fatalf("expected duplicate id in error message, got %v", err)
	}
}

func TestExecWithPlaceholderFallbackResultReturnsPrimaryErrorWhenNotPlaceholderRelated(t *testing.T) {
	t.Parallel()

	primaryErr := errors.New("UNIQUE constraint failed: users.email")
	exec := &scriptedExecutor{
		calls: []execCall{
			{query: "INSERT INTO users (email) VALUES (?)", err: primaryErr},
		},
	}

	_, err := execWithPlaceholderFallbackResult(
		context.Background(),
		exec,
		"INSERT INTO users (email) VALUES (?)",
		"dupe@example.com",
	)
	if !errors.Is(err, primaryErr) {
		t.Fatalf("expected primary error to be returned unchanged, got %v", err)
	}
	if len(exec.calls) != 0 {
		t.Fatalf("expected no fallback execution, remaining calls: %d", len(exec.calls))
	}
}

func TestExecWithPlaceholderFallbackResultRetriesOnPlaceholderSyntaxError(t *testing.T) {
	t.Parallel()

	exec := &scriptedExecutor{
		calls: []execCall{
			{
				query: "INSERT INTO rain_schema_migrations (id, checksum, applied_at, runtime_ms, tool_version, notes) VALUES (?, ?, ?, ?, ?, ?)",
				err:   errors.New(`ERROR: syntax error at or near "?"`),
			},
			{
				query:  "INSERT INTO rain_schema_migrations (id, checksum, applied_at, runtime_ms, tool_version, notes) VALUES ($1, $2, $3, $4, $5, $6)",
				err:    nil,
				result: stubResult{},
			},
		},
	}

	_, err := execWithPlaceholderFallbackResult(
		context.Background(),
		exec,
		"INSERT INTO rain_schema_migrations (id, checksum, applied_at, runtime_ms, tool_version, notes) VALUES (?, ?, ?, ?, ?, ?)",
		"202603011200_create_users",
		"abc123",
		"2026-03-01T12:00:00Z",
		int64(42),
		"",
		"",
	)
	if err != nil {
		t.Fatalf("expected fallback execution to succeed, got %v", err)
	}
	if len(exec.calls) != 0 {
		t.Fatalf("expected both scripted calls to be consumed, remaining calls: %d", len(exec.calls))
	}
}
