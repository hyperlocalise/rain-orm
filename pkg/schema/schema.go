// Package schema provides typed schema definitions and reusable SQL expressions.
package schema

import (
	"fmt"
	"reflect"
	"strings"
	"time"
)

// DataType represents a database column type.
type DataType string

// LengthSemantics describes how a type uses any configured length.
type LengthSemantics string

// TimestampKind describes timestamp timezone semantics.
type TimestampKind string

// IdentityKind describes PostgreSQL-style identity column semantics.
type IdentityKind string

// Supported schema data types.
const (
	TypeSmallSerial DataType = "SMALLSERIAL"
	TypeSerial      DataType = "SERIAL"
	TypeBigSerial   DataType = "BIGSERIAL"
	TypeSmallInt    DataType = "SMALLINT"
	TypeInteger     DataType = "INTEGER"
	TypeBigInt      DataType = "BIGINT"
	TypeReal        DataType = "REAL"
	TypeDouble      DataType = "DOUBLE"
	TypeDecimal     DataType = "DECIMAL"
	TypeText        DataType = "TEXT"
	TypeVarChar     DataType = "VARCHAR"
	TypeBoolean     DataType = "BOOLEAN"
	TypeJSON        DataType = "JSON"
	TypeJSONB       DataType = "JSONB"
	TypeUUID        DataType = "UUID"
	TypeBytes       DataType = "BYTES"
	TypeChar        DataType = "CHAR"
	TypeDate        DataType = "DATE"
	TypeTime        DataType = "TIME"
	TypeTimestamp   DataType = "TIMESTAMP"
	TypeTimestampTZ DataType = "TIMESTAMPTZ"
	TypeEnum        DataType = "ENUM"
)

const (
	LengthSemanticsUnspecified LengthSemantics = ""
	LengthSemanticsVariable    LengthSemantics = "variable"
	LengthSemanticsFixed       LengthSemantics = "fixed"
)

const (
	TimestampKindUnspecified TimestampKind = ""
	TimestampKindWithoutTZ   TimestampKind = "without_tz"
	TimestampKindWithTZ      TimestampKind = "with_tz"
)

const (
	IdentityNone      IdentityKind = ""
	IdentityAlways    IdentityKind = "always"
	IdentityByDefault IdentityKind = "by_default"
)

// SortDirection represents an ORDER BY or index column direction.
type SortDirection string

// Supported sort directions.
const (
	SortAsc  SortDirection = "ASC"
	SortDesc SortDirection = "DESC"
)

// NullsOrder represents a NULLS FIRST or NULLS LAST clause.
type NullsOrder string

const (
	NullsFirst NullsOrder = "NULLS FIRST"
	NullsLast  NullsOrder = "NULLS LAST"
)

// TableReference is implemented by typed table handles.
type TableReference interface {
	TableDef() *TableDef
}

// Expression is implemented by all query expressions.
type Expression interface {
	isExpression()
}

// Predicate is implemented by boolean SQL expressions.
type Predicate interface {
	Expression
	isPredicate()
}

// ExpressionMarker can be embedded to satisfy the Expression interface.
type ExpressionMarker struct{}

func (ExpressionMarker) isExpression() {}

// ColumnReference is implemented by typed and untyped column handles.
type ColumnReference interface {
	Expression
	ColumnDef() *ColumnDef
}

// ColumnType stores schema metadata about a column's logical type.
type ColumnType struct {
	DataType        DataType
	Size            int
	LengthSemantics LengthSemantics
	Precision       int
	Scale           int
	TimePrecision   int
	TimestampKind   TimestampKind
	EnumValues      []string
}

// TableDef stores immutable table metadata after schema construction.
type TableDef struct {
	Name        string
	Alias       string
	IsView      bool
	ViewQuery   Expression
	Columns     []*ColumnDef
	Indexes     []IndexDef
	Constraints []ConstraintDef
	ForeignKeys []ForeignKeyDef
	Relations   []RelationDef

	columnsByName   map[string]*ColumnDef
	relationsByName map[string]RelationDef
}

// ColumnDef stores immutable column metadata after schema construction.
type ColumnDef struct {
	Table           *TableDef
	Name            string
	Type            ColumnType
	Nullable        bool
	Default         any
	HasDefault      bool
	DefaultSQL      string
	DefaultExpr     Expression
	PrimaryKey      bool
	AutoIncrement   bool
	Unique          bool
	GeneratedExpr   Expression
	GeneratedStored bool
	Identity        IdentityKind
}

// ForeignKeyDef stores a foreign-key relationship.
type ForeignKeyDef struct {
	Name             string
	Column           *ColumnDef
	ReferencedTable  *TableDef
	ReferencedColumn *ColumnDef
	OnDelete         ForeignKeyAction
	OnUpdate         ForeignKeyAction
}

// ConstraintType identifies a portable table constraint kind.
type ConstraintType string

const (
	ConstraintPrimaryKey ConstraintType = "primary_key"
	ConstraintUnique     ConstraintType = "unique"
	ConstraintCheck      ConstraintType = "check"
	ConstraintForeignKey ConstraintType = "foreign_key"
)

// ForeignKeyAction identifies a portable foreign-key action.
type ForeignKeyAction string

const (
	ForeignKeyActionNoAction   ForeignKeyAction = "NO ACTION"
	ForeignKeyActionRestrict   ForeignKeyAction = "RESTRICT"
	ForeignKeyActionCascade    ForeignKeyAction = "CASCADE"
	ForeignKeyActionSetNull    ForeignKeyAction = "SET NULL"
	ForeignKeyActionSetDefault ForeignKeyAction = "SET DEFAULT"
)

// ConstraintDef stores portable table-level constraint metadata.
type ConstraintDef struct {
	Name            string
	Type            ConstraintType
	Columns         []*ColumnDef
	Check           Predicate
	ReferencedTable *TableDef
	ReferencedCols  []*ColumnDef
	OnDelete        ForeignKeyAction
	OnUpdate        ForeignKeyAction
}

// RelationType identifies how two tables are related.
type RelationType string

const (
	RelationTypeBelongsTo  RelationType = "belongs_to"
	RelationTypeHasOne     RelationType = "has_one"
	RelationTypeHasMany    RelationType = "has_many"
	RelationTypeManyToMany RelationType = "many_to_many"
)

// RelationDef stores table-level relation metadata used by relation loading.
type RelationDef struct {
	Name             string
	Type             RelationType
	SourceColumn     *ColumnDef
	TargetTable      *TableDef
	TargetColumn     *ColumnDef
	JoinTable        *TableDef
	JoinSourceColumn *ColumnDef
	JoinTargetColumn *ColumnDef
}

// IndexDef stores table-level index metadata.
type IndexDef struct {
	Name      string
	Unique    bool
	Columns   []IndexColumn
	Where     string
	WhereExpr Predicate
}

// IndexColumn stores an indexed column and its sort direction.
type IndexColumn struct {
	Column     ColumnReference
	Direction  SortDirection
	NullsOrder NullsOrder
}

// IndexColumnSpec is implemented by values that can be bound to an index.
type IndexColumnSpec interface {
	indexColumnSpec()
}

// ConstraintColumnSpec is implemented by values that can be bound to a table constraint.
type ConstraintColumnSpec interface {
	constraintColumnSpec()
}

// TableModel is embedded in user-defined table structs.
type TableModel struct {
	def *TableDef
}

// TableDef returns the underlying table metadata.
func (t *TableModel) TableDef() *TableDef {
	return t.def
}

// ColumnByName returns a column definition by name.
func (t *TableDef) ColumnByName(name string) (*ColumnDef, bool) {
	column, ok := t.columnsByName[name]
	return column, ok
}

// RelationByName returns a relation definition by name.
func (t *TableDef) RelationByName(name string) (RelationDef, bool) {
	relation, ok := t.relationsByName[name]
	return relation, ok
}

// C returns an untyped column handle by name for index definitions or dynamic access.
func (t *TableModel) C(name string) *AnyColumn {
	if t.def == nil {
		panic("schema: table model is not initialized")
	}

	col, ok := t.def.columnsByName[name]
	if !ok {
		panic(fmt.Sprintf("schema: unknown column %q on table %q", name, t.def.Name))
	}

	return &AnyColumn{def: col}
}

// SmallSerial adds a SMALLSERIAL column.
func (t *TableModel) SmallSerial(name string) *Column[int16] {
	return addColumn[int16](t.def, name, ColumnType{DataType: TypeSmallSerial}, false, true)
}

// Serial adds a SERIAL column.
func (t *TableModel) Serial(name string) *Column[int32] {
	return addColumn[int32](t.def, name, ColumnType{DataType: TypeSerial}, false, true)
}

// BigSerial adds a BIGSERIAL column.
func (t *TableModel) BigSerial(name string) *Column[int64] {
	return addColumn[int64](t.def, name, ColumnType{DataType: TypeBigSerial}, false, true)
}

// BigInt adds a BIGINT column.
func (t *TableModel) BigInt(name string) *Column[int64] {
	return addColumn[int64](t.def, name, ColumnType{DataType: TypeBigInt}, true, false)
}

// SmallInt adds a SMALLINT column intended for 16-bit integer values.
func (t *TableModel) SmallInt(name string) *Column[int16] {
	return addColumn[int16](t.def, name, ColumnType{DataType: TypeSmallInt}, true, false)
}

// Integer adds an INTEGER column intended for standard 32-bit integer values.
func (t *TableModel) Integer(name string) *Column[int32] {
	return addColumn[int32](t.def, name, ColumnType{DataType: TypeInteger}, true, false)
}

// Real adds a REAL/FLOAT-style column intended for single-precision values.
func (t *TableModel) Real(name string) *Column[float32] {
	return addColumn[float32](t.def, name, ColumnType{DataType: TypeReal}, true, false)
}

