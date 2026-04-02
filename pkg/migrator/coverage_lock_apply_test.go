package migrator

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hyperlocalise/rain-orm/pkg/migrate"
	_ "modernc.org/sqlite"
)

func TestApplySQLMigrationsSuccessAndSplitFailure(t *testing.T) {
	t.Parallel()

	t.Run("applies pending migrations", func(t *testing.T) {
		t.Parallel()

		db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "apply-success.sqlite"))
		if err != nil {
			t.Fatalf("sql.Open: %v", err)
		}
		defer func() { _ = db.Close() }()

		migrations := []DiskMigration{{
			ID:       "20260402010101_create_users",
			Checksum: "sum-1",
			SQL: `
CREATE TABLE "users" ("id" INTEGER PRIMARY KEY, "email" TEXT NOT NULL);
INSERT INTO "users" ("email") VALUES ('alice@example.com');
`,
		}}

		result, err := ApplySQLMigrations(context.Background(), db, "sqlite", "rain_schema_migrations", migrations)
		if err != nil {
			t.Fatalf("ApplySQLMigrations returned error: %v", err)
		}
		if len(result.AppliedIDs) != 1 || result.AppliedIDs[0] != migrations[0].ID {
			t.Fatalf("unexpected applied ids: %#v", result.AppliedIDs)
		}

		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM "users"`).Scan(&count); err != nil {
			t.Fatalf("count migrated rows: %v", err)
		}
		if count != 1 {
			t.Fatalf("expected 1 seeded row, got %d", count)
		}
	})

	t.Run("surfaces statement split errors", func(t *testing.T) {
		t.Parallel()

		db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "apply-split.sqlite"))
		if err != nil {
			t.Fatalf("sql.Open: %v", err)
		}
		defer func() { _ = db.Close() }()

		_, err = ApplySQLMigrations(context.Background(), db, "sqlite", "rain_schema_migrations", []DiskMigration{{
			ID:  "20260402010202_bad_sql",
			SQL: `INSERT INTO "users" ("email") VALUES ('unterminated);`,
		}})
		if err == nil || !strings.Contains(err.Error(), `split "20260402010202_bad_sql"`) {
			t.Fatalf("expected split error, got %v", err)
		}
	})
}

