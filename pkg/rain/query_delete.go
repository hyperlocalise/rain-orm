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
	returning []schema.Expression
	unbounded bool
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

// Unbounded allows DELETE without a WHERE clause.
func (q *DeleteQuery) Unbounded() *DeleteQuery {
	q.unbounded = true
	return q
}

// ToSQL compiles the delete into SQL and args.
func (q *DeleteQuery) ToSQL() (string, []any, error) {
	if q.table == nil {
		return "", nil, errors.New("rain: delete query requires a table")
	}
	if q.table.IsView {
		return "", nil, fmt.Errorf("rain: cannot delete from view %q", q.table.Name)
	}
	if len(q.where) == 0 && !q.unbounded {
		return "", nil, errors.New("rain: delete query requires at least one WHERE predicate; call Unbounded() to allow all rows")
	}

	ctx := newCompileContext(q.dialect)
	defer releaseCompileContext(ctx)
	ctx.writeString("DELETE FROM ")
	ctx.writeTableName(q.table)
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
