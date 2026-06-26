package rain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/hyperlocalise/rain-orm/pkg/dialect"
	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

// SelectQuery builds typed SELECT statements.
type SelectQuery struct {
	schema.ExpressionMarker
	runner          queryRunner
	dialect         dialect.Dialect
	cache           QueryCache
	table           *schema.TableDef
	tableSubquery   *SelectQuery
	tableAlias      string
	cols            []schema.Expression
	where           []schema.Predicate
	joins           []joinClause
	order           []schema.OrderExpr
	groupBy         []schema.Expression
	having          []schema.Predicate
	ctes            []cteDefinition
	firstOperand    *SelectQuery
	setOps          []setOperation
	distinct        bool
	distinctOn      []schema.Expression
	limit           int
	hasLimit        bool
	offset          int
	hasOffset       bool
	relationNames   []string
	relationConfigs map[string]RelationConfig
	cacheOptions    *queryCacheOptions
	locking         *selectLocking

	// OPTIMIZATION: Minimal internal buffers to avoid heap allocations for
	// common query shapes while keeping the struct size small.
	colsBuf    [4]schema.Expression
	whereBuf   [2]schema.Predicate
	orderBuf   [1]schema.OrderExpr
	joinsBuf   [1]joinClause
	groupByBuf [1]schema.Expression
	havingBuf  [1]schema.Predicate
}

var selectQueryPool = sync.Pool{
	New: func() any {
		return &SelectQuery{}
	},
}

func newSelectQuery(runner queryRunner, d dialect.Dialect, cache QueryCache) *SelectQuery {
	q := selectQueryPool.Get().(*SelectQuery)
	*q = SelectQuery{
		runner:  runner,
		dialect: d,
		cache:   cache,
	}
	q.cols = q.colsBuf[:0]
	q.where = q.whereBuf[:0]
	q.order = q.orderBuf[:0]
	q.joins = q.joinsBuf[:0]
	q.groupBy = q.groupByBuf[:0]
	q.having = q.havingBuf[:0]
	return q
}

func releaseSelectQuery(q *SelectQuery) {
	if q == nil {
		return
	}
	// Clear any references to prevent memory leaks while the query builder
	// sits in the pool.
	*q = SelectQuery{}
	selectQueryPool.Put(q)
}

// Table sets the table source for the query.
func (q *SelectQuery) Table(table schema.TableReference) *SelectQuery {
	q.table = table.TableDef()
	q.tableSubquery = nil
	q.tableAlias = ""
	return q
}

// From is a semantic alias for Table.
func (q *SelectQuery) From(table schema.TableReference) *SelectQuery {
	return q.Table(table)
}

// TableSubquery sets a subquery source for the query's FROM clause.
func (q *SelectQuery) TableSubquery(query *SelectQuery, alias string) *SelectQuery {
	q.tableSubquery = query
	q.tableAlias = alias
	q.table = nil
	return q
}

// Column sets the selected expressions.
func (q *SelectQuery) Column(cols ...schema.Expression) *SelectQuery {
	q.cols = append(q.cols, cols...)
	return q
}

// Where appends a WHERE predicate joined with AND.
func (q *SelectQuery) Where(predicate schema.Predicate) *SelectQuery {
	q.where = append(q.where, predicate)
	return q
}

// Join appends an INNER JOIN clause.
func (q *SelectQuery) Join(table schema.TableReference, on schema.Predicate) *SelectQuery {
	q.joins = append(q.joins, joinClause{kind: "INNER JOIN", table: tableSource{table: table.TableDef()}, on: on})
	return q
}

// LeftJoin appends a LEFT JOIN clause.
func (q *SelectQuery) LeftJoin(table schema.TableReference, on schema.Predicate) *SelectQuery {
	q.joins = append(q.joins, joinClause{kind: "LEFT JOIN", table: tableSource{table: table.TableDef()}, on: on})
	return q
}

// RightJoin appends a RIGHT JOIN clause.
func (q *SelectQuery) RightJoin(table schema.TableReference, on schema.Predicate) *SelectQuery {
	q.joins = append(q.joins, joinClause{kind: "RIGHT JOIN", table: tableSource{table: table.TableDef()}, on: on})
	return q
}

// FullJoin appends a FULL JOIN clause.
func (q *SelectQuery) FullJoin(table schema.TableReference, on schema.Predicate) *SelectQuery {
	q.joins = append(q.joins, joinClause{kind: "FULL JOIN", table: tableSource{table: table.TableDef()}, on: on})
	return q
}

// CrossJoin appends a CROSS JOIN clause.
func (q *SelectQuery) CrossJoin(table schema.TableReference) *SelectQuery {
	q.joins = append(q.joins, joinClause{kind: "CROSS JOIN", table: tableSource{table: table.TableDef()}})
	return q
}