func TestApplySQLMigrationsBranchCoverage(t *testing.T) {
	origAcquire := acquireMigrationLockFunc
	origRunner := newMigrationRunner
	t.Cleanup(func() {
		acquireMigrationLockFunc = origAcquire
		newMigrationRunner = origRunner
	})

	t.Run("returns lock acquisition error", func(t *testing.T) {
		db := openSQLiteTestDB(t, "branch-lock-error.sqlite")
		defer func() { _ = db.Close() }()

		acquireMigrationLockFunc = func(ctx context.Context, db *sql.DB, dialectName, tableName string) (context.Context, migrationLockHandle, error) {
			return nil, nil, errors.New("lock failed")
		}

		if _, err := ApplySQLMigrations(context.Background(), db, "sqlite", "rain_schema_migrations", nil); err == nil || !strings.Contains(err.Error(), "lock failed") {
			t.Fatalf("expected lock error, got %v", err)
		}
	})

	t.Run("joins preflight runner error with lock error", func(t *testing.T) {
		db := openSQLiteTestDB(t, "branch-preflight.sqlite")
		defer func() { _ = db.Close() }()

		lock := &testMigrationLock{err: errors.New("heartbeat failed")}
		acquireMigrationLockFunc = func(ctx context.Context, db *sql.DB, dialectName, tableName string) (context.Context, migrationLockHandle, error) {
			return ctx, lock, nil
		}
		newMigrationRunner = func(tableName, dialectName string) migrationApplier {
			return testMigrationApplier{
				apply: func(ctx context.Context, db *sql.DB, migrations []migrate.Migration) (migrate.ApplyResult, error) {
					if migrations != nil {
						t.Fatalf("expected preflight runner to fail before migrations execute")
					}
					return migrate.ApplyResult{}, errors.New("preflight failed")
				},
			}
		}

		if _, err := ApplySQLMigrations(context.Background(), db, "sqlite", "rain_schema_migrations", nil); err == nil || !strings.Contains(err.Error(), "preflight failed") || !strings.Contains(err.Error(), "heartbeat failed") {
			t.Fatalf("expected joined preflight and lock error, got %v", err)
		}
	})

	t.Run("returns preflight runner error without lock error", func(t *testing.T) {
		db := openSQLiteTestDB(t, "branch-preflight-no-lock.sqlite")
		defer func() { _ = db.Close() }()

		lock := &testMigrationLock{}
		acquireMigrationLockFunc = func(ctx context.Context, db *sql.DB, dialectName, tableName string) (context.Context, migrationLockHandle, error) {
			return ctx, lock, nil
		}
		newMigrationRunner = func(tableName, dialectName string) migrationApplier {
			return testMigrationApplier{
				apply: func(ctx context.Context, db *sql.DB, migrations []migrate.Migration) (migrate.ApplyResult, error) {
					if migrations != nil {
						t.Fatalf("expected preflight runner to fail before migrations execute")
					}
					return migrate.ApplyResult{}, errors.New("plain preflight failure")
				},
			}
		}

		if _, err := ApplySQLMigrations(context.Background(), db, "sqlite", "rain_schema_migrations", nil); err == nil || !strings.Contains(err.Error(), "plain preflight failure") {
			t.Fatalf("expected plain preflight error, got %v", err)
		}
	})

	t.Run("skips blank statements and executes statements in order", func(t *testing.T) {
		db := openSQLiteTestDB(t, "branch-skip-blank.sqlite")
		defer func() { _ = db.Close() }()

		lock := &testMigrationLock{}
		acquireMigrationLockFunc = func(ctx context.Context, db *sql.DB, dialectName, tableName string) (context.Context, migrationLockHandle, error) {
			return ctx, lock, nil
		}

		var gotStatements []string
		call := 0
		newMigrationRunner = func(tableName, dialectName string) migrationApplier {
			return testMigrationApplier{
				apply: func(ctx context.Context, db *sql.DB, migrations []migrate.Migration) (migrate.ApplyResult, error) {
					call++
					if call == 1 {
						if migrations != nil {
							t.Fatalf("expected nil migrations during preflight")
						}
						return migrate.ApplyResult{}, nil
					}
					if len(migrations) != 1 {
						t.Fatalf("expected one migration, got %d", len(migrations))
					}
					exec := testMigrationExecutor{
						exec: func(ctx context.Context, query string, args ...any) (sql.Result, error) {
							gotStatements = append(gotStatements, query)
							return driver.RowsAffected(1), nil
						},
					}
					if err := migrations[0].Up(ctx, exec); err != nil {
						t.Fatalf("migration Up returned error: %v", err)
					}
					return migrate.ApplyResult{AppliedIDs: []string{migrations[0].ID}}, nil
				},
			}
		}

		result, err := ApplySQLMigrations(context.Background(), db, "sqlite", "rain_schema_migrations", []DiskMigration{{
			ID:       "20260402010303_seed",
			Checksum: "sum-2",
			SQL:      "  ;\nINSERT INTO users VALUES (1);\n ;\nUPDATE users SET id = 2;",
		}})
		if err != nil {
			t.Fatalf("ApplySQLMigrations returned error: %v", err)
		}
		if !slices.Equal(result.AppliedIDs, []string{"20260402010303_seed"}) {
			t.Fatalf("unexpected applied ids: %#v", result.AppliedIDs)
		}
		if !slices.Equal(gotStatements, []string{"INSERT INTO users VALUES (1)", "UPDATE users SET id = 2"}) {
			t.Fatalf("unexpected executed statements: %#v", gotStatements)
		}
	})

	t.Run("returns statement execution error", func(t *testing.T) {
		db := openSQLiteTestDB(t, "branch-exec-error.sqlite")
		defer func() { _ = db.Close() }()

		lock := &testMigrationLock{}
		acquireMigrationLockFunc = func(ctx context.Context, db *sql.DB, dialectName, tableName string) (context.Context, migrationLockHandle, error) {
			return ctx, lock, nil
		}
		newMigrationRunner = func(tableName, dialectName string) migrationApplier {
			return testMigrationApplier{
				apply: func(ctx context.Context, db *sql.DB, migrations []migrate.Migration) (migrate.ApplyResult, error) {
					if migrations == nil {
						return migrate.ApplyResult{}, nil
					}
					return migrate.ApplyResult{}, migrations[0].Up(ctx, testMigrationExecutor{
						exec: func(ctx context.Context, query string, args ...any) (sql.Result, error) {
							return nil, errors.New("exec failed")
						},
					})
				},
			}
		}

		if _, err := ApplySQLMigrations(context.Background(), db, "sqlite", "rain_schema_migrations", []DiskMigration{{
			ID:  "20260402010404_bad_exec",
			SQL: "DELETE FROM users;",
		}}); err == nil || !strings.Contains(err.Error(), "exec failed") {
			t.Fatalf("expected exec error, got %v", err)
		}
	})

	t.Run("returns final lock error when apply succeeds", func(t *testing.T) {
		db := openSQLiteTestDB(t, "branch-final-lock.sqlite")
		defer func() { _ = db.Close() }()

		lock := &testMigrationLock{err: errors.New("lock lost")}
		acquireMigrationLockFunc = func(ctx context.Context, db *sql.DB, dialectName, tableName string) (context.Context, migrationLockHandle, error) {
			return ctx, lock, nil
		}
		newMigrationRunner = func(tableName, dialectName string) migrationApplier {
			return testMigrationApplier{
				apply: func(ctx context.Context, db *sql.DB, migrations []migrate.Migration) (migrate.ApplyResult, error) {
					return migrate.ApplyResult{AppliedIDs: []string{"ok"}}, nil
				},
			}
		}

		result, err := ApplySQLMigrations(context.Background(), db, "sqlite", "rain_schema_migrations", nil)
		if err == nil || !strings.Contains(err.Error(), "lock lost") {
			t.Fatalf("expected final lock error, got %v", err)
		}
		if !slices.Equal(result.AppliedIDs, []string{"ok"}) {
			t.Fatalf("unexpected result when lock fails late: %#v", result.AppliedIDs)
		}
	})

	t.Run("joins final runner error with lock error", func(t *testing.T) {
		db := openSQLiteTestDB(t, "branch-final-join.sqlite")
		defer func() { _ = db.Close() }()

		lock := &testMigrationLock{err: errors.New("lock lost")}
		acquireMigrationLockFunc = func(ctx context.Context, db *sql.DB, dialectName, tableName string) (context.Context, migrationLockHandle, error) {
			return ctx, lock, nil
		}
		newMigrationRunner = func(tableName, dialectName string) migrationApplier {
			return testMigrationApplier{
				apply: func(ctx context.Context, db *sql.DB, migrations []migrate.Migration) (migrate.ApplyResult, error) {
					if migrations == nil {
						return migrate.ApplyResult{}, nil
					}
					return migrate.ApplyResult{AppliedIDs: []string{"partial"}}, errors.New("apply failed")
				},
			}
		}

		result, err := ApplySQLMigrations(context.Background(), db, "sqlite", "rain_schema_migrations", []DiskMigration{{
			ID:  "20260402010505_partial",
			SQL: "DELETE FROM users;",
		}})
		if err == nil || !strings.Contains(err.Error(), "apply failed") || !strings.Contains(err.Error(), "lock lost") {
			t.Fatalf("expected joined final error, got %v", err)
		}
		if !slices.Equal(result.AppliedIDs, []string{"partial"}) {
			t.Fatalf("unexpected partial result: %#v", result.AppliedIDs)
		}
	})

	t.Run("returns unlock error when apply succeeds", func(t *testing.T) {
		db := openSQLiteTestDB(t, "branch-unlock.sqlite")
		defer func() { _ = db.Close() }()

		lock := &testMigrationLock{unlockErr: errors.New("unlock lost")}
		acquireMigrationLockFunc = func(ctx context.Context, db *sql.DB, dialectName, tableName string) (context.Context, migrationLockHandle, error) {
			return ctx, lock, nil
		}
		newMigrationRunner = func(tableName, dialectName string) migrationApplier {
			return testMigrationApplier{
				apply: func(ctx context.Context, db *sql.DB, migrations []migrate.Migration) (migrate.ApplyResult, error) {
					return migrate.ApplyResult{AppliedIDs: []string{"ok"}}, nil
				},
			}
		}

		result, err := ApplySQLMigrations(context.Background(), db, "sqlite", "rain_schema_migrations", nil)
		if err == nil || !strings.Contains(err.Error(), "unlock lost") {
			t.Fatalf("expected unlock error, got %v", err)
		}
		if !slices.Equal(result.AppliedIDs, []string{"ok"}) {
			t.Fatalf("unexpected result when unlock fails: %#v", result.AppliedIDs)
		}
	})

	t.Run("joins unlock error with apply error", func(t *testing.T) {
		db := openSQLiteTestDB(t, "branch-unlock-join.sqlite")
		defer func() { _ = db.Close() }()

		lock := &testMigrationLock{unlockErr: errors.New("unlock lost")}
		acquireMigrationLockFunc = func(ctx context.Context, db *sql.DB, dialectName, tableName string) (context.Context, migrationLockHandle, error) {
			return ctx, lock, nil
		}
		newMigrationRunner = func(tableName, dialectName string) migrationApplier {
			return testMigrationApplier{
				apply: func(ctx context.Context, db *sql.DB, migrations []migrate.Migration) (migrate.ApplyResult, error) {
					if migrations == nil {
						return migrate.ApplyResult{}, nil
					}
					return migrate.ApplyResult{AppliedIDs: []string{"partial"}}, errors.New("apply failed")
				},
			}
		}

		result, err := ApplySQLMigrations(context.Background(), db, "sqlite", "rain_schema_migrations", []DiskMigration{{
			ID:  "20260402010606_unlock_join",
			SQL: "DELETE FROM users;",
		}})
		if err == nil || !strings.Contains(err.Error(), "apply failed") || !strings.Contains(err.Error(), "unlock lost") {
			t.Fatalf("expected joined apply and unlock error, got %v", err)
		}
		if !slices.Equal(result.AppliedIDs, []string{"partial"}) {
			t.Fatalf("unexpected partial result: %#v", result.AppliedIDs)
		}
	})
}

