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
	runner        queryRunner
	dialect       dialect.Dialect
	table         *schema.TableDef
	model         any
	models        any
	values        []assignment
	rows          []map[schema.ColumnReference]any
	selectQuery   *SelectQuery
	columns       []schema.ColumnReference
	returning     []schema.Expression
	conflict      *insertConflictClause
	ctes          []cteDefinition
	defaultValues bool
}

type insertConflictAction uint8

const (
	insertConflictActionNone insertConflictAction = iota
	insertConflictActionDoNothing
	insertConflictActionDoUpdateSet
)

type insertConflictClause struct {
	columns     []schema.ColumnReference
	constraint  string
	targetWhere []schema.Predicate
	action      insertConflictAction
	updates     []assignment
	updateWhere []schema.Predicate
}

type excludedColumn struct {
	schema.ExpressionMarker
	column schema.ColumnReference
}

// InsertConflictBuilder configures conflict behavior for INSERT statements.
type InsertConflictBuilder struct {
	query *InsertQuery
}

// OnConstraint specifies a named constraint as the conflict target.
func (b *InsertConflictBuilder) OnConstraint(name string) *InsertConflictBuilder {
	b.query.conflict.constraint = name
	return b
}

// Where adds a filter to the conflict target (partial index).
func (b *InsertConflictBuilder) Where(predicate schema.Predicate) *InsertConflictBuilder {
	b.query.conflict.targetWhere = append(b.query.conflict.targetWhere, predicate)
	return b
}

// With appends a common table expression definition.
func (b *InsertConflictBuilder) With(name string, query *SelectQuery) *InsertConflictBuilder {
	b.query.With(name, query)
	return b
}

// InsertConflictUpdateBuilder configures the DO UPDATE SET clause.
type InsertConflictUpdateBuilder struct {
	query *InsertQuery
}

// Set adds a custom assignment to the DO UPDATE SET clause.
func (b *InsertConflictUpdateBuilder) Set(column schema.ColumnReference, value any) *InsertConflictUpdateBuilder {
	b.query.conflict.updates = append(b.query.conflict.updates, assignment{column: column, value: value})
	return b
}

// Where adds a filter to the DO UPDATE SET clause.
func (b *InsertConflictUpdateBuilder) Where(predicate schema.Predicate) *InsertConflictUpdateBuilder {
	b.query.conflict.updateWhere = append(b.query.conflict.updateWhere, predicate)
	return b
}

// With appends a common table expression definition.
func (b *InsertConflictUpdateBuilder) With(name string, query *SelectQuery) *InsertConflictUpdateBuilder {
	b.query.With(name, query)
	return b
}

// Returning adds RETURNING expressions to the query.
func (b *InsertConflictUpdateBuilder) Returning(exprs ...schema.Expression) *InsertQuery {
	return b.query.Returning(exprs...)
}

// Prepare compiles and prepares the INSERT query.
func (b *InsertConflictUpdateBuilder) Prepare(ctx context.Context) (*PreparedInsertQuery, error) {
	return b.query.Prepare(ctx)
}

// ToSQL compiles the insert into SQL and args.
func (b *InsertConflictUpdateBuilder) ToSQL() (string, []any, error) {
	return b.query.ToSQL()
}

// Exec executes the INSERT query.
func (b *InsertConflictUpdateBuilder) Exec(ctx context.Context) (sql.Result, error) {
	return b.query.Exec(ctx)
}

// Scan executes an INSERT ... RETURNING query and scans one row into dest.
func (b *InsertConflictUpdateBuilder) Scan(ctx context.Context, dest any) error {
	return b.query.Scan(ctx, dest)
}

// With appends a common table expression definition.
func (q *InsertQuery) With(name string, query *SelectQuery) *InsertQuery {
	q.ctes = append(q.ctes, cteDefinition{name: name, query: query})
	return q
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
	q.values = append(q.values, assignment{column: column, value: value})
	return q
}

// Values appends explicit row value sets for a bulk insert.
func (q *InsertQuery) Values(rows ...map[schema.ColumnReference]any) *InsertQuery {
	q.rows = append(q.rows, rows...)
	return q
}