// JoinSubquery appends an INNER JOIN against a subquery source.
func (q *SelectQuery) JoinSubquery(query *SelectQuery, alias string, on schema.Predicate) *SelectQuery {
	q.joins = append(q.joins, joinClause{kind: "INNER JOIN", table: tableSource{subquery: query, alias: alias}, on: on})
	return q
}

// LeftJoinSubquery appends a LEFT JOIN against a subquery source.
func (q *SelectQuery) LeftJoinSubquery(query *SelectQuery, alias string, on schema.Predicate) *SelectQuery {
	q.joins = append(q.joins, joinClause{kind: "LEFT JOIN", table: tableSource{subquery: query, alias: alias}, on: on})
	return q
}

// RightJoinSubquery appends a RIGHT JOIN against a subquery source.
func (q *SelectQuery) RightJoinSubquery(query *SelectQuery, alias string, on schema.Predicate) *SelectQuery {
	q.joins = append(q.joins, joinClause{kind: "RIGHT JOIN", table: tableSource{subquery: query, alias: alias}, on: on})
	return q
}

// FullJoinSubquery appends a FULL JOIN against a subquery source.
func (q *SelectQuery) FullJoinSubquery(query *SelectQuery, alias string, on schema.Predicate) *SelectQuery {
	q.joins = append(q.joins, joinClause{kind: "FULL JOIN", table: tableSource{subquery: query, alias: alias}, on: on})
	return q
}

// CrossJoinSubquery appends a CROSS JOIN against a subquery source.
func (q *SelectQuery) CrossJoinSubquery(query *SelectQuery, alias string) *SelectQuery {
	q.joins = append(q.joins, joinClause{kind: "CROSS JOIN", table: tableSource{subquery: query, alias: alias}})
	return q
}

// Distinct marks the SELECT query as DISTINCT.
func (q *SelectQuery) Distinct() *SelectQuery {
	q.distinct = true
	return q
}

// DistinctOn marks the SELECT query as DISTINCT ON the provided expressions.
// Supported by PostgreSQL.
func (q *SelectQuery) DistinctOn(exprs ...schema.Expression) *SelectQuery {
	q.distinctOn = append(q.distinctOn, exprs...)
	return q
}

// GroupBy appends GROUP BY expressions.
func (q *SelectQuery) GroupBy(exprs ...schema.Expression) *SelectQuery {
	q.groupBy = append(q.groupBy, exprs...)
	return q
}

// Having appends a HAVING predicate joined with AND.
func (q *SelectQuery) Having(predicate schema.Predicate) *SelectQuery {
	q.having = append(q.having, predicate)
	return q
}

// With appends a common table expression definition.
func (q *SelectQuery) With(name string, query *SelectQuery) *SelectQuery {
	q.ctes = append(q.ctes, cteDefinition{name: name, query: query})
	return q
}

// OrderBy appends ORDER BY expressions.
func (q *SelectQuery) OrderBy(order ...schema.OrderExpr) *SelectQuery {
	q.order = append(q.order, order...)
	return q
}

// Limit sets the LIMIT clause.
func (q *SelectQuery) Limit(limit int) *SelectQuery {
	q.limit = limit
	q.hasLimit = true
	return q
}

// Offset sets the OFFSET clause.
func (q *SelectQuery) Offset(offset int) *SelectQuery {
	q.offset = offset
	q.hasOffset = true
	return q
}

// WithRelations requests one or more named relations to be loaded after scanning base rows.
func (q *SelectQuery) WithRelations(names ...string) *SelectQuery {
	q.relationNames = append(q.relationNames, names...)
	return q
}

// RelationConfig provides optional filters and ordering for a relation.
type RelationConfig struct {
	Where   schema.Predicate
	OrderBy []schema.OrderExpr
	Columns []schema.Expression
}

// Relation configures filters and ordering for a named relation.
// It also adds the relation to the set of relations to be loaded.
func (q *SelectQuery) Relation(name string, config RelationConfig) *SelectQuery {
	if q.relationConfigs == nil {
		q.relationConfigs = make(map[string]RelationConfig)
	}
	q.relationConfigs[name] = config

	found := false
	for _, n := range q.relationNames {
		if n == name {
			found = true
			break
		}
	}
	if !found {
		q.relationNames = append(q.relationNames, name)
	}

	return q
}

// For applies a locking clause to the SELECT query.
func (q *SelectQuery) For(mode LockMode, config ...LockConfig) *SelectQuery {
	locking := &selectLocking{mode: mode}
	if len(config) > 0 {
		locking.of = config[0].Of
		locking.noWait = config[0].NoWait
		locking.skipLocked = config[0].SkipLocked
	}
	q.locking = locking
	return q
}