// Double adds a DOUBLE/DOUBLE PRECISION column for double-precision values.
func (t *TableModel) Double(name string) *Column[float64] {
	return addColumn[float64](t.def, name, ColumnType{DataType: TypeDouble}, true, false)
}

// Decimal adds a DECIMAL/NUMERIC column with fixed precision and scale.
func (t *TableModel) Decimal(name string, precision, scale int) *Column[string] {
	return addColumn[string](t.def, name, ColumnType{
		DataType:  TypeDecimal,
		Precision: precision,
		Scale:     scale,
	}, true, false)
}

// Text adds a TEXT column.
func (t *TableModel) Text(name string) *Column[string] {
	return addColumn[string](t.def, name, ColumnType{DataType: TypeText}, true, false)
}

// VarChar adds a VARCHAR column.
func (t *TableModel) VarChar(name string, size int) *Column[string] {
	return addColumn[string](t.def, name, ColumnType{
		DataType:        TypeVarChar,
		Size:            size,
		LengthSemantics: LengthSemanticsVariable,
	}, true, false)
}

// Char adds a CHAR column with fixed length.
func (t *TableModel) Char(name string, size int) *Column[string] {
	return addColumn[string](t.def, name, ColumnType{
		DataType:        TypeChar,
		Size:            size,
		LengthSemantics: LengthSemanticsFixed,
	}, true, false)
}

// Boolean adds a BOOLEAN column.
func (t *TableModel) Boolean(name string) *Column[bool] {
	return addColumn[bool](t.def, name, ColumnType{DataType: TypeBoolean}, true, false)
}

// JSON adds a JSON column for semi-structured values.
func (t *TableModel) JSON(name string) *Column[any] {
	return addColumn[any](t.def, name, ColumnType{DataType: TypeJSON}, true, false)
}

// JSONB adds a JSONB binary JSON column where supported.
func (t *TableModel) JSONB(name string) *Column[any] {
	return addColumn[any](t.def, name, ColumnType{DataType: TypeJSONB}, true, false)
}

// UUID adds a UUID column for canonical UUID string values.
func (t *TableModel) UUID(name string) *Column[string] {
	return addColumn[string](t.def, name, ColumnType{DataType: TypeUUID}, true, false)
}

// Bytes adds a bytes/blob column for arbitrary binary payloads.
func (t *TableModel) Bytes(name string) *Column[[]byte] {
	return addColumn[[]byte](t.def, name, ColumnType{DataType: TypeBytes}, true, false)
}

// Date adds a DATE column intended for calendar-date values.
func (t *TableModel) Date(name string) *Column[time.Time] {
	return addColumn[time.Time](t.def, name, ColumnType{DataType: TypeDate}, true, false)
}

// Time adds a TIME column without timezone semantics.
func (t *TableModel) Time(name string) *Column[time.Time] {
	return addColumn[time.Time](t.def, name, ColumnType{DataType: TypeTime}, true, false)
}

// TimePrecision adds a TIME column with explicit fractional precision.
func (t *TableModel) TimePrecision(name string, precision int) *Column[time.Time] {
	return addColumn[time.Time](t.def, name, ColumnType{
		DataType:      TypeTime,
		TimePrecision: precision,
	}, true, false)
}

// Timestamp adds a TIMESTAMP column without timezone semantics.
func (t *TableModel) Timestamp(name string) *Column[time.Time] {
	return addColumn[time.Time](t.def, name, ColumnType{
		DataType:      TypeTimestamp,
		TimestampKind: TimestampKindWithoutTZ,
	}, true, false)
}

// TimestampTZ adds a TIMESTAMPTZ column.
func (t *TableModel) TimestampTZ(name string) *Column[time.Time] {
	return addColumn[time.Time](t.def, name, ColumnType{
		DataType:      TypeTimestampTZ,
		TimestampKind: TimestampKindWithTZ,
	}, true, false)
}

// TimestampPrecision adds a TIMESTAMP column with explicit fractional precision.
func (t *TableModel) TimestampPrecision(name string, precision int) *Column[time.Time] {
	return addColumn[time.Time](t.def, name, ColumnType{
		DataType:      TypeTimestamp,
		TimePrecision: precision,
		TimestampKind: TimestampKindWithoutTZ,
	}, true, false)
}

// TimestampTZPrecision adds a TIMESTAMPTZ column with explicit fractional precision.
func (t *TableModel) TimestampTZPrecision(name string, precision int) *Column[time.Time] {
	return addColumn[time.Time](t.def, name, ColumnType{
		DataType:      TypeTimestampTZ,
		TimePrecision: precision,
		TimestampKind: TimestampKindWithTZ,
	}, true, false)
}

// Enum adds a string-backed enum-style column with allowed values metadata.
func (t *TableModel) Enum(name string, values ...string) *Column[string] {
	copiedValues := append([]string(nil), values...)
	return addColumn[string](t.def, name, ColumnType{
		DataType:   TypeEnum,
		EnumValues: copiedValues,
	}, true, false)
}

// Index declares a non-unique index.
func (t *TableModel) Index(name string) *IndexBuilder {
	idx := &IndexDef{Name: name}
	t.def.Indexes = append(t.def.Indexes, *idx)

	return &IndexBuilder{table: t.def, index: len(t.def.Indexes) - 1}
}

// UniqueIndex declares a unique index.
func (t *TableModel) UniqueIndex(name string) *IndexBuilder {
	idx := &IndexDef{Name: name, Unique: true}
	t.def.Indexes = append(t.def.Indexes, *idx)

	return &IndexBuilder{table: t.def, index: len(t.def.Indexes) - 1}
}

// PrimaryKey declares a table-level primary key constraint.
func (t *TableModel) PrimaryKey(name string) *ConstraintBuilder {
	if t.def == nil {
		panic("schema: table model is not initialized")
	}
	if name == "" {
		panic("schema: constraint name cannot be empty")
	}
	constraint := ConstraintDef{Name: name, Type: ConstraintPrimaryKey}
	t.def.Constraints = append(t.def.Constraints, constraint)

	return &ConstraintBuilder{table: t.def, constraint: len(t.def.Constraints) - 1}
}

// Unique declares a table-level unique constraint.
func (t *TableModel) Unique(name string) *ConstraintBuilder {
	if t.def == nil {
		panic("schema: table model is not initialized")
	}
	if name == "" {
		panic("schema: constraint name cannot be empty")
	}
	constraint := ConstraintDef{Name: name, Type: ConstraintUnique}
	t.def.Constraints = append(t.def.Constraints, constraint)

	return &ConstraintBuilder{table: t.def, constraint: len(t.def.Constraints) - 1}
}

// Check declares a table-level CHECK constraint.
func (t *TableModel) Check(name string, predicate Predicate) {
	if t.def == nil {
		panic("schema: table model is not initialized")
	}
	if name == "" {
		panic("schema: constraint name cannot be empty")
	}
	if predicate == nil {
		panic("schema: check constraint requires a predicate")
	}
	if logical, ok := predicate.(LogicalExpr); ok && len(logical.Exprs) == 0 {
		panic("schema: check constraint logical expression must contain at least one predicate")
	}

	t.def.Constraints = append(t.def.Constraints, ConstraintDef{
		Name:  name,
		Type:  ConstraintCheck,
		Check: predicate,
	})
}

// ForeignKey declares a table-level foreign key constraint.
func (t *TableModel) ForeignKey(name string) *ForeignKeyBuilder {
	if t.def == nil {
		panic("schema: table model is not initialized")
	}
	if name == "" {
		panic("schema: constraint name cannot be empty")
	}
	constraint := ConstraintDef{Name: name, Type: ConstraintForeignKey}
	t.def.Constraints = append(t.def.Constraints, constraint)

	return &ForeignKeyBuilder{table: t.def, constraint: len(t.def.Constraints) - 1}
}

// Define creates a typed table handle backed by schema metadata.
func Define[T any](name string, fn func(*T)) *T {
	handle := new(T)
	def := &TableDef{
		Name:            name,
		Columns:         make([]*ColumnDef, 0, 8),
		Indexes:         make([]IndexDef, 0, 4),
		Constraints:     make([]ConstraintDef, 0, 4),
		ForeignKeys:     make([]ForeignKeyDef, 0, 4),
		Relations:       make([]RelationDef, 0, 4),
		columnsByName:   make(map[string]*ColumnDef, 8),
		relationsByName: make(map[string]RelationDef, 4),
	}
	bindTableModel(handle, def)
	fn(handle)

	return handle
}

// DefineView creates a typed table handle for a database view.
func DefineView[T any](name string, fn func(*T), query Expression) *T {
	handle := Define(name, fn)
	def := tableDefOf(handle)
	def.IsView = true
	def.ViewQuery = query
	return handle
}

// Alias clones a typed table handle with a SQL alias.
func Alias[T any](src *T, alias string) *T {
	clone := new(T)
	srcValue := reflect.ValueOf(src).Elem()
	dstValue := reflect.ValueOf(clone).Elem()
	dstValue.Set(srcValue)

	aliasedDef := cloneTableDef(tableDefOf(src), alias)
	bindTableModel(clone, aliasedDef)
	rebindAliasedColumns(dstValue, aliasedDef)

	return clone
}

// AnyColumn is an untyped column reference.
type AnyColumn struct {
	def *ColumnDef
}

// Ref creates an untyped column reference from metadata.
func Ref(def *ColumnDef) *AnyColumn {
	return &AnyColumn{def: def}
}

// ColumnDef returns metadata for this column.
func (c *AnyColumn) ColumnDef() *ColumnDef {
	return c.def
}