// DefaultValues configures the INSERT to use default values for all columns.
// PostgreSQL and SQLite render "DEFAULT VALUES", while MySQL renders "() VALUES ()".
func (q *InsertQuery) DefaultValues() *InsertQuery {
	q.defaultValues = true
	return q
}

// OnConflict starts an upsert clause for PostgreSQL and SQLite dialects.
func (q *InsertQuery) OnConflict(columns ...schema.ColumnReference) *InsertConflictBuilder {
	q.conflict = &insertConflictClause{columns: columns}
	return &InsertConflictBuilder{query: q}
}

// DoNothing configures ON CONFLICT ... DO NOTHING.
func (b *InsertConflictBuilder) DoNothing() *InsertQuery {
	b.query.conflict.action = insertConflictActionDoNothing
	return b.query
}

// DoUpdateSet configures ON CONFLICT ... DO UPDATE SET.
// If columns are provided, they are automatically assigned using EXCLUDED values.
// Returns an InsertConflictUpdateBuilder for further customization.
func (b *InsertConflictBuilder) DoUpdateSet(columns ...schema.ColumnReference) *InsertConflictUpdateBuilder {
	b.query.conflict.action = insertConflictActionDoUpdateSet
	for _, col := range columns {
		b.query.conflict.updates = append(b.query.conflict.updates, assignment{
			column: col,
			value:  excludedColumn{column: col},
		})
	}
	return &InsertConflictUpdateBuilder{query: b.query}
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
		// Handles Model, Models, Values, and DefaultValues.
		if err := q.writeValuesSQL(ctx); err != nil {
			return compiledQuery{}, err
		}
	}

	return ctx.compiledQuery(), ctx.err
}

func (q *InsertQuery) writeValuesSQL(ctx *compileContext) error {
	if err := writeCTEs(ctx, q.ctes, "insert"); err != nil {
		return err
	}
	prevSkip := ctx.skipCTEs
	ctx.skipCTEs = true
	defer func() { ctx.skipCTEs = prevSkip }()

	if q.defaultValues {
		if err := q.validateSources(); err != nil {
			return err
		}
		ctx.writeString("INSERT INTO ")
		ctx.writeTableName(q.table)
		if ctx.dialect.Name() == "mysql" {
			ctx.writeString(" () VALUES ()")
		} else {
			ctx.writeString(" DEFAULT VALUES")
		}
	} else {
		if err := q.validateSources(); err != nil {
			return err
		}

		ctx.writeString("INSERT INTO ")
		ctx.writeTableName(q.table)

		if q.models != nil {
			if err := q.writeModelsSQL(ctx); err != nil {
				return err
			}
		} else if len(q.rows) > 0 {
			if err := q.writeMapRowsSQL(ctx); err != nil {
				return err
			}
		} else {
			// Single model and/or explicit .Set() values.
			if err := q.writeSingleRowSQL(ctx); err != nil {
				return err
			}
		}
	}

	if err := q.writeConflictClause(ctx); err != nil {
		return err
	}

	return ctx.writeReturning(q.returning, q.returningClause())
}

