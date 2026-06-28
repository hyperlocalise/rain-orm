// Package dialect provides database-specific implementations for Rain ORM.
// Implement this interface to add support for new database engines.
package dialect

import (
	"fmt"
	"strings"

	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

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

	// DataType returns the SQL type for a given schema column type.
	DataType(columnType schema.ColumnType) string

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

	// GeneratedClause returns the SQL for a generated column clause.
	GeneratedClause(expr string, stored bool) (string, error)

	// IdentityClause returns the SQL for a PostgreSQL identity column clause.
	IdentityClause(always bool) (string, error)
}

// BaseDialect provides common implementations.
type BaseDialect struct{}

// Features returns the default shared feature set.
func (d *BaseDialect) Features() Feature {
	return 0
}

// DataType returns default SQL type mapping.
func (d *BaseDialect) DataType(columnType schema.ColumnType) string {
	typ := normalizeType(columnType.DataType)

	switch typ {
	case "bigserial":
		return "BIGSERIAL"
	case "smallint":
		return "SMALLINT"
	case "string", "varchar":
		if columnType.Size > 0 {
			return "VARCHAR"
		}
		return "TEXT"
	case "text":
		return "TEXT"
	case "int", "int32", "integer":
		return "INTEGER"
	case "int64", "bigint":
		return "BIGINT"
	case "decimal":
		return renderDecimalType("DECIMAL", columnType)
	case "float32", "real":
		return "REAL"
	case "float64", "double":
		return "DOUBLE PRECISION"
	case "bool", "boolean":
		return "BOOLEAN"
	case "date":
		return "DATE"
	case "time":
		return "TIME"
	case "timestamp":
		return "TIMESTAMP"
	case "timestamptz":
		return "TIMESTAMP"
	case "json":
		return "JSON"
	case "jsonb":
		return "JSONB"
	case "uuid":
		return "UUID"
	case "bytes":
		return "BLOB"
	case "char":
		return "CHAR"
	case "enum":
		return "VARCHAR"
	default:
		return string(columnType.DataType)
	}
}

// DefaultValue returns default value representation.
func (d *BaseDialect) DefaultValue(value interface{}) string {
	return "DEFAULT"
}

// GeneratedClause returns an error by default as generated columns require dialect-specific syntax.
func (d *BaseDialect) GeneratedClause(expr string, stored bool) (string, error) {
	return "", fmt.Errorf("dialect does not support generated columns")
}

// IdentityClause returns an error by default as identity columns require dialect-specific syntax.
func (d *BaseDialect) IdentityClause(always bool) (string, error) {
	return "", fmt.Errorf("dialect does not support identity columns")
}

// UpsertClause returns generic upsert syntax.
func (d *BaseDialect) UpsertClause(table string, conflictCols []string, updateCols []string) string {
	return ""
}

func normalizeType(dataType schema.DataType) string {
	return strings.ToLower(string(dataType))
}

func renderDecimalType(base string, columnType schema.ColumnType) string {
	if columnType.Precision <= 0 {
		return base
	}

	return fmt.Sprintf("%s(%d,%d)", base, columnType.Precision, columnType.Scale)
}