// Eq compares this column to a value.
func (c *AnyColumn) Eq(value any) ComparisonExpr {
	return Eq(c, value)
}

// Ne compares this column to a value.
func (c *AnyColumn) Ne(value any) ComparisonExpr {
	return Ne(c, value)
}

// Gt compares this column to a value.
func (c *AnyColumn) Gt(value any) ComparisonExpr {
	return Gt(c, value)
}

// Gte compares this column to a value.
func (c *AnyColumn) Gte(value any) ComparisonExpr {
	return Gte(c, value)
}

// Lt compares this column to a value.
func (c *AnyColumn) Lt(value any) ComparisonExpr {
	return Lt(c, value)
}

// Lte compares this column to a value.
func (c *AnyColumn) Lte(value any) ComparisonExpr {
	return Lte(c, value)
}

// Asc returns an ascending sort expression.
func (c *AnyColumn) Asc() OrderExpr {
	return Asc(c)
}

// Desc returns a descending sort expression.
func (c *AnyColumn) Desc() OrderExpr {
	return Desc(c)
}

// As aliases this column in a SELECT list.
func (c *AnyColumn) As(alias string) AliasExpr {
	return As(c, alias)
}

// IsNull creates an IS NULL predicate.
func (c *AnyColumn) IsNull() NullCheckExpr {
	return IsNull(c)
}

// IsNotNull creates an IS NOT NULL predicate.
func (c *AnyColumn) IsNotNull() NullCheckExpr {
	return IsNotNull(c)
}

// In compares this column to a set of Go values or expressions using SQL IN.
func (c *AnyColumn) In(values ...any) InExpr {
	exprs := make([]Expression, 0, len(values))
	for _, value := range values {
		if expr, ok := value.(Expression); ok {
			exprs = append(exprs, expr)
		} else {
			exprs = append(exprs, ValueExpr{Value: value})
		}
	}
	return InExpr{Left: c, Values: exprs}
}

// InSubquery compares this column to the result of a subquery.
func (c *AnyColumn) InSubquery(subquery Expression) InExpr {
	return InExpr{Left: c, Values: []Expression{subquery}}
}

// Add adds a value or expression to this column.
func (c *AnyColumn) Add(value any) BinaryExpr {
	return BinaryExpr{Left: c, Operator: "+", Right: wrapValue(value)}
}

// Sub subtracts a value or expression from this column.
func (c *AnyColumn) Sub(value any) BinaryExpr {
	return BinaryExpr{Left: c, Operator: "-", Right: wrapValue(value)}
}

// Mul multiplies this column by a value or expression.
func (c *AnyColumn) Mul(value any) BinaryExpr {
	return BinaryExpr{Left: c, Operator: "*", Right: wrapValue(value)}
}

// Div divides this column by a value or expression.
func (c *AnyColumn) Div(value any) BinaryExpr {
	return BinaryExpr{Left: c, Operator: "/", Right: wrapValue(value)}
}

// Mod calculates the remainder of this column divided by a value or expression.
func (c *AnyColumn) Mod(value any) BinaryExpr {
	return BinaryExpr{Left: c, Operator: "%", Right: wrapValue(value)}
}

// NotInSubquery compares this column to the result of a subquery.
func (c *AnyColumn) NotInSubquery(subquery Expression) InExpr {
	return InExpr{Left: c, Values: []Expression{subquery}, Negated: true}
}

func (c *AnyColumn) isExpression()         {}
func (c *AnyColumn) indexColumnSpec()      {}
func (c *AnyColumn) constraintColumnSpec() {}

// Column represents a typed column handle.
type Column[T any] struct {
	def *ColumnDef
}

// ColumnDef returns metadata for this column.
func (c *Column[T]) ColumnDef() *ColumnDef {
	return c.def
}

// PrimaryKey marks the column as a primary key.
func (c *Column[T]) PrimaryKey() *Column[T] {
	c.def.PrimaryKey = true
	c.def.Nullable = false
	if c.def.Type.DataType == TypeSmallSerial || c.def.Type.DataType == TypeSerial || c.def.Type.DataType == TypeBigSerial {
		c.def.AutoIncrement = true
	}

	return c
}

// NotNull marks the column as NOT NULL.
func (c *Column[T]) NotNull() *Column[T] {
	c.def.Nullable = false
	return c
}

// Nullable marks the column as nullable.
func (c *Column[T]) Nullable() *Column[T] {
	c.def.Nullable = true
	return c
}

// Default sets a Go value default.
func (c *Column[T]) Default(value T) *Column[T] {
	c.def.HasDefault = true
	c.def.Default = value
	c.def.DefaultSQL = ""
	c.def.DefaultExpr = nil
	return c
}

// DefaultNow sets CURRENT_TIMESTAMP as the default value.
func (c *Column[T]) DefaultNow() *Column[T] {
	c.def.HasDefault = true
	c.def.DefaultSQL = "CURRENT_TIMESTAMP"
	c.def.Default = nil
	c.def.DefaultExpr = nil
	return c
}

// DefaultRaw sets a raw SQL expression as the default value.
func (c *Column[T]) DefaultRaw(expr Expression) *Column[T] {
	if expr == nil {
		panic("schema: DefaultRaw requires a non-nil expression")
	}
	c.def.HasDefault = true
	c.def.DefaultExpr = expr
	c.def.Default = nil
	c.def.DefaultSQL = ""
	return c
}

// Unique marks the column as unique.
func (c *Column[T]) Unique() *Column[T] {
	c.def.Unique = true
	return c
}

// GeneratedAlwaysAs marks the column as a generated column.
func (c *Column[T]) GeneratedAlwaysAs(expr Expression, stored bool) *Column[T] {
	if expr == nil {
		panic("schema: generated column requires an expression")
	}
	c.def.GeneratedExpr = expr
	c.def.GeneratedStored = stored
	return c
}

// GeneratedAlwaysAsIdentity marks the column as a PostgreSQL-style identity column (GENERATED ALWAYS AS IDENTITY).
func (c *Column[T]) GeneratedAlwaysAsIdentity() *Column[T] {
	c.def.Identity = IdentityAlways
	c.def.AutoIncrement = true
	return c
}

// GeneratedByDefaultAsIdentity marks the column as a PostgreSQL-style identity column (GENERATED BY DEFAULT AS IDENTITY).
func (c *Column[T]) GeneratedByDefaultAsIdentity() *Column[T] {
	c.def.Identity = IdentityByDefault
	c.def.AutoIncrement = true
	return c
}

// References creates a foreign-key reference to another column.
func (c *Column[T]) References(other ColumnReference) *Column[T] {
	ref := ForeignKeyDef{
		Column:           c.def,
		ReferencedTable:  other.ColumnDef().Table,
		ReferencedColumn: other.ColumnDef(),
	}
	c.def.Table.ForeignKeys = append(c.def.Table.ForeignKeys, ref)

	return c
}

// BelongsTo registers a belongs-to relation on the table.
func (t *TableModel) BelongsTo(name string, source ColumnReference, target ColumnReference) {
	t.addRelation(RelationDef{
		Name:         name,
		Type:         RelationTypeBelongsTo,
		SourceColumn: source.ColumnDef(),
		TargetTable:  target.ColumnDef().Table,
		TargetColumn: target.ColumnDef(),
	})
}

// HasOne registers a has-one relation on the table.
func (t *TableModel) HasOne(name string, source ColumnReference, target ColumnReference) {
	t.addRelation(RelationDef{
		Name:         name,
		Type:         RelationTypeHasOne,
		SourceColumn: source.ColumnDef(),
		TargetTable:  target.ColumnDef().Table,
		TargetColumn: target.ColumnDef(),
	})
}

// HasMany registers a has-many relation on the table.
func (t *TableModel) HasMany(name string, source ColumnReference, target ColumnReference) {
	t.addRelation(RelationDef{
		Name:         name,
		Type:         RelationTypeHasMany,
		SourceColumn: source.ColumnDef(),
		TargetTable:  target.ColumnDef().Table,
		TargetColumn: target.ColumnDef(),
	})
}

// ManyToMany registers a many-to-many relation on the table.
func (t *TableModel) ManyToMany(name string, source ColumnReference, target ColumnReference, joinTable TableReference, joinSource ColumnReference, joinTarget ColumnReference) {
	t.addRelation(RelationDef{
		Name:             name,
		Type:             RelationTypeManyToMany,
		SourceColumn:     source.ColumnDef(),
		TargetTable:      target.ColumnDef().Table,
		TargetColumn:     target.ColumnDef(),
		JoinTable:        joinTable.TableDef(),
		JoinSourceColumn: joinSource.ColumnDef(),
		JoinTargetColumn: joinTarget.ColumnDef(),
	})
}

func (t *TableModel) addRelation(relation RelationDef) {
	if t.def == nil {
		panic("schema: table model is not initialized")
	}
	if relation.Name == "" {
		panic("schema: relation name cannot be empty")
	}
	if relation.SourceColumn == nil || relation.TargetTable == nil || relation.TargetColumn == nil {
		panic(fmt.Sprintf("schema: relation %q requires source and target columns", relation.Name))
	}
	if relation.Type == RelationTypeManyToMany {
		if relation.JoinTable == nil || relation.JoinSourceColumn == nil || relation.JoinTargetColumn == nil {
			panic(fmt.Sprintf("schema: many-to-many relation %q requires join table and columns", relation.Name))
		}
	}
	if relation.SourceColumn.Table != t.def {
		panic(fmt.Sprintf("schema: relation %q source column must belong to table %q", relation.Name, t.def.Name))
	}
	if _, exists := t.def.relationsByName[relation.Name]; exists {
		panic(fmt.Sprintf("schema: relation %q already defined on table %q", relation.Name, t.def.Name))
	}

	t.def.Relations = append(t.def.Relations, relation)
	t.def.relationsByName[relation.Name] = relation
}

