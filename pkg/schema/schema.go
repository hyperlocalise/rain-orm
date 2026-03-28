// Package schema provides type-safe table and column definitions for Rain ORM.
// Define your database schema as Go structs with struct tags.
package schema

import "time"

// Table represents a database table definition.
type Table struct {
	Name        string
	Columns     []Column
	PrimaryKey  []string
	Indexes     []Index
	Constraints []Constraint
}

// Column represents a database column definition.
type Column struct {
	Name          string
	Type          ColumnType
	Nullable      bool
	Default       interface{}
	PrimaryKey    bool
	AutoIncrement bool
	Unique        bool
	Index         bool
	Comment       string
}

// ColumnType represents a column data type.
type ColumnType string

// Common column types.
const (
	TypeInteger   ColumnType = "INTEGER"
	TypeBigInt    ColumnType = "BIGINT"
	TypeSmallInt  ColumnType = "SMALLINT"
	TypeSerial    ColumnType = "SERIAL"
	TypeBigSerial ColumnType = "BIGSERIAL"
	TypeDecimal   ColumnType = "DECIMAL"
	TypeNumeric   ColumnType = "NUMERIC"
	TypeReal      ColumnType = "REAL"
	TypeDouble    ColumnType = "DOUBLE PRECISION"

	TypeVarchar ColumnType = "VARCHAR"
	TypeText    ColumnType = "TEXT"
	TypeChar    ColumnType = "CHAR"

	TypeBoolean ColumnType = "BOOLEAN"

	TypeTimestamp   ColumnType = "TIMESTAMP"
	TypeTimestampTZ ColumnType = "TIMESTAMPTZ"
	TypeDate        ColumnType = "DATE"
	TypeTime        ColumnType = "TIME"

	TypeJSON  ColumnType = "JSON"
	TypeJSONB ColumnType = "JSONB"

	TypeUUID  ColumnType = "UUID"
	TypeBytea ColumnType = "BYTEA"
	TypeInet  ColumnType = "INET"
)

// Index represents a database index.
type Index struct {
	Name    string
	Columns []string
	Unique  bool
	Where   string
}

// Constraint represents a table constraint.
type Constraint struct {
	Name string
	Type string // CHECK, FOREIGN KEY, etc.
	Def  string
}

// Schema provides methods to define tables.
type Schema struct {
	tables map[string]*Table
}

// NewSchema creates a new schema builder.
func NewSchema() *Schema {
	return &Schema{
		tables: make(map[string]*Table),
	}
}

// CreateTable registers a new table definition.
func (s *Schema) CreateTable(name string, def func(*TableBuilder)) *Table {
	builder := &TableBuilder{Name: name}
	if def != nil {
		def(builder)
	}
	table := builder.Build()
	s.tables[name] = table
	return table
}

// GetTable returns a table definition by name.
func (s *Schema) GetTable(name string) (*Table, bool) {
	table, ok := s.tables[name]
	return table, ok
}

// TableBuilder builds a table definition.
type TableBuilder struct {
	Name        string
	columns     []Column
	primaryKey  []string
	indexes     []Index
	constraints []Constraint
}

// Column adds a column to the table.
func (tb *TableBuilder) Column(name string, colType ColumnType) *ColumnBuilder {
	col := Column{
		Name: name,
		Type: colType,
	}
	tb.columns = append(tb.columns, col)
	return &ColumnBuilder{table: tb, index: len(tb.columns) - 1}
}

// PrimaryKey sets the primary key columns.
func (tb *TableBuilder) PrimaryKey(columns ...string) *TableBuilder {
	tb.primaryKey = columns
	return tb
}

// Index adds an index to the table.
func (tb *TableBuilder) Index(name string, columns ...string) *TableBuilder {
	tb.indexes = append(tb.indexes, Index{
		Name:    name,
		Columns: columns,
	})
	return tb
}

// UniqueIndex adds a unique index to the table.
func (tb *TableBuilder) UniqueIndex(name string, columns ...string) *TableBuilder {
	tb.indexes = append(tb.indexes, Index{
		Name:    name,
		Columns: columns,
		Unique:  true,
	})
	return tb
}

// Build creates the Table definition.
func (tb *TableBuilder) Build() *Table {
	return &Table{
		Name:        tb.Name,
		Columns:     tb.columns,
		PrimaryKey:  tb.primaryKey,
		Indexes:     tb.indexes,
		Constraints: tb.constraints,
	}
}

// ColumnBuilder builds a column definition.
type ColumnBuilder struct {
	table *TableBuilder
	index int
}

// NotNull marks the column as NOT NULL.
func (cb *ColumnBuilder) NotNull() *ColumnBuilder {
	cb.table.columns[cb.index].Nullable = false
	return cb
}

// Nullable marks the column as nullable.
func (cb *ColumnBuilder) Nullable() *ColumnBuilder {
	cb.table.columns[cb.index].Nullable = true
	return cb
}

// Default sets the default value.
func (cb *ColumnBuilder) Default(value interface{}) *ColumnBuilder {
	cb.table.columns[cb.index].Default = value
	return cb
}

// PrimaryKey marks the column as primary key.
func (cb *ColumnBuilder) PrimaryKey() *ColumnBuilder {
	cb.table.columns[cb.index].PrimaryKey = true
	cb.table.primaryKey = append(cb.table.primaryKey, cb.table.columns[cb.index].Name)
	return cb
}

// AutoIncrement marks the column as auto-increment.
func (cb *ColumnBuilder) AutoIncrement() *ColumnBuilder {
	cb.table.columns[cb.index].AutoIncrement = true
	return cb
}

// Unique marks the column as unique.
func (cb *ColumnBuilder) Unique() *ColumnBuilder {
	cb.table.columns[cb.index].Unique = true
	return cb
}

// Index adds an index on this column.
func (cb *ColumnBuilder) Index() *ColumnBuilder {
	cb.table.columns[cb.index].Index = true
	return cb
}

// Comment adds a comment to the column.
func (cb *ColumnBuilder) Comment(text string) *ColumnBuilder {
	cb.table.columns[cb.index].Comment = text
	return cb
}

// References creates a foreign key reference.
func (cb *ColumnBuilder) References(table, column string) *ColumnBuilder {
	// TODO: Add foreign key constraint
	return cb
}

// Common model fields that can be embedded.
type Timestamps struct {
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

// SoftDelete provides soft delete functionality.
type SoftDelete struct {
	DeletedAt *time.Time `db:"deleted_at"`
}
