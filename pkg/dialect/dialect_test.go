package dialect

import (
	"testing"

	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

func columnType(typ string, size int) schema.ColumnType {
	return schema.ColumnType{
		DataType: schema.DataType(typ),
		Size:     size,
	}
}

func TestFeatureHelpers(t *testing.T) {
	t.Parallel()

	features := FeatureInsertReturning | FeatureUpdateReturning | FeatureOffset

	if !HasFeature(features, FeatureInsertReturning) {
		t.Fatalf("expected single feature to be present")
	}
	if !HasFeature(features, FeatureInsertReturning|FeatureOffset) {
		t.Fatalf("expected combined feature mask to be present")
	}
	if HasFeature(features, FeatureDeleteReturning) {
		t.Fatalf("expected missing feature to be absent")
	}
	if HasFeature(features, FeatureInsertReturning|FeatureDeleteReturning) {
		t.Fatalf("expected incomplete feature mask to be absent")
	}
	if HasFeature(features, 0) {
		t.Fatalf("expected zero-value feature mask to be absent")
	}
	if !HasAnyFeature(features, FeatureDeleteReturning|FeatureOffset) {
		t.Fatalf("expected overlap to satisfy HasAnyFeature")
	}
	if HasAnyFeature(features, FeatureDeleteReturning|FeatureCTE) {
		t.Fatalf("expected missing features to fail HasAnyFeature")
	}
}

func TestBaseDialectDefaults(t *testing.T) {
	t.Parallel()

	d := &BaseDialect{}

	if got := d.Features(); got != 0 {
		t.Fatalf("unexpected default features: %b", got)
	}

	cases := []struct {
		typ  string
		size int
		want string
	}{
		{typ: "string", size: 0, want: "TEXT"},
		{typ: "string", size: 10, want: "VARCHAR"},
		{typ: "bigserial", want: "BIGSERIAL"},
		{typ: "int", want: "INTEGER"},
		{typ: "int32", want: "INTEGER"},
		{typ: "integer", want: "INTEGER"},
		{typ: "smallint", want: "SMALLINT"},
		{typ: "int64", want: "BIGINT"},
		{typ: "decimal", want: "DECIMAL"},
		{typ: "float32", want: "REAL"},
		{typ: "float64", want: "DOUBLE PRECISION"},
		{typ: "bool", want: "BOOLEAN"},
		{typ: "date", want: "DATE"},
		{typ: "timestamp", want: "TIMESTAMP"},
		{typ: "time", want: "TIMESTAMP"},
		{typ: "timestamptz", want: "TIMESTAMP"},
		{typ: "json", want: "JSON"},
		{typ: "jsonb", want: "JSONB"},
		{typ: "uuid", want: "UUID"},
		{typ: "bytes", want: "BLOB"},
		{typ: "enum", want: "VARCHAR"},
		{typ: "custom", want: "custom"},
	}

	for _, tc := range cases {
		if got := d.DataType(columnType(tc.typ, tc.size)); got != tc.want {
			t.Fatalf("DataType(%q, %d): want %q got %q", tc.typ, tc.size, tc.want, got)
		}
	}

	if got := d.DataType(schema.ColumnType{DataType: schema.TypeDecimal, Precision: 12, Scale: 2}); got != "DECIMAL(12,2)" {
		t.Fatalf("DataType(decimal 12,2): want %q got %q", "DECIMAL(12,2)", got)
	}

	if got := d.DefaultValue("ignored"); got != "DEFAULT" {
		t.Fatalf("unexpected default value: %q", got)
	}
	if got := d.UpsertClause("users", []string{"email"}, []string{"name"}); got != "" {
		t.Fatalf("unexpected base upsert clause: %q", got)
	}
}

func TestGetDialect(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		want string
	}{
		{name: "postgres", want: "postgres"},
		{name: "postgresql", want: "postgres"},
		{name: "mysql", want: "mysql"},
		{name: "sqlite", want: "sqlite"},
		{name: "sqlite3", want: "sqlite"},
	}

	for _, tc := range cases {
		d, err := GetDialect(tc.name)
		if err != nil {
			t.Fatalf("GetDialect(%q) returned error: %v", tc.name, err)
		}
		if got := d.Name(); got != tc.want {
			t.Fatalf("GetDialect(%q): want %q got %q", tc.name, tc.want, got)
		}
	}

	if _, err := GetDialect("unknown"); err == nil {
		t.Fatalf("expected unknown dialect to fail")
	}
}

