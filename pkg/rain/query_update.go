package rain

import (
	"context"
	"database/sql"
	"errors"

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

// Unbounded allows UPDATE without a WHERE clause.
func (q *UpdateQuery) Unbounded() *UpdateQuery {
	q.unbounded = true
	return q
}

// ToSQL compiles the update into SQL and args.
func (q *UpdateQuery) ToSQL() (string, []any, error) {
	if q.table == nil {
		return "", nil, errors.New("rain: update query requires a table")
	}
	if len(q.values) == 0 {
		return "", nil, errors.New("rain: update query requires at least one assignment")
	}
	if len(q.where) == 0 && !q.unbounded {
		return "", nil, errors.New("rain: update query requires at least one WHERE predicate; call Unbounded() to allow all rows")
	}

	ctx := newCompileContext(q.dialect)
	ctx.writeString("UPDATE ")
	ctx.writeTableName(q.table)
	ctx.writeString(" SET ")
	for idx, item := range q.values {
		if idx > 0 {
			ctx.writeString(", ")
		}
		ctx.writeQuotedIdentifier(item.column.ColumnDef().Name)
		ctx.writeString(" = ")
		if err := ctx.writeExpression(item.value); err != nil {
			return "", nil, err
		}
	}

	if len(q.where) > 0 {
		ctx.writeString(" WHERE ")
		if err := ctx.writePredicate(joinPredicates(q.where)); err != nil {
			return "", nil, err
		}
	}

	if err := ctx.writeReturning(q.returning, q.returningClause()); err != nil {
		return "", nil, err
	}

	compiled := ctx.compiledQuery()
	args, err := compiled.literalArgs()
	if err != nil {
		return "", nil, err
	}
	return compiled.sql, args, ctx.err
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
