package rain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/hyperlocalise/rain-orm/pkg/dialect"
	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

// DeleteQuery builds typed DELETE statements.
type DeleteQuery struct {
	runner    queryRunner
	dialect   dialect.Dialect
	table     *schema.TableDef
	where     []schema.Predicate
	using     []tableSource
	order     []schema.OrderExpr
	limit     int
	hasLimit  bool
	offset    int
	hasOffset bool
	ctes      []cteDefinition
	returning []schema.Expression
	unbounded bool

	// OPTIMIZATION: Minimal internal buffers to avoid heap allocations for
	// common query shapes while keeping the struct size small.
	whereBuf     [2]schema.Predicate
	returningBuf [1]schema.Expression
}

// Table sets the DELETE target table.
func (q *DeleteQuery) Table(table schema.TableReference) *DeleteQuery {
	q.table = table.TableDef()
	return q
}

// Where appends a WHERE predicate joined with AND.
func (q *DeleteQuery) Where(predicate schema.Predicate) *DeleteQuery {
	q.where = append(q.where, predicate)
	return q
}

// Returning adds RETURNING expressions when supported by the dialect.
func (q *DeleteQuery) Returning(exprs ...schema.Expression) *DeleteQuery {
	q.returning = append(q.returning, exprs...)
	return q
}

// Using appends additional table sources for the DELETE ... USING clause.
// Supported by PostgreSQL.
func (q *DeleteQuery) Using(tables ...schema.TableReference) *DeleteQuery {
	for _, table := range tables {
		q.using = append(q.using, tableSource{table: table.TableDef()})
	}
	return q
}

// UsingSubquery appends a subquery source for the DELETE ... USING clause.
func (q *DeleteQuery) UsingSubquery(query *SelectQuery, alias string) *DeleteQuery {
	q.using = append(q.using, tableSource{subquery: query, alias: alias})
	return q
}

// With appends a common table expression definition.
func (q *DeleteQuery) With(name string, query *SelectQuery) *DeleteQuery {
	q.ctes = append(q.ctes, cteDefinition{name: name, query: query})
	return q
}

// OrderBy appends ORDER BY expressions.
// Supported by MySQL and SQLite.
func (q *DeleteQuery) OrderBy(order ...schema.OrderExpr) *DeleteQuery {
	q.order = append(q.order, order...)
	return q
}

// Limit sets the LIMIT clause.
// Supported by MySQL and SQLite.
func (q *DeleteQuery) Limit(limit int) *DeleteQuery {
	q.limit = limit
	q.hasLimit = true
	return q
}

// Unbounded allows DELETE without a WHERE clause.
func (q *DeleteQuery) Unbounded() *DeleteQuery {
	q.unbounded = true
	return q
}

// Prepare compiles and prepares the DELETE query.
func (q *DeleteQuery) Prepare(ctx context.Context) (*PreparedDeleteQuery, error) {
	if q.runner == nil {
		return nil, ErrNoConnection
	}

	runner, ok := q.runner.(preparingQueryRunner)
	if !ok {
		return nil, ErrPrepareNotSupported
	}

	compiled, err := q.compile()
	if err != nil {
		return nil, err
	}

	stmt, err := runner.prepareContext(ctx, compiled.sql)
	if err != nil {
		return nil, err
	}

	return &PreparedDeleteQuery{
		table:    q.table,
		compiled: compiled,
		stmt:     stmt,
	}, nil
}

// ToSQL compiles the delete into SQL and args.
func (q *DeleteQuery) ToSQL() (string, []any, error) {
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

func (q *DeleteQuery) compile() (compiledQuery, error) {
	if q.table == nil {
		return compiledQuery{}, errors.New("rain: delete query requires a table")
	}
	if q.table.IsView {
		return compiledQuery{}, fmt.Errorf("rain: cannot delete from view %q", q.table.Name)
	}
	if len(q.where) == 0 && !q.unbounded {
		return compiledQuery{}, errors.New("rain: delete query requires at least one WHERE predicate; call Unbounded() to allow all rows")
	}

	ctx := newCompileContext(q.dialect)
	defer releaseCompileContext(ctx)

	if err := q.writeSQL(ctx); err != nil {
		return compiledQuery{}, err
	}

	return ctx.compiledQuery(), ctx.err
}

func (q *DeleteQuery) writeSQL(ctx *compileContext) error {
	ctx.ensureArgsCapacity(len(q.where))

	if err := writeCTEs(ctx, q.ctes, "delete"); err != nil {
		return err
	}

	ctx.writeString("DELETE FROM ")
	ctx.writeTable(q.table)

	if len(q.using) > 0 {
		if !dialect.HasFeature(ctx.dialect.Features(), dialect.FeatureDeleteUsing) {
			return fmt.Errorf("rain: DELETE ... USING is not supported by %s dialect", ctx.dialect.Name())
		}
		ctx.writeString(" USING ")
		for idx, source := range q.using {
			if idx > 0 {
				ctx.writeString(", ")
			}
			if err := source.writeSQL(ctx); err != nil {
				return err
			}
		}
	}

	if len(q.where) > 0 {
		ctx.writeString(" WHERE ")
		if err := ctx.writeJoinedPredicates(q.where, false); err != nil {
			return err
		}
	}

	if err := writeOrderLimit(ctx, q.order, q.limit, q.hasLimit, q.offset, q.hasOffset, dialect.FeatureDeleteOrder, dialect.FeatureDeleteLimit); err != nil {
		return err
	}

	return ctx.writeReturning(q.returning, q.returningClause())
}

func (q *DeleteQuery) returningClause() returningClause {
	return returningClause{
		feature: dialect.FeatureDeleteReturning,
		label:   "delete",
	}
}

// Exec executes the DELETE query.
func (q *DeleteQuery) Exec(ctx context.Context) (sql.Result, error) {
	if q.runner == nil {
		return nil, ErrNoConnection
	}

	query, args, err := q.ToSQL()
	if err != nil {
		return nil, err
	}

	return q.runner.execContext(ctx, query, args...)
}

// Scan executes a DELETE ... RETURNING query and scans results into dest.
func (q *DeleteQuery) Scan(ctx context.Context, dest any) error {
	if q.runner == nil {
		return ErrNoConnection
	}
	if len(q.returning) == 0 {
		return errors.New("rain: delete scan requires RETURNING")
	}

	query, args, err := q.ToSQL()
	if err != nil {
		return err
	}

	rows, err := q.runner.queryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer closeRows(rows, &err)

	err = scanRowsAgainstTable(rows, dest, q.table)
	return err
}