// Eq compares this column to a Go value.
func (c *Column[T]) Eq(value T) ComparisonExpr {
	return ComparisonExpr{Left: c, Operator: "=", Right: ValueExpr{Value: value}}
}

// EqExpr compares this column to another SQL expression.
func (c *Column[T]) EqExpr(expr Expression) ComparisonExpr {
	return ComparisonExpr{Left: c, Operator: "=", Right: expr}
}

// Ne compares this column to a Go value.
func (c *Column[T]) Ne(value T) ComparisonExpr {
	return ComparisonExpr{Left: c, Operator: "<>", Right: ValueExpr{Value: value}}
}

// NeExpr compares this column to another SQL expression.
func (c *Column[T]) NeExpr(expr Expression) ComparisonExpr {
	return ComparisonExpr{Left: c, Operator: "<>", Right: expr}
}

// Gt compares this column to a Go value.
func (c *Column[T]) Gt(value T) ComparisonExpr {
	return ComparisonExpr{Left: c, Operator: ">", Right: ValueExpr{Value: value}}
}

// GtExpr compares this column to another SQL expression.
func (c *Column[T]) GtExpr(expr Expression) ComparisonExpr {
	return ComparisonExpr{Left: c, Operator: ">", Right: expr}
}

// Gte compares this column to a Go value.
func (c *Column[T]) Gte(value T) ComparisonExpr {
	return ComparisonExpr{Left: c, Operator: ">=", Right: ValueExpr{Value: value}}
}

// GteExpr compares this column to another SQL expression.
func (c *Column[T]) GteExpr(expr Expression) ComparisonExpr {
	return ComparisonExpr{Left: c, Operator: ">=", Right: expr}
}

// Lt compares this column to a Go value.
func (c *Column[T]) Lt(value T) ComparisonExpr {
	return ComparisonExpr{Left: c, Operator: "<", Right: ValueExpr{Value: value}}
}

// LtExpr compares this column to another SQL expression.
func (c *Column[T]) LtExpr(expr Expression) ComparisonExpr {
	return ComparisonExpr{Left: c, Operator: "<", Right: expr}
}

// Lte compares this column to a Go value.
func (c *Column[T]) Lte(value T) ComparisonExpr {
	return ComparisonExpr{Left: c, Operator: "<=", Right: ValueExpr{Value: value}}
}

// LteExpr compares this column to another SQL expression.
func (c *Column[T]) LteExpr(expr Expression) ComparisonExpr {
	return ComparisonExpr{Left: c, Operator: "<=", Right: expr}
}

// EqCol compares this column to another column.
func (c *Column[T]) EqCol(other ColumnReference) ComparisonExpr {
	return ComparisonExpr{Left: c, Operator: "=", Right: other}
}

// In compares this column to a set of Go values using SQL IN.
func (c *Column[T]) In(values ...T) InExpr {
	exprs := make([]Expression, 0, len(values))
	for _, value := range values {
		exprs = append(exprs, ValueExpr{Value: value})
	}
	return InExpr{Left: c, Values: exprs}
}

// InSubquery compares this column to the result of a subquery.
func (c *Column[T]) InSubquery(subquery Expression) InExpr {
	return InExpr{Left: c, Values: []Expression{subquery}}
}

// Add adds a value to this column.
func (c *Column[T]) Add(value T) BinaryExpr {
	return BinaryExpr{Left: c, Operator: "+", Right: ValueExpr{Value: value}}
}

// AddExpr adds a SQL expression to this column.
func (c *Column[T]) AddExpr(expr Expression) BinaryExpr {
	return BinaryExpr{Left: c, Operator: "+", Right: expr}
}

// Sub subtracts a value from this column.
func (c *Column[T]) Sub(value T) BinaryExpr {
	return BinaryExpr{Left: c, Operator: "-", Right: ValueExpr{Value: value}}
}

// SubExpr subtracts a SQL expression from this column.
func (c *Column[T]) SubExpr(expr Expression) BinaryExpr {
	return BinaryExpr{Left: c, Operator: "-", Right: expr}
}

// Mul multiplies this column by a value.
func (c *Column[T]) Mul(value T) BinaryExpr {
	return BinaryExpr{Left: c, Operator: "*", Right: ValueExpr{Value: value}}
}

// MulExpr multiplies this column by a SQL expression.
func (c *Column[T]) MulExpr(expr Expression) BinaryExpr {
	return BinaryExpr{Left: c, Operator: "*", Right: expr}
}

// Div divides this column by a value.
func (c *Column[T]) Div(value T) BinaryExpr {
	return BinaryExpr{Left: c, Operator: "/", Right: ValueExpr{Value: value}}
}

// DivExpr divides this column by a SQL expression.
func (c *Column[T]) DivExpr(expr Expression) BinaryExpr {
	return BinaryExpr{Left: c, Operator: "/", Right: expr}
}

// Mod calculates the remainder of this column divided by a value.
func (c *Column[T]) Mod(value T) BinaryExpr {
	return BinaryExpr{Left: c, Operator: "%", Right: ValueExpr{Value: value}}
}

// ModExpr calculates the remainder of this column divided by a SQL expression.
func (c *Column[T]) ModExpr(expr Expression) BinaryExpr {
	return BinaryExpr{Left: c, Operator: "%", Right: expr}
}

// NotInSubquery compares this column to the result of a subquery.
func (c *Column[T]) NotInSubquery(subquery Expression) InExpr {
	return InExpr{Left: c, Values: []Expression{subquery}, Negated: true}
}

// IsNull creates an IS NULL predicate.
func (c *Column[T]) IsNull() NullCheckExpr {
	return NullCheckExpr{Expr: c, Negated: false}
}

// IsNotNull creates an IS NOT NULL predicate.
func (c *Column[T]) IsNotNull() NullCheckExpr {
	return NullCheckExpr{Expr: c, Negated: true}
}

// Asc returns an ascending sort expression.
func (c *Column[T]) Asc() OrderExpr {
	return OrderExpr{Expr: c, Direction: SortAsc}
}

// Desc returns a descending sort expression.
func (c *Column[T]) Desc() OrderExpr {
	return OrderExpr{Expr: c, Direction: SortDesc}
}

// As aliases this column in a SELECT list.
func (c *Column[T]) As(alias string) AliasExpr {
	return As(c, alias)
}

func (c *Column[T]) isExpression()         {}
func (c *Column[T]) indexColumnSpec()      {}
func (c *Column[T]) constraintColumnSpec() {}

// ValueExpr wraps a Go value for SQL rendering.
type ValueExpr struct {
	Value any
}

func (ValueExpr) isExpression() {}

// PlaceholderExpr references a named runtime value for prepared query execution.
type PlaceholderExpr struct {
	Name string
}

func (PlaceholderExpr) isExpression() {}

// Placeholder references a named runtime value in a prepared query.
func Placeholder(name string) PlaceholderExpr {
	if strings.TrimSpace(name) == "" {
		panic("schema: Placeholder requires a non-empty name")
	}
	return PlaceholderExpr{Name: name}
}

// ComparisonExpr compares two expressions.
type ComparisonExpr struct {
	Left     Expression
	Operator string
	Right    Expression
}

func (ComparisonExpr) isExpression() {}
func (ComparisonExpr) isPredicate()  {}

// InExpr renders an IN predicate.
type InExpr struct {
	Left    Expression
	Values  []Expression
	Negated bool
}

func (InExpr) isExpression() {}
func (InExpr) isPredicate()  {}

// BetweenExpr renders a BETWEEN predicate.
type BetweenExpr struct {
	Left    Expression
	Start   Expression
	End     Expression
	Negated bool
}

func (BetweenExpr) isExpression() {}
func (BetweenExpr) isPredicate()  {}

// NotExpr renders a logical NOT.
type NotExpr struct {
	Expr Predicate
}

func (NotExpr) isExpression() {}
func (NotExpr) isPredicate()  {}

// ExistsExpr renders an EXISTS or NOT EXISTS subquery.
type ExistsExpr struct {
	Subquery Expression
	Negated  bool
}

func (ExistsExpr) isExpression() {}
func (ExistsExpr) isPredicate()  {}

// NullCheckExpr renders IS NULL or IS NOT NULL.
type NullCheckExpr struct {
	Expr    Expression
	Negated bool
}

func (NullCheckExpr) isExpression() {}
func (NullCheckExpr) isPredicate()  {}

// BinaryExpr represents an arithmetic operation.
type BinaryExpr struct {
	Left     Expression
	Operator string
	Right    Expression
}

func (BinaryExpr) isExpression() {}

// As aliases this binary expression in a SELECT list.
func (b BinaryExpr) As(alias string) AliasExpr {
	return As(b, alias)
}

// Eq compares this binary expression to a value.
func (b BinaryExpr) Eq(value any) ComparisonExpr {
	return Eq(b, value)
}

// Ne compares this binary expression to a value.
func (b BinaryExpr) Ne(value any) ComparisonExpr {
	return Ne(b, value)
}

// Gt compares this binary expression to a value.
func (b BinaryExpr) Gt(value any) ComparisonExpr {
	return Gt(b, value)
}

// Gte compares this binary expression to a value.
func (b BinaryExpr) Gte(value any) ComparisonExpr {
	return Gte(b, value)
}

// Lt compares this binary expression to a value.
func (b BinaryExpr) Lt(value any) ComparisonExpr {
	return Lt(b, value)
}

// Lte compares this binary expression to a value.
func (b BinaryExpr) Lte(value any) ComparisonExpr {
	return Lte(b, value)
}

// Asc returns an ascending sort expression.
func (b BinaryExpr) Asc() OrderExpr {
	return Asc(b)
}

