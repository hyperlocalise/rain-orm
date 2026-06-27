package rain

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

type coverageUsersTable struct {
	schema.TableModel
	ID        *schema.Column[int64]
	Email     *schema.Column[string]
	Active    *schema.Column[bool]
	CreatedAt *schema.Column[time.Time]
	Profile   *schema.Column[any]
	Payload   *schema.Column[[]byte]
}

type coveragePostsTable struct {
	schema.TableModel
	ID     *schema.Column[int64]
	UserID *schema.Column[int64]
	Title  *schema.Column[string]
}

func defineCoverageTables() (*coverageUsersTable, *coveragePostsTable) {
	users := schema.Define("coverage_users", func(t *coverageUsersTable) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.Email = t.VarChar("email", 255).NotNull().Unique()
		t.Active = t.Boolean("active").NotNull().Default(true)
		t.CreatedAt = t.TimestampTZ("created_at").NotNull().DefaultNow()
		t.Profile = t.JSONB("profile").Nullable()
		t.Payload = t.Bytes("payload").Nullable()
		t.Index("coverage_users_email_idx").On(t.Email)
	})
	posts := schema.Define("coverage_posts", func(t *coveragePostsTable) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.UserID = t.BigInt("user_id").NotNull().References(users.ID)
		t.Title = t.Text("title").NotNull()
		t.ForeignKey("coverage_posts_user_fk").On(t.UserID).References(users.ID).OnDelete(schema.ForeignKeyActionCascade)
	})
	users.HasMany("posts", users.ID, posts.UserID)
	return users, posts
}

type scanWriteJSON struct{}

func (scanWriteJSON) Scan(any) error               { return nil }
func (scanWriteJSON) Value() (driver.Value, error) { return []byte(`{}`), nil }

type onlyScanner struct{}

func (onlyScanner) Scan(any) error { return nil }

type onlyValuer struct{}

func (onlyValuer) Value() (driver.Value, error) { return "x", nil }

type pointerSetString struct{}

func (*pointerSetString) rainSetType() reflect.Type { return reflect.TypeFor[string]() }

type writerRunner struct {
	execQueries  []string
	queryQueries []string
	db           *DB
	execErr      error
	queryErr     error
}

func (r *writerRunner) execContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	r.execQueries = append(r.execQueries, query)
	if r.execErr != nil {
		return nil, r.execErr
	}
	return r.db.execContext(ctx, query, args...)
}

func (r *writerRunner) queryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	r.queryQueries = append(r.queryQueries, query)
	if r.queryErr != nil {
		return nil, r.queryErr
	}
	return r.db.queryContext(ctx, query, args...)
}

func TestCoverageModelBindingHelpers(t *testing.T) {
	t.Parallel()

	users, _ := defineCoverageTables()
	type strictModel struct {
		ID        int64
		Email     string
		Active    bool
		CreatedAt time.Time
		Profile   scanWriteJSON
		Payload   []byte
	}

	if err := BindTableModel[strictModel](users); err != nil {
		t.Fatalf("BindTableModel returned error: %v", err)
	}
	MustBindTableModel[strictModel](users)
	if err := BindModel[strictModel](users); err != nil {
		t.Fatalf("BindModel returned error: %v", err)
	}
	MustBindModel[strictModel](users)

	if err := BindTableModel[int](users); err == nil || !strings.Contains(err.Error(), "struct or pointer to struct") {
		t.Fatalf("expected non-struct bind error, got %v", err)
	}
	if _, err := lookupTableModelBinding(reflect.TypeFor[*strictModel](), nil, true); err == nil || !strings.Contains(err.Error(), "non-nil table") {
		t.Fatalf("expected nil table binding error, got %v", err)
	}
	if _, err := lookupTableModelBinding(reflect.TypeFor[int](), users.TableDef(), true); err == nil || !strings.Contains(err.Error(), "struct or pointer to struct") {
		t.Fatalf("expected invalid model type binding error, got %v", err)
	}
	if _, err := structTypeForType(reflect.TypeFor[*strictModel]()); err != nil {
		t.Fatalf("expected pointer-to-struct type to validate, got %v", err)
	}
	first, err := lookupTableModelBinding(reflect.TypeFor[strictModel](), users.TableDef(), true)
	if err != nil {
		t.Fatalf("lookupTableModelBinding returned error: %v", err)
	}
	second, err := lookupTableModelBinding(reflect.TypeFor[strictModel](), users.TableDef(), true)
	if err != nil {
		t.Fatalf("lookupTableModelBinding cache lookup returned error: %v", err)
	}
	if first != second {
		t.Fatalf("expected lookupTableModelBinding to return cached binding")
	}
	defer func() {
		if recover() == nil {
			t.Fatalf("expected MustBindTableModel to panic")
		}
	}()
	MustBindTableModel[int](users)
}

