package rain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/hyperlocalise/rain-orm/pkg/dialect"
	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

// SelectQuery builds typed SELECT statements.
type SelectQuery struct {
	schema.ExpressionMarker
	runner        queryRunner
	dialect       dialect.Dialect
	cache         QueryCache
	table         selectTableSource
	cols          []schema.Expression
	where         []schema.Predicate
	joins         []joinClause
	order         []schema.OrderExpr
	groupBy       []schema.Expression
	having        []schema.Predicate
	ctes          []cteDefinition
	distinct      bool
	limit         int
	offset        int
	relationNames []string
	cacheOptions  *queryCacheOptions
}

// Table sets the table source for the query.
func (q *SelectQuery) Table(table schema.TableReference) *SelectQuery {
	q.table = tableDefSource{table: table.TableDef()}
	return q
}

// TableSubquery sets a subquery source for the query's FROM clause.
func (q *SelectQuery) TableSubquery(query *SelectQuery, alias string) *SelectQuery {
	q.table = subqueryTableSource{query: query, alias: alias}
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
	q.joins = append(q.joins, joinClause{kind: "INNER JOIN", table: tableDefSource{table: table.TableDef()}, on: on})
	return q
}

// LeftJoin appends a LEFT JOIN clause.
func (q *SelectQuery) LeftJoin(table schema.TableReference, on schema.Predicate) *SelectQuery {
	q.joins = append(q.joins, joinClause{kind: "LEFT JOIN", table: tableDefSource{table: table.TableDef()}, on: on})
	return q
}

// JoinSubquery appends an INNER JOIN against a subquery source.
func (q *SelectQuery) JoinSubquery(query *SelectQuery, alias string, on schema.Predicate) *SelectQuery {
	q.joins = append(q.joins, joinClause{kind: "INNER JOIN", table: subqueryTableSource{query: query, alias: alias}, on: on})
	return q
}

// LeftJoinSubquery appends a LEFT JOIN against a subquery source.
func (q *SelectQuery) LeftJoinSubquery(query *SelectQuery, alias string, on schema.Predicate) *SelectQuery {
	q.joins = append(q.joins, joinClause{kind: "LEFT JOIN", table: subqueryTableSource{query: query, alias: alias}, on: on})
	return q
}

// Distinct marks the SELECT query as DISTINCT.
func (q *SelectQuery) Distinct() *SelectQuery {
	q.distinct = true
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
	return q
}

// Offset sets the OFFSET clause.
func (q *SelectQuery) Offset(offset int) *SelectQuery {
	q.offset = offset
	return q
}