// Desc returns a descending sort expression.
func (b BinaryExpr) Desc() OrderExpr {
	return Desc(b)
}

// IsNull creates an IS NULL predicate.
func (b BinaryExpr) IsNull() NullCheckExpr {
	return IsNull(b)
}

// IsNotNull creates an IS NOT NULL predicate.
func (b BinaryExpr) IsNotNull() NullCheckExpr {
	return IsNotNull(b)
}

// Add adds a value or expression to this binary expression.
func (b BinaryExpr) Add(value any) BinaryExpr {
	return BinaryExpr{Left: b, Operator: "+", Right: wrapValue(value)}
}

// Sub subtracts a value or expression from this binary expression.
func (b BinaryExpr) Sub(value any) BinaryExpr {
	return BinaryExpr{Left: b, Operator: "-", Right: wrapValue(value)}
}

// Mul multiplies this binary expression by a value or expression.
func (b BinaryExpr) Mul(value any) BinaryExpr {
	return BinaryExpr{Left: b, Operator: "*", Right: wrapValue(value)}
}

// Div divides this binary expression by a value or expression.
func (b BinaryExpr) Div(value any) BinaryExpr {
	return BinaryExpr{Left: b, Operator: "/", Right: wrapValue(value)}
}

// Mod calculates the remainder of this binary expression divided by a value or expression.
func (b BinaryExpr) Mod(value any) BinaryExpr {
	return BinaryExpr{Left: b, Operator: "%", Right: wrapValue(value)}
}

// ConcatExpr represents a SQL concatenation.
type ConcatExpr struct {
	Exprs []Expression
}

func (ConcatExpr) isExpression() {}

// As aliases this concatenation expression in a SELECT list.
func (c ConcatExpr) As(alias string) AliasExpr {
	return As(c, alias)
}

// LogicalExpr groups predicates with AND or OR.
type LogicalExpr struct {
	Operator string
	Exprs    []Predicate
}

func (LogicalExpr) isExpression() {}
func (LogicalExpr) isPredicate()  {}

// OrderExpr renders ORDER BY expressions and indexed sort directions.
type OrderExpr struct {
	Expr       Expression
	Direction  SortDirection
	NullsOrder NullsOrder
}

// NullsFirst sets NULLS FIRST on the order expression.
func (o OrderExpr) NullsFirst() OrderExpr {
	o.NullsOrder = NullsFirst
	return o
}

// NullsLast sets NULLS LAST on the order expression.
func (o OrderExpr) NullsLast() OrderExpr {
	o.NullsOrder = NullsLast
	return o
}

func (OrderExpr) indexColumnSpec() {}

// CaseExpr represents a SQL CASE expression.
type CaseExpr struct {
	ValueExpression Expression // Used for simple CASE
	WhenThenPairs   []WhenThen
	ElseExpression  Expression
}

func (CaseExpr) isExpression() {}

// WhenThen represents a single WHEN ... THEN pair in a CASE expression.
type WhenThen struct {
	When Expression
	Then Expression
}

// CaseBuilder provides a fluent API for building CASE expressions.
type CaseBuilder struct {
	caseExpr CaseExpr
}

// Case starts a new CASE expression.
// If an expression is provided, it builds a simple CASE (CASE expr WHEN ...).
// If no expression is provided, it builds a searched CASE (CASE WHEN ...).
// Passing more than one expression is a programming error and will panic.
func Case(expr ...any) *CaseBuilder {
	if len(expr) > 1 {
		panic("schema: Case accepts at most one expression")
	}
	builder := &CaseBuilder{}
	if len(expr) == 1 {
		builder.caseExpr.ValueExpression = ReflectExpression(expr[0])
	}
	return builder
}

// When adds a WHEN ... THEN pair to the CASE expression.
func (b *CaseBuilder) When(when any, then any) *CaseBuilder {
	b.caseExpr.WhenThenPairs = append(b.caseExpr.WhenThenPairs, WhenThen{
		When: ReflectExpression(when),
		Then: ReflectExpression(then),
	})
	return b
}

// Else sets the optional ELSE expression for the CASE expression.
func (b *CaseBuilder) Else(elseExpr any) *CaseBuilder {
	b.caseExpr.ElseExpression = ReflectExpression(elseExpr)
	return b
}

// End finishes building the CASE expression and returns it.
func (b *CaseBuilder) End() CaseExpr {
	return b.caseExpr
}

// As aliases this CASE expression in a SELECT list.
func (c CaseExpr) As(alias string) AliasExpr {
	return As(c, alias)
}

// Eq compares this CASE expression to a value.
func (c CaseExpr) Eq(value any) ComparisonExpr {
	return Eq(c, value)
}

// Ne compares this CASE expression to a value.
func (c CaseExpr) Ne(value any) ComparisonExpr {
	return Ne(c, value)
}

// Gt compares this CASE expression to a value.
func (c CaseExpr) Gt(value any) ComparisonExpr {
	return Gt(c, value)
}

// Gte compares this CASE expression to a value.
func (c CaseExpr) Gte(value any) ComparisonExpr {
	return Gte(c, value)
}

// Lt compares this CASE expression to a value.
func (c CaseExpr) Lt(value any) ComparisonExpr {
	return Lt(c, value)
}

// Lte compares this CASE expression to a value.
func (c CaseExpr) Lte(value any) ComparisonExpr {
	return Lte(c, value)
}

// Asc returns an ascending sort expression.
func (c CaseExpr) Asc() OrderExpr {
	return Asc(c)
}

// Desc returns a descending sort expression.
func (c CaseExpr) Desc() OrderExpr {
	return Desc(c)
}

// IsNull creates an IS NULL predicate.
func (c CaseExpr) IsNull() NullCheckExpr {
	return IsNull(c)
}

// IsNotNull creates an IS NOT NULL predicate.
func (c CaseExpr) IsNotNull() NullCheckExpr {
	return IsNotNull(c)
}

// AggregateExpr renders SQL aggregate functions.
//
// Function must be non-empty. Distinct must not be combined with Star.
type AggregateExpr struct {
	Function string
	Expr     Expression
	Star     bool
	Distinct bool
}

func (AggregateExpr) isExpression() {}

// As aliases this computed expression in a SELECT list.
func (a AggregateExpr) As(alias string) AliasExpr {
	return As(a, alias)
}

// Eq compares this aggregate expression to a value.
func (a AggregateExpr) Eq(value any) ComparisonExpr {
	return Eq(a, value)
}

// Ne compares this aggregate expression to a value.
func (a AggregateExpr) Ne(value any) ComparisonExpr {
	return Ne(a, value)
}

// Gt compares this aggregate expression to a value.
func (a AggregateExpr) Gt(value any) ComparisonExpr {
	return Gt(a, value)
}

// Gte compares this aggregate expression to a value.
func (a AggregateExpr) Gte(value any) ComparisonExpr {
	return Gte(a, value)
}

// Lt compares this aggregate expression to a value.
func (a AggregateExpr) Lt(value any) ComparisonExpr {
	return Lt(a, value)
}

// Lte compares this aggregate expression to a value.
func (a AggregateExpr) Lte(value any) ComparisonExpr {
	return Lte(a, value)
}

// Asc returns an ascending sort expression.
func (a AggregateExpr) Asc() OrderExpr {
	return Asc(a)
}

// Desc returns a descending sort expression.
func (a AggregateExpr) Desc() OrderExpr {
	return Desc(a)
}

// IsNull creates an IS NULL predicate.
func (a AggregateExpr) IsNull() NullCheckExpr {
	return IsNull(a)
}

// IsNotNull creates an IS NOT NULL predicate.
func (a AggregateExpr) IsNotNull() NullCheckExpr {
	return IsNotNull(a)
}

// CoalesceExpr renders COALESCE(expr1, expr2, ...).
type CoalesceExpr struct {
	Exprs []Expression
}

func (CoalesceExpr) isExpression() {}

// As aliases this computed expression in a SELECT list.
func (c CoalesceExpr) As(alias string) AliasExpr {
	return As(c, alias)
}

// Eq compares this coalesce expression to a value.
func (c CoalesceExpr) Eq(value any) ComparisonExpr {
	return Eq(c, value)
}

// Ne compares this coalesce expression to a value.
func (c CoalesceExpr) Ne(value any) ComparisonExpr {
	return Ne(c, value)
}

// Gt compares this coalesce expression to a value.
func (c CoalesceExpr) Gt(value any) ComparisonExpr {
	return Gt(c, value)
}

// Gte compares this coalesce expression to a value.
func (c CoalesceExpr) Gte(value any) ComparisonExpr {
	return Gte(c, value)
}

// Lt compares this coalesce expression to a value.
func (c CoalesceExpr) Lt(value any) ComparisonExpr {
	return Lt(c, value)
}

// Lte compares this coalesce expression to a value.
func (c CoalesceExpr) Lte(value any) ComparisonExpr {
	return Lte(c, value)
}

// Asc returns an ascending sort expression.
func (c CoalesceExpr) Asc() OrderExpr {
	return Asc(c)
}

// Desc returns a descending sort expression.
func (c CoalesceExpr) Desc() OrderExpr {
	return Desc(c)
}

// IsNull creates an IS NULL predicate.
func (c CoalesceExpr) IsNull() NullCheckExpr {
	return IsNull(c)
}

// IsNotNull creates an IS NOT NULL predicate.
func (c CoalesceExpr) IsNotNull() NullCheckExpr {
	return IsNotNull(c)
}

// AliasExpr renames a computed expression in a select list.
type AliasExpr struct {
	Expr  Expression
	Alias string
}

func (AliasExpr) isExpression() {}

