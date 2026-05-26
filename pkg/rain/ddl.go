package rain

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hyperlocalise/rain-orm/pkg/dialect"
	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

// CreateTableSQL compiles a typed schema definition into a CREATE TABLE statement.
func (db *DB) CreateTableSQL(table schema.TableReference) (string, error) {
	if db == nil || db.dialect == nil {
		return "", errors.New("rain: create table requires a configured dialect")
	}
	if table == nil || table.TableDef() == nil {
		return "", errors.New("rain: create table requires a non-nil table")
	}

	if table.TableDef().IsView {
		return createViewSQL(db.dialect, table.TableDef())
	}

	return createTableSQL(db.dialect, table.TableDef())
}

// CreateIndexesSQL compiles schema index metadata into CREATE INDEX statements.
func (db *DB) CreateIndexesSQL(table schema.TableReference) ([]string, error) {
	if db == nil || db.dialect == nil {
		return nil, errors.New("rain: create indexes requires a configured dialect")
	}
	if table == nil || table.TableDef() == nil {
		return nil, errors.New("rain: create indexes requires a non-nil table")
	}

	if table.TableDef().IsView {
		return nil, nil
	}

	return createIndexesSQL(db.dialect, table.TableDef())
}

// ColumnDefinitionSQL compiles one column definition without the ALTER TABLE wrapper.
func (db *DB) ColumnDefinitionSQL(table schema.TableReference, columnName string) (string, error) {
	if db == nil || db.dialect == nil {
		return "", errors.New("rain: column definition requires a configured dialect")
	}
	if table == nil || table.TableDef() == nil {
		return "", errors.New("rain: column definition requires a non-nil table")
	}

	tableDef := table.TableDef()
	column, ok := tableDef.ColumnByName(columnName)
	if !ok {
		return "", fmt.Errorf("rain: table %q has no column %q", tableDef.Name, columnName)
	}

	if tableDef.IsView {
		return db.dialect.QuoteIdentifier(column.Name) + " " + ddlColumnTypeSQL(db.dialect, column), nil
	}

	inlinePrimaryKey := false
	tablePrimaryKey, err := tablePrimaryKeyConstraint(tableDef)
	if err != nil {
		return "", err
	}
	primaryKeys := primaryKeyColumns(tableDef)
	if tablePrimaryKey == nil && len(primaryKeys) == 1 && primaryKeys[0] == column {
		inlinePrimaryKey = true
	}

	return columnDefinitionSQL(db.dialect, tableDef, column, inlinePrimaryKey)
}

// AddConstraintSQL compiles one ALTER TABLE ... ADD ... statement for a named table constraint.
func (db *DB) AddConstraintSQL(table schema.TableReference, constraintName string) (string, error) {
	if db == nil || db.dialect == nil {
		return "", errors.New("rain: add constraint requires a configured dialect")
	}
	if table == nil || table.TableDef() == nil {
		return "", errors.New("rain: add constraint requires a non-nil table")
	}

	tableDef := table.TableDef()
	if tableDef.IsView {
		return "", fmt.Errorf("rain: view %q does not support constraints", tableDef.Name)
	}

	for _, constraint := range tableDef.Constraints {
		if constraint.Name != constraintName {
			continue
		}
		definition, err := constraintDefinitionSQL(db.dialect, tableDef, constraint)
		if err != nil {
			return "", err
		}
		return "ALTER TABLE " + db.dialect.QuoteIdentifier(tableDef.Name) + " ADD " + definition, nil
	}

	return "", fmt.Errorf("rain: table %q has no constraint %q", tableDef.Name, constraintName)
}

// AddForeignKeySQL compiles one ALTER TABLE ... ADD ... statement for a named foreign key.
func (db *DB) AddForeignKeySQL(table schema.TableReference, foreignKeyName string) (string, error) {
	if db == nil || db.dialect == nil {
		return "", errors.New("rain: add foreign key requires a configured dialect")
	}
	if table == nil || table.TableDef() == nil {
		return "", errors.New("rain: add foreign key requires a non-nil table")
	}

	tableDef := table.TableDef()
	if tableDef.IsView {
		return "", fmt.Errorf("rain: view %q does not support foreign keys", tableDef.Name)
	}

	for _, foreignKey := range tableDef.ForeignKeys {
		if foreignKey.Name != foreignKeyName {
			continue
		}
		definition, err := foreignKeyConstraintSQL(db.dialect, foreignKey)
		if err != nil {
			return "", err
		}
		return "ALTER TABLE " + db.dialect.QuoteIdentifier(tableDef.Name) + " ADD " + definition, nil
	}

	return "", fmt.Errorf("rain: table %q has no foreign key %q", tableDef.Name, foreignKeyName)
}