func TestCoverageValidateModelBindingAndTypes(t *testing.T) {
	t.Parallel()

	users, _ := defineCoverageTables()

	type unknownRelation struct {
		ID    int64
		Posts []int `rain:"relation:ghost"`
	}
	if err := BindTableModel[unknownRelation](users); err == nil || !strings.Contains(err.Error(), `unknown relation "ghost"`) {
		t.Fatalf("expected unknown relation error, got %v", err)
	}

	type scanColumnsExplicit struct {
		Email string `db:"ghost"`
	}
	if err := validateScanColumnsAgainstTable(reflect.TypeFor[scanColumnsExplicit](), nil, []string{"ghost"}); err != nil {
		t.Fatalf("expected nil table to skip scan column validation, got %v", err)
	}
	if err := validateScanColumnsAgainstTable(reflect.TypeFor[scanColumnsExplicit](), users.TableDef(), []string{"ghost"}); err != nil {
		t.Fatalf("expected explicit missing column to be ignored, got %v", err)
	}
	type scanColumnsImplicit struct {
		Email string
	}
	if err := validateScanColumnsAgainstTable(reflect.TypeFor[scanColumnsImplicit](), users.TableDef(), []string{"email"}); err != nil {
		t.Fatalf("expected scan column validation to pass, got %v", err)
	}
	type scanColumnsWrongType struct {
		Email int64
	}
	if err := validateScanColumnsAgainstTable(reflect.TypeFor[scanColumnsWrongType](), users.TableDef(), []string{"email"}); err == nil || !strings.Contains(err.Error(), "scan column") {
		t.Fatalf("expected incompatible scan column type error, got %v", err)
	}
	type scanColumnsMissing struct {
		Ghost string
	}
	if err := validateScanColumnsAgainstTable(reflect.TypeFor[scanColumnsMissing](), users.TableDef(), []string{"ghost"}); err == nil || !strings.Contains(err.Error(), `selected column "ghost" does not exist`) {
		t.Fatalf("expected implicit missing selected column error, got %v", err)
	}
	type badScanMeta struct {
		Email string `db:"email"`
		Other string `db:"email"`
	}
	if err := validateScanColumnsAgainstTable(reflect.TypeFor[badScanMeta](), users.TableDef(), []string{"email"}); err == nil || !strings.Contains(err.Error(), "duplicate model field mapping") {
		t.Fatalf("expected invalid scan metadata error, got %v", err)
	}

	if err := joinValidationErrors(nil); err != nil {
		t.Fatalf("expected nil joined error, got %v", err)
	}
	joined := joinValidationErrors([]error{errors.New("a"), nil, errors.New("b")})
	if joined == nil || joined.Error() != "a; b" {
		t.Fatalf("unexpected joined error: %v", joined)
	}

	column := users.Email.ColumnDef()
	if err := validateModelFieldCompatibility(column, reflect.TypeFor[int64]()); err == nil || !strings.Contains(err.Error(), "scan column") {
		t.Fatalf("expected scan incompatibility, got %v", err)
	}
	if err := validateModelFieldCompatibility(users.CreatedAt.ColumnDef(), reflect.TypeFor[string]()); err == nil || !strings.Contains(err.Error(), "write column") {
		t.Fatalf("expected write incompatibility after scan compatibility, got %v", err)
	}
	if err := validateWriteCompatibility(users.CreatedAt.ColumnDef(), reflect.TypeFor[string]()); err == nil || !strings.Contains(err.Error(), "write column") {
		t.Fatalf("expected write incompatibility, got %v", err)
	}
}

func TestCoverageSupportsScanWriteAndTypeHelpers(t *testing.T) {
	t.Parallel()

	users, _ := defineCoverageTables()
	jsonColumn := users.Profile.ColumnDef()
	bytesColumn := users.Payload.ColumnDef()
	timeColumn := users.CreatedAt.ColumnDef()
	boolColumn := users.Active.ColumnDef()
	intColumn := users.ID.ColumnDef()
	textColumn := users.Email.ColumnDef()
	decimalColumn := &schema.ColumnDef{Name: "amount", Type: schema.ColumnType{DataType: schema.TypeDecimal}}
	enumColumn := &schema.ColumnDef{Name: "status", Type: schema.ColumnType{DataType: schema.TypeEnum}}
	dateColumn := &schema.ColumnDef{Name: "birthday", Type: schema.ColumnType{DataType: schema.TypeDate}}
	unsupportedColumn := &schema.ColumnDef{Name: "mystery", Type: schema.ColumnType{}}

	if !supportsScanForColumn(jsonColumn, reflect.TypeFor[scanWriteJSON]()) {
		t.Fatalf("expected scanner/valuer JSON type to support scan")
	}
	if !supportsScanForColumn(jsonColumn, reflect.TypeFor[string]()) {
		t.Fatalf("expected JSON scan support via string")
	}
	if supportsScanForColumn(boolColumn, nil) {
		t.Fatalf("expected nil scan field type to be rejected")
	}
	if !supportsScanForColumn(intColumn, reflect.TypeFor[int64]()) {
		t.Fatalf("expected integer scan support")
	}
	if !supportsScanForColumn(&schema.ColumnDef{Name: "ratio", Type: schema.ColumnType{DataType: schema.TypeReal}}, reflect.TypeFor[float64]()) {
		t.Fatalf("expected float scan support")
	}
	if !supportsScanForColumn(textColumn, reflect.TypeFor[string]()) {
		t.Fatalf("expected text scan support")
	}
	if !supportsScanForColumn(decimalColumn, reflect.TypeFor[string]()) {
		t.Fatalf("expected decimal scan support via string")
	}
	if !supportsScanForColumn(enumColumn, reflect.TypeFor[string]()) {
		t.Fatalf("expected enum scan support via string")
	}
	if !supportsScanForColumn(dateColumn, reflect.TypeFor[time.Time]()) {
		t.Fatalf("expected date scan support via time.Time")
	}
	if supportsScanForColumn(boolColumn, reflect.TypeFor[string]()) {
		t.Fatalf("expected string to be incompatible with bool column")
	}
	if !supportsScanForColumn(bytesColumn, reflect.TypeFor[[]byte]()) {
		t.Fatalf("expected []byte scan support")
	}
	if !supportsScanForColumn(timeColumn, reflect.TypeFor[string]()) {
		t.Fatalf("expected string timestamp scan support")
	}
	if supportsScanForColumn(unsupportedColumn, reflect.TypeFor[string]()) {
		t.Fatalf("expected unsupported scan column type to be rejected")
	}
	if !supportsWriteForColumn(jsonColumn, reflect.TypeFor[scanWriteJSON]()) {
		t.Fatalf("expected scanner/valuer JSON type to support write")
	}
	if !supportsWriteForColumn(jsonColumn, reflect.TypeFor[string]()) {
		t.Fatalf("expected JSON write support via string")
	}
	if supportsWriteForColumn(boolColumn, nil) {
		t.Fatalf("expected nil write field type to be rejected")
	}
	if !supportsWriteForColumn(intColumn, reflect.TypeFor[int64]()) {
		t.Fatalf("expected integer write support")
	}
	if !supportsWriteForColumn(&schema.ColumnDef{Name: "ratio", Type: schema.ColumnType{DataType: schema.TypeDouble}}, reflect.TypeFor[float64]()) {
		t.Fatalf("expected float write support")
	}
	if !supportsWriteForColumn(textColumn, reflect.TypeFor[string]()) {
		t.Fatalf("expected text write support")
	}
	if !supportsWriteForColumn(decimalColumn, reflect.TypeFor[string]()) {
		t.Fatalf("expected decimal write support via string")
	}
	if !supportsWriteForColumn(enumColumn, reflect.TypeFor[string]()) {
		t.Fatalf("expected enum write support via string")
	}
	if !supportsWriteForColumn(dateColumn, reflect.TypeFor[time.Time]()) {
		t.Fatalf("expected date write support via time.Time")
	}
	if supportsWriteForColumn(timeColumn, reflect.TypeFor[string]()) {
		t.Fatalf("expected string timestamp write to be rejected")
	}
	if !supportsWriteForColumn(boolColumn, reflect.TypeFor[bool]()) {
		t.Fatalf("expected bool write support")
	}
	if supportsWriteForColumn(unsupportedColumn, reflect.TypeFor[string]()) {
		t.Fatalf("expected unsupported write column type to be rejected")
	}

	if base, explicit := unwrapFieldType(reflect.TypeFor[*Set[*string]]()); !explicit || base.Kind() != reflect.String {
		t.Fatalf("unexpected unwrap result: base=%v explicit=%v", base, explicit)
	}
	if _, ok := extractSetFieldType(reflect.TypeFor[Set[string]]()); !ok {
		t.Fatalf("expected Set type extraction to succeed")
	}
	if base, ok := extractSetFieldType(reflect.TypeFor[pointerSetString]()); !ok || base.Kind() != reflect.String {
		t.Fatalf("expected pointer receiver Set type extraction to succeed, base=%v ok=%v", base, ok)
	}
	if _, ok := extractSetFieldType(reflect.TypeFor[string]()); ok {
		t.Fatalf("expected non-Set type extraction to fail")
	}

	if !isIntegerKind(reflect.Int32) || isIntegerKind(reflect.String) {
		t.Fatalf("unexpected integer kind classification")
	}
	if !isBytesType(reflect.TypeFor[[]byte]()) || isBytesType(reflect.TypeFor[string]()) {
		t.Fatalf("unexpected bytes type classification")
	}
	if !isJSONCompatibleType(reflect.TypeFor[json.RawMessage]()) ||
		!isJSONCompatibleType(reflect.TypeFor[string]()) ||
		!isJSONCompatibleType(reflect.TypeFor[[]byte]()) ||
		!isJSONCompatibleType(reflect.TypeFor[scanWriteJSON]()) ||
		isJSONCompatibleType(reflect.TypeFor[onlyScanner]()) {
		t.Fatalf("unexpected JSON compatibility classification")
	}
	if !supportsValuer(reflect.TypeFor[onlyValuer]()) {
		t.Fatalf("expected onlyValuer to satisfy driver.Valuer support")
	}
	if supportsScanner(nil) || supportsValuer(nil) {
		t.Fatalf("expected nil scanner/valuer type support checks to be false")
	}
}

