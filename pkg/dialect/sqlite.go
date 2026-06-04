package dialect

import (
	"strconv"
	"strings"

	"github.com/hyperlocalise/rain-orm/pkg/schema"
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
		FeatureUpsert |
		FeatureSavepoint |
		FeatureNullsOrder |
		FeatureCTE |
		FeatureUpdateOrder |
		FeatureUpdateLimit |
		FeatureDeleteOrder |
		FeatureDeleteLimit |
		FeatureUnlimited
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
func (d *SQLiteDialect) DataType(columnType schema.ColumnType) string {
	typ := normalizeType(columnType.DataType)

	switch typ {
	case "smallserial", "serial", "bigserial":
		return "INTEGER"
	case "string", "varchar", "text":
		return "TEXT"
	case "smallint", "int", "int32", "int64", "integer", "bigint":
		return "INTEGER"
	case "decimal", "float32", "float64", "real", "double":
		return "REAL"
	case "bool", "boolean":
		return "INTEGER"
	case "date", "time", "timestamp", "timestamptz":
		return "TEXT"
	case "json", "jsonb":
		return "TEXT"
	case "uuid", "enum":
		return "TEXT"
	case "bytes":
		return "BLOB"
	case "char":
		return "TEXT"
	default:
		return string(columnType.DataType)
	}
}

// AutoIncrementKeyword returns AUTOINCREMENT for SQLite.
func (d *SQLiteDialect) AutoIncrementKeyword() string {
	return "AUTOINCREMENT"
}

// LimitOffset returns SQLite LIMIT/OFFSET syntax.
func (d *SQLiteDialect) LimitOffset(limit, offset int) string {
	if limit >= 0 {
		if offset > 0 {
			return "LIMIT " + strconv.Itoa(limit) + " OFFSET " + strconv.Itoa(offset)
		}
		return "LIMIT " + strconv.Itoa(limit)
	}
	if offset > 0 {
		return "LIMIT -1 OFFSET " + strconv.Itoa(offset)
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

// GeneratedClause returns SQLite generated column syntax.
func (d *SQLiteDialect) GeneratedClause(expr string, stored bool) (string, error) {
	kind := "VIRTUAL"
	if stored {
		kind = "STORED"
	}
	return "GENERATED ALWAYS AS (" + expr + ") " + kind, nil
}