func TestSplitSQLStatementsAndDollarMarkers(t *testing.T) {
	t.Parallel()

	if _, err := SplitSQLStatements(`SELECT 'unterminated`); err == nil || !strings.Contains(err.Error(), "unterminated") {
		t.Fatalf("expected unterminated single quote error, got %v", err)
	}
	if _, err := SplitSQLStatements(`SELECT "unterminated`); err == nil || !strings.Contains(err.Error(), "unterminated") {
		t.Fatalf("expected unterminated double quote error, got %v", err)
	}
	if _, err := SplitSQLStatements(`DO $$ BEGIN SELECT 1;`); err == nil || !strings.Contains(err.Error(), "unterminated") {
		t.Fatalf("expected unterminated dollar quote error, got %v", err)
	}

	if marker, ok := parseDollarQuoteMarker("$tag$ rest"); !ok || marker != "$tag$" {
		t.Fatalf("expected tagged dollar quote marker, got %q ok=%v", marker, ok)
	}
	if marker, ok := parseDollarQuoteMarker("$1_$ rest"); !ok || marker != "$1_$" {
		t.Fatalf("expected numeric/underscore marker, got %q ok=%v", marker, ok)
	}
	if _, ok := parseDollarQuoteMarker("plain text"); ok {
		t.Fatalf("expected non-marker input to be rejected")
	}
	if _, ok := parseDollarQuoteMarker("$"); ok {
		t.Fatalf("expected short input to be rejected")
	}
	if _, ok := parseDollarQuoteMarker("$bad-marker$"); ok {
		t.Fatalf("expected invalid marker with dash to be rejected")
	}
	if _, ok := parseDollarQuoteMarker("$unterminated"); ok {
		t.Fatalf("expected unterminated marker to be rejected")
	}
	statements, err := SplitSQLStatements("SELECT $value;")
	if err != nil {
		t.Fatalf("SplitSQLStatements with literal dollar sign returned error: %v", err)
	}
	if !slices.Equal(statements, []string{"SELECT $value"}) {
		t.Fatalf("unexpected statements for literal dollar sign: %#v", statements)
	}
	statements, err = SplitSQLStatements("SELECT 1")
	if err != nil {
		t.Fatalf("SplitSQLStatements without trailing semicolon returned error: %v", err)
	}
	if !slices.Equal(statements, []string{"SELECT 1"}) {
		t.Fatalf("unexpected statements without trailing semicolon: %#v", statements)
	}
}