// ColumnDefaultSQL renders one column default expression for snapshotting and migration checks.
func (db *DB) ColumnDefaultSQL(table schema.TableReference, columnName string) (string, error) {
	if db == nil || db.dialect == nil {
		return "", errors.New("rain: column default requires a configured dialect")
	}
	if table == nil || table.TableDef() == nil {
		return "", errors.New("rain: column default requires a non-nil table")
	}

	tableDef := table.TableDef()
	column, ok := tableDef.ColumnByName(columnName)
	if !ok {
		return "", fmt.Errorf("rain: table %q has no column %q", tableDef.Name, columnName)
	}
	if !column.HasDefault && column.DefaultSQL == "" {
		return "", nil
	}

	return columnDefaultSQL(db.dialect, column)
}

func createViewSQL(d dialect.Dialect, table *schema.TableDef) (string, error) {
	if d == nil {
		return "", errors.New("rain: create view requires a configured dialect")
	}
	if table == nil {
		return "", errors.New("rain: create view requires a non-nil table")
	}
	if !table.IsView {
		return "", fmt.Errorf("rain: table %q is not a view", table.Name)
	}
	if table.ViewQuery == nil {
		return "", fmt.Errorf("rain: view %q requires a defining query", table.Name)
	}

	ctx := newCompileContext(d)
	ctx.useLiterals = true
	if err := ctx.writeExpressionInContext(table.ViewQuery, expressionContext{noParens: true}); err != nil {
		return "", err
	}

	var builder strings.Builder
	builder.WriteString("CREATE VIEW ")
	builder.WriteString(d.QuoteIdentifier(table.Name))
	builder.WriteString(" AS ")
	builder.WriteString(ctx.String())

	return builder.String(), nil
}

func createTableSQL(d dialect.Dialect, table *schema.TableDef) (string, error) {
	if d == nil {
		return "", errors.New("rain: create table requires a configured dialect")
	}
	if table == nil {
		return "", errors.New("rain: create table requires a non-nil table")
	}

	var definitions []string
	tablePrimaryKey, err := tablePrimaryKeyConstraint(table)
	if err != nil {
		return "", err
	}
	primaryKeys := primaryKeyColumns(table)
	if tablePrimaryKey != nil && len(primaryKeys) > 0 {
		return "", fmt.Errorf("rain: table %q cannot mix column and table primary keys", table.Name)
	}
	inlinePrimaryKey := tablePrimaryKey == nil && len(primaryKeys) == 1

	for _, column := range table.Columns {
		definition, err := columnDefinitionSQL(d, table, column, inlinePrimaryKey && primaryKeys[0] == column)
		if err != nil {
			return "", err
		}
		definitions = append(definitions, definition)
	}

	if len(primaryKeys) > 1 {
		definitions = append(definitions, primaryKeyConstraintSQL(d, primaryKeys))
	}

	for _, foreignKey := range table.ForeignKeys {
		constraint, err := foreignKeyConstraintSQL(d, foreignKey)
		if err != nil {
			return "", err
		}
		definitions = append(definitions, constraint)
	}

	for _, constraint := range table.Constraints {
		definition, err := constraintDefinitionSQL(d, table, constraint)
		if err != nil {
			return "", err
		}
		definitions = append(definitions, definition)
	}

	var builder strings.Builder
	builder.WriteString("CREATE TABLE ")
	builder.WriteString(d.QuoteIdentifier(table.Name))
	builder.WriteString(" (\n")
	for idx, definition := range definitions {
		builder.WriteString("\t")
		builder.WriteString(definition)
		if idx < len(definitions)-1 {
			builder.WriteByte(',')
		}
		builder.WriteByte('\n')
	}
	builder.WriteByte(')')

	return builder.String(), nil
}

