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

	return createIndexesSQL(db.dialect, table.TableDef())
}

func createTableSQL(d dialect.Dialect, table *schema.TableDef) (string, error) {
	if d == nil {
		return "", errors.New("rain: create table requires a configured dialect")
	}
	if table == nil {
		return "", errors.New("rain: create table requires a non-nil table")
	}

	var definitions []string
	primaryKeys := primaryKeyColumns(table)
	inlinePrimaryKey := len(primaryKeys) == 1

	for _, column := range table.Columns {
		definition, err := columnDefinitionSQL(d, column, inlinePrimaryKey && primaryKeys[0] == column)
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

func columnDefinitionSQL(d dialect.Dialect, column *schema.ColumnDef, inlinePrimaryKey bool) (string, error) {
	var parts []string
	parts = append(parts, d.QuoteIdentifier(column.Name))

	typeSQL, err := columnTypeSQL(d, column)
	if err != nil {
		return "", err
	}
	parts = append(parts, typeSQL)

	if inlinePrimaryKey {
		parts = append(parts, "PRIMARY KEY")
	}
	if column.AutoIncrement && usesAutoIncrementKeyword(d, column) {
		parts = append(parts, d.AutoIncrementKeyword())
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

func columnTypeSQL(d dialect.Dialect, column *schema.ColumnDef) (string, error) {
	typeSQL := d.DataType(column.Type)

	if column.Type.DataType == schema.TypeVarChar && column.Type.Size > 0 && strings.EqualFold(typeSQL, "VARCHAR") {
		typeSQL = fmt.Sprintf("%s(%d)", typeSQL, column.Type.Size)
	}

	if d.Name() != "sqlite" && isTimestampColumn(column.Type) && column.Type.TimePrecision > 0 && !strings.Contains(typeSQL, "(") {
		typeSQL = fmt.Sprintf("%s(%d)", typeSQL, column.Type.TimePrecision)
	}

	if column.AutoIncrement && d.Name() == "sqlite" && column.Type.DataType == schema.TypeBigSerial {
		return "INTEGER", nil
	}

	return typeSQL, nil
}

func usesAutoIncrementKeyword(d dialect.Dialect, column *schema.ColumnDef) bool {
	if !column.AutoIncrement {
		return false
	}
	if column.Type.DataType != schema.TypeBigSerial {
		return true
	}

	switch d.Name() {
	case "postgres":
		return false
	case "sqlite":
		return true
	default:
		return true
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

	return fmt.Sprintf(
		"FOREIGN KEY (%s) REFERENCES %s (%s)",
		d.QuoteIdentifier(foreignKey.Column.Name),
		d.QuoteIdentifier(foreignKey.ReferencedTable.Name),
		d.QuoteIdentifier(foreignKey.ReferencedColumn.Name),
	), nil
}

func primaryKeyConstraintSQL(d dialect.Dialect, columns []*schema.ColumnDef) string {
	quoted := make([]string, 0, len(columns))
	for _, column := range columns {
		quoted = append(quoted, d.QuoteIdentifier(column.Name))
	}
	return fmt.Sprintf("PRIMARY KEY (%s)", strings.Join(quoted, ", "))
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