func TestAcquireMigrationLockDefaultNameAndFailurePaths(t *testing.T) {
	t.Parallel()

	t.Run("uses default lock name when migration table is blank", func(t *testing.T) {
		t.Parallel()

		db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "lock-default.sqlite"))
		if err != nil {
			t.Fatalf("sql.Open: %v", err)
		}
		defer func() { _ = db.Close() }()

		_, lock, err := acquireMigrationLock(context.Background(), db, "sqlite", "   ")
		if err != nil {
			t.Fatalf("acquireMigrationLock returned error: %v", err)
		}
		if lock.lockName != "default" {
			t.Fatalf("expected default lock name, got %q", lock.lockName)
		}
		if err := lock.Unlock(context.Background()); err != nil {
			t.Fatalf("Unlock returned error: %v", err)
		}
	})

	t.Run("returns ensure table error", func(t *testing.T) {
		t.Parallel()

		db := openExecScriptDB(t, func(query string, args []driver.NamedValue) (driver.Result, error) {
			return nil, errors.New("create table failed")
		})
		defer func() { _ = db.Close() }()

		if _, _, err := acquireMigrationLock(context.Background(), db, "sqlite", "rain_schema_migrations"); err == nil || !strings.Contains(err.Error(), "create migration lock table") {
			t.Fatalf("expected ensureTable error, got %v", err)
		}
	})

	t.Run("returns try acquire error", func(t *testing.T) {
		t.Parallel()

		call := 0
		db := openExecScriptDB(t, func(query string, args []driver.NamedValue) (driver.Result, error) {
			call++
			switch call {
			case 1:
				return driver.RowsAffected(0), nil
			case 2:
				return nil, errors.New("insert failed")
			default:
				t.Fatalf("unexpected exec %d for query %q", call, query)
				return nil, nil
			}
		})
		defer func() { _ = db.Close() }()

		if _, _, err := acquireMigrationLock(context.Background(), db, "sqlite", "rain_schema_migrations"); err == nil || !strings.Contains(err.Error(), `acquire migration lock "rain_schema_migrations"`) {
			t.Fatalf("expected tryAcquire error, got %v", err)
		}
	})
}