func TestPostgresDialect(t *testing.T) {
	t.Parallel()

	d := &PostgresDialect{}

	if got := d.Name(); got != "postgres" {
		t.Fatalf("unexpected name: %q", got)
	}
	if got := d.Features(); got != FeatureInsertReturning|FeatureUpdateReturning|FeatureDeleteReturning|FeatureOffset|FeatureUpsert|FeatureCTE|FeatureDefaultPlaceholder|FeatureSavepoint|FeatureSelectLocking|FeatureNullsOrder|FeatureSelectDistinctOn {
		t.Fatalf("unexpected features: %b", got)
	}
	if got := d.QuoteIdentifier(`user"name`); got != `"user""name"` {
		t.Fatalf("unexpected quoted identifier: %q", got)
	}
	if got := d.Placeholder(12); got != "$12" {
		t.Fatalf("unexpected placeholder: %q", got)
	}

	dataTypes := []struct {
		typ  string
		size int
		want string
	}{
		{"string", 0, "TEXT"},
		{"string", 32, "VARCHAR"},
		{"bigserial", 0, "BIGSERIAL"},
		{"int", 0, "INTEGER"},
		{"int32", 0, "INTEGER"},
		{"integer", 0, "INTEGER"},
		{"smallint", 0, "SMALLINT"},
		{"int64", 0, "BIGINT"},
		{"decimal", 0, "NUMERIC"},
		{"float32", 0, "REAL"},
		{"real", 0, "REAL"},
		{"float64", 0, "DOUBLE PRECISION"},
		{"double", 0, "DOUBLE PRECISION"},
		{"bool", 0, "BOOLEAN"},
		{"date", 0, "DATE"},
		{"timestamp", 0, "TIMESTAMP"},
		{"time", 0, "TIMESTAMPTZ"},
		{"timestamptz", 0, "TIMESTAMPTZ"},
		{"json", 0, "JSON"},
		{"jsonb", 0, "JSONB"},
		{"enum", 0, "TEXT"},
		{"uuid", 0, "UUID"},
		{"bytes", 0, "BYTEA"},
		{"custom", 0, "custom"},
	}
	for _, tc := range dataTypes {
		if got := d.DataType(columnType(tc.typ, tc.size)); got != tc.want {
			t.Fatalf("DataType(%q, %d): want %q got %q", tc.typ, tc.size, tc.want, got)
		}
	}

	if got := d.DataType(schema.ColumnType{DataType: schema.TypeDecimal, Precision: 12, Scale: 2}); got != "NUMERIC(12,2)" {
		t.Fatalf("DataType(decimal 12,2): want %q got %q", "NUMERIC(12,2)", got)
	}

	if got := d.AutoIncrementKeyword(); got != "SERIAL" {
		t.Fatalf("unexpected auto increment keyword: %q", got)
	}
	if got := d.LimitOffset(10, 20); got != "LIMIT 10 OFFSET 20" {
		t.Fatalf("unexpected limit/offset: %q", got)
	}
	if got := d.LimitOffset(10, 0); got != "LIMIT 10" {
		t.Fatalf("unexpected limit only clause: %q", got)
	}
	if got := d.LimitOffset(0, 20); got != "OFFSET 20" {
		t.Fatalf("unexpected offset only clause: %q", got)
	}
	if got := d.LimitOffset(0, 0); got != "" {
		t.Fatalf("unexpected empty limit clause: %q", got)
	}
	if got := d.UpsertClause("users", []string{"email"}, []string{"name"}); got != "ON CONFLICT DO UPDATE" {
		t.Fatalf("unexpected upsert clause: %q", got)
	}
	if got := d.DefaultValue(nil); got != "DEFAULT" {
		t.Fatalf("unexpected default value: %q", got)
	}
	if got := d.BooleanLiteral(true); got != "TRUE" {
		t.Fatalf("unexpected true literal: %q", got)
	}
	if got := d.BooleanLiteral(false); got != "FALSE" {
		t.Fatalf("unexpected false literal: %q", got)
	}
	if got := d.CurrentTimestamp(); got != "CURRENT_TIMESTAMP" {
		t.Fatalf("unexpected current timestamp: %q", got)
	}
}