// ForUpdate applies a FOR UPDATE locking clause.
func (q *SelectQuery) ForUpdate(config ...LockConfig) *SelectQuery {
	return q.For(LockUpdate, config...)
}

// ForShare applies a FOR SHARE locking clause.
func (q *SelectQuery) ForShare(config ...LockConfig) *SelectQuery {
	return q.For(LockShare, config...)
}

// LockMode identifies a SELECT locking strength (e.g. FOR UPDATE).
type LockMode string

// Supported SELECT locking modes.
const (
	LockUpdate      LockMode = "UPDATE"
	LockNoKeyUpdate LockMode = "NO KEY UPDATE"
	LockShare       LockMode = "SHARE"
	LockKeyShare    LockMode = "KEY SHARE"
)

// LockConfig provides optional modifiers for SELECT locking.
type LockConfig struct {
	Of         []schema.TableReference
	NoWait     bool
	SkipLocked bool
}

type selectLocking struct {
	mode       LockMode
	of         []schema.TableReference
	noWait     bool
	skipLocked bool
}

type setOperator string

const (
	setOpUnion        setOperator = "UNION"
	setOpUnionAll     setOperator = "UNION ALL"
	setOpIntersect    setOperator = "INTERSECT"
	setOpIntersectAll setOperator = "INTERSECT ALL"
	setOpExcept       setOperator = "EXCEPT"
	setOpExceptAll    setOperator = "EXCEPT ALL"
)

type setOperation struct {
	operator setOperator
	query    *SelectQuery
}

// Union combines results with another SELECT query using UNION.
func (q *SelectQuery) Union(other *SelectQuery) *SelectQuery {
	return q.wrapSetOp(setOpUnion, other)
}

// UnionAll combines results with another SELECT query using UNION ALL.
func (q *SelectQuery) UnionAll(other *SelectQuery) *SelectQuery {
	return q.wrapSetOp(setOpUnionAll, other)
}

// Intersect combines results with another SELECT query using INTERSECT.
func (q *SelectQuery) Intersect(other *SelectQuery) *SelectQuery {
	return q.wrapSetOp(setOpIntersect, other)
}

// IntersectAll combines results with another SELECT query using INTERSECT ALL.
func (q *SelectQuery) IntersectAll(other *SelectQuery) *SelectQuery {
	return q.wrapSetOp(setOpIntersectAll, other)
}

// Except combines results with another SELECT query using EXCEPT.
func (q *SelectQuery) Except(other *SelectQuery) *SelectQuery {
	return q.wrapSetOp(setOpExcept, other)
}

// ExceptAll combines results with another SELECT query using EXCEPT ALL.
func (q *SelectQuery) ExceptAll(other *SelectQuery) *SelectQuery {
	return q.wrapSetOp(setOpExceptAll, other)
}

// CloneForTable clones the SELECT query while binding it to a specific table definition.
// Satisfies schema.TableCloner.
func (q *SelectQuery) CloneForTable(table *schema.TableDef) any {
	return q.clone()
}

func (q *SelectQuery) clone() *SelectQuery {
	newQ := *q
	if q.tableSubquery != nil {
		newQ.tableSubquery = q.tableSubquery.clone()
	}

	// OPTIMIZATION: Utilize internal buffers for cloned slices if they fit,
	// avoiding redundant heap allocations for common query shapes.
	if len(q.cols) <= len(newQ.colsBuf) {
		newQ.cols = newQ.colsBuf[:len(q.cols)]
		copy(newQ.cols, q.cols)
	} else {
		newQ.cols = append([]schema.Expression(nil), q.cols...)
	}

	if len(q.where) <= len(newQ.whereBuf) {
		newQ.where = newQ.whereBuf[:len(q.where)]
		copy(newQ.where, q.where)
	} else {
		newQ.where = append([]schema.Predicate(nil), q.where...)
	}

	if len(q.order) <= len(newQ.orderBuf) {
		newQ.order = newQ.orderBuf[:len(q.order)]
		copy(newQ.order, q.order)
	} else {
		newQ.order = append([]schema.OrderExpr(nil), q.order...)
	}

	if len(q.joins) <= len(newQ.joinsBuf) {
		newQ.joins = newQ.joinsBuf[:len(q.joins)]
		copy(newQ.joins, q.joins)
	} else {
		newQ.joins = append([]joinClause(nil), q.joins...)
	}

	if len(q.groupBy) <= len(newQ.groupByBuf) {
		newQ.groupBy = newQ.groupByBuf[:len(q.groupBy)]
		copy(newQ.groupBy, q.groupBy)
	} else {
		newQ.groupBy = append([]schema.Expression(nil), q.groupBy...)
	}

	if len(q.having) <= len(newQ.havingBuf) {
		newQ.having = newQ.havingBuf[:len(q.having)]
		copy(newQ.having, q.having)
	} else {
		newQ.having = append([]schema.Predicate(nil), q.having...)
	}
	newQ.ctes = append([]cteDefinition(nil), q.ctes...)
	newQ.setOps = append([]setOperation(nil), q.setOps...)
	newQ.distinctOn = append([]schema.Expression(nil), q.distinctOn...)
	newQ.relationNames = append([]string(nil), q.relationNames...)

	if q.relationConfigs != nil {
		newQ.relationConfigs = make(map[string]RelationConfig, len(q.relationConfigs))
		for k, v := range q.relationConfigs {
			newQ.relationConfigs[k] = v
		}
	}
	if q.locking != nil {
		copyLocking := *q.locking
		copyLocking.of = append([]schema.TableReference(nil), q.locking.of...)
		newQ.locking = &copyLocking
	}
	return &newQ
}