// WithRelations requests one or more named relations to be loaded after scanning base rows.
func (q *SelectQuery) WithRelations(names ...string) *SelectQuery {
	q.relationNames = append(q.relationNames, names...)
	return q
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

func (q *SelectQuery) writeSQL(ctx *compileContext) error {
	if q.table == nil {
		return errors.New("rain: select query requires a table")
	}

	if len(q.ctes) > 0 {
		if !dialect.HasFeature(ctx.dialect.Features(), dialect.FeatureCTE) {
			return fmt.Errorf("rain: select queries do not support CTEs for %s dialect", ctx.dialect.Name())
		}
		ctx.writeString("WITH ")
		for idx, cte := range q.ctes {
			if idx > 0 {
				ctx.writeString(", ")
			}
			if strings.TrimSpace(cte.name) == "" {
				return errors.New("rain: CTE name cannot be empty")
			}
			if cte.query == nil {
				return fmt.Errorf("rain: CTE %q requires a query", cte.name)
			}
			if len(cte.query.ctes) > 0 {
				return fmt.Errorf("rain: CTE %q body cannot itself contain CTEs", cte.name)
			}
			ctx.writeQuotedIdentifier(cte.name)
			ctx.writeString(" AS (")
			if err := cte.query.writeSQL(ctx); err != nil {
				return err
			}
			ctx.writeByte(')')
		}
		ctx.writeByte(' ')
	}

	ctx.writeString("SELECT ")
	if q.distinct {
		ctx.writeString("DISTINCT ")
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
	if err := q.table.writeSQL(ctx); err != nil {
		return err
	}

	for _, join := range q.joins {
		ctx.writeByte(' ')
		ctx.writeString(join.kind)
		ctx.writeByte(' ')
		if err := join.table.writeSQL(ctx); err != nil {
			return err
		}
		ctx.writeString(" ON ")
		if err := ctx.writePredicate(join.on); err != nil {
			return err
		}
	}

	if len(q.where) > 0 {
		ctx.writeString(" WHERE ")
		if err := ctx.writePredicate(joinPredicates(q.where)); err != nil {
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
		if err := ctx.writePredicate(joinPredicates(q.having)); err != nil {
			return err
		}
	}

	if len(q.order) > 0 {
		ctx.writeString(" ORDER BY ")
		for idx, item := range q.order {
			if idx > 0 {
				ctx.writeString(", ")
			}
			if err := ctx.writeExpression(item.Expr); err != nil {
				return err
			}
			ctx.writeByte(' ')
			ctx.writeString(string(item.Direction))
		}
	}

	if clause := q.dialect.LimitOffset(q.limit, q.offset); clause != "" {
		ctx.writeByte(' ')
		ctx.writeString(clause)
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
	if cacheOptions != nil && !cacheOptions.bypass && len(q.relationNames) == 0 {
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
		if cacheKey != "" && cacheOptions != nil && !cacheOptions.bypass {
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
	if len(q.joins) > 0 {
		return nil
	}
	return tableDefFromSelectSource(q.table)
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
	if q.table == nil {
		return compiledQuery{}, errors.New("rain: select query requires a table")
	}

	ctx := newCompileContext(q.dialect)
	if err := q.writeSQL(ctx); err != nil {
		return compiledQuery{}, err
	}
	return ctx.compiledQuery(), nil
}

func (q *SelectQuery) compileAggregate(selection string) (compiledQuery, error) {
	if q.table == nil {
		return compiledQuery{}, errors.New("rain: select query requires a table")
	}
	if len(q.ctes) > 0 {
		return compiledQuery{}, errors.New("rain: aggregate helpers do not support WITH clauses")
	}
	if q.distinct || len(q.groupBy) > 0 || len(q.having) > 0 {
		return compiledQuery{}, errors.New("rain: aggregate helpers do not support DISTINCT, GROUP BY, or HAVING clauses")
	}

	ctx := newCompileContext(q.dialect)
	ctx.writeString("SELECT ")
	ctx.writeString(selection)
	ctx.writeString(" FROM ")
	if err := q.table.writeSQL(ctx); err != nil {
		return compiledQuery{}, err
	}

	for _, join := range q.joins {
		ctx.writeByte(' ')
		ctx.writeString(join.kind)
		ctx.writeByte(' ')
		if err := join.table.writeSQL(ctx); err != nil {
			return compiledQuery{}, err
		}
		ctx.writeString(" ON ")
		if err := ctx.writePredicate(join.on); err != nil {
			return compiledQuery{}, err
		}
	}

	if len(q.where) > 0 {
		ctx.writeString(" WHERE ")
		if err := ctx.writePredicate(joinPredicates(q.where)); err != nil {
			return compiledQuery{}, err
		}
	}

	return ctx.compiledQuery(), ctx.err
}

func (q *SelectQuery) compileExists() (compiledQuery, error) {
	compiled, err := q.compile()
	if err != nil {
		return compiledQuery{}, err
	}
	return wrapExistsCompiled(compiled)
}

func wrapExistsCompiled(compiled compiledQuery) (compiledQuery, error) {
	existsQuery := compiledQuery{
		sql:      "SELECT EXISTS(" + compiled.sql + ")",
		argPlan:  make([]compiledArg, len(compiled.argPlan)),
		hasNames: compiled.hasNames,
	}
	copy(existsQuery.argPlan, compiled.argPlan)
	return existsQuery, nil
}