func TestMySQLDialect(t *testing.T) {
	t.Parallel()

	d := &MySQLDialect{}

	if got := d.Name(); got != "mysql" {
		t.Fatalf("unexpected name: %q", got)
	}
	if got := d.Features(); got != FeatureOffset|FeatureUpsert|FeatureSavepoint|FeatureSelectLocking|FeatureCTE|FeatureUpdateOrder|FeatureUpdateLimit|FeatureDeleteOrder|FeatureDeleteLimit {
		t.Fatalf("unexpected features: %b", got)
	}
	if got := d.QuoteIdentifier("user`name"); got != "`user``name`" {
		t.Fatalf("unexpected quoted identifier: %q", got)
	}
	if got := d.Placeholder(12); got != "?" {
		t.Fatalf("unexpected placeholder: %q", got)
	}

	dataTypes := []struct {
		typ  string
		size int
		want string
	}{
		{"string", 0, "TEXT"},
		{"string", 32, "VARCHAR"},
		{"bigserial", 0, "BIGINT"},
		{"int", 0, "INT"},
		{"int32", 0, "INT"},
		{"integer", 0, "INT"},
		{"smallint", 0, "SMALLINT"},
		{"int64", 0, "BIGINT"},
		{"decimal", 0, "DECIMAL"},
		{"float32", 0, "FLOAT"},
		{"real", 0, "FLOAT"},
		{"float64", 0, "DOUBLE"},
		{"double", 0, "DOUBLE"},
		{"bool", 0, "BOOLEAN"},
		{"date", 0, "DATE"},
		{"timestamp", 0, "TIMESTAMP"},
		{"time", 0, "DATETIME"},
		{"timestamptz", 0, "DATETIME"},
		{"json", 0, "JSON"},
		{"jsonb", 0, "JSON"},
		{"uuid", 0, "CHAR(36)"},
		{"enum", 0, "VARCHAR(255)"},
		{"bytes", 0, "BLOB"},
		{"custom", 0, "custom"},
	}
	for _, tc := range dataTypes {
		if got := d.DataType(columnType(tc.typ, tc.size)); got != tc.want {
			t.Fatalf("DataType(%q, %d): want %q got %q", tc.typ, tc.size, tc.want, got)
		}
	}

	if got := d.DataType(schema.ColumnType{DataType: schema.TypeDecimal, Precision: 12, Scale: 2}); got != "DECIMAL(12,2)" {
		t.Fatalf("DataType(decimal 12,2): want %q got %q", "DECIMAL(12,2)", got)
	}

	if got := d.AutoIncrementKeyword(); got != "AUTO_INCREMENT" {
		t.Fatalf("unexpected auto increment keyword: %q", got)
	}
	if got := d.LimitOffset(10, 20); got != "LIMIT 20, 10" {
		t.Fatalf("unexpected limit/offset: %q", got)
	}
	if got := d.LimitOffset(10, 0); got != "LIMIT 10" {
		t.Fatalf("unexpected limit only clause: %q", got)
	}
	if got := d.LimitOffset(0, 20); got != "LIMIT 18446744073709551615 OFFSET 20" {
		t.Fatalf("unexpected offset only clause: %q", got)
	}
	if got := d.LimitOffset(0, 0); got != "" {
		t.Fatalf("unexpected empty limit clause: %q", got)
	}
	if got := d.UpsertClause("users", []string{"email"}, []string{"name"}); got != "ON DUPLICATE KEY UPDATE" {
		t.Fatalf("unexpected upsert clause: %q", got)
	}
	if got := d.DefaultValue(nil); got != "DEFAULT" {
		t.Fatalf("unexpected default value: %q", got)
	}
	if got := d.BooleanLiteral(true); got != "1" {
		t.Fatalf("unexpected true literal: %q", got)
	}
	if got := d.BooleanLiteral(false); got != "0" {
		t.Fatalf("unexpected false literal: %q", got)
	}
	if got := d.CurrentTimestamp(); got != "CURRENT_TIMESTAMP" {
		t.Fatalf("unexpected current timestamp: %q", got)
	}
}