func (q *SelectQuery) withSQLiteInsertSelectConflictWhere() *SelectQuery {
	rewritten, _ := q.withSQLiteInsertSelectConflictWhereChanged()
	return rewritten
}

func (q *SelectQuery) withSQLiteInsertSelectConflictWhereChanged() (*SelectQuery, bool) {
	if q == nil {
		return q, false
	}
	if q.firstOperand != nil {
		var changed bool
		newQ := q.clone()
		if firstOperand, ok := q.firstOperand.withSQLiteInsertSelectConflictWhereChanged(); ok {
			newQ.firstOperand = firstOperand
			changed = true
		}
		for idx, setOp := range q.setOps {
			if setOp.query == nil {
				continue
			}
			query, ok := setOp.query.withSQLiteInsertSelectConflictWhereChanged()
			if !ok {
				continue
			}
			newQ.setOps[idx].query = query
			changed = true
		}
		if !changed {
			return q, false
		}
		return newQ, true
	}
	if len(q.where) > 0 {
		return q, false
	}

	newQ := q.clone()
	newQ.where = append(newQ.where, schema.Raw("1 = 1"))
	return newQ, true
}

func (q *SelectQuery) isBareCompound() bool {
	return q.firstOperand != nil &&
		len(q.order) == 0 && !q.hasLimit && !q.hasOffset &&
		!q.distinct && len(q.distinctOn) == 0 && len(q.cols) == 0 &&
		q.table == nil && q.tableSubquery == nil &&
		len(q.where) == 0 && len(q.joins) == 0 &&
		len(q.groupBy) == 0 && len(q.having) == 0 &&
		len(q.relationNames) == 0 && len(q.ctes) == 0 &&
		q.locking == nil
}

func (q *SelectQuery) wrapSetOp(operator setOperator, other *SelectQuery) *SelectQuery {
	// If the current query is already a compound query and has no root-level modifiers,
	// flatten the new operation into the existing one to match Drizzle's behavior.
	if q.isBareCompound() {
		newQ := q.clone()
		newQ.setOps = append(newQ.setOps, setOperation{operator: operator, query: other})
		return newQ
	}

	return &SelectQuery{
		runner:       q.runner,
		dialect:      q.dialect,
		cache:        q.cache,
		cacheOptions: q.cacheOptions,
		firstOperand: q,
		setOps:       []setOperation{{operator: operator, query: other}},
	}
}

// Cache enables opt-in query caching for this SELECT with TTL and optional metadata.
// Queries that use WithRelations do not read from or write to the query cache.
func (q *SelectQuery) Cache(options QueryCacheOptions) *SelectQuery {
	q.cacheOptions = normalizeQueryCacheOptions(options)
	return q
}

// ToSQL compiles the query into SQL and args.
func (q *SelectQuery) ToSQL() (string, []any, error) {
	compiled, err := q.compile()
	if err != nil {
		return "", nil, err
	}
	args, err := compiled.literalArgs()
	if err != nil {
		return "", nil, err
	}
	return compiled.sql, args, nil
}

// FirstToSQL compiles the query with an implicit LIMIT 1 into SQL and args.
func (q *SelectQuery) FirstToSQL() (string, []any, error) {
	return q.clone().Limit(1).ToSQL()
}

