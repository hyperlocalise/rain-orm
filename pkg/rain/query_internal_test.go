package rain

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hyperlocalise/rain-orm/pkg/dialect"
	"github.com/hyperlocalise/rain-orm/pkg/schema"
	_ "modernc.org/sqlite"
)

type internalQueryUsersTable struct {
	schema.TableModel
	ID        *schema.Column[int64]
	Email     *schema.Column[string]
	Name      *schema.Column[string]
	Active    *schema.Column[bool]
	Nickname  *schema.Column[string]
	CreatedAt *schema.Column[time.Time]
}

type internalQueryPostsTable struct {
	schema.TableModel
	ID     *schema.Column[int64]
	UserID *schema.Column[int64]
	Title  *schema.Column[string]
}

type internalInsertModel struct {
	ID       int64   `db:"id"`
	Email    string  `db:"email"`
	Name     string  `db:"name"`
	Active   bool    `db:"active"`
	Nickname *string `db:"nickname"`
}

type internalUserRow struct {
	ID       int64   `db:"id"`
	Email    string  `db:"email"`
	Name     string  `db:"name"`
	Nickname *string `db:"nickname"`
}

func defineInternalQueryTables() (*internalQueryUsersTable, *internalQueryPostsTable) {
	users := schema.Define("users", func(t *internalQueryUsersTable) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.Email = t.VarChar("email", 255).NotNull()
		t.Name = t.Text("name").NotNull().Default("guest")
		t.Active = t.Boolean("active").NotNull().Default(true)
		t.Nickname = t.Text("nickname").Nullable().Default("buddy")
		t.CreatedAt = t.TimestampTZ("created_at").NotNull().DefaultNow()
	})

	posts := schema.Define("posts", func(t *internalQueryPostsTable) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.UserID = t.BigInt("user_id").NotNull().References(users.ID)
		t.Title = t.Text("title").NotNull()
	})

	return users, posts
}

func openInternalQueryDB(t *testing.T) *DB {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "query-internal.sqlite")
	db, err := Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	return db
}

func createInternalQuerySchema(t *testing.T, ctx context.Context, db *DB) {
	t.Helper()

	users, posts := defineInternalQueryTables()

	for _, table := range []schema.TableReference{users, posts} {
		statement, err := db.CreateTableSQL(table)
		if err != nil {
			t.Fatalf("compile schema for %q: %v", table.TableDef().Name, err)
		}
		if _, err := db.Exec(ctx, statement); err != nil {
			t.Fatalf("exec schema statement %q: %v", statement, err)
		}
	}
}

func TestQueryExecutionPaths(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openInternalQueryDB(t)
	users, posts := defineInternalQueryTables()
	createInternalQuerySchema(t, ctx, db)

	insert := db.Insert().
		Table(users).
		Model(&internalInsertModel{Email: "alice@example.com", Name: "Alice"})
	result, err := insert.Exec(ctx)
	if err != nil {
		t.Fatalf("insert exec failed: %v", err)
	}
	insertedID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id failed: %v", err)
	}

	if _, err := db.Insert().
		Table(posts).
		Set(posts.UserID, insertedID).
		Set(posts.Title, "Hello").
		Exec(ctx); err != nil {
		t.Fatalf("insert post failed: %v", err)
	}

	count, err := db.Select().Table(users).Where(users.Active.Eq(true)).Count(ctx)
	if err != nil {
		t.Fatalf("count failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected count 1, got %d", count)
	}

	exists, err := db.Select().Table(users).Where(users.Email.Eq("alice@example.com")).Exists(ctx)
	if err != nil {
		t.Fatalf("exists failed: %v", err)
	}
	if !exists {
		t.Fatalf("expected row to exist")
	}

	var row internalUserRow
	if err := db.Select().
		Table(users).
		Where(users.ID.Eq(insertedID)).
		Scan(ctx, &row); err != nil {
		t.Fatalf("scan select failed: %v", err)
	}
	if row.Email != "alice@example.com" || row.Name != "Alice" {
		t.Fatalf("unexpected row: %#v", row)
	}

	if err := db.Update().
		Table(users).
		Set(users.Name, "Alice Updated").
		Where(users.ID.Eq(insertedID)).
		Returning(users.ID, users.Name).
		Scan(ctx, &row); err != nil {
		t.Fatalf("update returning scan failed: %v", err)
	}
	if row.ID != insertedID || row.Name != "Alice Updated" {
		t.Fatalf("unexpected updated row: %#v", row)
	}

	if err := db.Delete().
		Table(users).
		Where(users.ID.Eq(insertedID)).
		Returning(users.ID, users.Email).
		Scan(ctx, &row); err != nil {
		t.Fatalf("delete returning scan failed: %v", err)
	}
	if row.ID != insertedID || row.Email != "alice@example.com" {
		t.Fatalf("unexpected deleted row: %#v", row)
	}

	if _, err := db.Select().Table(users).Where(users.ID.Eq(insertedID)).Count(ctx); !errors.Is(err, sql.ErrNoRows) && err != nil {
		t.Fatalf("unexpected count error after delete: %v", err)
	}
}

