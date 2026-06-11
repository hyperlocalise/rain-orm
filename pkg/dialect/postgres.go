package dialect

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/hyperlocalise/rain-orm/pkg/schema"
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
		FeatureDefaultPlaceholder |
		FeatureSavepoint |
		FeatureSelectLocking |
		FeatureNullsOrder |
		FeatureSelectDistinctOn |
		FeatureUnlimited |
		FeaturePartialIndex |
		FeatureUpdateFrom |
		FeatureDeleteUsing
}

// QuoteIdentifier quotes identifiers with double quotes.
// Inner double quotes are escaped by doubling them.
func (d *PostgresDialect) QuoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

var postgresPlaceholderCache [8193]string

func init() {
	for i := 1; i <= 8192; i++ {
		postgresPlaceholderCache[i] = "$" + strconv.Itoa(i)
	}
}

// Placeholder returns PostgreSQL-style $n placeholders.
func (d *PostgresDialect) Placeholder(n int) string {
	if n > 0 && n <= 8192 {
		return postgresPlaceholderCache[n]
	}
	// OPTIMIZATION: Use strconv.AppendInt with a byte buffer and convert to string
	// to reduce allocations to exactly 1 on cache misses.
	var b [16]byte
	b[0] = '$'
	return string(strconv.AppendInt(b[:1], int64(n), 10))
}

// DataType returns PostgreSQL-specific type.
func (d *PostgresDialect) DataType(columnType schema.ColumnType) string {
	typ := normalizeType(columnType.DataType)

	switch typ {
	case "smallserial":
		return "SMALLSERIAL"
	case "serial":
		return "SERIAL"
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
		return renderDecimalType("NUMERIC", columnType)
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
		return "TIMESTAMPTZ"
	case "json":
		return "JSON"
	case "jsonb":
		return "JSONB"
	case "enum":
		return "TEXT"
	case "uuid":
		return "UUID"
	case "bytes":
		return "BYTEA"
	case "char":
		return "CHAR"
	default:
		return string(columnType.DataType)
	}
}

// AutoIncrementKeyword returns SERIAL for PostgreSQL.
func (d *PostgresDialect) AutoIncrementKeyword() string {
	return "SERIAL"
}

// LimitOffset returns PostgreSQL LIMIT/OFFSET syntax.
func (d *PostgresDialect) LimitOffset(limit, offset int) string {
	if limit >= 0 {
		if offset > 0 {
			return "LIMIT " + strconv.Itoa(limit) + " OFFSET " + strconv.Itoa(offset)
		}
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

// GeneratedClause returns PostgreSQL generated column syntax.
func (d *PostgresDialect) GeneratedClause(expr string, stored bool) (string, error) {
	if !stored {
		return "", fmt.Errorf("postgres: generated columns must be STORED")
	}
	return "GENERATED ALWAYS AS (" + expr + ") STORED", nil
}
