// Package rain provides the main entry point and typed SQL builders for Rain ORM.
package rain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/hyperlocalise/rain-orm/pkg/dialect"
)

// ErrNoConnection is returned when execution is requested without a live database handle.
var ErrNoConnection = errors.New("rain: no database connection configured")

// ErrNestedTxNotSupported is returned when nested transactions are requested on a dialect without savepoint support.
var ErrNestedTxNotSupported = errors.New("rain: nested transactions are not supported by this dialect")

// DB represents a database connection pool.
type DB struct {
	db      *sql.DB
	dialect dialect.Dialect
}

// Open creates a database handle for the selected dialect.
func Open(driver, dsn string) (*DB, error) {
	d, err := dialect.GetDialect(driver)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open(driver, dsn)
	if err != nil {
		if d.Name() != driver && strings.Contains(err.Error(), "unknown driver") {
			return nil, fmt.Errorf("rain: open %s database: %w (dialect %q maps to %q, but sql.Open requires the registered database/sql driver name)", driver, err, driver, d.Name())
		}
		return nil, fmt.Errorf("rain: open %s database: %w", driver, err)
	}

	return &DB{
		db:      db,
		dialect: d,
	}, nil
}

// OpenDialect creates a dialect-only handle that can compile SQL without a live database connection.
func OpenDialect(driver string) (*DB, error) {
	d, err := dialect.GetDialect(driver)
	if err != nil {
		return nil, err
	}

	return &DB{
		dialect: d,
	}, nil
}

// Close closes the database connection.
func (db *DB) Close() error {
	if db.db == nil {
		return nil
	}

	return db.db.Close()
}

// Dialect returns the configured SQL dialect.
func (db *DB) Dialect() dialect.Dialect {
	return db.dialect
}

// Select starts a typed SELECT query builder.
func (db *DB) Select() *SelectQuery {
	return &SelectQuery{runner: db, dialect: db.dialect}
}

// Insert starts a typed INSERT query builder.
func (db *DB) Insert() *InsertQuery {
	return &InsertQuery{runner: db, dialect: db.dialect}
}

// Update starts a typed UPDATE query builder.
func (db *DB) Update() *UpdateQuery {
	return &UpdateQuery{runner: db, dialect: db.dialect}
}

// Delete starts a typed DELETE query builder.
func (db *DB) Delete() *DeleteQuery {
	return &DeleteQuery{runner: db, dialect: db.dialect}
}

// Exec executes raw SQL against the database.
func (db *DB) Exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if db.db == nil {
		return nil, ErrNoConnection
	}

	return db.db.ExecContext(ctx, query, args...)
}

// Query executes a SQL query and returns rows.
func (db *DB) Query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if db.db == nil {
		return nil, ErrNoConnection
	}

	return db.db.QueryContext(ctx, query, args...)
}

func (db *DB) execContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return db.Exec(ctx, query, args...)
}

func (db *DB) queryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return db.Query(ctx, query, args...)
}

// QueryRow executes a query that returns a single row.
func (db *DB) QueryRow(ctx context.Context, query string, args ...any) *sql.Row {
	if db.db == nil {
		return nil
	}

	return db.db.QueryRowContext(ctx, query, args...)
}

// Begin starts a new transaction.
func (db *DB) Begin(ctx context.Context) (*Tx, error) {
	if db.db == nil {
		return nil, ErrNoConnection
	}

	tx, err := db.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}

	return &Tx{tx: tx, dialect: db.dialect, savepointSeq: new(int64)}, nil
}

// RunInTx executes fn in a transaction, rolling back on error and committing on success.
func (db *DB) RunInTx(ctx context.Context, fn func(*Tx) error) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}

	return runInRootTx(tx, fn)
}

// Tx represents a database transaction.
type Tx struct {
	tx      *sql.Tx
	dialect dialect.Dialect

	savepointSeq *int64
}

// Commit commits the transaction.
func (tx *Tx) Commit() error {
	if tx.tx == nil {
		return ErrNoConnection
	}

	return tx.tx.Commit()
}

// Rollback rolls the transaction back.
func (tx *Tx) Rollback() error {
	if tx.tx == nil {
		return ErrNoConnection
	}

	return tx.tx.Rollback()
}

// RunInTx executes fn in a nested transaction using a savepoint.
func (tx *Tx) RunInTx(ctx context.Context, fn func(*Tx) error) error {
	if !dialect.HasFeature(tx.dialect.Features(), dialect.FeatureSavepoint) {
		return ErrNestedTxNotSupported
	}

	if tx.tx == nil {
		return ErrNoConnection
	}

	savepoint := tx.nextSavepointName()
	if _, err := tx.execContext(ctx, "SAVEPOINT "+savepoint); err != nil {
		return fmt.Errorf("rain: create savepoint %q: %w", savepoint, err)
	}

	if err := fn(tx); err != nil {
		if rbErr := tx.rollbackSavepoint(ctx, savepoint); rbErr != nil {
			return errors.Join(err, rbErr)
		}
		return err
	}

	if _, err := tx.execContext(ctx, "RELEASE SAVEPOINT "+savepoint); err != nil {
		return fmt.Errorf("rain: release savepoint %q: %w", savepoint, err)
	}

	return nil
}

// Select starts a typed SELECT query builder in the transaction.
func (tx *Tx) Select() *SelectQuery {
	return &SelectQuery{runner: tx, dialect: tx.dialect}
}

// Insert starts a typed INSERT query builder in the transaction.
func (tx *Tx) Insert() *InsertQuery {
	return &InsertQuery{runner: tx, dialect: tx.dialect}
}

// Update starts a typed UPDATE query builder in the transaction.
func (tx *Tx) Update() *UpdateQuery {
	return &UpdateQuery{runner: tx, dialect: tx.dialect}
}

// Delete starts a typed DELETE query builder in the transaction.
func (tx *Tx) Delete() *DeleteQuery {
	return &DeleteQuery{runner: tx, dialect: tx.dialect}
}

func (tx *Tx) execContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if tx.tx == nil {
		return nil, ErrNoConnection
	}

	return tx.tx.ExecContext(ctx, query, args...)
}

func (tx *Tx) queryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if tx.tx == nil {
		return nil, ErrNoConnection
	}

	return tx.tx.QueryContext(ctx, query, args...)
}

func runInRootTx(tx *Tx, fn func(*Tx) error) error {
	if err := fn(tx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			return errors.Join(err, fmt.Errorf("rain: rollback transaction: %w", rbErr))
		}
		return err
	}

	if err := tx.Commit(); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			return errors.Join(fmt.Errorf("rain: commit transaction: %w", err), fmt.Errorf("rain: rollback after commit failure: %w", rbErr))
		}
		return fmt.Errorf("rain: commit transaction: %w", err)
	}

	return nil
}

func (tx *Tx) nextSavepointName() string {
	n := int64(1)
	if tx.savepointSeq != nil {
		n = atomic.AddInt64(tx.savepointSeq, 1)
	}

	return fmt.Sprintf("rain_sp_%d", n)
}

func (tx *Tx) rollbackSavepoint(ctx context.Context, savepoint string) error {
	if _, err := tx.execContext(ctx, "ROLLBACK TO SAVEPOINT "+savepoint); err != nil {
		return fmt.Errorf("rain: rollback to savepoint %q: %w", savepoint, err)
	}

	if _, err := tx.execContext(ctx, "RELEASE SAVEPOINT "+savepoint); err != nil {
		return fmt.Errorf("rain: release savepoint %q after rollback: %w", savepoint, err)
	}

	return nil
}