func (q *InsertQuery) writeModelsSQL(ctx *compileContext) error {
	value := reflect.ValueOf(q.models)
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return errors.New("rain: insert models cannot be nil")
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Slice && value.Kind() != reflect.Array {
		return errors.New("rain: Models expects a slice or array")
	}
	if value.Len() == 0 {
		return errors.New("rain: Models expects at least one model")
	}

	modelType := value.Type().Elem()
	plan, err := lookupModelAssignmentPlan(q.table, modelType)
	if err != nil {
		return err
	}

	// We establish the set of columns from the first model.
	// NOTE: We only include columns that are actually present in the model
	// (determined by lookupModelAssignmentPlan) and not skipped by fieldValueForInsert.
	firstModelVal := value.Index(0)
	for firstModelVal.Kind() == reflect.Pointer {
		if firstModelVal.IsNil() {
			return errors.New("rain: insert model pointer cannot be nil")
		}
		firstModelVal = firstModelVal.Elem()
	}

	type activeField struct {
		column *schema.ColumnDef
		index  []int
	}
	activeFields := make([]activeField, 0, len(plan.fields))
	for _, f := range plan.fields {
		fieldValue := firstModelVal.FieldByIndex(f.index)
		if _, include := fieldValueForInsert(f.column, fieldValue, true); include {
			activeFields = append(activeFields, activeField(f))
		}
	}

	if len(activeFields) == 0 {
		return errors.New("rain: insert models produced no values")
	}

	ctx.writeString(" (")
	for idx, f := range activeFields {
		if idx > 0 {
			ctx.writeString(", ")
		}
		ctx.writeQuotedIdentifier(f.column.Name)
	}
	ctx.writeString(") VALUES ")

	ctx.ensureArgsCapacity(value.Len() * len(activeFields))

	for i := 0; i < value.Len(); i++ {
		if i > 0 {
			ctx.writeString(", ")
		}

		rowVal := value.Index(i)
		for rowVal.Kind() == reflect.Pointer {
			if rowVal.IsNil() {
				return fmt.Errorf("rain: insert row %d model pointer cannot be nil", i+1)
			}
			rowVal = rowVal.Elem()
		}

		ctx.writeByte('(')
		rowActiveCount := 0
		for _, f := range plan.fields {
			fieldVal := rowVal.FieldByIndex(f.index)
			if _, include := fieldValueForInsert(f.column, fieldVal, true); include {
				rowActiveCount++
			}
		}
		if rowActiveCount != len(activeFields) {
			return fmt.Errorf("rain: insert row %d targets %d columns, expected %d", i+1, rowActiveCount, len(activeFields))
		}

		for j, f := range activeFields {
			if j > 0 {
				ctx.writeString(", ")
			}
			fieldVal := rowVal.FieldByIndex(f.index)
			resolved, include := fieldValueForInsert(f.column, fieldVal, true)
			if !include {
				return fmt.Errorf("rain: insert row %d is missing column %q", i+1, f.column.Name)
			}
			if err := ctx.writeAny(resolved); err != nil {
				return err
			}
		}
		ctx.writeByte(')')
	}

	return nil
}