// First executes the SELECT query with an implicit LIMIT 1 and scans the result into dest.
// Returns sql.ErrNoRows if the result set is empty.
// Destination must be a pointer to a struct.
func (q *SelectQuery) First(ctx context.Context, dest any) error {
	v := reflect.ValueOf(dest)
	if v.Kind() != reflect.Pointer || v.IsNil() || v.Elem().Kind() != reflect.Struct {
		return errors.New("rain: First destination must be a non-nil pointer to a struct")
	}

	// OPTIMIZATION: Temporarily override the limit to avoid a full query clone.
	// This reduces allocations for point lookups using the First() helper.
	origLimit, origHasLimit := q.limit, q.hasLimit
	q.limit = 1
	q.hasLimit = true
	defer func() {
		q.limit = origLimit
		q.hasLimit = origHasLimit
	}()

	return q.Scan(ctx, dest)
}

func (q *SelectQuery) writeSQL(ctx *compileContext) error {
	if err := writeCTEs(ctx, q.ctes, "select"); err != nil {
		return err
	}

	if q.firstOperand != nil {
		if err := q.firstOperand.writeCompoundOperandSQL(ctx); err != nil {
			return err
		}
		for _, setOp := range q.setOps {
			ctx.writeByte(' ')
			ctx.writeString(string(setOp.operator))
			ctx.writeByte(' ')
			if setOp.query == nil {
				return fmt.Errorf("rain: %s requires a query", setOp.operator)
			}
			if err := setOp.query.writeCompoundOperandSQL(ctx); err != nil {
				return err
			}
		}
		if err := writeOrderLimit(ctx, q.order, q.limit, q.hasLimit, q.offset, q.hasOffset, dialect.FeatureUnlimited, dialect.FeatureUnlimited); err != nil {
			return err
		}
		return q.writeLocking(ctx)
	}

	if q.table == nil && q.tableSubquery == nil {
		if q.tableAlias != "" {
			return errors.New("rain: subquery table source requires a non-nil query")
		}
		return errors.New("rain: select query requires a table")
	}

	ctx.writeString("SELECT ")
	if q.distinct {
		ctx.writeString("DISTINCT ")
	} else if len(q.distinctOn) > 0 {
		if !dialect.HasFeature(ctx.dialect.Features(), dialect.FeatureSelectDistinctOn) {
			return fmt.Errorf("rain: SELECT DISTINCT ON is not supported by %s dialect", ctx.dialect.Name())
		}
		ctx.writeString("DISTINCT ON (")
		for idx, expr := range q.distinctOn {
			if idx > 0 {
				ctx.writeString(", ")
			}
			if err := ctx.writeExpression(expr); err != nil {
				return err
			}
		}
		ctx.writeString(") ")
	}
	if len(q.cols) == 0 {
		ctx.writeString("*")
	} else {
		for idx, column := range q.cols {
			if idx > 0 {
				ctx.writeString(", ")
			}
			if err := ctx.writeSelectExpression(column); err != nil {
				return err
			}
		}
	}

	ctx.writeString(" FROM ")
	if err := q.writeTableSourceSQL(ctx); err != nil {
		return err
	}

	if err := q.writeJoins(ctx); err != nil {
		return err
	}

	if len(q.where) > 0 {
		ctx.writeString(" WHERE ")
		ctx.ensureArgsCapacity(len(q.where))
		if err := ctx.writeJoinedPredicates(q.where, false); err != nil {
			return err
		}
	}

	if len(q.groupBy) > 0 {
		ctx.writeString(" GROUP BY ")
		for idx, expr := range q.groupBy {
			if idx > 0 {
				ctx.writeString(", ")
			}
			if err := ctx.writeExpression(expr); err != nil {
				return err
			}
		}
	}

	if len(q.having) > 0 {
		ctx.writeString(" HAVING ")
		ctx.ensureArgsCapacity(len(q.having))
		if err := ctx.writeJoinedPredicates(q.having, false); err != nil {
			return err
		}
	}

	if err := writeOrderLimit(ctx, q.order, q.limit, q.hasLimit, q.offset, q.hasOffset, dialect.FeatureUnlimited, dialect.FeatureUnlimited); err != nil {
		return err
	}

	return q.writeLocking(ctx)
}

func (q *SelectQuery) writeLocking(ctx *compileContext) error {
	if q.locking == nil {
		return nil
	}

	if !dialect.HasFeature(ctx.dialect.Features(), dialect.FeatureSelectLocking) {
		return fmt.Errorf("rain: select locking is not supported by %s dialect", ctx.dialect.Name())
	}

	if q.locking.noWait && q.locking.skipLocked {
		return errors.New("rain: select locking cannot combine NOWAIT and SKIP LOCKED")
	}

	mode := q.locking.mode
	if ctx.dialect.Name() == "mysql" {
		if mode != LockUpdate && mode != LockShare {
			return fmt.Errorf("rain: MySQL select locking only supports UPDATE and SHARE modes, got %s", mode)
		}
	}

	ctx.writeString(" FOR ")
	ctx.writeString(string(mode))

	if len(q.locking.of) > 0 {
		ctx.writeString(" OF ")
		for idx, table := range q.locking.of {
			if idx > 0 {
				ctx.writeString(", ")
			}
			def := table.TableDef()
			name := def.Name
			if def.Alias != "" {
				name = def.Alias
			}
			ctx.writeQuotedIdentifier(name)
		}
	}

	if q.locking.noWait {
		ctx.writeString(" NOWAIT")
	} else if q.locking.skipLocked {
		ctx.writeString(" SKIP LOCKED")
	}

	return nil
}

