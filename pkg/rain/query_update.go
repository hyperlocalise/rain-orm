package rain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/hyperlocalise/rain-orm/pkg/dialect"
	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

// UpdateQuery builds typed UPDATE statements.
type UpdateQuery struct {
	runner    queryRunner
	dialect   dialect.Dialect
	table     *schema.TableDef
	values    []assignment
	where     []schema.Predicate
	order     []schema.OrderExpr
	limit     *int
	ctes      []cteDefinition
	returning []schema.Expression
	unbounded bool
}

// Table sets the UPDATE target table.
func (q *UpdateQuery) Table(table schema.TableReference) *UpdateQuery {
	q.table = table.TableDef()
	return q
}

// Set adds an explicit typed assignment.
func (q *UpdateQuery) Set(column schema.ColumnReference, value any) *UpdateQuery {
	var expr schema.Expression
	if e, ok := value.(schema.Expression); ok {
		expr = e
	} else {
		expr = schema.ValueExpr{Value: value}
	}

	q.values = append(q.values, assignment{column: column, value: expr})
	return q
}

// Where appends a WHERE predicate joined with AND.
func (q *UpdateQuery) Where(predicate schema.Predicate) *UpdateQuery {
	q.where = append(q.where, predicate)
	return q
}

// Returning adds RETURNING expressions when supported by the dialect.
func (q *UpdateQuery) Returning(exprs ...schema.Expression) *UpdateQuery {
	q.returning = append(q.returning, exprs...)
	return q
}

// With appends a common table expression definition.
func (q *UpdateQuery) With(name string, query *SelectQuery) *UpdateQuery {
	q.ctes = append(q.ctes, cteDefinition{name: name, query: query})
	return q
}

// OrderBy appends ORDER BY expressions.
// Supported by MySQL and SQLite.
func (q *UpdateQuery) OrderBy(order ...schema.OrderExpr) *UpdateQuery {
	q.order = append(q.order, order...)
	return q
}

// Limit sets the LIMIT clause.
// Supported by MySQL and SQLite.
func (q *UpdateQuery) Limit(limit int) *UpdateQuery {
	q.limit = &limit
	return q
}

// Unbounded allows UPDATE without a WHERE clause.
func (q *UpdateQuery) Unbounded() *UpdateQuery {
	q.unbounded = true
	return q
}

// Prepare compiles and prepares the UPDATE query.
func (q *UpdateQuery) Prepare(ctx context.Context) (*PreparedUpdateQuery, error) {
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

	return &PreparedUpdateQuery{
		table:    q.table,
		compiled: compiled,
		stmt:     stmt,
	}, nil
}

// ToSQL compiles the update into SQL and args.
func (q *UpdateQuery) ToSQL() (string, []any, error) {
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

func (q *UpdateQuery) compile() (compiledQuery, error) {
	if q.table == nil {
		return compiledQuery{}, errors.New("rain: update query requires a table")
	}
	if q.table.IsView {
		return compiledQuery{}, fmt.Errorf("rain: cannot update view %q", q.table.Name)
	}
	if len(q.values) == 0 {
		return compiledQuery{}, errors.New("rain: update query requires at least one assignment")
	}
	if len(q.where) == 0 && !q.unbounded {
		return compiledQuery{}, errors.New("rain: update query requires at least one WHERE predicate; call Unbounded() to allow all rows")
	}

	ctx := newCompileContext(q.dialect)
	defer releaseCompileContext(ctx)

	if err := q.writeSQL(ctx); err != nil {
		return compiledQuery{}, err
	}

	return ctx.compiledQuery(), ctx.err
}

func (q *UpdateQuery) writeSQL(ctx *compileContext) error {
	if err := writeCTEs(ctx, q.ctes, "update"); err != nil {
		return err
	}

	ctx.writeString("UPDATE ")
	ctx.writeTableName(q.table)
	ctx.writeString(" SET ")
	for idx, item := range q.values {
		if err := validateAssignmentTarget(q.table, item); err != nil {
			return err
		}
		if idx > 0 {
			ctx.writeString(", ")
		}
		ctx.writeQuotedIdentifier(item.column.ColumnDef().Name)
		ctx.writeString(" = ")
		if err := ctx.writeExpression(item.value); err != nil {
			return err
		}
	}

	if len(q.where) > 0 {
		ctx.writeString(" WHERE ")
		if err := ctx.writePredicate(joinPredicates(q.where)); err != nil {
			return err
		}
	}

	if err := writeOrderLimit(ctx, q.order, q.limit, nil, dialect.FeatureUpdateOrder, dialect.FeatureUpdateLimit); err != nil {
		return err
	}

	return ctx.writeReturning(q.returning, q.returningClause())
}

func (q *UpdateQuery) returningClause() returningClause {
	return returningClause{
		feature: dialect.FeatureUpdateReturning,
		label:   "update",
	}
}

// Exec executes the UPDATE query.
func (q *UpdateQuery) Exec(ctx context.Context) (sql.Result, error) {
	if q.runner == nil {
		return nil, ErrNoConnection
	}

	query, args, err := q.ToSQL()
	if err != nil {
		return nil, err
	}

	return q.runner.execContext(ctx, query, args...)
}

// Scan executes an UPDATE ... RETURNING query and scans results into dest.
func (q *UpdateQuery) Scan(ctx context.Context, dest any) error {
	if q.runner == nil {
		return ErrNoConnection
	}
	if len(q.returning) == 0 {
		return errors.New("rain: update scan requires RETURNING")
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