func TestMigrationLockHelpers(t *testing.T) {
	t.Parallel()

	t.Run("tryAcquire takes over expired lock", func(t *testing.T) {
		t.Parallel()

		call := 0
		db := openExecScriptDB(t, func(query string, args []driver.NamedValue) (driver.Result, error) {
			call++
			switch call {
			case 1:
				return nil, errors.New("UNIQUE constraint failed")
			case 2:
				return driver.RowsAffected(1), nil
			default:
				t.Fatalf("unexpected exec %d for query %q", call, query)
				return nil, nil
			}
		})
		defer func() { _ = db.Close() }()

		lock := testMigrationLockWithConn(t, db)
		lock.dialectName = "sqlite"
		lock.tableName = defaultLockTable
		lock.lockName = "locks"
		lock.owner = "owner-1"
		if err := lock.tryAcquire(context.Background(), time.Now().UTC()); err != nil {
			t.Fatalf("tryAcquire returned error: %v", err)
		}
	})

	t.Run("tryAcquire succeeds on first insert", func(t *testing.T) {
		t.Parallel()

		db := openExecScriptDB(t, func(query string, args []driver.NamedValue) (driver.Result, error) {
			return driver.RowsAffected(1), nil
		})
		defer func() { _ = db.Close() }()

		lock := testMigrationLockWithConn(t, db)
		lock.dialectName = "sqlite"
		lock.tableName = defaultLockTable
		lock.lockName = "locks"
		lock.owner = "owner-1"
		if err := lock.tryAcquire(context.Background(), time.Now().UTC()); err != nil {
			t.Fatalf("tryAcquire returned error: %v", err)
		}
	})

	t.Run("tryAcquire reports active lock when takeover update errors", func(t *testing.T) {
		t.Parallel()

		call := 0
		db := openExecScriptDB(t, func(query string, args []driver.NamedValue) (driver.Result, error) {
			call++
			if call == 1 {
				return nil, errors.New("duplicate entry")
			}
			return nil, errors.New("update failed")
		})
		defer func() { _ = db.Close() }()

		lock := testMigrationLockWithConn(t, db)
		lock.dialectName = "sqlite"
		lock.tableName = defaultLockTable
		lock.lockName = "locks"
		lock.owner = "owner-1"
		if err := lock.tryAcquire(context.Background(), time.Now().UTC()); err == nil || !strings.Contains(err.Error(), "another migration run is active") {
			t.Fatalf("expected active-lock error, got %v", err)
		}
	})

	t.Run("renew reports missing rows", func(t *testing.T) {
		t.Parallel()

		db := openExecScriptDB(t, func(query string, args []driver.NamedValue) (driver.Result, error) {
			return driver.RowsAffected(0), nil
		})
		defer func() { _ = db.Close() }()

		lock := testMigrationLockWithConn(t, db)
		lock.dialectName = "sqlite"
		lock.tableName = defaultLockTable
		lock.lockName = "locks"
		lock.owner = "owner-1"
		if err := lock.renew(context.Background(), time.Now().UTC()); err == nil || !strings.Contains(err.Error(), "missing") {
			t.Fatalf("expected missing-row renew error, got %v", err)
		}
	})

	t.Run("renew returns exec error", func(t *testing.T) {
		t.Parallel()

		db := openExecScriptDB(t, func(query string, args []driver.NamedValue) (driver.Result, error) {
			return nil, errors.New("renew failed")
		})
		defer func() { _ = db.Close() }()

		lock := testMigrationLockWithConn(t, db)
		lock.dialectName = "sqlite"
		lock.tableName = defaultLockTable
		lock.lockName = "locks"
		lock.owner = "owner-1"
		if err := lock.renew(context.Background(), time.Now().UTC()); err == nil || !strings.Contains(err.Error(), "renew failed") {
			t.Fatalf("expected renew exec error, got %v", err)
		}
	})

	t.Run("unlock reports lost lock row", func(t *testing.T) {
		t.Parallel()

		db := openExecScriptDB(t, func(query string, args []driver.NamedValue) (driver.Result, error) {
			return driver.RowsAffected(0), nil
		})
		defer func() { _ = db.Close() }()

		lock := testMigrationLockWithConn(t, db)
		lock.dialectName = "sqlite"
		lock.tableName = defaultLockTable
		lock.lockName = "locks"
		lock.owner = "owner-1"
		if err := lock.Unlock(context.Background()); err == nil || !strings.Contains(err.Error(), "was lost before release") {
			t.Fatalf("expected lost-lock error, got %v", err)
		}
	})

	t.Run("unlock wraps delete error", func(t *testing.T) {
		t.Parallel()

		db := openExecScriptDB(t, func(query string, args []driver.NamedValue) (driver.Result, error) {
			return nil, errors.New("delete failed")
		})
		defer func() { _ = db.Close() }()

		lock := testMigrationLockWithConn(t, db)
		lock.dialectName = "sqlite"
		lock.tableName = defaultLockTable
		lock.lockName = "locks"
		lock.owner = "owner-1"
		if err := lock.Unlock(context.Background()); err == nil || !strings.Contains(err.Error(), "release migration lock") {
			t.Fatalf("expected release error, got %v", err)
		}
	})

	t.Run("fail keeps first error", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		lock := &migrationLock{cancel: cancel}
		lock.fail(errors.New("first"))
		lock.fail(errors.New("second"))
		if err := lock.Err(); err == nil || err.Error() != "first" {
			t.Fatalf("expected first error to be retained, got %v", err)
		}
		<-ctx.Done()
	})
}