func (q *SelectQuery) writeCompoundOperandSQL(ctx *compileContext) error {
	if len(q.ctes) > 0 {
		return fmt.Errorf("rain: compound query operand cannot contain CTEs")
	}
	// Use parentheses if the operand has its own ORDER BY, LIMIT, locking, or is itself a compound query.
	// Flattening is handled during builder chaining in wrapSetOp.
	useParens := len(q.order) > 0 || q.hasLimit || q.hasOffset || q.locking != nil || q.firstOperand != nil
	if useParens {
		ctx.writeByte('(')
	}
	// CTEs must only appear at the very beginning of the entire compound query.
	// When rendering an operand, we signal to skip CTEs to prevent invalid SQL.
	prevSkip := ctx.skipCTEs
	ctx.skipCTEs = true
	defer func() { ctx.skipCTEs = prevSkip }()
	err := q.writeSQL(ctx)
	if err != nil {
		return err
	}
	if useParens {
		ctx.writeByte(')')
	}
	return nil
}

func (q *SelectQuery) writeJoins(ctx *compileContext) error {
	for _, join := range q.joins {
		ctx.writeByte(' ')
		ctx.writeString(join.kind)
		ctx.writeByte(' ')
		if err := join.table.writeSQL(ctx); err != nil {
			return err
		}
		if join.kind != "CROSS JOIN" {
			if join.on == nil {
				return fmt.Errorf("rain: %s requires an ON clause", join.kind)
			}
			ctx.writeString(" ON ")
			if err := ctx.writePredicate(join.on); err != nil {
				return err
			}
		} else if join.on != nil {
			return errors.New("rain: CROSS JOIN does not support an ON clause")
		}
	}
	return nil
}

// Scan executes the SELECT query and scans results into dest.
func (q *SelectQuery) Scan(ctx context.Context, dest any) error {
	if q.runner == nil {
		return ErrNoConnection
	}

	compiled, err := q.compile()
	if err != nil {
		return err
	}
	args, err := compiled.literalArgs()
	if err != nil {
		return err
	}
	query := compiled.sql

	cacheKey, cacheOptions, err := q.resolveCacheKey(query, args)
	if err != nil {
		return err
	}
	table := q.scanValidationTable()
	if cacheOptions != nil && !cacheOptions.bypass && len(q.relationNames) == 0 && q.locking == nil {
		cached, ok, cacheErr := q.cache.Get(ctx, cacheKey)
		if cacheErr != nil {
			return cacheErr
		}
		if ok {
			if result, err := decodeCachedSelectRows(cached); err == nil {
				return scanCachedRowsAgainstTable(result, dest, table)
			}
		}
	}
	rows, err := q.runner.queryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer closeRows(rows, &err)

	if len(q.relationNames) == 0 {
		if cacheKey != "" && cacheOptions != nil && !cacheOptions.bypass && q.locking == nil {
			result, readErr := readCachedSelectRows(rows)
			if readErr != nil {
				return readErr
			}
			err = scanCachedRowsAgainstTable(result, dest, table)
			if err != nil {
				return err
			}
			return q.writeCachedSelectResult(ctx, cacheKey, cacheOptions, result)
		}
		err = scanRowsAgainstTableDirect(rows, dest, table)
	} else {
		err = q.scanRowsWithRelations(ctx, rows, dest)
	}
	if err != nil {
		return err
	}
	return nil
}

// Count executes SELECT COUNT(*).
func (q *SelectQuery) Count(ctx context.Context) (int64, error) {
	if q.locking != nil {
		return 0, errors.New("rain: aggregate helpers do not support FOR locking clauses")
	}
	if q.runner == nil {
		return 0, ErrNoConnection
	}

	query, args, err := q.toAggregateSQL("COUNT(*)")
	if err != nil {
		return 0, err
	}

	cacheKey, cacheOptions, err := q.resolveCacheKey(query, args)
	if err != nil {
		return 0, err
	}
	if cacheOptions != nil && !cacheOptions.bypass {
		cached, ok, cacheErr := q.cache.Get(ctx, cacheKey)
		if cacheErr != nil {
			return 0, cacheErr
		}
		if ok {
			if count, err := decodeCachedInt64(cached); err == nil {
				return count, nil
			}
		}
	}
	rows, err := q.runner.queryContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	defer closeRows(rows, &err)

	var count int64
	if !rows.Next() {
		err = sql.ErrNoRows
		return 0, err
	}
	if err := rows.Scan(&count); err != nil {
		return 0, err
	}

	err = rows.Err()
	if err != nil {
		return 0, err
	}
	err = q.writeCachedInt64(ctx, cacheKey, cacheOptions, count)
	return count, err
}

