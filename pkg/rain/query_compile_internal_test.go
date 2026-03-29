package rain

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/hyperlocalise/rain-orm/pkg/dialect"
	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

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
	if err := newCompileContext(dialectForTest(t, "postgres")).writeExpression(users.ID.In()); err == nil || !strings.Contains(err.Error(), "requires at least one value") {
		t.Fatalf("expected empty IN error, got %v", err)
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