func TestSQLiteDialect(t *testing.T) {
	t.Parallel()

	d := &SQLiteDialect{}

	if got := d.Name(); got != "sqlite" {
		t.Fatalf("unexpected name: %q", got)
	}
	if got := d.Features(); got != FeatureInsertReturning|FeatureUpdateReturning|FeatureDeleteReturning|FeatureOffset|FeatureUpsert|FeatureSavepoint|FeatureNullsOrder|FeatureCTE|FeatureUpdateOrder|FeatureUpdateLimit|FeatureDeleteOrder|FeatureDeleteLimit {
		t.Fatalf("unexpected features: %b", got)
	}
	if got := d.QuoteIdentifier(`user"name`); got != `"user""name"` {
		t.Fatalf("unexpected quoted identifier: %q", got)
	}
	if got := d.Placeholder(12); got != "?" {
		t.Fatalf("unexpected placeholder: %q", got)
	}

	dataTypes := []struct {
		typ  string
		size int
		want string
	}{
		{"string", 0, "TEXT"},
		{"bigserial", 0, "INTEGER"},
		{"smallint", 0, "INTEGER"},
		{"int", 0, "INTEGER"},
		{"int32", 0, "INTEGER"},
		{"integer", 0, "INTEGER"},
		{"int64", 0, "INTEGER"},
		{"decimal", 0, "REAL"},
		{"float32", 0, "REAL"},
		{"real", 0, "REAL"},
		{"float64", 0, "REAL"},
		{"double", 0, "REAL"},
		{"bool", 0, "INTEGER"},
		{"date", 0, "TEXT"},
		{"timestamp", 0, "TEXT"},
		{"time", 0, "TEXT"},
		{"timestamptz", 0, "TEXT"},
		{"json", 0, "TEXT"},
		{"jsonb", 0, "TEXT"},
		{"uuid", 0, "TEXT"},
		{"enum", 0, "TEXT"},
		{"bytes", 0, "BLOB"},
		{"custom", 0, "custom"},
	}
	for _, tc := range dataTypes {
		if got := d.DataType(columnType(tc.typ, tc.size)); got != tc.want {
			t.Fatalf("DataType(%q, %d): want %q got %q", tc.typ, tc.size, tc.want, got)
		}
	}

	if got := d.AutoIncrementKeyword(); got != "AUTOINCREMENT" {
		t.Fatalf("unexpected auto increment keyword: %q", got)
	}
	if got := d.LimitOffset(10, 20); got != "LIMIT 10 OFFSET 20" {
		t.Fatalf("unexpected limit/offset: %q", got)
	}
	if got := d.LimitOffset(10, 0); got != "LIMIT 10" {
		t.Fatalf("unexpected limit only clause: %q", got)
	}
	if got := d.LimitOffset(0, 20); got != "LIMIT -1 OFFSET 20" {
		t.Fatalf("unexpected offset only clause: %q", got)
	}
	if got := d.LimitOffset(0, 0); got != "" {
		t.Fatalf("unexpected empty limit clause: %q", got)
	}
	if got := d.UpsertClause("users", []string{"email"}, []string{"name"}); got != "ON CONFLICT DO UPDATE" {
		t.Fatalf("unexpected upsert clause: %q", got)
	}
	if got := d.DefaultValue(nil); got != "DEFAULT" {
		t.Fatalf("unexpected default value: %q", got)
	}
	if got := d.BooleanLiteral(true); got != "1" {
		t.Fatalf("unexpected true literal: %q", got)
	}
	if got := d.BooleanLiteral(false); got != "0" {
		t.Fatalf("unexpected false literal: %q", got)
	}
	if got := d.CurrentTimestamp(); got != "CURRENT_TIMESTAMP" {
		t.Fatalf("unexpected current timestamp: %q", got)
	}
}

func TestSQLiteDialectJSONBFallback(t *testing.T) {
	t.Parallel()

	d := &SQLiteDialect{}
	if got := d.DataType(columnType("jsonb", 0)); got != "TEXT" {
		t.Fatalf("expected JSONB fallback to TEXT on sqlite, got %q", got)
	}
}