// Exists executes a SELECT EXISTS query.
func (q *SelectQuery) Exists(ctx context.Context) (bool, error) {
	if q.locking != nil {
		return false, errors.New("rain: exists checks do not support FOR locking clauses")
	}
	if q.runner == nil {
		return false, ErrNoConnection
	}

	compiled, err := q.compile()
	if err != nil {
		return false, err
	}
	existsQuery, err := wrapExistsCompiled(compiled)
	if err != nil {
		return false, err
	}
	args, err := existsQuery.literalArgs()
	if err != nil {
		return false, err
	}

	cacheKey, cacheOptions, err := q.resolveCacheKey(existsQuery.sql, args)
	if err != nil {
		return false, err
	}
	if cacheOptions != nil && !cacheOptions.bypass {
		cached, ok, cacheErr := q.cache.Get(ctx, cacheKey)
		if cacheErr != nil {
			return false, cacheErr
		}
		if ok {
			if exists, err := decodeCachedBool(cached); err == nil {
				return exists, nil
			}
		}
	}
	rows, err := q.runner.queryContext(ctx, existsQuery.sql, args...)
	if err != nil {
		return false, err
	}
	defer closeRows(rows, &err)

	var exists bool
	if !rows.Next() {
		err = sql.ErrNoRows
		return false, err
	}
	if err := rows.Scan(&exists); err != nil {
		return false, err
	}

	err = rows.Err()
	if err != nil {
		return false, err
	}
	err = q.writeCachedBool(ctx, cacheKey, cacheOptions, exists)
	return exists, err
}

func (q *SelectQuery) resolveCacheKey(query string, args []any) (string, *queryCacheOptions, error) {
	if q.cacheOptions == nil || q.cache == nil {
		return "", nil, nil
	}
	key, err := buildQueryCacheKey(q.dialect.Name(), query, args, q.relationNames, q.cacheOptions)
	if err != nil {
		return "", nil, err
	}
	return key, q.cacheOptions, nil
}

func (q *SelectQuery) scanValidationTable() *schema.TableDef {
	if q.firstOperand != nil {
		return nil
	}
	if len(q.joins) > 0 {
		return nil
	}
	return q.table
}

func (q *SelectQuery) writeCachedSelectResult(ctx context.Context, key string, options *queryCacheOptions, value *cachedSelectRows) error {
	if options == nil || options.bypass {
		return nil
	}
	encoded, err := encodeCachedSelectRows(value)
	if err != nil {
		return err
	}
	return q.cache.Set(ctx, key, encoded, options.ttl, options.tags)
}

func (q *SelectQuery) writeCachedInt64(ctx context.Context, key string, options *queryCacheOptions, value int64) error {
	if options == nil || options.bypass {
		return nil
	}
	encoded, err := encodeCachedInt64(value)
	if err != nil {
		return err
	}
	return q.cache.Set(ctx, key, encoded, options.ttl, options.tags)
}

func (q *SelectQuery) writeCachedBool(ctx context.Context, key string, options *queryCacheOptions, value bool) error {
	if options == nil || options.bypass {
		return nil
	}
	encoded, err := encodeCachedBool(value)
	if err != nil {
		return err
	}
	return q.cache.Set(ctx, key, encoded, options.ttl, options.tags)
}

func (q *SelectQuery) toAggregateSQL(selection string) (string, []any, error) {
	compiled, err := q.compileAggregate(selection)
	if err != nil {
		return "", nil, err
	}
	args, err := compiled.literalArgs()
	if err != nil {
		return "", nil, err
	}
	return compiled.sql, args, nil
}

