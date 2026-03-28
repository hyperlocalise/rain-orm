package dialect

import (
	"strconv"
	"strings"
)

// SQLiteDialect implements SQLite-specific SQL.
type SQLiteDialect struct {
	BaseDialect
}

// Name returns the dialect name.
func (d *SQLiteDialect) Name() string {
	return "sqlite"
}

// Features returns SQLite capabilities supported by Rain.
func (d *SQLiteDialect) Features() Feature {
	return FeatureInsertReturning |
		FeatureUpdateReturning |
		FeatureDeleteReturning |
		FeatureOffset |
		FeatureUpsert
}

// QuoteIdentifier quotes identifiers with double quotes.
// Inner double quotes are escaped by doubling them.
func (d *SQLiteDialect) QuoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// Placeholder returns SQLite-style ? placeholders.
func (d *SQLiteDialect) Placeholder(n int) string {
	return "?"
}

// DataType returns SQLite-specific type.
func (d *SQLiteDialect) DataType(typ string, size int) string {
	switch typ {
	case "string":
		return "TEXT"
	case "int", "int32", "int64":
		return "INTEGER"
	case "float32", "float64":
		return "REAL"
	case "bool":
		return "INTEGER"
	case "time":
		return "TEXT"
	case "json":
		return "TEXT"
	default:
		return typ
	}
}

// AutoIncrementKeyword returns AUTOINCREMENT for SQLite.
func (d *SQLiteDialect) AutoIncrementKeyword() string {
	return "AUTOINCREMENT"
}

// LimitOffset returns SQLite LIMIT/OFFSET syntax.
func (d *SQLiteDialect) LimitOffset(limit, offset int) string {
	if limit > 0 {
		if offset > 0 {
			return "LIMIT " + strconv.Itoa(limit) + " OFFSET " + strconv.Itoa(offset)
		}
		return "LIMIT " + strconv.Itoa(limit)
	}
	return ""
}

// UpsertClause returns SQLite upsert syntax.
func (d *SQLiteDialect) UpsertClause(table string, conflictCols []string, updateCols []string) string {
	return "ON CONFLICT DO UPDATE"
}

// DefaultValue returns SQLite default value.
func (d *SQLiteDialect) DefaultValue(value interface{}) string {
	return "DEFAULT"
}

// BooleanLiteral returns SQLite boolean literals.
func (d *SQLiteDialect) BooleanLiteral(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

// CurrentTimestamp returns SQLite current timestamp.
func (d *SQLiteDialect) CurrentTimestamp() string {
	return "CURRENT_TIMESTAMP"
}