func TestExecWithPlaceholdersFallback(t *testing.T) {
	t.Parallel()

	t.Run("retries with numbered placeholders", func(t *testing.T) {
		t.Parallel()

		call := 0
		db := openExecScriptDB(t, func(query string, args []driver.NamedValue) (driver.Result, error) {
			call++
			switch call {
			case 1:
				if strings.Contains(query, "$1") {
					t.Fatalf("expected first query to use question mark placeholders, got %q", query)
				}
				return nil, errors.New(`near "?": syntax error`)
			case 2:
				if !strings.Contains(query, "$1") || !strings.Contains(query, "$2") {
					t.Fatalf("expected fallback query to use numbered placeholders, got %q", query)
				}
				return driver.RowsAffected(1), nil
			default:
				t.Fatalf("unexpected exec %d for query %q", call, query)
				return nil, nil
			}
		})
		defer func() { _ = db.Close() }()

		if _, err := execWithPlaceholders(context.Background(), db, "", `DELETE FROM "locks" WHERE lock_name = ? AND owner = ?`, "name", "owner"); err != nil {
			t.Fatalf("execWithPlaceholders returned error: %v", err)
		}
	})

	t.Run("does not retry non-placeholder query", func(t *testing.T) {
		t.Parallel()

		wantErr := errors.New("plain exec failed")
		db := openExecScriptDB(t, func(query string, args []driver.NamedValue) (driver.Result, error) {
			return nil, wantErr
		})
		defer func() { _ = db.Close() }()

		_, err := execWithPlaceholders(context.Background(), db, "", `DELETE FROM "locks"`, "ignored")
		if !errors.Is(err, wantErr) {
			t.Fatalf("expected original error, got %v", err)
		}
	})

	t.Run("does not retry unrelated placeholder errors", func(t *testing.T) {
		t.Parallel()

		wantErr := errors.New("driver offline")
		db := openExecScriptDB(t, func(query string, args []driver.NamedValue) (driver.Result, error) {
			return nil, wantErr
		})
		defer func() { _ = db.Close() }()

		_, err := execWithPlaceholders(context.Background(), db, "", `DELETE FROM "locks" WHERE lock_name = ?`, "name")
		if !errors.Is(err, wantErr) {
			t.Fatalf("expected original error, got %v", err)
		}
	})

	t.Run("uses postgres placeholders without retrying", func(t *testing.T) {
		t.Parallel()

		db := openExecScriptDB(t, func(query string, args []driver.NamedValue) (driver.Result, error) {
			if !strings.Contains(query, "$1") || !strings.Contains(query, "$2") {
				t.Fatalf("expected postgres query to use numbered placeholders, got %q", query)
			}
			return driver.RowsAffected(1), nil
		})
		defer func() { _ = db.Close() }()

		if _, err := execWithPlaceholders(context.Background(), db, "postgres", `DELETE FROM "locks" WHERE lock_name = ? AND owner = ?`, "name", "owner"); err != nil {
			t.Fatalf("execWithPlaceholders returned error: %v", err)
		}
	})
}