// Count renders COUNT(*) when no expression is provided, or COUNT(expr) when one expression is provided.
func Count(exprs ...Expression) AggregateExpr {
	switch len(exprs) {
	case 0:
		return AggregateExpr{Function: "COUNT", Star: true}
	case 1:
		return AggregateExpr{Function: "COUNT", Expr: exprs[0]}
	default:
		panic("schema: Count accepts zero or one expression")
	}
}

// Sum renders SUM(expr).
func Sum(expr Expression) AggregateExpr {
	if expr == nil {
		panic("schema: Sum requires a non-nil expression")
	}
	return AggregateExpr{Function: "SUM", Expr: expr}
}

// Avg renders AVG(expr).
func Avg(expr Expression) AggregateExpr {
	if expr == nil {
		panic("schema: Avg requires a non-nil expression")
	}
	return AggregateExpr{Function: "AVG", Expr: expr}
}

// Min renders MIN(expr).
func Min(expr Expression) AggregateExpr {
	if expr == nil {
		panic("schema: Min requires a non-nil expression")
	}
	return AggregateExpr{Function: "MIN", Expr: expr}
}

// Max renders MAX(expr).
func Max(expr Expression) AggregateExpr {
	if expr == nil {
		panic("schema: Max requires a non-nil expression")
	}
	return AggregateExpr{Function: "MAX", Expr: expr}
}

// Coalesce renders COALESCE(expr1, expr2, ...).
func Coalesce(exprs ...any) CoalesceExpr {
	if len(exprs) < 2 {
		panic("schema: Coalesce requires at least two expressions")
	}
	resolved := make([]Expression, 0, len(exprs))
	for _, expr := range exprs {
		if expr == nil {
			panic("schema: Coalesce requires non-nil expressions")
		}
		resolved = append(resolved, ReflectExpression(expr))
	}
	return CoalesceExpr{Exprs: resolved}
}

// NotIn compares this column to a set of Go values using SQL NOT IN.
func (c *AnyColumn) NotIn(values ...any) InExpr {
	expr := c.In(values...)
	expr.Negated = true
	return expr
}

// Like compares this column to a pattern using SQL LIKE.
// Intended for string-typed columns.
func (c *AnyColumn) Like(pattern string) ComparisonExpr {
	return ComparisonExpr{Left: c, Operator: "LIKE", Right: ValueExpr{Value: pattern}}
}

// NotLike compares this column to a pattern using SQL NOT LIKE.
// Intended for string-typed columns.
func (c *AnyColumn) NotLike(pattern string) ComparisonExpr {
	return ComparisonExpr{Left: c, Operator: "NOT LIKE", Right: ValueExpr{Value: pattern}}
}

// ILike compares this column to a pattern using SQL ILIKE (case-insensitive LIKE).
// Intended for string-typed columns.
func (c *AnyColumn) ILike(pattern string) ComparisonExpr {
	return ComparisonExpr{Left: c, Operator: "ILIKE", Right: ValueExpr{Value: pattern}}
}

// NotILike compares this column to a pattern using SQL NOT ILIKE.
// Intended for string-typed columns.
func (c *AnyColumn) NotILike(pattern string) ComparisonExpr {
	return ComparisonExpr{Left: c, Operator: "NOT ILIKE", Right: ValueExpr{Value: pattern}}
}

// Between compares this column to a range using SQL BETWEEN.
func (c *AnyColumn) Between(start, end any) BetweenExpr {
	return BetweenExpr{
		Left:  c,
		Start: ValueExpr{Value: start},
		End:   ValueExpr{Value: end},
	}
}

// NotBetween compares this column to a range using SQL NOT BETWEEN.
func (c *AnyColumn) NotBetween(start, end any) BetweenExpr {
	expr := c.Between(start, end)
	expr.Negated = true
	return expr
}

// As aliases an expression in a SELECT list.
func As(expr Expression, alias string) AliasExpr {
	if expr == nil {
		panic("schema: As requires a non-nil expression")
	}
	if alias == "" {
		panic("schema: As requires a non-empty alias")
	}
	return AliasExpr{Expr: expr, Alias: alias}
}

// NotIn compares this column to a set of Go values using SQL NOT IN.
func (c *Column[T]) NotIn(values ...T) InExpr {
	expr := c.In(values...)
	expr.Negated = true
	return expr
}

// Like compares this column to a pattern using SQL LIKE.
// Intended for string-typed columns.
func (c *Column[T]) Like(pattern string) ComparisonExpr {
	return ComparisonExpr{Left: c, Operator: "LIKE", Right: ValueExpr{Value: pattern}}
}

// NotLike compares this column to a pattern using SQL NOT LIKE.
// Intended for string-typed columns.
func (c *Column[T]) NotLike(pattern string) ComparisonExpr {
	return ComparisonExpr{Left: c, Operator: "NOT LIKE", Right: ValueExpr{Value: pattern}}
}

// ILike compares this column to a pattern using SQL ILIKE (case-insensitive LIKE).
// Intended for string-typed columns.
func (c *Column[T]) ILike(pattern string) ComparisonExpr {
	return ComparisonExpr{Left: c, Operator: "ILIKE", Right: ValueExpr{Value: pattern}}
}

// NotILike compares this column to a pattern using SQL NOT ILIKE.
// Intended for string-typed columns.
func (c *Column[T]) NotILike(pattern string) ComparisonExpr {
	return ComparisonExpr{Left: c, Operator: "NOT ILIKE", Right: ValueExpr{Value: pattern}}
}

// Between compares this column to a range using SQL BETWEEN.
func (c *Column[T]) Between(start, end T) BetweenExpr {
	return BetweenExpr{
		Left:  c,
		Start: ValueExpr{Value: start},
		End:   ValueExpr{Value: end},
	}
}

// NotBetween compares this column to a range using SQL NOT BETWEEN.
func (c *Column[T]) NotBetween(start, end T) BetweenExpr {
	expr := c.Between(start, end)
	expr.Negated = true
	return expr
}

// RawExpr is an escape hatch for raw SQL expressions or predicates with bound args.
type RawExpr struct {
	SQL  string
	Args []any
}

func (RawExpr) isExpression() {}
func (RawExpr) isPredicate()  {}

// As aliases this raw expression in a SELECT list.
func (r RawExpr) As(alias string) AliasExpr {
	return As(r, alias)
}

// Eq compares this raw expression to a value.
func (r RawExpr) Eq(value any) ComparisonExpr {
	return Eq(r, value)
}

// Ne compares this raw expression to a value.
func (r RawExpr) Ne(value any) ComparisonExpr {
	return Ne(r, value)
}

// Gt compares this raw expression to a value.
func (r RawExpr) Gt(value any) ComparisonExpr {
	return Gt(r, value)
}

// Gte compares this raw expression to a value.
func (r RawExpr) Gte(value any) ComparisonExpr {
	return Gte(r, value)
}

// Lt compares this raw expression to a value.
func (r RawExpr) Lt(value any) ComparisonExpr {
	return Lt(r, value)
}

// Lte compares this raw expression to a value.
func (r RawExpr) Lte(value any) ComparisonExpr {
	return Lte(r, value)
}

// Asc returns an ascending sort expression.
func (r RawExpr) Asc() OrderExpr {
	return Asc(r)
}

// Desc returns a descending sort expression.
func (r RawExpr) Desc() OrderExpr {
	return Desc(r)
}

// IsNull creates an IS NULL predicate.
func (r RawExpr) IsNull() NullCheckExpr {
	return IsNull(r)
}

// IsNotNull creates an IS NOT NULL predicate.
func (r RawExpr) IsNotNull() NullCheckExpr {
	return IsNotNull(r)
}

// Raw returns a raw SQL expression.
func Raw(sql string, args ...any) RawExpr {
	return RawExpr{SQL: sql, Args: args}
}

// SQL is an alias for Raw, matching Drizzle's familiar entry point.
func SQL(sql string, args ...any) RawExpr {
	return Raw(sql, args...)
}

// And combines predicates with AND.
func And(predicates ...Predicate) LogicalExpr {
	return LogicalExpr{Operator: "AND", Exprs: predicates}
}

// Or combines predicates with OR.
func Or(predicates ...Predicate) LogicalExpr {
	return LogicalExpr{Operator: "OR", Exprs: predicates}
}

// Not negates a predicate using SQL NOT.
func Not(predicate Predicate) NotExpr {
	return NotExpr{Expr: predicate}
}

// Exists checks if a subquery returns any rows.
func Exists(subquery Expression) ExistsExpr {
	return ExistsExpr{Subquery: subquery}
}

// NotExists checks if a subquery returns no rows.
func NotExists(subquery Expression) ExistsExpr {
	return ExistsExpr{Subquery: subquery, Negated: true}
}

// Concat renders a SQL concatenation of multiple expressions or values.
func Concat(values ...any) ConcatExpr {
	if len(values) < 2 {
		panic("schema: Concat requires at least two values")
	}
	exprs := make([]Expression, 0, len(values))
	for _, v := range values {
		exprs = append(exprs, ReflectExpression(v))
	}
	return ConcatExpr{Exprs: exprs}
}

func wrapValue(v any) Expression {
	if expr, ok := v.(Expression); ok {
		return expr
	}
	return ValueExpr{Value: v}
}

// ReflectExpression converts a raw value to an Expression.
// If the value already implements Expression, it is returned as-is.
// Otherwise, it is wrapped in a ValueExpr.
func ReflectExpression(v any) Expression {
	return wrapValue(v)
}

// Eq creates an equality comparison expression.
func Eq(left, right any) ComparisonExpr {
	return ComparisonExpr{Left: ReflectExpression(left), Operator: "=", Right: ReflectExpression(right)}
}

// Ne creates a not-equal comparison expression.
func Ne(left, right any) ComparisonExpr {
	return ComparisonExpr{Left: ReflectExpression(left), Operator: "<>", Right: ReflectExpression(right)}
}

