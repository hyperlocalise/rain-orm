package rain

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hyperlocalise/rain-orm/pkg/dialect"
	_ "modernc.org/sqlite"
)

type noSavepointDialect struct {
	dialect.BaseDialect
}

func (d *noSavepointDialect) Name() string {
	return "no-savepoint"
}

func (d *noSavepointDialect) QuoteIdentifier(name string) string {
	return name
}

func (d *noSavepointDialect) Placeholder(_ int) string {
	return "?"
}

func (d *noSavepointDialect) AutoIncrementKeyword() string {
	return ""
}

func (d *noSavepointDialect) LimitOffset(_, _ int) string {
	return ""
}

func (d *noSavepointDialect) CurrentTimestamp() string {
	return "CURRENT_TIMESTAMP"
}

func (d *noSavepointDialect) BooleanLiteral(v bool) string {
	if v {
		return "TRUE"
	}

	return "FALSE"
}

func openTxHelperDB(t *testing.T) *DB {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "tx-helper.sqlite")
	db, err := Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	return db
}

func createTxHelperSchema(t *testing.T, ctx context.Context, db *DB) {
	t.Helper()

	if _, err := db.Exec(ctx, `CREATE TABLE tx_helper_users (id INTEGER PRIMARY KEY AUTOINCREMENT, email TEXT NOT NULL)`); err != nil {
		t.Fatalf("create table tx_helper_users: %v", err)
	}
}

func countTxHelperRows(t *testing.T, ctx context.Context, db *DB) int {
	t.Helper()

	row := db.QueryRow(ctx, `SELECT COUNT(*) FROM tx_helper_users`)
	if row == nil {
		t.Fatalf("QueryRow returned nil")
	}

	var count int
	if err := row.Scan(&count); err != nil {
		t.Fatalf("scan row count: %v", err)
	}

	return count
}

func TestDBRunInTxCommitsOnSuccess(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTxHelperDB(t)
	createTxHelperSchema(t, ctx, db)

	err := db.RunInTx(ctx, func(tx *Tx) error {
		_, execErr := tx.execContext(ctx, `INSERT INTO tx_helper_users (email) VALUES (?)`, "success@example.com")
		return execErr
	})
	if err != nil {
		t.Fatalf("RunInTx returned error: %v", err)
	}

	if got := countTxHelperRows(t, ctx, db); got != 1 {
		t.Fatalf("expected 1 row after commit, got %d", got)
	}
}

func TestDBRunInTxRollsBackOnCallbackError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTxHelperDB(t)
	createTxHelperSchema(t, ctx, db)

	callbackErr := errors.New("callback failed")
	err := db.RunInTx(ctx, func(tx *Tx) error {
		if _, execErr := tx.execContext(ctx, `INSERT INTO tx_helper_users (email) VALUES (?)`, "rollback@example.com"); execErr != nil {
			return execErr
		}

		return callbackErr
	})
	if !errors.Is(err, callbackErr) {
		t.Fatalf("expected callback error %v, got %v", callbackErr, err)
	}

	if got := countTxHelperRows(t, ctx, db); got != 0 {
		t.Fatalf("expected 0 rows after rollback, got %d", got)
	}
}

func TestDBRunInTxRollsBackWhenCommitFails(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTxHelperDB(t)
	createTxHelperSchema(t, ctx, db)

	err := db.RunInTx(ctx, func(tx *Tx) error {
		if _, execErr := tx.execContext(ctx, `INSERT INTO tx_helper_users (email) VALUES (?)`, "manual-commit@example.com"); execErr != nil {
			return execErr
		}

		return tx.Commit()
	})
	if err == nil {
		t.Fatalf("expected commit failure error")
	}
	if !strings.Contains(err.Error(), "commit transaction") {
		t.Fatalf("expected commit failure error message, got %v", err)
	}

	if got := countTxHelperRows(t, ctx, db); got != 1 {
		t.Fatalf("expected row to remain committed by callback commit, got %d", got)
	}
}

func TestTxRunInTxNestedSuccess(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTxHelperDB(t)
	createTxHelperSchema(t, ctx, db)

	err := db.RunInTx(ctx, func(tx *Tx) error {
		return tx.RunInTx(ctx, func(nested *Tx) error {
			_, execErr := nested.execContext(ctx, `INSERT INTO tx_helper_users (email) VALUES (?)`, "nested-success@example.com")
			return execErr
		})
	})
	if err != nil {
		t.Fatalf("RunInTx with nested transaction returned error: %v", err)
	}

	if got := countTxHelperRows(t, ctx, db); got != 1 {
		t.Fatalf("expected 1 row after nested commit, got %d", got)
	}
}

func TestTxRunInTxNestedFailureRollsBackToSavepoint(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTxHelperDB(t)
	createTxHelperSchema(t, ctx, db)

	nestedErr := errors.New("nested failed")
	err := db.RunInTx(ctx, func(tx *Tx) error {
		if nestedRunErr := tx.RunInTx(ctx, func(nested *Tx) error {
			if _, execErr := nested.execContext(ctx, `INSERT INTO tx_helper_users (email) VALUES (?)`, "nested-failure@example.com"); execErr != nil {
				return execErr
			}

			return nestedErr
		}); !errors.Is(nestedRunErr, nestedErr) {
			t.Fatalf("expected nested error %v, got %v", nestedErr, nestedRunErr)
		}

		_, execErr := tx.execContext(ctx, `INSERT INTO tx_helper_users (email) VALUES (?)`, "outer-success@example.com")
		return execErr
	})
	if err != nil {
		t.Fatalf("outer RunInTx returned error: %v", err)
	}

	if got := countTxHelperRows(t, ctx, db); got != 1 {
		t.Fatalf("expected only outer insert to persist, got %d rows", got)
	}
}

func TestTxRunInTxReturnsErrorWhenSavepointsUnsupported(t *testing.T) {
	t.Parallel()

	tx := &Tx{dialect: &noSavepointDialect{}}
	err := tx.RunInTx(context.Background(), func(_ *Tx) error {
		return nil
	})
	if !errors.Is(err, ErrNestedTxNotSupported) {
		t.Fatalf("expected ErrNestedTxNotSupported, got %v", err)
	}
}

func TestTxRunInTxNestedHandleCannotCommitOrRollback(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTxHelperDB(t)
	createTxHelperSchema(t, ctx, db)

	err := db.RunInTx(ctx, func(tx *Tx) error {
		return tx.RunInTx(ctx, func(nested *Tx) error {
			if commitErr := nested.Commit(); !errors.Is(commitErr, ErrNestedTxControlNotAllowed) {
				t.Fatalf("expected ErrNestedTxControlNotAllowed from Commit, got %v", commitErr)
			}
			if rollbackErr := nested.Rollback(); !errors.Is(rollbackErr, ErrNestedTxControlNotAllowed) {
				t.Fatalf("expected ErrNestedTxControlNotAllowed from Rollback, got %v", rollbackErr)
			}
			_, execErr := nested.execContext(ctx, `INSERT INTO tx_helper_users (email) VALUES (?)`, "nested-safe@example.com")
			return execErr
		})
	})
	if err != nil {
		t.Fatalf("RunInTx returned error: %v", err)
	}

	if got := countTxHelperRows(t, ctx, db); got != 1 {
		t.Fatalf("expected 1 row after nested callback, got %d", got)
	}
}

func TestNextSavepointNamePanicsWhenSequenceMissing(t *testing.T) {
	t.Parallel()

	tx := &Tx{}
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic when savepoint sequence is missing")
		}
	}()

	_ = tx.nextSavepointName()
}