func createIndexesSQL(d dialect.Dialect, table *schema.TableDef) ([]string, error) {
	if d == nil {
		return nil, errors.New("rain: create indexes requires a configured dialect")
	}
	if table == nil {
		return nil, errors.New("rain: create indexes requires a non-nil table")
	}

	statements := make([]string, 0, len(table.Indexes))
	for _, index := range table.Indexes {
		if len(index.Columns) == 0 {
			return nil, fmt.Errorf("rain: index %q on table %q must reference at least one column", index.Name, table.Name)
		}

		var builder strings.Builder
		builder.WriteString("CREATE ")
		if index.Unique {
			builder.WriteString("UNIQUE ")
		}
		builder.WriteString("INDEX ")
		builder.WriteString(d.QuoteIdentifier(index.Name))
		builder.WriteString(" ON ")
		builder.WriteString(d.QuoteIdentifier(table.Name))
		builder.WriteString(" (")
		for idx, column := range index.Columns {
			if idx > 0 {
				builder.WriteString(", ")
			}
			builder.WriteString(d.QuoteIdentifier(column.Column.ColumnDef().Name))
			if column.Direction != "" {
				builder.WriteByte(' ')
				builder.WriteString(string(column.Direction))
			}
		}
		builder.WriteByte(')')
		if strings.TrimSpace(index.Where) != "" {
			builder.WriteString(" WHERE ")
			builder.WriteString(index.Where)
		}
		statements = append(statements, builder.String())
	}

	return statements, nil
}

func constraintDefinitionSQL(d dialect.Dialect, table *schema.TableDef, constraint schema.ConstraintDef) (string, error) {
	if strings.TrimSpace(constraint.Name) == "" {
		return "", fmt.Errorf("rain: constraint on table %q requires a non-empty name", table.Name)
	}

	prefix := "CONSTRAINT " + d.QuoteIdentifier(constraint.Name) + " "
	switch constraint.Type {
	case schema.ConstraintPrimaryKey:
		if len(constraint.Columns) == 0 {
			return "", fmt.Errorf("rain: primary key constraint %q on table %q must reference at least one column", constraint.Name, table.Name)
		}
		return prefix + primaryKeyConstraintSQL(d, constraint.Columns), nil
	case schema.ConstraintUnique:
		if len(constraint.Columns) == 0 {
			return "", fmt.Errorf("rain: unique constraint %q on table %q must reference at least one column", constraint.Name, table.Name)
		}
		return prefix + uniqueConstraintSQL(d, constraint.Columns), nil
	case schema.ConstraintCheck:
		if constraint.Check == nil {
			return "", fmt.Errorf("rain: check constraint %q on table %q requires a predicate", constraint.Name, table.Name)
		}
		checkSQL, err := predicateDDLSQL(d, table, constraint.Check)
		if err != nil {
			return "", fmt.Errorf("rain: check constraint %q on table %q: %w", constraint.Name, table.Name, err)
		}
		return prefix + "CHECK (" + checkSQL + ")", nil
	case schema.ConstraintForeignKey:
		if len(constraint.Columns) == 0 || len(constraint.ReferencedCols) == 0 || constraint.ReferencedTable == nil {
			return "", fmt.Errorf("rain: foreign key constraint %q on table %q requires source and referenced columns", constraint.Name, table.Name)
		}
		if len(constraint.Columns) != len(constraint.ReferencedCols) {
			return "", fmt.Errorf("rain: foreign key constraint %q on table %q must reference the same number of source and target columns", constraint.Name, table.Name)
		}
		if err := validateForeignKeyAction(constraint.OnDelete); err != nil {
			return "", fmt.Errorf("rain: foreign key constraint %q on table %q: %w", constraint.Name, table.Name, err)
		}
		if err := validateForeignKeyAction(constraint.OnUpdate); err != nil {
			return "", fmt.Errorf("rain: foreign key constraint %q on table %q: %w", constraint.Name, table.Name, err)
		}
		definition := foreignKeyColumnsConstraintSQL(d, constraint.Columns, constraint.ReferencedTable, constraint.ReferencedCols)
		if action := foreignKeyActionSQL(constraint.OnDelete); action != "" {
			definition += " ON DELETE " + action
		}
		if action := foreignKeyActionSQL(constraint.OnUpdate); action != "" {
			definition += " ON UPDATE " + action
		}
		return prefix + definition, nil
	default:
		return "", fmt.Errorf("rain: unsupported constraint type %q on table %q", constraint.Type, table.Name)
	}
}