// Gt creates a greater-than comparison expression.
func Gt(left, right any) ComparisonExpr {
	return ComparisonExpr{Left: ReflectExpression(left), Operator: ">", Right: ReflectExpression(right)}
}

// Gte creates a greater-than-or-equal comparison expression.
func Gte(left, right any) ComparisonExpr {
	return ComparisonExpr{Left: ReflectExpression(left), Operator: ">=", Right: ReflectExpression(right)}
}

// Lt creates a less-than comparison expression.
func Lt(left, right any) ComparisonExpr {
	return ComparisonExpr{Left: ReflectExpression(left), Operator: "<", Right: ReflectExpression(right)}
}

// Lte creates a less-than-or-equal comparison expression.
func Lte(left, right any) ComparisonExpr {
	return ComparisonExpr{Left: ReflectExpression(left), Operator: "<=", Right: ReflectExpression(right)}
}

// Like creates a LIKE comparison expression.
func Like(left any, pattern string) ComparisonExpr {
	return ComparisonExpr{Left: ReflectExpression(left), Operator: "LIKE", Right: ValueExpr{Value: pattern}}
}

// NotLike creates a NOT LIKE comparison expression.
func NotLike(left any, pattern string) ComparisonExpr {
	return ComparisonExpr{Left: ReflectExpression(left), Operator: "NOT LIKE", Right: ValueExpr{Value: pattern}}
}

// ILike creates an ILIKE comparison expression.
func ILike(left any, pattern string) ComparisonExpr {
	return ComparisonExpr{Left: ReflectExpression(left), Operator: "ILIKE", Right: ValueExpr{Value: pattern}}
}

// NotILike creates a NOT ILIKE comparison expression.
func NotILike(left any, pattern string) ComparisonExpr {
	return ComparisonExpr{Left: ReflectExpression(left), Operator: "NOT ILIKE", Right: ValueExpr{Value: pattern}}
}

// In creates an IN comparison expression.
func In(left any, values ...any) InExpr {
	exprs := make([]Expression, 0, len(values))
	for _, v := range values {
		exprs = append(exprs, ReflectExpression(v))
	}
	return InExpr{Left: ReflectExpression(left), Values: exprs}
}

// NotIn creates a NOT IN comparison expression.
func NotIn(left any, values ...any) InExpr {
	expr := In(left, values...)
	expr.Negated = true
	return expr
}

// Between creates a BETWEEN comparison expression.
func Between(left, start, end any) BetweenExpr {
	return BetweenExpr{
		Left:  ReflectExpression(left),
		Start: ReflectExpression(start),
		End:   ReflectExpression(end),
	}
}

// NotBetween creates a NOT BETWEEN comparison expression.
func NotBetween(left, start, end any) BetweenExpr {
	expr := Between(left, start, end)
	expr.Negated = true
	return expr
}

// IsNull creates an IS NULL expression.
func IsNull(expr any) NullCheckExpr {
	return NullCheckExpr{Expr: ReflectExpression(expr), Negated: false}
}

// IsNotNull creates an IS NOT NULL expression.
func IsNotNull(expr any) NullCheckExpr {
	return NullCheckExpr{Expr: ReflectExpression(expr), Negated: true}
}

// Asc creates an ascending sort expression.
func Asc(expr any) OrderExpr {
	return OrderExpr{Expr: ReflectExpression(expr), Direction: SortAsc}
}

// Desc creates a descending sort expression.
func Desc(expr any) OrderExpr {
	return OrderExpr{Expr: ReflectExpression(expr), Direction: SortDesc}
}

// IndexBuilder configures a table index.
type IndexBuilder struct {
	table *TableDef
	index int
}

// On binds ordered columns to the index.
func (b *IndexBuilder) On(columns ...IndexColumnSpec) *IndexBuilder {
	resolved := make([]IndexColumn, 0, len(columns))
	for _, column := range columns {
		switch value := column.(type) {
		case ColumnReference:
			resolved = append(resolved, IndexColumn{Column: value, Direction: SortAsc})
		case OrderExpr:
			col, ok := value.Expr.(ColumnReference)
			if !ok {
				panic("schema: index order expression must wrap a column")
			}
			resolved = append(resolved, IndexColumn{
				Column:     col,
				Direction:  value.Direction,
				NullsOrder: value.NullsOrder,
			})
		default:
			panic(fmt.Sprintf("schema: unsupported index column type %T", column))
		}
	}
	b.table.Indexes[b.index].Columns = resolved
	return b
}

// Where adds a filter to the index definition (partial index).
func (b *IndexBuilder) Where(predicate Predicate) *IndexBuilder {
	b.table.Indexes[b.index].WhereExpr = predicate
	return b
}

// ConstraintBuilder configures a table constraint backed by columns.
type ConstraintBuilder struct {
	table      *TableDef
	constraint int
}

// On binds columns to the table constraint.
func (b *ConstraintBuilder) On(columns ...ConstraintColumnSpec) *ConstraintBuilder {
	b.table.Constraints[b.constraint].Columns = resolveConstraintColumns(b.table, columns...)
	return b
}

// ForeignKeyBuilder configures a table-level foreign key constraint.
type ForeignKeyBuilder struct {
	table      *TableDef
	constraint int
}

// On binds source columns to the foreign key.
func (b *ForeignKeyBuilder) On(columns ...ConstraintColumnSpec) *ForeignKeyBuilder {
	b.table.Constraints[b.constraint].Columns = resolveConstraintColumns(b.table, columns...)
	return b
}

// References binds referenced columns to the foreign key.
func (b *ForeignKeyBuilder) References(columns ...ConstraintColumnSpec) *ForeignKeyBuilder {
	resolved := resolveConstraintColumns(nil, columns...)
	constraint := &b.table.Constraints[b.constraint]
	if len(resolved) == 0 {
		panic("schema: foreign key requires at least one referenced column")
	}

	referencedTable := resolved[0].Table
	for _, column := range resolved[1:] {
		if column.Table != referencedTable {
			panic("schema: foreign key referenced columns must belong to the same table")
		}
	}

	constraint.ReferencedTable = referencedTable
	constraint.ReferencedCols = resolved
	return b
}

// OnDelete sets the ON DELETE action for the foreign key.
func (b *ForeignKeyBuilder) OnDelete(action ForeignKeyAction) *ForeignKeyBuilder {
	b.table.Constraints[b.constraint].OnDelete = action
	return b
}

// OnUpdate sets the ON UPDATE action for the foreign key.
func (b *ForeignKeyBuilder) OnUpdate(action ForeignKeyAction) *ForeignKeyBuilder {
	b.table.Constraints[b.constraint].OnUpdate = action
	return b
}