func (q *SelectQuery) compile() (compiledQuery, error) {
	if q.table == nil && q.tableSubquery == nil && q.firstOperand == nil {
		if q.tableAlias != "" {
			return compiledQuery{}, errors.New("rain: subquery table source requires a non-nil query")
		}
		return compiledQuery{}, errors.New("rain: select query requires a table")
	}

	if q.distinct && len(q.distinctOn) > 0 {
		return compiledQuery{}, errors.New("rain: SELECT DISTINCT and DISTINCT ON cannot be used together")
	}

	if q.firstOperand != nil {
		if q.distinct {
			return compiledQuery{}, errors.New("rain: compound queries do not support DISTINCT")
		}
		if len(q.distinctOn) > 0 {
			return compiledQuery{}, errors.New("rain: compound queries do not support DISTINCT ON")
		}
		if len(q.cols) > 0 {
			return compiledQuery{}, errors.New("rain: compound queries do not support Column()")
		}
		if q.table != nil || q.tableSubquery != nil {
			return compiledQuery{}, errors.New("rain: compound queries do not support Table() (already has operands)")
		}
		if len(q.where) > 0 {
			return compiledQuery{}, errors.New("rain: compound queries do not support WHERE")
		}
		if len(q.joins) > 0 {
			return compiledQuery{}, errors.New("rain: compound queries do not support JOIN")
		}
		if len(q.groupBy) > 0 {
			return compiledQuery{}, errors.New("rain: compound queries do not support GROUP BY")
		}
		if len(q.having) > 0 {
			return compiledQuery{}, errors.New("rain: compound queries do not support HAVING")
		}
		if len(q.relationNames) > 0 {
			return compiledQuery{}, errors.New("rain: compound queries do not support WithRelations()")
		}
		if q.locking != nil {
			return compiledQuery{}, errors.New("rain: compound queries do not support FOR locking clauses")
		}
	}

	ctx := newCompileContext(q.dialect)
	defer releaseCompileContext(ctx)
	if err := q.writeSQL(ctx); err != nil {
		return compiledQuery{}, err
	}
	return ctx.compiledQuery(), nil
}

func (q *SelectQuery) compileAggregate(selection string) (compiledQuery, error) {
	if q.firstOperand != nil {
		return compiledQuery{}, errors.New("rain: aggregate helpers do not support compound queries (UNION, etc.); use TableSubquery as a workaround")
	}
	if q.table == nil && q.tableSubquery == nil {
		if q.tableAlias != "" {
			return compiledQuery{}, errors.New("rain: subquery table source requires a non-nil query")
		}
		return compiledQuery{}, errors.New("rain: select query requires a table")
	}
	if len(q.ctes) > 0 {
		return compiledQuery{}, errors.New("rain: aggregate helpers do not support WITH clauses")
	}
	if q.distinct || len(q.distinctOn) > 0 || len(q.groupBy) > 0 || len(q.having) > 0 {
		return compiledQuery{}, errors.New("rain: aggregate helpers do not support DISTINCT, DISTINCT ON, GROUP BY, or HAVING clauses")
	}
	if q.locking != nil {
		return compiledQuery{}, errors.New("rain: aggregate helpers do not support FOR locking clauses")
	}

	ctx := newCompileContext(q.dialect)
	defer releaseCompileContext(ctx)
	ctx.writeString("SELECT ")
	ctx.writeString(selection)
	ctx.writeString(" FROM ")
	if err := q.writeTableSourceSQL(ctx); err != nil {
		return compiledQuery{}, err
	}

	if err := q.writeJoins(ctx); err != nil {
		return compiledQuery{}, err
	}

	if len(q.where) > 0 {
		ctx.writeString(" WHERE ")
		if err := ctx.writeJoinedPredicates(q.where, false); err != nil {
			return compiledQuery{}, err
		}
	}

	return ctx.compiledQuery(), ctx.err
}

func (q *SelectQuery) compileExists() (compiledQuery, error) {
	if q.locking != nil {
		return compiledQuery{}, errors.New("rain: exists checks do not support FOR locking clauses")
	}
	compiled, err := q.compile()
	if err != nil {
		return compiledQuery{}, err
	}
	return wrapExistsCompiled(compiled)
}

func (q *SelectQuery) writeTableSourceSQL(ctx *compileContext) error {
	if q.table != nil {
		ctx.writeTable(q.table)
		return nil
	}
	if strings.TrimSpace(q.tableAlias) == "" {
		return errors.New("rain: subquery table source requires a non-empty alias")
	}
	if q.tableSubquery == nil {
		return errors.New("rain: subquery table source requires a non-nil query")
	}
	ctx.writeByte('(')
	if err := q.tableSubquery.writeSQL(ctx); err != nil {
		return err
	}
	ctx.writeString(") AS ")
	ctx.writeQuotedIdentifier(q.tableAlias)
	return nil
}

func wrapExistsCompiled(compiled compiledQuery) (compiledQuery, error) {
	// NOTE: This shallow copies the input compiledQuery and wraps the SQL.
	// The argPlan and args slices are shared with the original. This is safe
	// because compileExists (the only caller) does not use the original after
	// this call.
	compiled.sql = "SELECT EXISTS(" + compiled.sql + ")"
	return compiled, nil
}