func columnDefinitionSQL(d dialect.Dialect, table *schema.TableDef, column *schema.ColumnDef, inlinePrimaryKey bool) (string, error) {
	var parts []string
	parts = append(parts, d.QuoteIdentifier(column.Name))

	typeSQL := ddlColumnTypeSQL(d, column)
	parts = append(parts, typeSQL)

	if inlinePrimaryKey {
		parts = append(parts, "PRIMARY KEY")
	}
	if column.AutoIncrement && shouldEmitAutoIncrementKeyword(d, column, inlinePrimaryKey) {
		parts = append(parts, d.AutoIncrementKeyword())
	}
	if column.GeneratedExpr != nil {
		exprSQL, err := expressionDDLSQL(d, table, column.GeneratedExpr)
		if err != nil {
			return "", err
		}
		clause, err := d.GeneratedClause(exprSQL, column.GeneratedStored)
		if err != nil {
			return "", err
		}
		parts = append(parts, clause)
	}

	if !column.Nullable && !inlinePrimaryKey {
		parts = append(parts, "NOT NULL")
	}
	if column.Unique {
		parts = append(parts, "UNIQUE")
	}
	if column.HasDefault || column.DefaultSQL != "" {
		defaultSQL, err := columnDefaultSQL(d, column)
		if err != nil {
			return "", err
		}
		parts = append(parts, "DEFAULT", defaultSQL)
	}
	if len(column.Type.EnumValues) > 0 {
		parts = append(parts, enumConstraintSQL(d, column))
	}

	return strings.Join(parts, " "), nil
}

func ddlColumnTypeSQL(d dialect.Dialect, column *schema.ColumnDef) string {
	typeSQL := d.DataType(column.Type)

	if column.Type.DataType == schema.TypeVarChar && column.Type.Size > 0 && strings.EqualFold(typeSQL, "VARCHAR") {
		typeSQL = fmt.Sprintf("%s(%d)", typeSQL, column.Type.Size)
	}

	if d.Name() != "sqlite" && isTimestampColumn(column.Type) && column.Type.TimePrecision > 0 && !strings.Contains(typeSQL, "(") {
		typeSQL = fmt.Sprintf("%s(%d)", typeSQL, column.Type.TimePrecision)
	}

	if column.AutoIncrement && d.Name() == "sqlite" && column.Type.DataType == schema.TypeBigSerial {
		return "INTEGER"
	}

	return typeSQL
}

func shouldEmitAutoIncrementKeyword(d dialect.Dialect, column *schema.ColumnDef, inlinePrimaryKey bool) bool {
	if !column.AutoIncrement {
		return false
	}
	if !inlinePrimaryKey {
		return false
	}
	switch d.Name() {
	case "postgres":
		return !isPostgresSerialType(column.Type.DataType)
	case "sqlite":
		return true
	default:
		return true
	}
}

func isPostgresSerialType(dataType schema.DataType) bool {
	switch dataType {
	case schema.TypeBigSerial, schema.TypeSerial, schema.TypeSmallSerial:
		return true
	default:
		return false
	}
}

func columnDefaultSQL(d dialect.Dialect, column *schema.ColumnDef) (string, error) {
	if column.DefaultSQL != "" {
		return column.DefaultSQL, nil
	}

	switch value := column.Default.(type) {
	case nil:
		return "NULL", nil
	case string:
		return quoteStringLiteral(value), nil
	case bool:
		return d.BooleanLiteral(value), nil
	case int:
		return strconv.Itoa(value), nil
	case int8:
		return strconv.FormatInt(int64(value), 10), nil
	case int16:
		return strconv.FormatInt(int64(value), 10), nil
	case int32:
		return strconv.FormatInt(int64(value), 10), nil
	case int64:
		return strconv.FormatInt(value, 10), nil
	case uint:
		return strconv.FormatUint(uint64(value), 10), nil
	case uint8:
		return strconv.FormatUint(uint64(value), 10), nil
	case uint16:
		return strconv.FormatUint(uint64(value), 10), nil
	case uint32:
		return strconv.FormatUint(uint64(value), 10), nil
	case uint64:
		return strconv.FormatUint(value, 10), nil
	case float32:
		return strconv.FormatFloat(float64(value), 'f', -1, 32), nil
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64), nil
	case time.Time:
		return quoteStringLiteral(value.UTC().Format(time.RFC3339Nano)), nil
	case []byte:
		return quoteStringLiteral(string(value)), nil
	default:
		return "", fmt.Errorf("rain: unsupported default value type %T for column %q", value, column.Name)
	}
}

func enumConstraintSQL(d dialect.Dialect, column *schema.ColumnDef) string {
	values := make([]string, 0, len(column.Type.EnumValues))
	for _, value := range column.Type.EnumValues {
		values = append(values, quoteStringLiteral(value))
	}

	return fmt.Sprintf("CHECK (%s IN (%s))", d.QuoteIdentifier(column.Name), strings.Join(values, ", "))
}

