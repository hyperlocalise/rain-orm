// Package dialect provides database-specific implementations for Rain ORM.
// Implement this interface to add support for new database engines.
package dialect

// Dialect represents a database-specific SQL dialect.
type Dialect interface {
	// Name returns the dialect name (e.g., "postgres", "mysql", "sqlite").
	Name() string

	// Features returns the set of SQL capabilities supported by the dialect.
	Features() Feature

	// QuoteIdentifier quotes a database identifier (table/column name).
	QuoteIdentifier(name string) string

	// Placeholder returns the parameter placeholder for the nth parameter.
	Placeholder(n int) string

	// DataType returns the SQL type for a given schema type.
	DataType(typ string, size int) string

	// DefaultValue returns the SQL representation of a default value.
	DefaultValue(value interface{}) string

	// AutoIncrementKeyword returns the auto-increment keyword.
	AutoIncrementKeyword() string

	// LimitOffset returns the LIMIT/OFFSET clause SQL.
	LimitOffset(limit, offset int) string

	// UpsertClause returns the UPSERT syntax (INSERT ... ON CONFLICT, etc.).
	UpsertClause(table string, conflictCols []string, updateCols []string) string

	// CurrentTimestamp returns the SQL for current timestamp.
	CurrentTimestamp() string

	// BooleanLiteral returns the SQL boolean literal (TRUE/FALSE or 1/0).
	BooleanLiteral(v bool) string
}

// BaseDialect provides common implementations.
type BaseDialect struct{}

// Features returns the default shared feature set.
func (d *BaseDialect) Features() Feature {
	return 0
}

// DataType returns default SQL type mapping.
func (d *BaseDialect) DataType(typ string, size int) string {
	switch typ {
	case "smallint":
		return "SMALLINT"
	case "string":
		if size > 0 {
			return "VARCHAR"
		}
		return "TEXT"
	case "int", "int32", "integer":
		return "INTEGER"
	case "int64":
		return "BIGINT"
	case "decimal":
		return "DECIMAL"
	case "float32":
		return "REAL"
	case "float64":
		return "DOUBLE PRECISION"
	case "bool":
		return "BOOLEAN"
	case "date":
		return "DATE"
	case "timestamp":
		return "TIMESTAMP"
	case "time":
		return "TIMESTAMP"
	case "json":
		return "JSON"
	case "jsonb":
		return "JSONB"
	case "uuid":
		return "UUID"
	case "bytes":
		return "BLOB"
	case "enum":
		return "VARCHAR"
	default:
		return typ
	}
}

// DefaultValue returns default value representation.
func (d *BaseDialect) DefaultValue(value interface{}) string {
	return "DEFAULT"
}

// UpsertClause returns generic upsert syntax.
func (d *BaseDialect) UpsertClause(table string, conflictCols []string, updateCols []string) string {
	return ""
}
