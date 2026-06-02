// Package rain provides the main entry point and typed SQL builders for Rain ORM.
package rain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/hyperlocalise/rain-orm/pkg/dialect"
	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

// ErrNoConnection is returned when execution is requested without a live database handle.
var ErrNoConnection = errors.New("rain: no database connection configured")

// ErrNestedTxNotSupported is returned when nested transactions are requested on a dialect without savepoint support.
var ErrNestedTxNotSupported = errors.New("rain: nested transactions are not supported by this dialect")

// ErrNestedTxControlNotAllowed is returned when nested callbacks attempt to commit or roll back the outer transaction directly.
var ErrNestedTxControlNotAllowed = errors.New("rain: nested RunInTx callbacks cannot call Commit or Rollback directly")

// ReplicaSelector chooses which read replica should serve a SELECT query.
type ReplicaSelector func(replicas []*DB) *DB

type dbSharedState struct {
	queryCache QueryCache
}

type replicaRoute struct {
	primary  *DB
	replicas []*DB
	selector ReplicaSelector
	all      []*DB

	closeOnce sync.Once
	closeErr  error
}

// DB represents a database connection pool.
type DB struct {
	db                *sql.DB
	dialect           dialect.Dialect
	shared            *dbSharedState
	replicaRoute      *replicaRoute
	forcePrimaryReads bool
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
		shared:  &dbSharedState{},
	}, nil
}

// MustOpenDialect is a helper that panics on error, intended for use in schema definitions.
func MustOpenDialect(driver string) *DB {
	db, err := OpenDialect(driver)
	if err != nil {
		panic(err)
	}
	return db
}

// OpenDialect creates a dialect-only handle that can compile SQL without a live database connection.
func OpenDialect(driver string) (*DB, error) {
	d, err := dialect.GetDialect(driver)
	if err != nil {
		return nil, err
	}

	return &DB{
		dialect: d,
		shared:  &dbSharedState{},
	}, nil
}

// WithReplicas returns a DB handle that routes SELECT queries to read replicas while
// keeping writes, raw SQL, and transactions on the primary handle.
func WithReplicas(primary *DB, replicas []*DB, selector ReplicaSelector) (*DB, error) {
	if primary == nil {
		return nil, errors.New("rain: read replicas require a non-nil primary database")
	}
	if len(replicas) == 0 {
		return nil, errors.New("rain: read replicas require at least one replica database")
	}

	shared := resolveReplicaSharedState(primary, replicas)
	validatedReplicas := make([]*DB, 0, len(replicas))
	seen := make(map[*DB]struct{}, len(replicas)+1)
	underlying := make([]*DB, 0, len(replicas)+1)

	seen[primary] = struct{}{}
	underlying = append(underlying, primary)

	for idx, replica := range replicas {
		if replica == nil {
			return nil, fmt.Errorf("rain: read replica %d is nil", idx+1)
		}
		if replica.Dialect().Name() != primary.Dialect().Name() {
			return nil, fmt.Errorf(
				"rain: read replica %d uses dialect %q, expected %q",
				idx+1,
				replica.Dialect().Name(),
				primary.Dialect().Name(),
			)
		}
		replica.shared = shared
		validatedReplicas = append(validatedReplicas, replica)
		if _, ok := seen[replica]; ok {
			continue
		}
		seen[replica] = struct{}{}
		underlying = append(underlying, replica)
	}

	if selector == nil {
		selector = randomReplicaSelector
	}

	primary.shared = shared
	route := &replicaRoute{
		primary:  primary,
		replicas: validatedReplicas,
		selector: selector,
		all:      underlying,
	}

	return &DB{
		db:           primary.db,
		dialect:      primary.dialect,
		shared:       shared,
		replicaRoute: route,
	}, nil
}

// Close closes the database connection.
func (db *DB) Close() error {
	if db.replicaRoute != nil {
		return db.replicaRoute.close()
	}
	if db.db == nil {
		return nil
	}

	return db.db.Close()
}

// Dialect returns the configured SQL dialect.
func (db *DB) Dialect() dialect.Dialect {
	return db.dialect
}

// Primary returns a DB view that forces reads to use the primary handle.
func (db *DB) Primary() *DB {
	if db == nil || db.replicaRoute == nil {
		return db
	}

	return &DB{
		db:                db.replicaRoute.primary.db,
		dialect:           db.replicaRoute.primary.dialect,
		shared:            db.shared,
		replicaRoute:      db.replicaRoute,
		forcePrimaryReads: true,
	}
}

// Select starts a typed SELECT query builder.
func (db *DB) Select() *SelectQuery {
	return &SelectQuery{runner: db.selectRunner(), dialect: db.dialect, cache: db.queryCache()}
}

// WithQueryCache sets the shared SELECT query cache backend on DB.
func (db *DB) WithQueryCache(cache QueryCache) *DB {
	db.ensureSharedState().queryCache = cache
	return db
}

// InvalidateQueryCache removes cached query entries associated with any provided tag.
func (db *DB) InvalidateQueryCache(ctx context.Context, tags ...string) error {
	if db.queryCache() == nil {
		return nil
	}
	return db.queryCache().InvalidateTags(ctx, tags...)
}