func foreignKeyConstraintSQL(d dialect.Dialect, foreignKey schema.ForeignKeyDef) (string, error) {
	if foreignKey.Column == nil || foreignKey.ReferencedTable == nil || foreignKey.ReferencedColumn == nil {
		return "", errors.New("rain: foreign key requires source and referenced columns")
	}

	definition := foreignKeyColumnsConstraintSQL(
		d,
		[]*schema.ColumnDef{foreignKey.Column},
		foreignKey.ReferencedTable,
		[]*schema.ColumnDef{foreignKey.ReferencedColumn},
	)
	if err := validateForeignKeyAction(foreignKey.OnDelete); err != nil {
		return "", fmt.Errorf("rain: foreign key %q: %w", foreignKey.Column.Name, err)
	}
	if err := validateForeignKeyAction(foreignKey.OnUpdate); err != nil {
		return "", fmt.Errorf("rain: foreign key %q: %w", foreignKey.Column.Name, err)
	}
	if action := foreignKeyActionSQL(foreignKey.OnDelete); action != "" {
		definition += " ON DELETE " + action
	}
	if action := foreignKeyActionSQL(foreignKey.OnUpdate); action != "" {
		definition += " ON UPDATE " + action
	}
	if foreignKey.Name != "" {
		definition = "CONSTRAINT " + d.QuoteIdentifier(foreignKey.Name) + " " + definition
	}

	return definition, nil
}

func primaryKeyConstraintSQL(d dialect.Dialect, columns []*schema.ColumnDef) string {
	quoted := make([]string, 0, len(columns))
	for _, column := range columns {
		quoted = append(quoted, d.QuoteIdentifier(column.Name))
	}
	return fmt.Sprintf("PRIMARY KEY (%s)", strings.Join(quoted, ", "))
}

func uniqueConstraintSQL(d dialect.Dialect, columns []*schema.ColumnDef) string {
	quoted := make([]string, 0, len(columns))
	for _, column := range columns {
		quoted = append(quoted, d.QuoteIdentifier(column.Name))
	}
	return fmt.Sprintf("UNIQUE (%s)", strings.Join(quoted, ", "))
}

func foreignKeyColumnsConstraintSQL(d dialect.Dialect, columns []*schema.ColumnDef, referencedTable *schema.TableDef, referencedCols []*schema.ColumnDef) string {
	return fmt.Sprintf(
		"FOREIGN KEY (%s) REFERENCES %s (%s)",
		quotedColumnsSQL(d, columns),
		d.QuoteIdentifier(referencedTable.Name),
		quotedColumnsSQL(d, referencedCols),
	)
}

func quotedColumnsSQL(d dialect.Dialect, columns []*schema.ColumnDef) string {
	quoted := make([]string, 0, len(columns))
	for _, column := range columns {
		quoted = append(quoted, d.QuoteIdentifier(column.Name))
	}
	return strings.Join(quoted, ", ")
}

func foreignKeyActionSQL(action schema.ForeignKeyAction) string {
	switch action {
	case "":
		return ""
	case schema.ForeignKeyActionNoAction,
		schema.ForeignKeyActionRestrict,
		schema.ForeignKeyActionCascade,
		schema.ForeignKeyActionSetNull,
		schema.ForeignKeyActionSetDefault:
		return string(action)
	default:
		return ""
	}
}

func validateForeignKeyAction(action schema.ForeignKeyAction) error {
	switch action {
	case "",
		schema.ForeignKeyActionNoAction,
		schema.ForeignKeyActionRestrict,
		schema.ForeignKeyActionCascade,
		schema.ForeignKeyActionSetNull,
		schema.ForeignKeyActionSetDefault:
		return nil
	default:
		return fmt.Errorf("unsupported foreign key action %q", action)
	}
}

func tablePrimaryKeyConstraint(table *schema.TableDef) (*schema.ConstraintDef, error) {
	var primaryKey *schema.ConstraintDef
	for idx := range table.Constraints {
		if table.Constraints[idx].Type != schema.ConstraintPrimaryKey {
			continue
		}
		if primaryKey != nil {
			return nil, fmt.Errorf("rain: table %q cannot declare more than one table primary key", table.Name)
		}
		primaryKey = &table.Constraints[idx]
	}
	return primaryKey, nil
}

