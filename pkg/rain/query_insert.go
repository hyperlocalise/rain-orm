package rain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"

	"github.com/hyperlocalise/rain-orm/pkg/dialect"
	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

// InsertQuery builds typed INSERT statements.
type InsertQuery struct {
	runner      queryRunner
	dialect     dialect.Dialect
	table       *schema.TableDef
	model       any
	models      any
	values      []assignment
	rows        []map[schema.ColumnReference]any
	selectQuery *SelectQuery
	columns     []schema.ColumnReference
	returning   []schema.Expression
	conflict    *insertConflictClause
}

type insertConflictAction uint8

const (
	insertConflictActionNone insertConflictAction = iota
	insertConflictActionDoNothing
	insertConflictActionDoUpdateSet
)

type insertConflictClause struct {
	targets     []schema.Expression
	targetWhere schema.Predicate
	constraint  string
	action      insertConflictAction
	updates     []assignment
	where       schema.Predicate
}

type excludedColumn struct {
	schema.ExpressionMarker
	column schema.ColumnReference
}

// InsertConflictBuilder configures conflict behavior for INSERT statements.
type InsertConflictBuilder struct {
	query *InsertQuery
}

// Table sets the INSERT target table.
func (q *InsertQuery) Table(table schema.TableReference) *InsertQuery {
	q.table = table.TableDef()
	return q
}

// Model sets a struct payload for the insert.
// Plain fields are treated as explicit values, including zero values.
// Nil pointers are omitted, and rain.Set[T]{Valid:false} omits a value so
// schema defaults can apply.
func (q *InsertQuery) Model(model any) *InsertQuery {
	q.model = model
	return q
}

// Models sets multiple struct payloads for a bulk insert.
func (q *InsertQuery) Models(models any) *InsertQuery {
	q.models = models
	return q
}

// Select sets a SELECT query as the data source for the insert.
func (q *InsertQuery) Select(query *SelectQuery) *InsertQuery {
	q.selectQuery = query
	return q
}

// Columns sets the target columns for the insert.
func (q *InsertQuery) Columns(cols ...schema.ColumnReference) *InsertQuery {
	q.columns = append(q.columns, cols...)
	return q
}

// Set adds an explicit column assignment.
func (q *InsertQuery) Set(column schema.ColumnReference, value any) *InsertQuery {
	var expr schema.Expression
	if e, ok := value.(schema.Expression); ok {
		expr = e
	} else {
		expr = schema.ValueExpr{Value: value}
	}

	q.values = append(q.values, assignment{column: column, value: expr})
	return q
}

// Values appends explicit row value sets for a bulk insert.
func (q *InsertQuery) Values(rows ...map[schema.ColumnReference]any) *InsertQuery {
	q.rows = append(q.rows, rows...)
	return q
}

// OnConflict starts an upsert clause for PostgreSQL and SQLite dialects.
func (q *InsertQuery) OnConflict(targets ...schema.Expression) *InsertConflictBuilder {
	q.conflict = &insertConflictClause{targets: targets}
	return &InsertConflictBuilder{query: q}
}

// OnConstraint configures ON CONFLICT ON CONSTRAINT.
func (b *InsertConflictBuilder) OnConstraint(name string) *InsertConflictBuilder {
	b.query.conflict.constraint = name
	return b
}

// TargetWhere configures the filter condition for the conflict target (e.g. for partial indexes).
func (b *InsertConflictBuilder) TargetWhere(p schema.Predicate) *InsertConflictBuilder {
	b.query.conflict.targetWhere = p
	return b
}

// DoNothing configures ON CONFLICT ... DO NOTHING.
func (b *InsertConflictBuilder) DoNothing() *InsertQuery {
	b.query.conflict.action = insertConflictActionDoNothing
	return b.query
}