func TestQueryBuilderAndHelperErrors(t *testing.T) {
	t.Parallel()

	db, err := OpenDialect("postgres")
	if err != nil {
		t.Fatalf("OpenDialect returned error: %v", err)
	}
	users, posts := defineInternalQueryTables()

	if _, _, err := db.Select().ToSQL(); err == nil || !strings.Contains(err.Error(), "requires a table") {
		t.Fatalf("expected select table error, got %v", err)
	}
	selectNoRunner := &SelectQuery{dialect: db.Dialect(), table: tableDefSource{table: users.TableDef()}}
	if err := selectNoRunner.Scan(context.Background(), &internalUserRow{}); !errors.Is(err, ErrNoConnection) {
		t.Fatalf("expected select scan ErrNoConnection, got %v", err)
	}
	if _, err := (&SelectQuery{dialect: db.Dialect()}).Count(context.Background()); !errors.Is(err, ErrNoConnection) {
		t.Fatalf("expected select count ErrNoConnection, got %v", err)
	}
	if _, err := (&SelectQuery{dialect: db.Dialect()}).Exists(context.Background()); !errors.Is(err, ErrNoConnection) {
		t.Fatalf("expected select exists ErrNoConnection, got %v", err)
	}

	if _, _, err := db.Insert().ToSQL(); err == nil || !strings.Contains(err.Error(), "requires a table") {
		t.Fatalf("expected insert table error, got %v", err)
	}
	if _, _, err := db.Insert().Table(users).ToSQL(); err == nil || !strings.Contains(err.Error(), "requires either explicit values or a model") {
		t.Fatalf("expected insert values error, got %v", err)
	}
	if err := (&InsertQuery{dialect: db.Dialect(), table: users.TableDef(), returning: []schema.Expression{users.ID}}).Scan(context.Background(), &internalUserRow{}); !errors.Is(err, ErrNoConnection) {
		t.Fatalf("expected insert scan ErrNoConnection, got %v", err)
	}

	insertNoRunner := &InsertQuery{dialect: db.Dialect(), table: users.TableDef(), returning: []schema.Expression{users.ID}}
	if err := insertNoRunner.Scan(context.Background(), &internalUserRow{}); !errors.Is(err, ErrNoConnection) {
		t.Fatalf("expected insert returning scan ErrNoConnection, got %v", err)
	}
	insertNoReturning := &InsertQuery{runner: db, dialect: db.Dialect(), table: users.TableDef()}
	if err := insertNoReturning.Scan(context.Background(), &internalUserRow{}); err == nil || !strings.Contains(err.Error(), "requires RETURNING") {
		t.Fatalf("expected insert returning error, got %v", err)
	}

	if _, _, err := db.Update().ToSQL(); err == nil || !strings.Contains(err.Error(), "requires a table") {
		t.Fatalf("expected update table error, got %v", err)
	}
	if _, _, err := db.Update().Table(users).ToSQL(); err == nil || !strings.Contains(err.Error(), "requires at least one assignment") {
		t.Fatalf("expected update assignment error, got %v", err)
	}
	if _, _, err := db.Update().Table(users).Set(users.Name, "Alice").ToSQL(); err == nil || !strings.Contains(err.Error(), "requires at least one WHERE predicate") {
		t.Fatalf("expected update WHERE guard error, got %v", err)
	}
	if _, _, err := db.Update().Table(users).Set(users.Name, "Alice").Unbounded().ToSQL(); err != nil {
		t.Fatalf("expected unbounded update to succeed, got %v", err)
	}
	updateNoRunner := &UpdateQuery{dialect: db.Dialect(), table: users.TableDef(), values: []assignment{{column: users.Name, value: schema.ValueExpr{Value: "Alice"}}}, returning: []schema.Expression{users.ID}}
	if err := updateNoRunner.Scan(context.Background(), &internalUserRow{}); !errors.Is(err, ErrNoConnection) {
		t.Fatalf("expected update scan ErrNoConnection, got %v", err)
	}
	updateNoReturning := &UpdateQuery{runner: db, dialect: db.Dialect(), table: users.TableDef(), values: []assignment{{column: users.Name, value: schema.ValueExpr{Value: "Alice"}}}}
	if err := updateNoReturning.Scan(context.Background(), &internalUserRow{}); err == nil || !strings.Contains(err.Error(), "requires RETURNING") {
		t.Fatalf("expected update returning error, got %v", err)
	}

	if _, _, err := db.Delete().ToSQL(); err == nil || !strings.Contains(err.Error(), "requires a table") {
		t.Fatalf("expected delete table error, got %v", err)
	}
	if _, _, err := db.Delete().Table(users).ToSQL(); err == nil || !strings.Contains(err.Error(), "requires at least one WHERE predicate") {
		t.Fatalf("expected delete WHERE guard error, got %v", err)
	}
	if _, _, err := db.Delete().Table(users).Unbounded().ToSQL(); err != nil {
		t.Fatalf("expected unbounded delete to succeed, got %v", err)
	}
	deleteNoRunner := &DeleteQuery{dialect: db.Dialect(), table: users.TableDef(), returning: []schema.Expression{users.ID}}
	if err := deleteNoRunner.Scan(context.Background(), &internalUserRow{}); !errors.Is(err, ErrNoConnection) {
		t.Fatalf("expected delete scan ErrNoConnection, got %v", err)
	}
	deleteNoReturning := &DeleteQuery{runner: db, dialect: db.Dialect(), table: users.TableDef()}
	if err := deleteNoReturning.Scan(context.Background(), &internalUserRow{}); err == nil || !strings.Contains(err.Error(), "requires RETURNING") {
		t.Fatalf("expected delete returning error, got %v", err)
	}

	leftJoinSQL, _, err := db.Select().
		Table(users).
		Column(users.ID).
		LeftJoin(posts, users.ID.EqCol(posts.UserID)).
		Where(users.Active.Eq(true)).
		Where(users.Email.Eq("alice@example.com")).
		OrderBy(users.ID.Asc(), users.Email.Desc()).
		Limit(5).
		Offset(10).
		ToSQL()
	if err != nil {
		t.Fatalf("left join ToSQL failed: %v", err)
	}
	if !strings.Contains(leftJoinSQL, "LEFT JOIN") || !strings.Contains(leftJoinSQL, "OFFSET 10") {
		t.Fatalf("unexpected left join SQL: %s", leftJoinSQL)
	}
}

