package dialect

import (
	"strconv"
	"strings"
)

// PostgresDialect implements PostgreSQL-specific SQL.
type PostgresDialect struct {
	BaseDialect
}

// Name returns the dialect name.
func (d *PostgresDialect) Name() string {
	return "postgres"
}

// Features returns PostgreSQL capabilities supported by Rain.
func (d *PostgresDialect) Features() Feature {
	return FeatureInsertReturning |
		FeatureUpdateReturning |
		FeatureDeleteReturning |
		FeatureOffset |
		FeatureUpsert |
		FeatureCTE |
		FeatureDefaultPlaceholder
}

// QuoteIdentifier quotes identifiers with double quotes.
// Inner double quotes are escaped by doubling them.
func (d *PostgresDialect) QuoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// Placeholder returns PostgreSQL-style $n placeholders.
func (d *PostgresDialect) Placeholder(n int) string {
	return "$" + strconv.Itoa(n)
}

// DataType returns PostgreSQL-specific type.
func (d *PostgresDialect) DataType(typ string, size int) string {
	switch typ {
	case "string":
		if size > 0 {
			return "VARCHAR"
		}
		return "TEXT"
	case "int", "int32":
		return "INTEGER"
	case "int64":
		return "BIGINT"
	case "float32":
		return "REAL"
	case "float64":
		return "DOUBLE PRECISION"
	case "bool":
		return "BOOLEAN"
	case "time":
		return "TIMESTAMPTZ"
	case "json":
		return "JSONB"
	case "uuid":
		return "UUID"
	case "bytes":
		return "BYTEA"
	default:
		return typ
	}
}

// AutoIncrementKeyword returns SERIAL for PostgreSQL.
func (d *PostgresDialect) AutoIncrementKeyword() string {
	return "SERIAL"
}

// LimitOffset returns PostgreSQL LIMIT/OFFSET syntax.
func (d *PostgresDialect) LimitOffset(limit, offset int) string {
	if limit > 0 && offset > 0 {
		return "LIMIT " + strconv.Itoa(limit) + " OFFSET " + strconv.Itoa(offset)
	}
	if limit > 0 {
		return "LIMIT " + strconv.Itoa(limit)
	}
	if offset > 0 {
		return "OFFSET " + strconv.Itoa(offset)
	}
	return ""
}

// UpsertClause returns PostgreSQL upsert syntax.
func (d *PostgresDialect) UpsertClause(table string, conflictCols []string, updateCols []string) string {
	return "ON CONFLICT DO UPDATE"
}

// DefaultValue returns PostgreSQL default value.
func (d *PostgresDialect) DefaultValue(value interface{}) string {
	return "DEFAULT"
}

// BooleanLiteral returns PostgreSQL boolean literals.
func (d *PostgresDialect) BooleanLiteral(v bool) string {
	if v {
		return "TRUE"
	}
	return "FALSE"
}

// CurrentTimestamp returns PostgreSQL current timestamp.
func (d *PostgresDialect) CurrentTimestamp() string {
	return "CURRENT_TIMESTAMP"
}
