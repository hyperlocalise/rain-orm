package rain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/hyperlocalise/rain-orm/pkg/dialect"
	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

type queryRunner interface {
	execContext(context.Context, string, ...any) (sql.Result, error)
	queryContext(context.Context, string, ...any) (*sql.Rows, error)
}

type joinClause struct {
	kind  string
	table selectTableSource
	on    schema.Predicate
}

type assignment struct {
	column schema.ColumnReference
	value  schema.Expression
}

type returningClause struct {
	feature dialect.Feature
	label   string
}

type selectTableSource interface {
	writeSQL(*compileContext) error
}

type tableDefSource struct {
	table *schema.TableDef
}

func (s tableDefSource) writeSQL(ctx *compileContext) error {
	ctx.writeTable(s.table)
	return nil
}

type subqueryTableSource struct {
	query *SelectQuery
	alias string
}

func (s subqueryTableSource) writeSQL(ctx *compileContext) error {
	if strings.TrimSpace(s.alias) == "" {
		return errors.New("rain: subquery table source requires a non-empty alias")
	}
	if s.query == nil {
		return fmt.Errorf("rain: subquery table source %q requires a non-nil query", s.alias)
	}
	ctx.writeByte('(')
	if err := s.query.writeSQL(ctx); err != nil {
		return err
	}
	ctx.writeString(") AS ")
	ctx.writeQuotedIdentifier(s.alias)
	return nil
}

type cteDefinition struct {
	name  string
	query *SelectQuery
}

func closeRows(rows *sql.Rows, errp *error) {
	if err := rows.Close(); err != nil && *errp == nil {
		*errp = err
	}
}