// DoUpdateSet configures ON CONFLICT ... DO UPDATE SET using EXCLUDED values (PostgreSQL/SQLite) or VALUES() references for MySQL.
func (b *InsertConflictBuilder) DoUpdateSet(columns ...schema.ColumnReference) *InsertQuery {
	b.query.conflict.action = insertConflictActionDoUpdateSet
	for _, col := range columns {
		b.query.conflict.updates = append(b.query.conflict.updates, assignment{
			column: col,
			value:  excludedColumn{column: col},
		})
	}
	return b.query
}

// Set adds an explicit assignment to the DO UPDATE SET clause.
func (b *InsertConflictBuilder) Set(column schema.ColumnReference, value any) *InsertConflictBuilder {
	var expr schema.Expression
	if e, ok := value.(schema.Expression); ok {
		expr = e
	} else {
		expr = schema.ValueExpr{Value: value}
	}
	b.query.conflict.updates = append(b.query.conflict.updates, assignment{column: column, value: expr})
	return b
}

// Where adds a filter condition to the DO UPDATE SET clause.
func (b *InsertConflictBuilder) Where(p schema.Predicate) *InsertConflictBuilder {
	b.query.conflict.where = p
	return b
}

// DoUpdate configures ON CONFLICT ... DO UPDATE SET.
// Use this after calling Set() to finish the builder.
func (b *InsertConflictBuilder) DoUpdate() *InsertQuery {
	b.query.conflict.action = insertConflictActionDoUpdateSet
	return b.query
}

// Returning adds RETURNING expressions when supported by the dialect.
func (q *InsertQuery) Returning(exprs ...schema.Expression) *InsertQuery {
	q.returning = append(q.returning, exprs...)
	return q
}

// Prepare compiles and prepares the INSERT query.
func (q *InsertQuery) Prepare(ctx context.Context) (*PreparedInsertQuery, error) {
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

	return &PreparedInsertQuery{
		table:    q.table,
		compiled: compiled,
		stmt:     stmt,
	}, nil
}