func TestCoverageUpdateDeleteExecAndScan(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openInternalQueryDB(t)
	users, _ := defineInternalQueryTables()
	createInternalQuerySchema(t, ctx, db)

	if _, err := (&UpdateQuery{}).Exec(ctx); !errors.Is(err, ErrNoConnection) {
		t.Fatalf("expected update Exec ErrNoConnection, got %v", err)
	}
	if _, err := (&DeleteQuery{}).Exec(ctx); !errors.Is(err, ErrNoConnection) {
		t.Fatalf("expected delete Exec ErrNoConnection, got %v", err)
	}
	if _, _, err := (&UpdateQuery{dialect: db.Dialect(), table: users.TableDef(), values: []assignment{{column: users.Name, value: schema.ValueExpr{Value: "After"}}}, where: []schema.Predicate{users.Email.EqExpr(schema.Placeholder("email"))}}).ToSQL(); !errors.Is(err, ErrPreparedArgsRequired) {
		t.Fatalf("expected update ToSQL prepared args error, got %v", err)
	}
	if _, _, err := (&UpdateQuery{dialect: db.Dialect(), table: users.TableDef(), values: []assignment{{column: users.Name, value: schema.PlaceholderExpr{Name: ""}}}, where: []schema.Predicate{users.Email.Eq("before@example.com")}}).ToSQL(); err == nil || !strings.Contains(err.Error(), "placeholder name cannot be empty") {
		t.Fatalf("expected update ToSQL assignment expression error, got %v", err)
	}
	if _, _, err := (&UpdateQuery{dialect: db.Dialect(), table: users.TableDef(), values: []assignment{{column: users.Name, value: schema.ValueExpr{Value: "After"}}}, where: []schema.Predicate{users.Email.EqExpr(schema.PlaceholderExpr{Name: ""})}}).ToSQL(); err == nil || !strings.Contains(err.Error(), "placeholder name cannot be empty") {
		t.Fatalf("expected update ToSQL predicate error, got %v", err)
	}
	if _, _, err := (&DeleteQuery{dialect: db.Dialect(), table: users.TableDef(), where: []schema.Predicate{users.Email.EqExpr(schema.Placeholder("email"))}}).ToSQL(); !errors.Is(err, ErrPreparedArgsRequired) {
		t.Fatalf("expected delete ToSQL prepared args error, got %v", err)
	}
	if _, _, err := (&DeleteQuery{dialect: db.Dialect(), table: users.TableDef(), where: []schema.Predicate{users.Email.EqExpr(schema.PlaceholderExpr{Name: ""})}}).ToSQL(); err == nil || !strings.Contains(err.Error(), "placeholder name cannot be empty") {
		t.Fatalf("expected delete ToSQL predicate error, got %v", err)
	}

	runner := &writerRunner{db: db}
	if _, err := db.Insert().Table(users).Set(users.Email, "before@example.com").Set(users.Name, "Before").Set(users.Active, true).Exec(ctx); err != nil {
		t.Fatalf("insert seed row: %v", err)
	}

	update := &UpdateQuery{runner: runner, dialect: db.Dialect(), table: users.TableDef()}
	update.Set(users.Name, "After").Where(users.Email.Eq("before@example.com"))
	if _, err := update.Exec(ctx); err != nil {
		t.Fatalf("update Exec returned error: %v", err)
	}
	if len(runner.execQueries) == 0 {
		t.Fatalf("expected update Exec to call runner")
	}
	updateErrRunner := &writerRunner{db: db, execErr: errors.New("exec boom")}
	if _, err := (&UpdateQuery{runner: updateErrRunner, dialect: db.Dialect(), table: users.TableDef(), values: []assignment{{column: users.Name, value: schema.ValueExpr{Value: "Nope"}}}, where: []schema.Predicate{users.Email.Eq("before@example.com")}}).Exec(ctx); err == nil || !strings.Contains(err.Error(), "exec boom") {
		t.Fatalf("expected update Exec runner error, got %v", err)
	}
	if _, err := (&UpdateQuery{runner: runner, dialect: db.Dialect(), table: users.TableDef(), values: []assignment{{column: users.Name, value: schema.PlaceholderExpr{Name: ""}}}, where: []schema.Predicate{users.Email.Eq("before@example.com")}}).Exec(ctx); err == nil || !strings.Contains(err.Error(), "placeholder name cannot be empty") {
		t.Fatalf("expected update Exec ToSQL error, got %v", err)
	}

	var updated []internalUserRow
	updateReturning := &UpdateQuery{runner: runner, dialect: db.Dialect(), table: users.TableDef()}
	updateReturning.Set(users.Name, "Final").Where(users.Email.Eq("before@example.com")).Returning(users.ID, users.Email, users.Name)
	if err := updateReturning.Scan(ctx, &updated); err != nil {
		t.Fatalf("update Scan returned error: %v", err)
	}
	if len(updated) != 1 || updated[0].Name != "Final" {
		t.Fatalf("unexpected updated rows: %#v", updated)
	}
	updateQueryErrRunner := &writerRunner{db: db, queryErr: errors.New("query boom")}
	if err := (&UpdateQuery{runner: updateQueryErrRunner, dialect: db.Dialect(), table: users.TableDef(), values: []assignment{{column: users.Name, value: schema.ValueExpr{Value: "Again"}}}, where: []schema.Predicate{users.Email.Eq("before@example.com")}, returning: []schema.Expression{users.ID}}).Scan(ctx, &updated); err == nil || !strings.Contains(err.Error(), "query boom") {
		t.Fatalf("expected update Scan query error, got %v", err)
	}
	if err := (&UpdateQuery{runner: runner, dialect: db.Dialect(), table: users.TableDef(), values: []assignment{{column: users.Name, value: schema.PlaceholderExpr{Name: ""}}}, where: []schema.Predicate{users.Email.Eq("before@example.com")}, returning: []schema.Expression{users.ID}}).Scan(ctx, &updated); err == nil || !strings.Contains(err.Error(), "placeholder name cannot be empty") {
		t.Fatalf("expected update Scan ToSQL error, got %v", err)
	}

	deleteQ := &DeleteQuery{runner: runner, dialect: db.Dialect(), table: users.TableDef()}
	deleteQ.Where(users.Email.Eq("before@example.com"))
	if _, err := deleteQ.Exec(ctx); err != nil {
		t.Fatalf("delete Exec returned error: %v", err)
	}
	deleteErrRunner := &writerRunner{db: db, execErr: errors.New("delete boom")}
	if _, err := (&DeleteQuery{runner: deleteErrRunner, dialect: db.Dialect(), table: users.TableDef(), where: []schema.Predicate{users.Email.Eq("missing@example.com")}}).Exec(ctx); err == nil || !strings.Contains(err.Error(), "delete boom") {
		t.Fatalf("expected delete Exec runner error, got %v", err)
	}
	if _, err := (&DeleteQuery{runner: runner, dialect: db.Dialect(), table: users.TableDef(), where: []schema.Predicate{users.Email.EqExpr(schema.PlaceholderExpr{Name: ""})}}).Exec(ctx); err == nil || !strings.Contains(err.Error(), "placeholder name cannot be empty") {
		t.Fatalf("expected delete Exec ToSQL error, got %v", err)
	}

	if _, err := db.Insert().Table(users).Set(users.Email, "delete@example.com").Set(users.Name, "Delete").Set(users.Active, true).Exec(ctx); err != nil {
		t.Fatalf("insert delete row: %v", err)
	}
	var deleted []internalUserRow
	deleteReturning := &DeleteQuery{runner: runner, dialect: db.Dialect(), table: users.TableDef()}
	deleteReturning.Where(users.Email.Eq("delete@example.com")).Returning(users.ID, users.Email, users.Name)
	if err := deleteReturning.Scan(ctx, &deleted); err != nil {
		t.Fatalf("delete Scan returned error: %v", err)
	}
	if len(deleted) != 1 || deleted[0].Email != "delete@example.com" {
		t.Fatalf("unexpected deleted rows: %#v", deleted)
	}
	deleteQueryErrRunner := &writerRunner{db: db, queryErr: errors.New("delete query boom")}
	if err := (&DeleteQuery{runner: deleteQueryErrRunner, dialect: db.Dialect(), table: users.TableDef(), where: []schema.Predicate{users.Email.Eq("nobody@example.com")}, returning: []schema.Expression{users.ID}}).Scan(ctx, &deleted); err == nil || !strings.Contains(err.Error(), "delete query boom") {
		t.Fatalf("expected delete Scan query error, got %v", err)
	}
	if err := (&DeleteQuery{runner: runner, dialect: db.Dialect(), table: users.TableDef(), where: []schema.Predicate{users.Email.EqExpr(schema.PlaceholderExpr{Name: ""})}, returning: []schema.Expression{users.ID}}).Scan(ctx, &deleted); err == nil || !strings.Contains(err.Error(), "placeholder name cannot be empty") {
		t.Fatalf("expected delete Scan ToSQL error, got %v", err)
	}
}