func TestMigrationLockHeartbeatFailure(t *testing.T) {
	origLease := defaultLockLease
	defaultLockLease = 20 * time.Millisecond
	defer func() { defaultLockLease = origLease }()

	db := openExecScriptDB(t, func(query string, args []driver.NamedValue) (driver.Result, error) {
		return nil, errors.New("renew boom")
	})
	defer func() { _ = db.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	lock := &migrationLock{
		cancel:      cancel,
		done:        make(chan struct{}),
		conn:        testConn(t, db),
		dialectName: "sqlite",
		tableName:   defaultLockTable,
		lockName:    "locks",
		owner:       "owner-1",
	}

	go lock.heartbeat(ctx)

	select {
	case <-lock.done:
	case <-time.After(time.Second):
		t.Fatal("heartbeat did not stop after renew failure")
	}

	if err := lock.Err(); err == nil || !strings.Contains(err.Error(), `renew migration lock "locks"`) {
		t.Fatalf("expected heartbeat renew error, got %v", err)
	}
}

func TestMigrationLockHeartbeatSuccessThenCancel(t *testing.T) {
	origLease := defaultLockLease
	defaultLockLease = 20 * time.Millisecond
	defer func() { defaultLockLease = origLease }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	renewed := make(chan struct{}, 1)
	db := openExecScriptDB(t, func(query string, args []driver.NamedValue) (driver.Result, error) {
		select {
		case renewed <- struct{}{}:
		default:
		}
		return driver.RowsAffected(1), nil
	})
	defer func() { _ = db.Close() }()

	lock := &migrationLock{
		cancel:      cancel,
		done:        make(chan struct{}),
		conn:        testConn(t, db),
		dialectName: "sqlite",
		tableName:   defaultLockTable,
		lockName:    "locks",
		owner:       "owner-1",
	}

	go lock.heartbeat(ctx)

	select {
	case <-renewed:
	case <-time.After(time.Second):
		t.Fatal("heartbeat did not renew in time")
	}

	cancel()

	select {
	case <-lock.done:
	case <-time.After(time.Second):
		t.Fatal("heartbeat did not stop after cancel")
	}

	if err := lock.Err(); err != nil {
		t.Fatalf("expected nil heartbeat error, got %v", err)
	}
}

type execScriptFunc func(query string, args []driver.NamedValue) (driver.Result, error)

type testMigrationLock struct {
	err       error
	unlockErr error
}

func (l *testMigrationLock) Unlock(context.Context) error { return l.unlockErr }
func (l *testMigrationLock) Err() error                   { return l.err }

func TestNewMigrationLockOwnerUniqueness(t *testing.T) {
	t.Parallel()

	owners := map[string]struct{}{}
	for range 16 {
		owner := newMigrationLockOwner()
		if _, exists := owners[owner]; exists {
			t.Fatalf("expected unique owner, got duplicate %q", owner)
		}
		if strings.Count(owner, "-") < 2 {
			t.Fatalf("expected owner to include pid and sequence, got %q", owner)
		}
		owners[owner] = struct{}{}
	}
}

type testMigrationApplier struct {
	apply func(context.Context, *sql.DB, []migrate.Migration) (migrate.ApplyResult, error)
}

func (a testMigrationApplier) ApplyPending(ctx context.Context, db *sql.DB, migrations []migrate.Migration) (migrate.ApplyResult, error) {
	return a.apply(ctx, db, migrations)
}

type testMigrationExecutor struct {
	exec func(context.Context, string, ...any) (sql.Result, error)
}

func (e testMigrationExecutor) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return e.exec(ctx, query, args...)
}