func TestCompileContextAndAssignmentsHelpers(t *testing.T) {
	t.Parallel()

	users, posts := defineInternalQueryTables()

	ctx := newCompileContext(dialectForTest(t, "postgres"))
	if err := ctx.writeRaw(schema.Raw("NOW()")); err != nil {
		t.Fatalf("writeRaw without args failed: %v", err)
	}
	if ctx.String() != "NOW()" {
		t.Fatalf("unexpected raw SQL: %s", ctx.String())
	}

	ctx = newCompileContext(dialectForTest(t, "postgres"))
	if err := ctx.writeRaw(schema.Raw("? + ?", 1, 2)); err != nil {
		t.Fatalf("writeRaw placeholders failed: %v", err)
	}
	if ctx.String() != "$1 + $2" {
		t.Fatalf("unexpected placeholder SQL: %s", ctx.String())
	}

	if err := newCompileContext(dialectForTest(t, "postgres")).writeRaw(schema.Raw("?", 1, 2)); err == nil || !strings.Contains(err.Error(), "unused args") {
		t.Fatalf("expected raw unused args error, got %v", err)
	}
	if err := newCompileContext(dialectForTest(t, "postgres")).writeRaw(schema.Raw("? ?", 1)); err == nil || !strings.Contains(err.Error(), "placeholder count") {
		t.Fatalf("expected raw placeholder mismatch error, got %v", err)
	}
	if err := newCompileContext(dialectForTest(t, "postgres")).writeExpression(nil); err == nil || !strings.Contains(err.Error(), "unsupported expression type") {
		t.Fatalf("expected unsupported expression error, got %v", err)
	}

	merged, err := mergeAssignments(users.TableDef(),
		[]assignment{
			{column: users.Email, value: schema.ValueExpr{Value: "base@example.com"}},
			{column: users.Name, value: schema.ValueExpr{Value: "Base"}},
		},
		[]assignment{
			{column: users.Name, value: schema.ValueExpr{Value: "Override"}},
			{column: users.Active, value: schema.ValueExpr{Value: true}},
		},
	)
	if err != nil {
		t.Fatalf("mergeAssignments failed: %v", err)
	}
	if len(merged) != 3 {
		t.Fatalf("expected 3 merged assignments, got %d", len(merged))
	}
	if merged[1].column.ColumnDef().Name != "name" || merged[2].column.ColumnDef().Name != "active" {
		t.Fatalf("unexpected merged order: %#v", merged)
	}
	if merged[1].value.(schema.ValueExpr).Value != "Override" {
		t.Fatalf("expected override assignment to win, got %#v", merged[1].value)
	}

	if _, err := mergeAssignments(users.TableDef(), nil, []assignment{{column: posts.Title, value: schema.ValueExpr{Value: "bad"}}}); err == nil || !strings.Contains(err.Error(), "belongs to table posts") {
		t.Fatalf("expected foreign table assignment error, got %v", err)
	}

	ghostColumn := schema.Ref(&schema.ColumnDef{Table: users.TableDef(), Name: "ghost"})
	if _, err := mergeAssignments(users.TableDef(), nil, []assignment{{column: ghostColumn, value: schema.ValueExpr{Value: "bad"}}}); err == nil || !strings.Contains(err.Error(), "unknown column ghost") {
		t.Fatalf("expected unknown column assignment error, got %v", err)
	}

	if got := joinPredicates([]schema.Predicate{users.Active.Eq(true)}); got != users.Active.Eq(true) {
		t.Fatalf("expected single predicate to pass through")
	}
	if _, ok := joinPredicates([]schema.Predicate{users.Active.Eq(true), users.Email.Eq("alice@example.com")}).(schema.LogicalExpr); !ok {
		t.Fatalf("expected multiple predicates to produce logical expression")
	}
}