func TestCoverageDDLMethodsAndHelpers(t *testing.T) {
	t.Parallel()

	users, posts := defineCoverageTables()
	sqlite := dialectForTest(t, "sqlite")
	pg := dialectForTest(t, "postgres")
	mysqlDialect := dialectForTest(t, "mysql")
	db, err := OpenDialect("postgres")
	if err != nil {
		t.Fatalf("OpenDialect returned error: %v", err)
	}
	var nilDB *DB
	noDialectDB := &DB{}

	if _, err := (*DB)(nil).CreateIndexesSQL(users); err == nil {
		t.Fatalf("expected CreateIndexesSQL on nil DB to fail")
	}
	if _, err := nilDB.ColumnDefinitionSQL(users, "id"); err == nil {
		t.Fatalf("expected ColumnDefinitionSQL on nil DB to fail")
	}
	if _, err := noDialectDB.ColumnDefinitionSQL(users, "id"); err == nil {
		t.Fatalf("expected ColumnDefinitionSQL on DB without dialect to fail")
	}
	if _, err := db.CreateIndexesSQL(nil); err == nil {
		t.Fatalf("expected CreateIndexesSQL on nil table to fail")
	}
	if _, err := db.ColumnDefinitionSQL(nil, "id"); err == nil {
		t.Fatalf("expected ColumnDefinitionSQL on nil table to fail")
	}
	if _, err := db.ColumnDefinitionSQL(users, "ghost"); err == nil {
		t.Fatalf("expected ColumnDefinitionSQL unknown column to fail")
	}
	if sql, err := db.ColumnDefinitionSQL(users, "id"); err != nil || !strings.Contains(sql, `"id" BIGSERIAL PRIMARY KEY`) {
		t.Fatalf("unexpected column definition: %q err=%v", sql, err)
	}
	multiPKColumnTable := schema.Define("multi_pk_column", func(t *struct {
		schema.TableModel
		ID *schema.Column[int64]
	},
	) {
		t.ID = t.BigInt("id").NotNull()
		t.PrimaryKey("p1").On(t.ID)
		t.PrimaryKey("p2").On(t.ID)
	})
	if _, err := db.ColumnDefinitionSQL(multiPKColumnTable, "id"); err == nil {
		t.Fatalf("expected ColumnDefinitionSQL table primary key validation to fail")
	}
	if _, err := db.AddConstraintSQL(nil, "x"); err == nil {
		t.Fatalf("expected AddConstraintSQL nil table to fail")
	}
	if _, err := nilDB.AddConstraintSQL(posts, "coverage_posts_user_fk"); err == nil {
		t.Fatalf("expected AddConstraintSQL nil DB to fail")
	}
	if _, err := noDialectDB.AddConstraintSQL(posts, "coverage_posts_user_fk"); err == nil {
		t.Fatalf("expected AddConstraintSQL without dialect to fail")
	}
	if _, err := db.AddConstraintSQL(posts, "ghost"); err == nil {
		t.Fatalf("expected AddConstraintSQL unknown constraint to fail")
	}
	if sql, err := db.AddConstraintSQL(posts, "coverage_posts_user_fk"); err != nil || !strings.Contains(sql, `ADD CONSTRAINT "coverage_posts_user_fk"`) {
		t.Fatalf("unexpected AddConstraintSQL output: %q err=%v", sql, err)
	}
	badConstraintTable := schema.Define("bad_constraint_table", func(t *struct {
		schema.TableModel
		ID *schema.Column[int64]
	},
	) {
		t.ID = t.BigSerial("id").PrimaryKey()
	})
	badConstraintTable.TableDef().Constraints = append(badConstraintTable.TableDef().Constraints, schema.ConstraintDef{Name: "bad_unique", Type: schema.ConstraintUnique})
	if _, err := db.AddConstraintSQL(badConstraintTable, "bad_unique"); err == nil {
		t.Fatalf("expected AddConstraintSQL constraint definition error")
	}
	if _, err := db.AddForeignKeySQL(nil, "x"); err == nil {
		t.Fatalf("expected AddForeignKeySQL nil table to fail")
	}
	if _, err := nilDB.AddForeignKeySQL(posts, "ghost"); err == nil {
		t.Fatalf("expected AddForeignKeySQL nil DB to fail")
	}
	if _, err := noDialectDB.AddForeignKeySQL(posts, "ghost"); err == nil {
		t.Fatalf("expected AddForeignKeySQL without dialect to fail")
	}
	if _, err := db.AddForeignKeySQL(posts, "ghost"); err == nil {
		t.Fatalf("expected AddForeignKeySQL unknown foreign key to fail")
	}
	if _, err := db.AddForeignKeySQL(posts, "coverage_posts_user_fk"); err == nil {
		t.Fatalf("expected AddForeignKeySQL to only resolve column-level foreign keys")
	}
	columnLevelFK := schema.Define("coverage_posts_column_fk", func(t *struct {
		schema.TableModel
		ID     *schema.Column[int64]
		UserID *schema.Column[int64]
	},
	) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.UserID = t.BigInt("user_id").NotNull().References(users.ID)
	})
	columnLevelFK.TableDef().ForeignKeys[0].Name = "coverage_posts_column_fk_user_id_fk"
	if sql, err := db.AddForeignKeySQL(columnLevelFK, "coverage_posts_column_fk_user_id_fk"); err != nil || !strings.Contains(sql, `ADD CONSTRAINT "coverage_posts_column_fk_user_id_fk"`) {
		t.Fatalf("unexpected AddForeignKeySQL output: %q err=%v", sql, err)
	}
	columnLevelFK.TableDef().ForeignKeys[0].OnUpdate = schema.ForeignKeyAction("bad")
	if _, err := db.AddForeignKeySQL(columnLevelFK, "coverage_posts_column_fk_user_id_fk"); err == nil {
		t.Fatalf("expected AddForeignKeySQL foreign key validation error")
	}
	if _, err := db.ColumnDefaultSQL(nil, "x"); err == nil {
		t.Fatalf("expected ColumnDefaultSQL nil table to fail")
	}
	if _, err := nilDB.ColumnDefaultSQL(users, "active"); err == nil {
		t.Fatalf("expected ColumnDefaultSQL nil DB to fail")
	}
	if _, err := noDialectDB.ColumnDefaultSQL(users, "active"); err == nil {
		t.Fatalf("expected ColumnDefaultSQL without dialect to fail")
	}
	if _, err := db.ColumnDefaultSQL(users, "ghost"); err == nil {
		t.Fatalf("expected ColumnDefaultSQL unknown column to fail")
	}
	if sql, err := db.ColumnDefaultSQL(users, "active"); err != nil || sql != "TRUE" {
		t.Fatalf("unexpected bool default: %q err=%v", sql, err)
	}
	if sql, err := db.ColumnDefaultSQL(users, "profile"); err != nil || sql != "" {
		t.Fatalf("unexpected no-default output: %q err=%v", sql, err)
	}

	if _, err := createTableSQL(nil, users.TableDef()); err == nil {
		t.Fatalf("expected createTableSQL nil dialect to fail")
	}
	if _, err := createTableSQL(pg, nil); err == nil {
		t.Fatalf("expected createTableSQL nil table to fail")
	}
	if _, err := createIndexesSQL(nil, users.TableDef()); err == nil {
		t.Fatalf("expected createIndexesSQL nil dialect to fail")
	}
	if _, err := createIndexesSQL(pg, nil); err == nil {
		t.Fatalf("expected createIndexesSQL nil table to fail")
	}
	badIndex := schema.Define("bad_index", func(t *struct {
		schema.TableModel
		ID *schema.Column[int64]
	},
	) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.Index("bad").On()
	})
	if _, err := createIndexesSQL(sqlite, badIndex.TableDef()); err == nil {
		t.Fatalf("expected createIndexesSQL empty index to fail")
	}
	filteredIndex := schema.Define("filtered_index", func(t *struct {
		schema.TableModel
		ID     *schema.Column[int64]
		Active *schema.Column[bool]
	},
	) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.Active = t.Boolean("active").NotNull()
		t.Index("filtered_idx").On(t.ID)
	})
	filteredIndex.TableDef().Indexes[0].Where = `"active" = TRUE`
	if sqls, err := createIndexesSQL(pg, filteredIndex.TableDef()); err != nil || len(sqls) != 1 || !strings.Contains(sqls[0], ` WHERE "active" = TRUE`) {
		t.Fatalf("unexpected filtered index SQL: %#v err=%v", sqls, err)
	}
	multiColumnPrimaryKeys := schema.Define("multi_column_primary_keys", func(t *struct {
		schema.TableModel
		A *schema.Column[int64]
		B *schema.Column[int64]
	},
	) {
		t.A = t.BigInt("a").PrimaryKey()
		t.B = t.BigInt("b").PrimaryKey()
	})
	if sql, err := createTableSQL(pg, multiColumnPrimaryKeys.TableDef()); err != nil || !strings.Contains(sql, `PRIMARY KEY ("a", "b")`) {
		t.Fatalf("unexpected createTableSQL multi-column primary key output: %q err=%v", sql, err)
	}
	mixedPrimaryKeys := schema.Define("mixed_primary_keys", func(t *struct {
		schema.TableModel
		ID    *schema.Column[int64]
		Email *schema.Column[string]
	},
	) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.Email = t.Text("email").NotNull()
		t.PrimaryKey("mixed_pk").On(t.Email)
	})
	if _, err := createTableSQL(pg, mixedPrimaryKeys.TableDef()); err == nil {
		t.Fatalf("expected createTableSQL mixed primary key error")
	}
	if _, err := createTableSQL(pg, multiPKColumnTable.TableDef()); err == nil {
		t.Fatalf("expected createTableSQL duplicate table primary key error")
	}
	badCreateFKTable := schema.Define("bad_create_fk_table", func(t *struct {
		schema.TableModel
		ID *schema.Column[int64]
	},
	) {
		t.ID = t.BigSerial("id").PrimaryKey()
	})
	badCreateFKTable.TableDef().ForeignKeys = append(badCreateFKTable.TableDef().ForeignKeys, schema.ForeignKeyDef{Name: "bad_fk"})
	if _, err := createTableSQL(pg, badCreateFKTable.TableDef()); err == nil {
		t.Fatalf("expected createTableSQL foreign key error")
	}
	badCreateColumnTable := schema.Define("bad_create_column_table", func(t *struct {
		schema.TableModel
		ID *schema.Column[int64]
	},
	) {
		t.ID = t.BigSerial("id").PrimaryKey()
	})
	badCreateColumnTable.TableDef().Columns = append(badCreateColumnTable.TableDef().Columns, &schema.ColumnDef{Name: "broken_default", Type: schema.ColumnType{DataType: schema.TypeText}, HasDefault: true, Default: struct{}{}})
	if _, err := createTableSQL(pg, badCreateColumnTable.TableDef()); err == nil {
		t.Fatalf("expected createTableSQL column definition error")
	}
	badCreateConstraintTable := schema.Define("bad_create_constraint_table", func(t *struct {
		schema.TableModel
		ID *schema.Column[int64]
	},
	) {
		t.ID = t.BigSerial("id").PrimaryKey()
	})
	badCreateConstraintTable.TableDef().Constraints = append(badCreateConstraintTable.TableDef().Constraints, schema.ConstraintDef{Name: "bad_check", Type: schema.ConstraintCheck})
	if _, err := createTableSQL(pg, badCreateConstraintTable.TableDef()); err == nil {
		t.Fatalf("expected createTableSQL constraint error")
	}

	unnamed := schema.ConstraintDef{Type: schema.ConstraintUnique, Columns: []*schema.ColumnDef{users.Email.ColumnDef()}}
	if _, err := constraintDefinitionSQL(pg, users.TableDef(), unnamed); err == nil {
		t.Fatalf("expected unnamed constraint to fail")
	}
	if _, err := constraintDefinitionSQL(pg, users.TableDef(), schema.ConstraintDef{Name: "empty_pk", Type: schema.ConstraintPrimaryKey}); err == nil {
		t.Fatalf("expected empty primary key constraint to fail")
	}
	if _, err := constraintDefinitionSQL(pg, users.TableDef(), schema.ConstraintDef{Name: "empty_unique", Type: schema.ConstraintUnique}); err == nil {
		t.Fatalf("expected empty unique constraint to fail")
	}
	checkNil := schema.ConstraintDef{Name: "chk", Type: schema.ConstraintCheck}
	if _, err := constraintDefinitionSQL(pg, users.TableDef(), checkNil); err == nil {
		t.Fatalf("expected nil check constraint to fail")
	}
	unsupported := schema.ConstraintDef{Name: "bad", Type: schema.ConstraintType("mystery")}
	if _, err := constraintDefinitionSQL(pg, users.TableDef(), unsupported); err == nil {
		t.Fatalf("expected unsupported constraint type to fail")
	}
	fkMismatch := schema.ConstraintDef{
		Name:            "fk",
		Type:            schema.ConstraintForeignKey,
		Columns:         []*schema.ColumnDef{posts.UserID.ColumnDef()},
		ReferencedCols:  []*schema.ColumnDef{users.ID.ColumnDef(), users.Email.ColumnDef()},
		ReferencedTable: users.TableDef(),
	}
	if _, err := constraintDefinitionSQL(pg, posts.TableDef(), fkMismatch); err == nil {
		t.Fatalf("expected mismatched foreign key columns to fail")
	}
	if _, err := constraintDefinitionSQL(pg, posts.TableDef(), schema.ConstraintDef{Name: "fk_missing", Type: schema.ConstraintForeignKey}); err == nil {
		t.Fatalf("expected missing foreign key columns to fail")
	}
	if _, err := constraintDefinitionSQL(pg, posts.TableDef(), schema.ConstraintDef{Name: "fk_bad_update", Type: schema.ConstraintForeignKey, Columns: []*schema.ColumnDef{posts.UserID.ColumnDef()}, ReferencedCols: []*schema.ColumnDef{users.ID.ColumnDef()}, ReferencedTable: users.TableDef(), OnUpdate: schema.ForeignKeyAction("bad")}); err == nil {
		t.Fatalf("expected invalid foreign key update action to fail")
	}

	if got, err := columnDefinitionSQL(pg, users.TableDef(), users.Active.ColumnDef(), false); err != nil || !strings.Contains(got, "DEFAULT TRUE") {
		t.Fatalf("unexpected columnDefinitionSQL: %q err=%v", got, err)
	}
	if got, err := columnDefinitionSQL(pg, users.TableDef(), &schema.ColumnDef{Name: "name", Type: schema.ColumnType{DataType: schema.TypeText}}, false); err != nil || !strings.Contains(got, `"name" TEXT`) {
		t.Fatalf("unexpected simple columnDefinitionSQL: %q err=%v", got, err)
	}
	if _, err := columnDefinitionSQL(pg, users.TableDef(), &schema.ColumnDef{Name: "broken_default", Type: schema.ColumnType{DataType: schema.TypeText}, HasDefault: true, Default: struct{}{}}, false); err == nil {
		t.Fatalf("expected columnDefinitionSQL default error")
	}
	if got := columnTypeSQL(sqlite, users.CreatedAt.ColumnDef()); got != "TEXT" {
		t.Fatalf("unexpected sqlite timestamp type: %q", got)
	}
	if shouldEmitAutoIncrementKeyword(pg, &schema.ColumnDef{Name: "id", Type: schema.ColumnType{DataType: schema.TypeBigSerial}}, true) {
		t.Fatalf("expected postgres bigserial to suppress auto increment keyword")
	}
	if !shouldEmitAutoIncrementKeyword(mysqlDialect, &schema.ColumnDef{Name: "id", AutoIncrement: true, Type: schema.ColumnType{DataType: schema.TypeBigInt}}, true) {
		t.Fatalf("expected non-bigserial auto increment keyword emission")
	}
	if shouldEmitAutoIncrementKeyword(pg, &schema.ColumnDef{Name: "id", Type: schema.ColumnType{DataType: schema.TypeBigInt}}, true) {
		t.Fatalf("expected non-auto-increment column to suppress auto increment keyword")
	}
	if !shouldEmitAutoIncrementKeyword(mysqlDialect, users.ID.ColumnDef(), true) ||
		shouldEmitAutoIncrementKeyword(pg, users.ID.ColumnDef(), true) ||
		!shouldEmitAutoIncrementKeyword(sqlite, users.ID.ColumnDef(), true) ||
		shouldEmitAutoIncrementKeyword(mysqlDialect, users.ID.ColumnDef(), false) {
		t.Fatalf("unexpected auto increment keyword decisions")
	}

	for _, tc := range []struct {
		value any
		want  string
	}{
		{value: nil, want: "NULL"},
		{value: "x", want: "'x'"},
		{value: true, want: "TRUE"},
		{value: 1, want: "1"},
		{value: int8(2), want: "2"},
		{value: int16(3), want: "3"},
		{value: int32(4), want: "4"},
		{value: int64(5), want: "5"},
		{value: uint(6), want: "6"},
		{value: uint8(7), want: "7"},
		{value: uint16(8), want: "8"},
		{value: uint32(9), want: "9"},
		{value: uint64(10), want: "10"},
		{value: float32(1.5), want: "1.5"},
		{value: float64(2.5), want: "2.5"},
		{value: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), want: "'2026-01-02T03:04:05Z'"},
		{value: []byte("abc"), want: "'abc'"},
	} {
		if got, err := columnDefaultSQL(pg, users.TableDef(), &schema.ColumnDef{Name: "x", Default: tc.value}); err != nil || got != tc.want {
			t.Fatalf("unexpected columnDefaultSQL for %#v: %q err=%v", tc.value, got, err)
		}
		if got, err := literalDDLSQL(pg, tc.value); err != nil || got != tc.want {
			t.Fatalf("unexpected literalDDLSQL for %#v: %q err=%v", tc.value, got, err)
		}
	}
	if got, err := columnDefaultSQL(pg, users.TableDef(), &schema.ColumnDef{Name: "x", DefaultSQL: "NOW()"}); err != nil || got != "NOW()" {
		t.Fatalf("unexpected DefaultSQL passthrough: %q err=%v", got, err)
	}
	if _, err := columnDefaultSQL(pg, users.TableDef(), &schema.ColumnDef{Name: "x", Default: struct{}{}}); err == nil {
		t.Fatalf("expected unsupported default type to fail")
	}
	if _, err := literalDDLSQL(pg, struct{}{}); err == nil {
		t.Fatalf("expected unsupported literal type to fail")
	}

	if got := foreignKeyActionSQL(schema.ForeignKeyAction("bad")); got != "" {
		t.Fatalf("expected invalid foreign key action SQL to be empty, got %q", got)
	}
	if _, err := foreignKeyConstraintSQL(pg, schema.ForeignKeyDef{}); err == nil {
		t.Fatalf("expected incomplete foreign key to fail")
	}
	badFK := schema.ForeignKeyDef{Column: posts.UserID.ColumnDef(), ReferencedTable: users.TableDef(), ReferencedColumn: users.ID.ColumnDef(), OnDelete: schema.ForeignKeyAction("bad")}
	if _, err := foreignKeyConstraintSQL(pg, badFK); err == nil {
		t.Fatalf("expected invalid foreign key action to fail")
	}
	badUpdateFK := schema.ForeignKeyDef{Column: posts.UserID.ColumnDef(), ReferencedTable: users.TableDef(), ReferencedColumn: users.ID.ColumnDef(), OnUpdate: schema.ForeignKeyAction("bad")}
	if _, err := foreignKeyConstraintSQL(pg, badUpdateFK); err == nil {
		t.Fatalf("expected invalid foreign key update action to fail")
	}
	if got, err := foreignKeyConstraintSQL(pg, schema.ForeignKeyDef{Name: "fk", Column: posts.UserID.ColumnDef(), ReferencedTable: users.TableDef(), ReferencedColumn: users.ID.ColumnDef(), OnDelete: schema.ForeignKeyActionCascade}); err != nil || !strings.Contains(got, `CONSTRAINT "fk"`) {
		t.Fatalf("unexpected foreignKeyConstraintSQL: %q err=%v", got, err)
	}
	if got, err := foreignKeyConstraintSQL(pg, schema.ForeignKeyDef{Column: posts.UserID.ColumnDef(), ReferencedTable: users.TableDef(), ReferencedColumn: users.ID.ColumnDef(), OnUpdate: schema.ForeignKeyActionRestrict}); err != nil || !strings.Contains(got, "ON UPDATE RESTRICT") {
		t.Fatalf("unexpected foreignKeyConstraintSQL update action: %q err=%v", got, err)
	}
	if got := foreignKeyColumnsConstraintSQL(pg, []*schema.ColumnDef{posts.UserID.ColumnDef()}, users.TableDef(), []*schema.ColumnDef{users.ID.ColumnDef()}); !strings.Contains(got, `REFERENCES "coverage_users"`) {
		t.Fatalf("unexpected foreignKeyColumnsConstraintSQL: %q", got)
	}
	if got := quotedColumnsSQL(pg, []*schema.ColumnDef{users.ID.ColumnDef(), users.Email.ColumnDef()}); got != `"id", "email"` {
		t.Fatalf("unexpected quotedColumnsSQL: %q", got)
	}
	multiPK := schema.Define("multi_pk", func(t *struct {
		schema.TableModel
		A *schema.Column[int64]
		B *schema.Column[int64]
	},
	) {
		t.A = t.BigInt("a").NotNull()
		t.B = t.BigInt("b").NotNull()
		t.PrimaryKey("p1").On(t.A)
		t.PrimaryKey("p2").On(t.B)
	})
	if _, err := tablePrimaryKeyConstraint(multiPK.TableDef()); err == nil {
		t.Fatalf("expected multiple table primary keys to fail")
	}
	if !isTimestampColumn(schema.ColumnType{TimestampKind: schema.TimestampKindWithTZ}) {
		t.Fatalf("expected explicit timestamp kind to be treated as timestamp")
	}
	if !isTimestampColumn(schema.ColumnType{DataType: schema.TypeTimestamp}) || !isTimestampColumn(schema.ColumnType{DataType: schema.TypeTimestampTZ}) {
		t.Fatalf("expected timestamp data types to be treated as timestamps")
	}
	if isTimestampColumn(schema.ColumnType{DataType: schema.TypeText}) {
		t.Fatalf("expected plain text to not be timestamp")
	}

	if _, err := expressionDDLSQL(pg, users.TableDef(), schema.Ref(nil)); err == nil {
		t.Fatalf("expected nil metadata column expression to fail")
	}
	otherUsers, _ := defineInternalQueryTables()
	if _, err := expressionDDLSQL(pg, users.TableDef(), otherUsers.Email); err == nil {
		t.Fatalf("expected foreign table column expression to fail")
	}
	if got, err := expressionDDLSQL(pg, users.TableDef(), users.Email); err != nil || got != `"email"` {
		t.Fatalf("unexpected column expression: %q err=%v", got, err)
	}
	if got, err := expressionDDLSQL(pg, users.TableDef(), schema.ValueExpr{Value: "x"}); err != nil || got != "'x'" {
		t.Fatalf("unexpected value expression: %q err=%v", got, err)
	}
	if got, err := expressionDDLSQL(pg, users.TableDef(), users.Email.Eq("x")); err != nil || !strings.Contains(got, `"email" = 'x'`) {
		t.Fatalf("unexpected comparison expression: %q err=%v", got, err)
	}
	if _, err := expressionDDLSQL(pg, users.TableDef(), schema.ComparisonExpr{Left: schema.Ref(nil), Operator: "=", Right: schema.ValueExpr{Value: "x"}}); err == nil {
		t.Fatalf("expected comparison expression left-hand error")
	}
	if _, err := expressionDDLSQL(pg, users.TableDef(), schema.ComparisonExpr{Left: users.Email, Operator: "=", Right: schema.Ref(nil)}); err == nil {
		t.Fatalf("expected comparison expression right-hand error")
	}
	if _, err := expressionDDLSQL(pg, users.TableDef(), users.Email.In()); err == nil {
		t.Fatalf("expected empty IN expression to fail")
	}
	if got, err := expressionDDLSQL(pg, users.TableDef(), users.Email.In("a", "b")); err != nil || !strings.Contains(got, `'a', 'b'`) {
		t.Fatalf("unexpected IN expression: %q err=%v", got, err)
	}
	if _, err := expressionDDLSQL(pg, users.TableDef(), schema.InExpr{Left: schema.Ref(nil), Values: []schema.Expression{schema.ValueExpr{Value: "x"}}}); err == nil {
		t.Fatalf("expected IN expression left-hand error")
	}
	if _, err := expressionDDLSQL(pg, users.TableDef(), schema.InExpr{Left: users.Email, Values: []schema.Expression{schema.Ref(nil)}}); err == nil {
		t.Fatalf("expected IN expression item error")
	}
	if got, err := expressionDDLSQL(pg, users.TableDef(), users.Email.IsNull()); err != nil || !strings.Contains(got, `IS NULL`) {
		t.Fatalf("unexpected null expression: %q err=%v", got, err)
	}
	if got, err := expressionDDLSQL(pg, users.TableDef(), users.Email.IsNotNull()); err != nil || !strings.Contains(got, `IS NOT NULL`) {
		t.Fatalf("unexpected not-null expression: %q err=%v", got, err)
	}
	if _, err := expressionDDLSQL(pg, users.TableDef(), schema.NullCheckExpr{Expr: schema.Ref(nil)}); err == nil {
		t.Fatalf("expected null check inner expression error")
	}
	if got, err := expressionDDLSQL(pg, users.TableDef(), schema.And(users.Active.Eq(true), users.Email.Eq("x"))); err != nil || !strings.Contains(got, " AND ") {
		t.Fatalf("unexpected logical expression: %q err=%v", got, err)
	}
	if _, err := expressionDDLSQL(pg, users.TableDef(), schema.LogicalExpr{Operator: "AND", Exprs: []schema.Predicate{schema.ComparisonExpr{Left: schema.Ref(nil), Operator: "=", Right: schema.ValueExpr{Value: "x"}}}}); err == nil {
		t.Fatalf("expected logical expression child error")
	}
	if got, err := expressionDDLSQL(pg, users.TableDef(), schema.Raw("1 = 1")); err != nil || got != "1 = 1" {
		t.Fatalf("unexpected raw expression: %q err=%v", got, err)
	}
	if got, err := expressionDDLSQL(pg, users.TableDef(), schema.Raw("?", 1)); err != nil || got != "1" {
		t.Fatalf("unexpected raw expression with args: %q err=%v", got, err)
	}
	if _, err := expressionDDLSQL(pg, users.TableDef(), schema.AliasExpr{}); err == nil {
		t.Fatalf("expected unsupported expression to fail")
	}
}
