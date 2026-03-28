package dialect

import (
	"strconv"
	"strings"
)

// MySQLDialect implements MySQL-specific SQL.
type MySQLDialect struct {
	BaseDialect
}

// Name returns the dialect name.
func (d *MySQLDialect) Name() string {
	return "mysql"
}

// Features returns MySQL capabilities supported by Rain.
func (d *MySQLDialect) Features() Feature {
	return FeatureOffset | FeatureUpsert
}

// QuoteIdentifier quotes identifiers with backticks.
// Inner backticks are escaped by doubling them.
func (d *MySQLDialect) QuoteIdentifier(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

// Placeholder returns MySQL-style ? placeholders.
func (d *MySQLDialect) Placeholder(n int) string {
	return "?"
}

// DataType returns MySQL-specific type.
func (d *MySQLDialect) DataType(typ string, size int) string {
	switch typ {
	case "smallint":
		return "SMALLINT"
	case "string":
		if size > 0 {
			return "VARCHAR"
		}
		return "TEXT"
	case "int", "int32", "integer":
		return "INT"
	case "int64":
		return "BIGINT"
	case "decimal":
		return "DECIMAL"
	case "float32":
		return "FLOAT"
	case "float64":
		return "DOUBLE"
	case "bool":
		return "BOOLEAN"
	case "date":
		return "DATE"
	case "timestamp":
		return "TIMESTAMP"
	case "time":
		return "DATETIME"
	case "json":
		return "JSON"
	case "jsonb":
		return "JSON"
	case "uuid", "enum":
		return "VARCHAR"
	case "bytes":
		return "BLOB"
	default:
		return typ
	}
}

// AutoIncrementKeyword returns AUTO_INCREMENT for MySQL.
func (d *MySQLDialect) AutoIncrementKeyword() string {
	return "AUTO_INCREMENT"
}

// LimitOffset returns MySQL LIMIT/OFFSET syntax.
func (d *MySQLDialect) LimitOffset(limit, offset int) string {
	if limit > 0 {
		if offset > 0 {
			return "LIMIT " + strconv.Itoa(offset) + ", " + strconv.Itoa(limit)
		}
		return "LIMIT " + strconv.Itoa(limit)
	}
	if offset > 0 {
		return "LIMIT 18446744073709551615 OFFSET " + strconv.Itoa(offset)
	}
	return ""
}

// UpsertClause returns MySQL upsert syntax.
func (d *MySQLDialect) UpsertClause(table string, conflictCols []string, updateCols []string) string {
	return "ON DUPLICATE KEY UPDATE"
}

// DefaultValue returns MySQL default value.
func (d *MySQLDialect) DefaultValue(value interface{}) string {
	return "DEFAULT"
}

// BooleanLiteral returns MySQL boolean literals.
func (d *MySQLDialect) BooleanLiteral(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

// CurrentTimestamp returns MySQL current timestamp.
func (d *MySQLDialect) CurrentTimestamp() string {
	return "CURRENT_TIMESTAMP"
}