// Timestamps can be embedded into models used for scans and payloads.
type Timestamps struct {
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

// SoftDelete can be embedded into models used for scans and payloads.
type SoftDelete struct {
	DeletedAt *time.Time `db:"deleted_at"`
}

// TableCloner is implemented by expressions that need to be rebound when a table is aliased.
type TableCloner interface {
	CloneForTable(*TableDef) any
}

func (c *AnyColumn) CloneForTable(table *TableDef) any {
	clonedMeta, ok := table.columnsByName[c.def.Name]
	if !ok {
		panic(fmt.Sprintf("schema: alias missing column %q", c.def.Name))
	}

	return &AnyColumn{def: clonedMeta}
}

func (c *Column[T]) CloneForTable(table *TableDef) any {
	clonedMeta, ok := table.columnsByName[c.def.Name]
	if !ok {
		panic(fmt.Sprintf("schema: alias missing column %q", c.def.Name))
	}

	return &Column[T]{def: clonedMeta}
}

func addColumn[T any](table *TableDef, name string, columnType ColumnType, nullable bool, autoIncrement bool) *Column[T] {
	if table == nil {
		panic("schema: table model is not initialized")
	}
	if _, exists := table.columnsByName[name]; exists {
		panic(fmt.Sprintf("schema: duplicate column %q on table %q", name, table.Name))
	}

	def := &ColumnDef{
		Table:         table,
		Name:          name,
		Type:          columnType,
		Nullable:      nullable,
		AutoIncrement: autoIncrement,
	}
	table.Columns = append(table.Columns, def)
	table.columnsByName[name] = def

	return &Column[T]{def: def}
}

func resolveConstraintColumns(table *TableDef, columns ...ConstraintColumnSpec) []*ColumnDef {
	resolved := make([]*ColumnDef, 0, len(columns))
	for _, column := range columns {
		ref, ok := column.(ColumnReference)
		if !ok {
			panic(fmt.Sprintf("schema: unsupported constraint column type %T", column))
		}
		def := ref.ColumnDef()
		if def == nil {
			panic("schema: constraint column must have metadata")
		}
		if table != nil && def.Table != table {
			panic(fmt.Sprintf("schema: constraint column %q must belong to table %q", def.Name, table.Name))
		}
		resolved = append(resolved, def)
	}
	return resolved
}

func cloneTableDef(src *TableDef, alias string) *TableDef {
	cloned := &TableDef{
		Name:            src.Name,
		Alias:           alias,
		IsView:          src.IsView,
		Columns:         make([]*ColumnDef, 0, len(src.Columns)),
		Indexes:         make([]IndexDef, len(src.Indexes)),
		Constraints:     make([]ConstraintDef, len(src.Constraints)),
		ForeignKeys:     make([]ForeignKeyDef, 0, len(src.ForeignKeys)),
		Relations:       make([]RelationDef, 0, len(src.Relations)),
		columnsByName:   make(map[string]*ColumnDef, len(src.Columns)),
		relationsByName: make(map[string]RelationDef, len(src.Relations)),
	}

	if src.ViewQuery != nil {
		cloned.ViewQuery = cloneExpressionForTable(src.ViewQuery, cloned)
	}

	for _, column := range src.Columns {
		copyColumn := *column
		copyColumn.Type.EnumValues = append([]string(nil), column.Type.EnumValues...)
		copyColumn.Table = cloned
		if column.GeneratedExpr != nil {
			copyColumn.GeneratedExpr = cloneExpressionForTable(column.GeneratedExpr, cloned)
		}
		cloned.Columns = append(cloned.Columns, &copyColumn)
		cloned.columnsByName[copyColumn.Name] = &copyColumn
	}

	for idx := range src.Indexes {
		clonedIndex := IndexDef{
			Name:   src.Indexes[idx].Name,
			Unique: src.Indexes[idx].Unique,
			Where:  src.Indexes[idx].Where,
		}
		for _, indexedColumn := range src.Indexes[idx].Columns {
			clonedIndex.Columns = append(clonedIndex.Columns, IndexColumn{
				Column:    &AnyColumn{def: cloned.columnsByName[indexedColumn.Column.ColumnDef().Name]},
				Direction: indexedColumn.Direction,
			})
		}
		cloned.Indexes[idx] = clonedIndex
	}

	for idx := range src.Constraints {
		clonedConstraint := ConstraintDef{
			Name:            src.Constraints[idx].Name,
			Type:            src.Constraints[idx].Type,
			ReferencedTable: src.Constraints[idx].ReferencedTable,
			OnDelete:        src.Constraints[idx].OnDelete,
			OnUpdate:        src.Constraints[idx].OnUpdate,
		}
		for _, column := range src.Constraints[idx].Columns {
			clonedConstraint.Columns = append(clonedConstraint.Columns, cloned.columnsByName[column.Name])
		}
		for _, column := range src.Constraints[idx].ReferencedCols {
			if src.Constraints[idx].ReferencedTable == src {
				clonedConstraint.ReferencedCols = append(clonedConstraint.ReferencedCols, cloned.columnsByName[column.Name])
				clonedConstraint.ReferencedTable = cloned
				continue
			}
			clonedConstraint.ReferencedCols = append(clonedConstraint.ReferencedCols, column)
		}
		if src.Constraints[idx].Check != nil {
			clonedConstraint.Check = cloneExpressionForTable(src.Constraints[idx].Check, cloned).(Predicate)
		}
		cloned.Constraints[idx] = clonedConstraint
	}

	for _, foreignKey := range src.ForeignKeys {
		cloned.ForeignKeys = append(cloned.ForeignKeys, ForeignKeyDef{
			Name:             foreignKey.Name,
			Column:           cloned.columnsByName[foreignKey.Column.Name],
			ReferencedTable:  foreignKey.ReferencedTable,
			ReferencedColumn: foreignKey.ReferencedColumn,
			OnDelete:         foreignKey.OnDelete,
			OnUpdate:         foreignKey.OnUpdate,
		})
	}

	for _, relation := range src.Relations {
		aliasedRelation := RelationDef{
			Name:             relation.Name,
			Type:             relation.Type,
			SourceColumn:     cloned.columnsByName[relation.SourceColumn.Name],
			TargetTable:      relation.TargetTable,
			TargetColumn:     relation.TargetColumn,
			JoinTable:        relation.JoinTable,
			JoinSourceColumn: relation.JoinSourceColumn,
			JoinTargetColumn: relation.JoinTargetColumn,
		}
		if relation.TargetTable == src {
			aliasedRelation.TargetTable = cloned
			aliasedRelation.TargetColumn = cloned.columnsByName[relation.TargetColumn.Name]
		}
		if relation.JoinTable == src {
			aliasedRelation.JoinTable = cloned
			aliasedRelation.JoinSourceColumn = cloned.columnsByName[relation.JoinSourceColumn.Name]
			aliasedRelation.JoinTargetColumn = cloned.columnsByName[relation.JoinTargetColumn.Name]
		}
		cloned.Relations = append(cloned.Relations, aliasedRelation)
		cloned.relationsByName[aliasedRelation.Name] = aliasedRelation
	}

	return cloned
}

func cloneExpressionForTable(expr Expression, table *TableDef) Expression {
	switch value := expr.(type) {
	case TableCloner:
		cloned, ok := value.CloneForTable(table).(Expression)
		if !ok {
			panic(fmt.Sprintf("schema: cloned expression %T is not an expression", value))
		}
		return cloned
	case ValueExpr:
		return value
	case PlaceholderExpr:
		return value
	case ComparisonExpr:
		return ComparisonExpr{
			Left:     cloneExpressionForTable(value.Left, table),
			Operator: value.Operator,
			Right:    cloneExpressionForTable(value.Right, table),
		}
	case InExpr:
		cloned := InExpr{
			Left:    cloneExpressionForTable(value.Left, table),
			Negated: value.Negated,
		}
		for _, item := range value.Values {
			cloned.Values = append(cloned.Values, cloneExpressionForTable(item, table))
		}
		return cloned
	case BetweenExpr:
		return BetweenExpr{
			Left:    cloneExpressionForTable(value.Left, table),
			Start:   cloneExpressionForTable(value.Start, table),
			End:     cloneExpressionForTable(value.End, table),
			Negated: value.Negated,
		}
	case NotExpr:
		return NotExpr{
			Expr: cloneExpressionForTable(value.Expr, table).(Predicate),
		}
	case ExistsExpr:
		return ExistsExpr{
			Subquery: cloneExpressionForTable(value.Subquery, table),
			Negated:  value.Negated,
		}
	case NullCheckExpr:
		return NullCheckExpr{Expr: cloneExpressionForTable(value.Expr, table), Negated: value.Negated}
	case LogicalExpr:
		cloned := LogicalExpr{Operator: value.Operator, Exprs: make([]Predicate, 0, len(value.Exprs))}
		for _, part := range value.Exprs {
			cloned.Exprs = append(cloned.Exprs, cloneExpressionForTable(part, table).(Predicate))
		}
		return cloned
	case AggregateExpr:
		cloned := value
		if value.Expr != nil {
			cloned.Expr = cloneExpressionForTable(value.Expr, table)
		}
		return cloned
	case CaseExpr:
		cloned := CaseExpr{
			WhenThenPairs: make([]WhenThen, len(value.WhenThenPairs)),
		}
		if value.ValueExpression != nil {
			cloned.ValueExpression = cloneExpressionForTable(value.ValueExpression, table)
		}
		for idx, pair := range value.WhenThenPairs {
			cloned.WhenThenPairs[idx] = WhenThen{
				When: cloneExpressionForTable(pair.When, table),
				Then: cloneExpressionForTable(pair.Then, table),
			}
		}
		if value.ElseExpression != nil {
			cloned.ElseExpression = cloneExpressionForTable(value.ElseExpression, table)
		}
		return cloned
	case AliasExpr:
		return AliasExpr{Expr: cloneExpressionForTable(value.Expr, table), Alias: value.Alias}
	case BinaryExpr:
		return BinaryExpr{
			Left:     cloneExpressionForTable(value.Left, table),
			Operator: value.Operator,
			Right:    cloneExpressionForTable(value.Right, table),
		}
	case ConcatExpr:
		cloned := ConcatExpr{
			Exprs: make([]Expression, 0, len(value.Exprs)),
		}
		for _, expr := range value.Exprs {
			cloned.Exprs = append(cloned.Exprs, cloneExpressionForTable(expr, table))
		}
		return cloned
	case RawExpr:
		cloned := RawExpr{SQL: value.SQL, Args: make([]any, 0, len(value.Args))}
		for _, arg := range value.Args {
			if cloner, ok := arg.(TableCloner); ok {
				cloned.Args = append(cloned.Args, cloner.CloneForTable(table))
				continue
			}
			cloned.Args = append(cloned.Args, arg)
		}
		return cloned
	default:
		panic(fmt.Sprintf("schema: unsupported expression clone type %T", expr))
	}
}

func bindTableModel(target any, def *TableDef) {
	value := reflect.ValueOf(target)
	if value.Kind() != reflect.Pointer || value.IsNil() {
		panic("schema: target must be a non-nil pointer")
	}

	model := locateTableModel(value.Elem())
	if !model.IsValid() {
		panic("schema: typed table structs must embed schema.TableModel")
	}

	model.Set(reflect.ValueOf(TableModel{def: def}))
}

func locateTableModel(value reflect.Value) reflect.Value {
	for fieldIndex := range value.NumField() {
		field := value.Field(fieldIndex)
		fieldType := value.Type().Field(fieldIndex)
		if fieldType.Type == reflect.TypeFor[TableModel]() {
			return field
		}
		if field.Kind() == reflect.Struct {
			nested := locateTableModel(field)
			if nested.IsValid() {
				return nested
			}
		}
	}

	return reflect.Value{}
}

func tableDefOf(value any) *TableDef {
	table, ok := value.(TableReference)
	if !ok {
		panic(fmt.Sprintf("schema: %T does not implement schema.TableReference", value))
	}

	return table.TableDef()
}

func rebindAliasedColumns(value reflect.Value, table *TableDef) {
	if !value.IsValid() {
		return
	}

	switch value.Kind() {
	case reflect.Pointer:
		if value.IsNil() || !value.CanSet() {
			return
		}
		if cloner, ok := value.Interface().(TableCloner); ok {
			value.Set(reflect.ValueOf(cloner.CloneForTable(table)))
			return
		}
		rebindAliasedColumns(value.Elem(), table)
	case reflect.Struct:
		if value.Type() == reflect.TypeFor[TableModel]() {
			return
		}
		for _, field := range value.Fields() {
			rebindAliasedColumns(field, table)
		}
	}
}