type execScriptDriver struct {
	exec execScriptFunc
}

type execScriptConn struct {
	exec execScriptFunc
}

func openExecScriptDB(t *testing.T, exec execScriptFunc) *sql.DB {
	t.Helper()

	name := fmt.Sprintf("migrator-exec-script-%d", atomic.AddUint64(&execScriptDriverCounter, 1))
	sql.Register(name, execScriptDriver{exec: exec})

	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatalf("sql.Open(%q): %v", name, err)
	}
	return db
}

func openSQLiteTestDB(t *testing.T, name string) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), name))
	if err != nil {
		t.Fatalf("sql.Open sqlite: %v", err)
	}
	return db
}

func testConn(t *testing.T, db *sql.DB) *sql.Conn {
	t.Helper()

	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatalf("db.Conn: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func testMigrationLockWithConn(t *testing.T, db *sql.DB) *migrationLock {
	t.Helper()

	return &migrationLock{conn: testConn(t, db)}
}

func (d execScriptDriver) Open(name string) (driver.Conn, error) {
	return &execScriptConn{exec: d.exec}, nil
}

func (c *execScriptConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("not implemented")
}
func (c *execScriptConn) Close() error              { return nil }
func (c *execScriptConn) Begin() (driver.Tx, error) { return nil, errors.New("not implemented") }

func (c *execScriptConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	return c.exec(query, args)
}

func (c *execScriptConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	return nil, errors.New("not implemented")
}

var execScriptDriverCounter uint64

var (
	_ driver.Driver         = execScriptDriver{}
	_ driver.Conn           = (*execScriptConn)(nil)
	_ driver.ExecerContext  = (*execScriptConn)(nil)
	_ driver.QueryerContext = (*execScriptConn)(nil)
	_ driver.ConnBeginTx    = (*execScriptConn)(nil)
)

func (c *execScriptConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	return nil, errors.New("not implemented")
}