func TestModelAssignmentAndValueHelpers(t *testing.T) {
	t.Parallel()

	users, _ := defineInternalQueryTables()

	nickname := "ally"
	assignments, err := assignmentsFromModel(users.TableDef(), &internalInsertModel{
		ID:       0,
		Email:    "alice@example.com",
		Name:     "",
		Active:   false,
		Nickname: &nickname,
	}, true)
	if err != nil {
		t.Fatalf("assignmentsFromModel failed: %v", err)
	}
	if len(assignments) != 2 {
		t.Fatalf("expected 2 assignments after skipping default-backed zero values, got %d", len(assignments))
	}
	if assignments[0].column.ColumnDef().Name != "email" || assignments[1].column.ColumnDef().Name != "nickname" {
		t.Fatalf("unexpected assignments: %#v", assignments)
	}

	assignments, err = assignmentsFromModel(users.TableDef(), &internalInsertModel{
		ID:     42,
		Email:  "bob@example.com",
		Name:   "Bob",
		Active: true,
	}, false)
	if err != nil {
		t.Fatalf("assignmentsFromModel skipAuto=false failed: %v", err)
	}
	if len(assignments) != 4 {
		t.Fatalf("expected 4 assignments when auto id is retained, got %d", len(assignments))
	}

	if _, include := fieldValueForInsert(users.ID.ColumnDef(), reflect.ValueOf(int64(0)), true); include {
		t.Fatalf("expected zero auto-increment id to be skipped")
	}
	if _, include := fieldValueForInsert(users.Name.ColumnDef(), reflect.ValueOf(""), true); include {
		t.Fatalf("expected default-backed zero string to be skipped")
	}
	if _, include := fieldValueForInsert(users.Nickname.ColumnDef(), reflect.ValueOf((*string)(nil)), true); include {
		t.Fatalf("expected nil pointer to be skipped")
	}
	if value, include := fieldValueForInsert(users.Name.ColumnDef(), reflect.ValueOf("Alice"), true); !include || value != "Alice" {
		t.Fatalf("expected non-zero string to be included, got %#v include=%v", value, include)
	}

	type pointerHolder struct {
		Value **string
	}
	var nilStringPtr *string
	holder := pointerHolder{Value: &nilStringPtr}
	if _, isNil := dereferenceValue(reflect.ValueOf(holder).Field(0)); !isNil {
		t.Fatalf("expected nested nil pointer to be detected")
	}

	name := "Alice"
	namePtr := &name
	holder = pointerHolder{Value: &namePtr}
	resolved, isNil := dereferenceValue(reflect.ValueOf(holder).Field(0))
	if isNil || resolved.Kind() != reflect.String || resolved.String() != "Alice" {
		t.Fatalf("unexpected dereference result: %#v isNil=%v", resolved, isNil)
	}
}

func dialectForTest(t *testing.T, driver string) dialect.Dialect {
	t.Helper()

	db, err := OpenDialect(driver)
	if err != nil {
		t.Fatalf("OpenDialect returned error: %v", err)
	}

	return db.Dialect()
}