func (q *InsertQuery) writeMapRowsSQL(ctx *compileContext) error {
	if len(q.rows) == 0 {
		return errors.New("rain: insert values produced no rows")
	}

	// Establish columns from the first map row.
	firstRow := q.rows[0]
	if len(firstRow) == 0 {
		return errors.New("rain: insert row 1 has no values")
	}

	// We use mergeAssignments (with base=nil) to get the ordered set of columns
	// from the first row's map.
	overrides := make([]assignment, 0, len(firstRow))
	for column, value := range firstRow {
		overrides = append(overrides, assignment{column: column, value: value})
	}
	firstAssignments, err := mergeAssignments(q.table, nil, overrides)
	if err != nil {
		return err
	}

	ctx.writeString(" (")
	for idx, item := range firstAssignments {
		if idx > 0 {
			ctx.writeString(", ")
		}
		ctx.writeQuotedIdentifier(item.column.ColumnDef().Name)
	}
	ctx.writeString(") VALUES ")

	ctx.ensureArgsCapacity(len(q.rows) * len(firstAssignments))

	for rowIdx, row := range q.rows {
		if rowIdx > 0 {
			ctx.writeString(", ")
		}

		if len(row) != len(firstAssignments) {
			return fmt.Errorf("rain: insert row %d targets %d columns, expected %d", rowIdx+1, len(row), len(firstAssignments))
		}

		ctx.writeByte('(')
		for idx, item := range firstAssignments {
			if idx > 0 {
				ctx.writeString(", ")
			}
			col := item.column
			val, ok := row[col]
			if !ok {
				return fmt.Errorf("rain: insert row %d column mismatch: missing %q", rowIdx+1, col.ColumnDef().Name)
			}
			if err := ctx.writeAny(val); err != nil {
				return err
			}
		}
		ctx.writeByte(')')
	}

	return nil
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

func (q *InsertQuery) writeSingleRowSQL(ctx *compileContext) error {
	// Single row might be from a model and/or explicit .Set() values.
	// We use assignmentsFromModelAndSet which is already
	// relatively efficient for a single row.
	row, err := q.assignmentsFromModelAndSet()
	if err != nil {
		return err
	}

	ctx.writeString(" (")
	for idx, item := range row {
		if idx > 0 {
			ctx.writeString(", ")
		}
		ctx.writeQuotedIdentifier(item.column.ColumnDef().Name)
	}
	ctx.writeString(") VALUES (")

	ctx.ensureArgsCapacity(len(row))

	for idx, item := range row {
		if idx > 0 {
			ctx.writeString(", ")
		}
		if err := ctx.writeAny(item.value); err != nil {
			return err
		}
	}
	ctx.writeByte(')')

	return nil
}

func (q *InsertQuery) writeSelectSQL(ctx *compileContext) error {
	if err := writeCTEs(ctx, q.ctes, "insert"); err != nil {
		return err
	}
	prevSkip := ctx.skipCTEs
	ctx.skipCTEs = true
	defer func() { ctx.skipCTEs = prevSkip }()

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
	if q.defaultValues {
		sources++
	}

	if sources == 0 {
		return errors.New("rain: insert query requires a data source: Model/Set, Models, Values, Select, or DefaultValues")
	}
	if sources > 1 {
		return errors.New("rain: insert query requires exactly one data source: Model/Set, Models, Values, Select, or DefaultValues")
	}

	return nil
}

func (q *InsertQuery) writeConflictClause(ctx *compileContext) error {
	if q.conflict == nil {
		return nil
	}
	if q.conflict.action == insertConflictActionNone {
		return errors.New("rain: conflict action is required; call DoNothing() or DoUpdateSet(...)")
	}

	if q.dialect.Name() != "postgres" && q.dialect.Name() != "sqlite" && q.dialect.Name() != "mysql" {
		return fmt.Errorf("rain: insert conflict clauses are not implemented for %s dialect", q.dialect.Name())
	}

	if q.dialect.Name() == "mysql" {
		if len(q.conflict.columns) > 0 || q.conflict.constraint != "" || len(q.conflict.targetWhere) > 0 {
			return errors.New("rain: MySQL ON DUPLICATE KEY UPDATE does not support conflict targets (columns, constraints, or WHERE); call OnConflict() without modifiers")
		}
		if len(q.conflict.updateWhere) > 0 {
			return errors.New("rain: MySQL ON DUPLICATE KEY UPDATE does not support WHERE filters")
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
			ctx.writeString(" ON DUPLICATE KEY UPDATE ")
			for idx, item := range q.conflict.updates {
				if err := validateAssignmentTarget(q.table, item); err != nil {
					return err
				}
				if idx > 0 {
					ctx.writeString(", ")
				}
				ctx.writeColumnName(item.column)
				ctx.writeString(" = ")
				if err := ctx.writeAny(item.value); err != nil {
					return err
				}
			}
		}
		return nil
	}

	if len(q.conflict.columns) == 0 && q.conflict.constraint == "" {
		return errors.New("rain: conflict clause requires at least one target (columns or constraint)")
	}

	ctx.writeString(" ON CONFLICT")
	if q.conflict.constraint != "" {
		ctx.writeString(" ON CONSTRAINT ")
		ctx.writeQuotedIdentifier(q.conflict.constraint)
	} else if len(q.conflict.columns) > 0 {
		ctx.writeString(" (")
		for idx, col := range q.conflict.columns {
			if err := validateColumnBelongsToTable(q.table, col.ColumnDef()); err != nil {
				return err
			}
			if idx > 0 {
				ctx.writeString(", ")
			}
			ctx.writeColumnName(col)
		}
		ctx.writeByte(')')
	}

	if len(q.conflict.targetWhere) > 0 {
		ctx.writeString(" WHERE ")
		if err := ctx.writeJoinedPredicates(q.conflict.targetWhere, true); err != nil {
			return err
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
		for idx, item := range q.conflict.updates {
			if err := validateAssignmentTarget(q.table, item); err != nil {
				return err
			}
			if idx > 0 {
				ctx.writeString(", ")
			}
			ctx.writeColumnName(item.column)
			ctx.writeString(" = ")
			if err := ctx.writeAny(item.value); err != nil {
				return err
			}
		}
		if len(q.conflict.updateWhere) > 0 {
			ctx.writeString(" WHERE ")
			if err := ctx.writeJoinedPredicates(q.conflict.updateWhere, true); err != nil {
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