// ToSQL compiles the insert into SQL and args.
func (q *InsertQuery) ToSQL() (string, []any, error) {
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

func (q *InsertQuery) compile() (compiledQuery, error) {
	ctx := newCompileContext(q.dialect)
	defer releaseCompileContext(ctx)

	if q.selectQuery != nil {
		if err := q.writeSelectSQL(ctx); err != nil {
			return compiledQuery{}, err
		}
	} else {
		if err := q.writeValuesSQL(ctx); err != nil {
			return compiledQuery{}, err
		}
	}

	return ctx.compiledQuery(), ctx.err
}

func (q *InsertQuery) writeValuesSQL(ctx *compileContext) error {
	rows, err := q.insertAssignments()
	if err != nil {
		return err
	}

	ctx.writeString("INSERT INTO ")
	ctx.writeTableName(q.table)
	ctx.writeString(" (")
	for idx, item := range rows[0] {
		if idx > 0 {
			ctx.writeString(", ")
		}
		ctx.writeQuotedIdentifier(item.column.ColumnDef().Name)
	}
	ctx.writeString(") VALUES ")
	for rowIdx, row := range rows {
		if rowIdx > 0 {
			ctx.writeString(", ")
		}
		ctx.writeByte('(')
		for idx, item := range row {
			if idx > 0 {
				ctx.writeString(", ")
			}
			if err := ctx.writeExpression(item.value); err != nil {
				return err
			}
		}
		ctx.writeByte(')')
	}

	if err := q.writeConflictClause(ctx); err != nil {
		return err
	}

	return ctx.writeReturning(q.returning, q.returningClause())
}

func (q *InsertQuery) writeSelectSQL(ctx *compileContext) error {
	if err := q.validateSources(); err != nil {
		return err
	}
	if err := q.validateInsertSelectColumns(); err != nil {
		return err
	}

	selectQuery := q.selectQuery
	if q.dialect.Name() == "sqlite" && q.conflict != nil {
		selectQuery = selectQuery.withSQLiteInsertSelectConflictWhere()
	}

	ctx.writeString("INSERT INTO ")
	ctx.writeTableName(q.table)

	if len(q.columns) > 0 {
		ctx.writeString(" (")
		for idx, col := range q.columns {
			if idx > 0 {
				ctx.writeString(", ")
			}
			ctx.writeQuotedIdentifier(col.ColumnDef().Name)
		}
		ctx.writeByte(')')
	}

	ctx.writeByte(' ')
	if err := selectQuery.writeSQL(ctx); err != nil {
		return err
	}

	if err := q.writeConflictClause(ctx); err != nil {
		return err
	}

	return ctx.writeReturning(q.returning, q.returningClause())
}

func (q *InsertQuery) validateInsertSelectColumns() error {
	for _, col := range q.columns {
		if err := validateAssignmentTarget(q.table, assignment{column: col}); err != nil {
			return err
		}
	}

	return nil
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

	err = scanRowsAgainstTable(rows, dest, q.table)
	return err
}

func (q *InsertQuery) validateSources() error {
	if q.table == nil {
		return errors.New("rain: insert query requires a table")
	}
	if q.table.IsView {
		return fmt.Errorf("rain: cannot insert into view %q", q.table.Name)
	}

	sources := 0
	if q.model != nil || len(q.values) > 0 {
		sources++
	}
	if q.models != nil {
		sources++
	}
	if len(q.rows) > 0 {
		sources++
	}
	if q.selectQuery != nil {
		sources++
	}

	if sources == 0 {
		return errors.New("rain: insert query requires a data source: Model/Set, Models, Values, or Select")
	}
	if sources > 1 {
		return errors.New("rain: insert query requires exactly one data source: Model/Set, Models, Values, or Select")
	}

	return nil
}

func (q *InsertQuery) insertAssignments() ([][]assignment, error) {
	if err := q.validateSources(); err != nil {
		return nil, err
	}

	var rows [][]assignment
	if q.models != nil {
		modelRows, err := q.assignmentsFromModels()
		if err != nil {
			return nil, err
		}
		rows = append(rows, modelRows...)
	}
	if len(q.rows) > 0 {
		valueRows, err := q.assignmentsFromRows()
		if err != nil {
			return nil, err
		}
		rows = append(rows, valueRows...)
	}
	if q.model != nil || len(q.values) > 0 {
		singleRow, err := q.assignmentsFromModelAndSet()
		if err != nil {
			return nil, err
		}
		rows = append(rows, singleRow)
	}

	if len(rows) == 0 {
		return nil, errors.New("rain: insert query produced no values")
	}

	if err := validateInsertRowShape(rows); err != nil {
		return nil, err
	}

	return rows, nil
}

func (q *InsertQuery) assignmentsFromModelAndSet() ([]assignment, error) {
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

func (q *InsertQuery) assignmentsFromModels() ([][]assignment, error) {
	value := reflect.ValueOf(q.models)
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil, errors.New("rain: insert models cannot be nil")
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Slice && value.Kind() != reflect.Array {
		return nil, errors.New("rain: Models expects a slice or array")
	}
	if value.Len() == 0 {
		return nil, errors.New("rain: Models expects at least one model")
	}

	rows := make([][]assignment, 0, value.Len())
	for idx := range value.Len() {
		assignments, err := assignmentsFromModel(q.table, value.Index(idx).Interface(), true)
		if err != nil {
			return nil, err
		}
		if len(assignments) == 0 {
			return nil, fmt.Errorf("rain: insert row %d produced no values", idx+1)
		}
		rows = append(rows, assignments)
	}

	return rows, nil
}

func (q *InsertQuery) assignmentsFromRows() ([][]assignment, error) {
	rows := make([][]assignment, 0, len(q.rows))
	for idx, row := range q.rows {
		if len(row) == 0 {
			return nil, fmt.Errorf("rain: insert row %d has no values", idx+1)
		}

		overrides := make([]assignment, 0, len(row))
		for column, value := range row {
			var expr schema.Expression
			if e, ok := value.(schema.Expression); ok {
				expr = e
			} else {
				expr = schema.ValueExpr{Value: value}
			}
			overrides = append(overrides, assignment{column: column, value: expr})
		}

		assignments, err := mergeAssignments(q.table, nil, overrides)
		if err != nil {
			return nil, err
		}
		if len(assignments) == 0 {
			return nil, fmt.Errorf("rain: insert row %d produced no values", idx+1)
		}
		rows = append(rows, assignments)
	}

	return rows, nil
}

func validateInsertRowShape(rows [][]assignment) error {
	want := rows[0]
	wantColumns := make([]string, 0, len(want))
	for _, item := range want {
		wantColumns = append(wantColumns, item.column.ColumnDef().Name)
	}

	for rowIdx := 1; rowIdx < len(rows); rowIdx++ {
		row := rows[rowIdx]
		if len(row) != len(want) {
			return fmt.Errorf("rain: insert row %d targets %d columns, expected %d", rowIdx+1, len(row), len(want))
		}
		for colIdx := range row {
			if row[colIdx].column.ColumnDef().Name != wantColumns[colIdx] {
				return fmt.Errorf("rain: insert row %d column mismatch at position %d: got %q, expected %q", rowIdx+1, colIdx+1, row[colIdx].column.ColumnDef().Name, wantColumns[colIdx])
			}
		}
	}

	return nil
}

func (q *InsertQuery) writeConflictClause(ctx *compileContext) error {
	if q.conflict == nil {
		return nil
	}
	if q.conflict.action == insertConflictActionNone {
		return errors.New("rain: conflict action is required; call DoNothing(), DoUpdateSet(...), or DoUpdate()")
	}

	if q.dialect.Name() != "postgres" && q.dialect.Name() != "sqlite" && q.dialect.Name() != "mysql" {
		return fmt.Errorf("rain: insert conflict clauses are not implemented for %s dialect", q.dialect.Name())
	}

	if q.dialect.Name() == "mysql" {
		if len(q.conflict.targets) > 0 {
			return errors.New("rain: MySQL ON DUPLICATE KEY UPDATE cannot target specific conflict columns; call OnConflict() without columns")
		}
		if q.conflict.constraint != "" {
			return errors.New("rain: MySQL ON DUPLICATE KEY UPDATE does not support ON CONSTRAINT")
		}
		if q.conflict.targetWhere != nil {
			return errors.New("rain: MySQL ON DUPLICATE KEY UPDATE does not support target WHERE clause")
		}
		if q.conflict.where != nil {
			return errors.New("rain: MySQL ON DUPLICATE KEY UPDATE does not support DO UPDATE WHERE clause")
		}

		if q.conflict.action == insertConflictActionDoNothing {
			noopColumn, err := mysqlConflictNoopColumn(q.table)
			if err != nil {
				return err
			}
			ctx.writeString(" ON DUPLICATE KEY UPDATE ")
			ctx.writeQuotedIdentifier(noopColumn.Name)
			ctx.writeString(" = ")
			ctx.writeQuotedIdentifier(noopColumn.Name)
			return nil
		}
		if q.conflict.action == insertConflictActionDoUpdateSet {
			if len(q.conflict.updates) == 0 {
				return errors.New("rain: conflict DO UPDATE requires at least one update column")
			}
			if q.selectQuery != nil {
				return errors.New("rain: MySQL conflict DO UPDATE is not supported for INSERT ... SELECT")
			}
			ctx.writeString(" ON DUPLICATE KEY UPDATE ")
			updates, err := mergeAssignments(q.table, nil, q.conflict.updates)
			if err != nil {
				return err
			}
			for idx, item := range updates {
				if idx > 0 {
					ctx.writeString(", ")
				}
				ctx.writeQuotedIdentifier(item.column.ColumnDef().Name)
				ctx.writeString(" = ")
				if err := ctx.writeExpression(item.value); err != nil {
					return err
				}
			}
		}
		return nil
	}

	if q.conflict.constraint != "" {
		if q.dialect.Name() != "postgres" {
			return fmt.Errorf("rain: ON CONSTRAINT is not supported by %s dialect", q.dialect.Name())
		}
		if len(q.conflict.targets) > 0 {
			return errors.New("rain: ON CONFLICT cannot specify both targets and ON CONSTRAINT")
		}
		ctx.writeString(" ON CONFLICT ON CONSTRAINT ")
		ctx.writeQuotedIdentifier(q.conflict.constraint)
	} else {
		ctx.writeString(" ON CONFLICT")
		if len(q.conflict.targets) > 0 {
			ctx.writeString(" (")
			for idx, target := range q.conflict.targets {
				if idx > 0 {
					ctx.writeString(", ")
				}
				if col, ok := target.(schema.ColumnReference); ok {
					if err := validateColumnBelongsToTable(q.table, col.ColumnDef()); err != nil {
						return err
					}
				}
				if err := ctx.writeExpressionInContext(target, expressionContext{unqualified: true}); err != nil {
					return err
				}
			}
			ctx.writeByte(')')
		} else if q.conflict.action == insertConflictActionDoUpdateSet {
			return errors.New("rain: conflict DO UPDATE requires at least one target column or constraint")
		}

		if q.conflict.targetWhere != nil {
			if len(q.conflict.targets) == 0 {
				return errors.New("rain: conflict targetWhere requires at least one conflict target column")
			}
			ctx.writeString(" WHERE ")
			oldUseLiterals := ctx.useLiterals
			ctx.useLiterals = true
			err := ctx.writeExpressionInContext(q.conflict.targetWhere, expressionContext{unqualified: true})
			ctx.useLiterals = oldUseLiterals
			if err != nil {
				return err
			}
		}
	}

	switch q.conflict.action {
	case insertConflictActionDoNothing:
		ctx.writeString(" DO NOTHING")
	case insertConflictActionDoUpdateSet:
		if len(q.conflict.updates) == 0 {
			return errors.New("rain: conflict DO UPDATE requires at least one update column")
		}
		ctx.writeString(" DO UPDATE SET ")
		updates, err := mergeAssignments(q.table, nil, q.conflict.updates)
		if err != nil {
			return err
		}
		for idx, item := range updates {
			if idx > 0 {
				ctx.writeString(", ")
			}
			ctx.writeQuotedIdentifier(item.column.ColumnDef().Name)
			ctx.writeString(" = ")
			if err := ctx.writeExpression(item.value); err != nil {
				return err
			}
		}
		if q.conflict.where != nil {
			ctx.writeString(" WHERE ")
			if err := ctx.writePredicate(q.conflict.where); err != nil {
				return err
			}
		}
	}

	return nil
}

func mysqlConflictNoopColumn(table *schema.TableDef) (*schema.ColumnDef, error) {
	if table == nil {
		return nil, errors.New("rain: insert query requires a table")
	}

	if primaryKey, err := tablePrimaryKeyConstraint(table); err != nil {
		return nil, err
	} else if primaryKey != nil && len(primaryKey.Columns) > 0 {
		return primaryKey.Columns[0], nil
	}

	if primaryKeys := primaryKeyColumns(table); len(primaryKeys) > 0 {
		return primaryKeys[0], nil
	}

	if len(table.Columns) == 0 {
		return nil, fmt.Errorf("rain: table %q has no columns for MySQL conflict DO NOTHING", table.Name)
	}

	// MySQL requires an assignment after ON DUPLICATE KEY UPDATE. A table without
	// primary key metadata can still conflict on a unique index, so use the first
	// declared column only as a visible no-op target.
	return table.Columns[0], nil
}