// SelectQuery builds typed SELECT statements.
type SelectQuery struct {
	runner   queryRunner
	dialect  dialect.Dialect
	table    selectTableSource
	cols     []schema.Expression
	where    []schema.Predicate
	joins    []joinClause
	order    []schema.OrderExpr
	groupBy  []schema.Expression
	having   []schema.Predicate
	ctes     []cteDefinition
	distinct bool
	limit    int
	offset   int
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

// ToSQL compiles the query into SQL and args.
func (q *SelectQuery) ToSQL() (string, []any, error) {
	ctx := newCompileContext(q.dialect)
	if err := q.writeSQL(ctx); err != nil {
		return "", nil, err
	}
	return ctx.String(), ctx.args, nil
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
			if err := ctx.writeExpression(column); err != nil {
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

	query, args, err := q.ToSQL()
	if err != nil {
		return err
	}

	rows, err := q.runner.queryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer closeRows(rows, &err)

	err = scanRows(rows, dest)
	return err
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
	return count, err
}

// Exists executes a SELECT EXISTS query.
func (q *SelectQuery) Exists(ctx context.Context) (bool, error) {
	if q.runner == nil {
		return false, ErrNoConnection
	}

	sqlText, args, err := q.ToSQL()
	if err != nil {
		return false, err
	}

	ctxCompiler := newCompileContext(q.dialect)
	ctxCompiler.writeString("SELECT EXISTS(")
	ctxCompiler.writeString(sqlText)
	ctxCompiler.writeByte(')')
	ctxCompiler.args = append(ctxCompiler.args, args...)

	rows, err := q.runner.queryContext(ctx, ctxCompiler.String(), ctxCompiler.args...)
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
	return exists, err
}

func (q *SelectQuery) toAggregateSQL(selection string) (string, []any, error) {
	if q.table == nil {
		return "", nil, errors.New("rain: select query requires a table")
	}
	if len(q.ctes) > 0 {
		return "", nil, errors.New("rain: aggregate helpers do not support WITH clauses")
	}
	if q.distinct || len(q.groupBy) > 0 || len(q.having) > 0 {
		return "", nil, errors.New("rain: aggregate helpers do not support DISTINCT, GROUP BY, or HAVING clauses")
	}

	ctx := newCompileContext(q.dialect)
	ctx.writeString("SELECT ")
	ctx.writeString(selection)
	ctx.writeString(" FROM ")
	if err := q.table.writeSQL(ctx); err != nil {
		return "", nil, err
	}

	for _, join := range q.joins {
		ctx.writeByte(' ')
		ctx.writeString(join.kind)
		ctx.writeByte(' ')
		if err := join.table.writeSQL(ctx); err != nil {
			return "", nil, err
		}
		ctx.writeString(" ON ")
		if err := ctx.writePredicate(join.on); err != nil {
			return "", nil, err
		}
	}

	if len(q.where) > 0 {
		ctx.writeString(" WHERE ")
		if err := ctx.writePredicate(joinPredicates(q.where)); err != nil {
			return "", nil, err
		}
	}

	return ctx.String(), ctx.args, ctx.err
}

// InsertQuery builds typed INSERT statements.
type InsertQuery struct {
	runner    queryRunner
	dialect   dialect.Dialect
	table     *schema.TableDef
	model     any
	values    []assignment
	returning []schema.Expression
}

// Table sets the INSERT target table.
func (q *InsertQuery) Table(table schema.TableReference) *InsertQuery {
	q.table = table.TableDef()
	return q
}

// Model sets a struct payload for the insert.
// Zero-valued fields for columns with schema defaults are omitted so the
// database default applies; use Set to override that behavior explicitly.
func (q *InsertQuery) Model(model any) *InsertQuery {
	q.model = model
	return q
}

// Set adds an explicit column assignment.
func (q *InsertQuery) Set(column schema.ColumnReference, value any) *InsertQuery {
	q.values = append(q.values, assignment{column: column, value: schema.ValueExpr{Value: value}})
	return q
}

// Returning adds RETURNING expressions when supported by the dialect.
func (q *InsertQuery) Returning(exprs ...schema.Expression) *InsertQuery {
	q.returning = append(q.returning, exprs...)
	return q
}

// ToSQL compiles the insert into SQL and args.
func (q *InsertQuery) ToSQL() (string, []any, error) {
	assignments, err := q.insertAssignments()
	if err != nil {
		return "", nil, err
	}

	ctx := newCompileContext(q.dialect)
	ctx.writeString("INSERT INTO ")
	ctx.writeTableName(q.table)
	ctx.writeString(" (")
	for idx, item := range assignments {
		if idx > 0 {
			ctx.writeString(", ")
		}
		ctx.writeQuotedIdentifier(item.column.ColumnDef().Name)
	}
	ctx.writeString(") VALUES (")
	for idx, item := range assignments {
		if idx > 0 {
			ctx.writeString(", ")
		}
		if err := ctx.writeExpression(item.value); err != nil {
			return "", nil, err
		}
	}
	ctx.writeByte(')')

	if err := ctx.writeReturning(q.returning, q.returningClause()); err != nil {
		return "", nil, err
	}

	return ctx.String(), ctx.args, ctx.err
}

func (q *InsertQuery) returningClause() returningClause {
	return returningClause{
		feature: dialect.FeatureInsertReturning,
		label:   "insert",
	}
}

// Exec executes the INSERT query.
func (q *InsertQuery) Exec(ctx context.Context) (sql.Result, error) {
	if q.runner == nil {
		return nil, ErrNoConnection
	}

	query, args, err := q.ToSQL()
	if err != nil {
		return nil, err
	}

	return q.runner.execContext(ctx, query, args...)
}

// Scan executes an INSERT ... RETURNING query and scans one row into dest.
func (q *InsertQuery) Scan(ctx context.Context, dest any) error {
	if q.runner == nil {
		return ErrNoConnection
	}
	if len(q.returning) == 0 {
		return errors.New("rain: insert scan requires RETURNING")
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

	err = scanRows(rows, dest)
	return err
}

func (q *InsertQuery) insertAssignments() ([]assignment, error) {
	if q.table == nil {
		return nil, errors.New("rain: insert query requires a table")
	}
	if q.model == nil && len(q.values) == 0 {
		return nil, errors.New("rain: insert query requires either explicit values or a model")
	}

	var (
		modelAssignments []assignment
		err              error
	)
	if q.model != nil {
		modelAssignments, err = assignmentsFromModel(q.table, q.model, true)
		if err != nil {
			return nil, err
		}
	}

	assignments, err := mergeAssignments(q.table, modelAssignments, q.values)
	if err != nil {
		return nil, err
	}
	if len(assignments) == 0 {
		return nil, errors.New("rain: insert query produced no values")
	}

	return assignments, nil
}

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
	q.values = append(q.values, assignment{column: column, value: schema.ValueExpr{Value: value}})
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

	return ctx.String(), ctx.args, ctx.err
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

	err = scanRows(rows, dest)
	return err
}

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
	if len(q.where) == 0 && !q.unbounded {
		return "", nil, errors.New("rain: delete query requires at least one WHERE predicate; call Unbounded() to allow all rows")
	}

	ctx := newCompileContext(q.dialect)
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

	return ctx.String(), ctx.args, ctx.err
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

	err = scanRows(rows, dest)
	return err
}

type compileContext struct {
	builder strings.Builder
	dialect dialect.Dialect
	args    []any
	err     error
}

func newCompileContext(d dialect.Dialect) *compileContext {
	return &compileContext{
		dialect: d,
		args:    make([]any, 0, 8),
	}
}

func (c *compileContext) String() string {
	return c.builder.String()
}

func (c *compileContext) writeByte(ch byte) {
	c.builder.WriteByte(ch)
}

func (c *compileContext) writeString(value string) {
	c.builder.WriteString(value)
}

func (c *compileContext) writeQuotedIdentifier(name string) {
	c.writeString(c.dialect.QuoteIdentifier(name))
}

func (c *compileContext) writeTableName(table *schema.TableDef) {
	c.writeQuotedIdentifier(table.Name)
}

func (c *compileContext) writeTable(table *schema.TableDef) {
	c.writeTableName(table)
	if table.Alias != "" {
		c.writeString(" AS ")
		c.writeQuotedIdentifier(table.Alias)
	}
}

func (c *compileContext) writeReturning(exprs []schema.Expression, clause returningClause) error {
	if len(exprs) == 0 {
		return nil
	}
	if !dialect.HasFeature(c.dialect.Features(), clause.feature) {
		return fmt.Errorf("rain: %s queries do not support RETURNING for %s dialect", clause.label, c.dialect.Name())
	}

	c.writeString(" RETURNING ")
	for idx, expr := range exprs {
		if idx > 0 {
			c.writeString(", ")
		}
		if err := c.writeExpression(expr); err != nil {
			return err
		}
	}

	return nil
}

func (c *compileContext) writePredicate(predicate schema.Predicate) error {
	return c.writeExpression(predicate)
}

func (c *compileContext) writeExpression(expr schema.Expression) error {
	switch value := expr.(type) {
	case schema.ColumnReference:
		c.writeColumn(value)
	case schema.ValueExpr:
		c.args = append(c.args, value.Value)
		c.writeString(c.dialect.Placeholder(len(c.args)))
	case schema.ComparisonExpr:
		if err := c.writeExpression(value.Left); err != nil {
			return err
		}
		c.writeByte(' ')
		c.writeString(value.Operator)
		c.writeByte(' ')
		if err := c.writeExpression(value.Right); err != nil {
			return err
		}
	case schema.NullCheckExpr:
		if err := c.writeExpression(value.Expr); err != nil {
			return err
		}
		if value.Negated {
			c.writeString(" IS NOT NULL")
		} else {
			c.writeString(" IS NULL")
		}
	case schema.LogicalExpr:
		c.writeByte('(')
		for idx, part := range value.Exprs {
			if idx > 0 {
				c.writeByte(' ')
				c.writeString(value.Operator)
				c.writeByte(' ')
			}
			if err := c.writePredicate(part); err != nil {
				return err
			}
		}
		c.writeByte(')')
	case schema.RawExpr:
		if err := c.writeRaw(value); err != nil {
			return err
		}
	default:
		return fmt.Errorf("rain: unsupported expression type %T", expr)
	}

	return nil
}

func (c *compileContext) writeRaw(raw schema.RawExpr) error {
	argIndex := 0
	for idx := range len(raw.SQL) {
		if raw.SQL[idx] != '?' {
			c.writeByte(raw.SQL[idx])
			continue
		}
		if argIndex >= len(raw.Args) {
			return errors.New("rain: raw SQL placeholder count does not match args")
		}
		c.args = append(c.args, raw.Args[argIndex])
		c.writeString(c.dialect.Placeholder(len(c.args)))
		argIndex++
	}
	if argIndex != len(raw.Args) {
		return errors.New("rain: raw SQL has unused args")
	}

	return nil
}

func (c *compileContext) writeColumn(column schema.ColumnReference) {
	def := column.ColumnDef()
	table := def.Table
	qualifier := table.Name
	if table.Alias != "" {
		qualifier = table.Alias
	}

	c.writeQuotedIdentifier(qualifier)
	c.writeByte('.')
	c.writeQuotedIdentifier(def.Name)
}

func joinPredicates(predicates []schema.Predicate) schema.Predicate {
	if len(predicates) == 1 {
		return predicates[0]
	}

	return schema.And(predicates...)
}

func assignmentsFromModel(table *schema.TableDef, model any, skipAuto bool) ([]assignment, error) {
	meta, value, err := lookupModelMeta(model)
	if err != nil {
		return nil, err
	}

	assignments := make([]assignment, 0, len(table.Columns))
	for _, column := range table.Columns {
		field, ok := meta.byColumn[column.Name]
		if !ok {
			continue
		}

		fieldValue := value.FieldByIndex(field.index)
		resolvedValue, include := fieldValueForInsert(column, fieldValue, skipAuto)
		if !include {
			continue
		}

		assignments = append(assignments, assignment{
			column: schema.Ref(column),
			value:  schema.ValueExpr{Value: resolvedValue},
		})
	}

	return assignments, nil
}

func mergeAssignments(table *schema.TableDef, base, overrides []assignment) ([]assignment, error) {
	ordered := make([]assignment, 0, len(table.Columns))
	assignmentsByName := make(map[string]assignment, len(table.Columns))

	for _, item := range base {
		if err := validateAssignmentTarget(table, item); err != nil {
			return nil, err
		}
		assignmentsByName[item.column.ColumnDef().Name] = item
	}
	for _, item := range overrides {
		if err := validateAssignmentTarget(table, item); err != nil {
			return nil, err
		}
		assignmentsByName[item.column.ColumnDef().Name] = item
	}

	for _, column := range table.Columns {
		item, ok := assignmentsByName[column.Name]
		if !ok {
			continue
		}
		ordered = append(ordered, item)
		delete(assignmentsByName, column.Name)
	}

	if len(assignmentsByName) > 0 {
		names := make([]string, 0, len(assignmentsByName))
		for name := range assignmentsByName {
			names = append(names, name)
		}
		slices.Sort(names)
		return nil, fmt.Errorf("rain: insert assignments contain unknown target columns: %s", strings.Join(names, ", "))
	}

	return ordered, nil
}

func validateAssignmentTarget(table *schema.TableDef, item assignment) error {
	column := item.column.ColumnDef()
	if column.Table.Name != table.Name {
		return fmt.Errorf("rain: column %s belongs to table %s, not %s", column.Name, column.Table.Name, table.Name)
	}
	if _, ok := table.ColumnByName(column.Name); !ok {
		return fmt.Errorf("rain: unknown column %s on table %s", column.Name, table.Name)
	}

	return nil
}

func fieldValueForInsert(column *schema.ColumnDef, fieldValue reflect.Value, skipAuto bool) (any, bool) {
	resolved, isNil := dereferenceValue(fieldValue)
	if isNil {
		return nil, false
	}

	if skipAuto && column.AutoIncrement && resolved.IsZero() {
		return nil, false
	}
	if column.HasDefault && resolved.IsZero() {
		return nil, false
	}

	return resolved.Interface(), true
}

func dereferenceValue(value reflect.Value) (reflect.Value, bool) {
	current := value
	for current.Kind() == reflect.Pointer {
		if current.IsNil() {
			return reflect.Value{}, true
		}
		current = current.Elem()
	}

	return current, false
}