// Insert starts a typed INSERT query builder.
func (db *DB) Insert() *InsertQuery {
	return &InsertQuery{runner: db.primaryRunner(), dialect: db.dialect}
}

// Update starts a typed UPDATE query builder.
func (db *DB) Update() *UpdateQuery {
	return &UpdateQuery{runner: db.primaryRunner(), dialect: db.dialect}
}

// Delete starts a typed DELETE query builder.
func (db *DB) Delete() *DeleteQuery {
	return &DeleteQuery{runner: db.primaryRunner(), dialect: db.dialect}
}

// Excluded returns an expression that references the conflicting row's value during an UPSERT.
// Used in .OnConflict().DoUpdateSet().
func Excluded(column schema.ColumnReference) schema.Expression {
	return excludedColumn{column: column}
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

func (db *DB) prepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	if db.db == nil {
		return nil, ErrNoConnection
	}

	return db.db.PrepareContext(ctx, query)
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
	primary := db.primaryHandle()
	if primary.db == nil {
		return nil, ErrNoConnection
	}

	tx, err := primary.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}

	return &Tx{tx: tx, dialect: db.dialect, savepointSeq: new(int64), canControlTx: true, queryCache: db.queryCache()}, nil
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
	canControlTx bool
	queryCache   QueryCache
}

// Commit commits the transaction.
func (tx *Tx) Commit() error {
	if !tx.canControlTx {
		return ErrNestedTxControlNotAllowed
	}

	if tx.tx == nil {
		return ErrNoConnection
	}

	return tx.tx.Commit()
}

// Rollback rolls the transaction back.
func (tx *Tx) Rollback() error {
	if !tx.canControlTx {
		return ErrNestedTxControlNotAllowed
	}

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

	nestedTx := &Tx{
		tx:           tx.tx,
		dialect:      tx.dialect,
		savepointSeq: tx.savepointSeq,
		canControlTx: false,
		queryCache:   tx.queryCache,
	}

	if err := fn(nestedTx); err != nil {
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
	return &SelectQuery{runner: tx, dialect: tx.dialect, cache: tx.queryCache}
}

// InvalidateQueryCache removes cached query entries associated with any provided tag.
func (tx *Tx) InvalidateQueryCache(ctx context.Context, tags ...string) error {
	if tx.queryCache == nil {
		return nil
	}
	return tx.queryCache.InvalidateTags(ctx, tags...)
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

func (tx *Tx) prepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	if tx.tx == nil {
		return nil, ErrNoConnection
	}

	return tx.tx.PrepareContext(ctx, query)
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
	if tx.savepointSeq == nil {
		panic("rain: savepointSeq is nil — Tx must be created via DB.Begin()")
	}
	n := atomic.AddInt64(tx.savepointSeq, 1)

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

func (db *DB) ensureSharedState() *dbSharedState {
	if db.shared == nil {
		db.shared = &dbSharedState{}
	}
	return db.shared
}

func resolveReplicaSharedState(primary *DB, replicas []*DB) *dbSharedState {
	var shared *dbSharedState
	if primary != nil && primary.shared != nil {
		shared = primary.shared
	}
	if shared == nil {
		for _, replica := range replicas {
			if replica != nil && replica.shared != nil {
				shared = replica.shared
				break
			}
		}
	}
	if shared == nil {
		shared = &dbSharedState{}
	}
	if shared.queryCache != nil {
		return shared
	}
	if primary != nil && primary.queryCache() != nil {
		shared.queryCache = primary.queryCache()
		return shared
	}
	for _, replica := range replicas {
		if replica != nil && replica.queryCache() != nil {
			shared.queryCache = replica.queryCache()
			return shared
		}
	}
	return shared
}

func (db *DB) queryCache() QueryCache {
	if db.shared == nil {
		return nil
	}
	return db.shared.queryCache
}

func (db *DB) primaryHandle() *DB {
	if db == nil || db.replicaRoute == nil {
		return db
	}
	return db.replicaRoute.primary
}

func (db *DB) primaryRunner() queryRunner {
	return db.primaryHandle()
}

func (db *DB) selectRunner() queryRunner {
	if db == nil || db.replicaRoute == nil || db.forcePrimaryReads {
		return db.primaryRunner()
	}
	return db.replicaRoute.pickReplica()
}

func randomReplicaSelector(replicas []*DB) *DB {
	if len(replicas) == 0 {
		return nil
	}
	return replicas[rand.Intn(len(replicas))]
}

func (r *replicaRoute) pickReplica() *DB {
	if r == nil || len(r.replicas) == 0 {
		return nil
	}
	chosen := r.selector(r.replicas)
	for _, replica := range r.replicas {
		if replica == chosen {
			return replica
		}
	}
	return randomReplicaSelector(r.replicas)
}

func (r *replicaRoute) close() error {
	if r == nil {
		return nil
	}
	r.closeOnce.Do(func() {
		var errs []error
		for _, handle := range r.all {
			if handle == nil || handle.db == nil {
				continue
			}
			if err := handle.db.Close(); err != nil {
				errs = append(errs, err)
			}
		}
		r.closeErr = errors.Join(errs...)
	})
	return r.closeErr
}
