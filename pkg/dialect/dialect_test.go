package dialect

import "testing"

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
		{typ: "int", want: "INTEGER"},
		{typ: "int32", want: "INTEGER"},
		{typ: "int64", want: "BIGINT"},
		{typ: "float32", want: "REAL"},
		{typ: "float64", want: "DOUBLE PRECISION"},
		{typ: "bool", want: "BOOLEAN"},
		{typ: "time", want: "TIMESTAMP"},
		{typ: "custom", want: "custom"},
	}

	for _, tc := range cases {
		if got := d.DataType(tc.typ, tc.size); got != tc.want {
			t.Fatalf("DataType(%q, %d): want %q got %q", tc.typ, tc.size, tc.want, got)
		}
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
	if got := d.Features(); got != FeatureInsertReturning|FeatureUpdateReturning|FeatureDeleteReturning|FeatureOffset|FeatureUpsert|FeatureCTE|FeatureDefaultPlaceholder {
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
		{"int", 0, "INTEGER"},
		{"int32", 0, "INTEGER"},
		{"int64", 0, "BIGINT"},
		{"float32", 0, "REAL"},
		{"float64", 0, "DOUBLE PRECISION"},
		{"bool", 0, "BOOLEAN"},
		{"time", 0, "TIMESTAMPTZ"},
		{"json", 0, "JSONB"},
		{"uuid", 0, "UUID"},
		{"bytes", 0, "BYTEA"},
		{"custom", 0, "custom"},
	}
	for _, tc := range dataTypes {
		if got := d.DataType(tc.typ, tc.size); got != tc.want {
			t.Fatalf("DataType(%q, %d): want %q got %q", tc.typ, tc.size, tc.want, got)
		}
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
	if got := d.Features(); got != FeatureOffset|FeatureUpsert {
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
		{"int", 0, "INT"},
		{"int32", 0, "INT"},
		{"int64", 0, "BIGINT"},
		{"float32", 0, "FLOAT"},
		{"float64", 0, "DOUBLE"},
		{"bool", 0, "BOOLEAN"},
		{"time", 0, "DATETIME"},
		{"json", 0, "JSON"},
		{"custom", 0, "custom"},
	}
	for _, tc := range dataTypes {
		if got := d.DataType(tc.typ, tc.size); got != tc.want {
			t.Fatalf("DataType(%q, %d): want %q got %q", tc.typ, tc.size, tc.want, got)
		}
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
	if got := d.Features(); got != FeatureInsertReturning|FeatureUpdateReturning|FeatureDeleteReturning|FeatureOffset|FeatureUpsert {
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
		{"int", 0, "INTEGER"},
		{"int32", 0, "INTEGER"},
		{"int64", 0, "INTEGER"},
		{"float32", 0, "REAL"},
		{"float64", 0, "REAL"},
		{"bool", 0, "INTEGER"},
		{"time", 0, "TEXT"},
		{"json", 0, "TEXT"},
		{"custom", 0, "custom"},
	}
	for _, tc := range dataTypes {
		if got := d.DataType(tc.typ, tc.size); got != tc.want {
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
	if got := d.LimitOffset(0, 20); got != "" {
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