func primaryKeyColumns(table *schema.TableDef) []*schema.ColumnDef {
	var primaryKeys []*schema.ColumnDef
	for _, column := range table.Columns {
		if column.PrimaryKey {
			primaryKeys = append(primaryKeys, column)
		}
	}
	return primaryKeys
}

func isTimestampColumn(columnType schema.ColumnType) bool {
	if columnType.TimestampKind != schema.TimestampKindUnspecified {
		return true
	}

	switch columnType.DataType {
	case schema.TypeTimestamp, schema.TypeTimestampTZ:
		return true
	default:
		return false
	}
}

func quoteStringLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func predicateDDLSQL(d dialect.Dialect, table *schema.TableDef, predicate schema.Predicate) (string, error) {
	return expressionDDLSQL(d, table, predicate)
}

func expressionDDLSQL(d dialect.Dialect, table *schema.TableDef, expr schema.Expression) (string, error) {
	switch value := expr.(type) {
	case schema.ColumnReference:
		column := value.ColumnDef()
		if column == nil {
			return "", errors.New("column expression requires metadata")
		}
		if column.Table != table {
			return "", fmt.Errorf("column %q must belong to table %q", column.Name, table.Name)
		}
		return d.QuoteIdentifier(column.Name), nil
	case schema.ValueExpr:
		return literalDDLSQL(d, value.Value)
	case schema.ComparisonExpr:
		left, err := expressionDDLSQL(d, table, value.Left)
		if err != nil {
			return "", err
		}
		right, err := expressionDDLSQL(d, table, value.Right)
		if err != nil {
			return "", err
		}
		return left + " " + value.Operator + " " + right, nil
	case schema.InExpr:
		if len(value.Values) == 0 {
			return "", errors.New("IN predicate requires at least one value")
		}
		left, err := expressionDDLSQL(d, table, value.Left)
		if err != nil {
			return "", err
		}
		items := make([]string, 0, len(value.Values))
		for _, item := range value.Values {
			rendered, err := expressionDDLSQL(d, table, item)
			if err != nil {
				return "", err
			}
			items = append(items, rendered)
		}
		return left + " IN (" + strings.Join(items, ", ") + ")", nil
	case schema.NullCheckExpr:
		inner, err := expressionDDLSQL(d, table, value.Expr)
		if err != nil {
			return "", err
		}
		if value.Negated {
			return inner + " IS NOT NULL", nil
		}
		return inner + " IS NULL", nil
	case schema.LogicalExpr:
		parts := make([]string, 0, len(value.Exprs))
		for _, part := range value.Exprs {
			rendered, err := predicateDDLSQL(d, table, part)
			if err != nil {
				return "", err
			}
			parts = append(parts, rendered)
		}
		return "(" + strings.Join(parts, " "+value.Operator+" ") + ")", nil
	case schema.RawExpr:
		if len(value.Args) != 0 {
			return "", errors.New("raw SQL CHECK expressions cannot contain args")
		}
		return value.SQL, nil
	default:
		return "", fmt.Errorf("unsupported DDL expression type %T", expr)
	}
}

func literalDDLSQL(d dialect.Dialect, value any) (string, error) {
	switch typed := value.(type) {
	case nil:
		return "NULL", nil
	case bool:
		return d.BooleanLiteral(typed), nil
	case string:
		return quoteStringLiteral(typed), nil
	case int:
		return strconv.Itoa(typed), nil
	case int8:
		return strconv.FormatInt(int64(typed), 10), nil
	case int16:
		return strconv.FormatInt(int64(typed), 10), nil
	case int32:
		return strconv.FormatInt(int64(typed), 10), nil
	case int64:
		return strconv.FormatInt(typed, 10), nil
	case uint:
		return strconv.FormatUint(uint64(typed), 10), nil
	case uint8:
		return strconv.FormatUint(uint64(typed), 10), nil
	case uint16:
		return strconv.FormatUint(uint64(typed), 10), nil
	case uint32:
		return strconv.FormatUint(uint64(typed), 10), nil
	case uint64:
		return strconv.FormatUint(typed, 10), nil
	case float32:
		return strconv.FormatFloat(float64(typed), 'f', -1, 32), nil
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64), nil
	case time.Time:
		return quoteStringLiteral(typed.UTC().Format(time.RFC3339Nano)), nil
	case []byte:
		return quoteStringLiteral(string(typed)), nil
	default:
		return "", fmt.Errorf("unsupported DDL literal type %T", value)
	}
}
