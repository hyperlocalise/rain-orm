// Package dialect provides database-specific implementations for Rain ORM.
// Implement this interface to add support for new database engines.
package dialect

// Dialect represents a database-specific SQL dialect.
type Dialect interface {
	// Name returns the dialect name (e.g., "postgres", "mysql", "sqlite").
	Name() string

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

	// ReturningClause returns true if the dialect supports RETURNING.
	ReturningClause() bool

	// UpsertClause returns the UPSERT syntax (INSERT ... ON CONFLICT, etc.).
	UpsertClause(table string, conflictCols []string, updateCols []string) string

	// CurrentTimestamp returns the SQL for current timestamp.
	CurrentTimestamp() string

	// BooleanLiteral returns the SQL boolean literal (TRUE/FALSE or 1/0).
	BooleanLiteral(v bool) string
}

// BaseDialect provides common implementations.
type BaseDialect struct{}

// DataType returns default SQL type mapping.
func (d *BaseDialect) DataType(typ string, size int) string {
	// Default type mapping
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
		return "TIMESTAMP"
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

// PostgresDialect implements PostgreSQL-specific SQL.
type PostgresDialect struct {
	BaseDialect
}

// Name returns the dialect name.
func (d *PostgresDialect) Name() string {
	return "postgres"
}

// QuoteIdentifier quotes identifiers with double quotes.
func (d *PostgresDialect) QuoteIdentifier(name string) string {
	return `"` + name + `"`
}

// Placeholder returns PostgreSQL-style $n placeholders.
func (d *PostgresDialect) Placeholder(n int) string {
	return "$" + itoa(n)
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
		return "LIMIT " + itoa(limit) + " OFFSET " + itoa(offset)
	}
	if limit > 0 {
		return "LIMIT " + itoa(limit)
	}
	if offset > 0 {
		return "OFFSET " + itoa(offset)
	}
	return ""
}

// ReturningClause returns true (PostgreSQL supports RETURNING).
func (d *PostgresDialect) ReturningClause() bool {
	return true
}

// UpsertClause returns PostgreSQL upsert syntax.
func (d *PostgresDialect) UpsertClause(table string, conflictCols []string, updateCols []string) string {
	// INSERT ... ON CONFLICT DO UPDATE
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

// MySQLDialect implements MySQL-specific SQL.
type MySQLDialect struct {
	BaseDialect
}

// Name returns the dialect name.
func (d *MySQLDialect) Name() string {
	return "mysql"
}

// QuoteIdentifier quotes identifiers with backticks.
func (d *MySQLDialect) QuoteIdentifier(name string) string {
	return "`" + name + "`"
}

// Placeholder returns MySQL-style ? placeholders.
func (d *MySQLDialect) Placeholder(n int) string {
	return "?"
}

// DataType returns MySQL-specific type.
func (d *MySQLDialect) DataType(typ string, size int) string {
	switch typ {
	case "string":
		if size > 0 {
			return "VARCHAR"
		}
		return "TEXT"
	case "int", "int32":
		return "INT"
	case "int64":
		return "BIGINT"
	case "float32":
		return "FLOAT"
	case "float64":
		return "DOUBLE"
	case "bool":
		return "BOOLEAN"
	case "time":
		return "DATETIME"
	case "json":
		return "JSON"
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
			return "LIMIT " + itoa(offset) + ", " + itoa(limit)
		}
		return "LIMIT " + itoa(limit)
	}
	return ""
}

// ReturningClause returns false (MySQL doesn't support RETURNING until 8.0.19).
func (d *MySQLDialect) ReturningClause() bool {
	return false
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

// SQLiteDialect implements SQLite-specific SQL.
type SQLiteDialect struct {
	BaseDialect
}

// Name returns the dialect name.
func (d *SQLiteDialect) Name() string {
	return "sqlite"
}

// QuoteIdentifier quotes identifiers with double quotes.
func (d *SQLiteDialect) QuoteIdentifier(name string) string {
	return `"` + name + `"`
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
			return "LIMIT " + itoa(limit) + " OFFSET " + itoa(offset)
		}
		return "LIMIT " + itoa(limit)
	}
	return ""
}

// ReturningClause returns true (SQLite 3.35+ supports RETURNING).
func (d *SQLiteDialect) ReturningClause() bool {
	return true
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

// GetDialect returns a dialect by name.
func GetDialect(name string) Dialect {
	switch name {
	case "postgres", "postgresql":
		return &PostgresDialect{}
	case "mysql":
		return &MySQLDialect{}
	case "sqlite", "sqlite3":
		return &SQLiteDialect{}
	default:
		return &PostgresDialect{}
	}
}

// itoa converts int to string.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var result []byte
	negative := n < 0
	if negative {
		n = -n
	}
	for n > 0 {
		result = append([]byte{byte('0' + n%10)}, result...)
		n /= 10
	}
	if negative {
		result = append([]byte{'-'}, result...)
	}
	return string(result)
}
