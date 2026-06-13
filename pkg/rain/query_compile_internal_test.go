package rain

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

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
	if _, err := selectNoRunner.Prepare(context.Background()); !errors.Is(err, ErrNoConnection) {
		t.Fatalf("expected select prepare ErrNoConnection, got %v", err)
	}
	selectUnsupportedPrepare := &SelectQuery{runner: &countingRunner{}, dialect: db.Dialect(), table: tableDefSource{table: users.TableDef()}}
	if _, err := selectUnsupportedPrepare.Prepare(context.Background()); !errors.Is(err, ErrPrepareNotSupported) {
		t.Fatalf("expected select prepare ErrPrepareNotSupported, got %v", err)
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
	if _, _, err := db.Insert().Table(users).ToSQL(); err == nil || !strings.Contains(err.Error(), "requires a data source") {
		t.Fatalf("expected insert values error, got %v", err)
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

	t.Run("WriteRawWithoutArgs", func(t *testing.T) {
		ctx := newCompileContext(dialectForTest(t, "postgres"))
		defer releaseCompileContext(ctx)
		if err := ctx.writeRaw(schema.Raw("NOW()")); err != nil {
			t.Fatalf("writeRaw without args failed: %v", err)
		}
		if ctx.String() != "NOW()" {
			t.Fatalf("unexpected raw SQL: %s", ctx.String())
		}
	})

	t.Run("WriteRawWithPlaceholders", func(t *testing.T) {
		ctx := newCompileContext(dialectForTest(t, "postgres"))
		defer releaseCompileContext(ctx)
		if err := ctx.writeRaw(schema.Raw("? + ?", 1, 2)); err != nil {
			t.Fatalf("writeRaw placeholders failed: %v", err)
		}
		if ctx.String() != "$1 + $2" {
			t.Fatalf("unexpected placeholder SQL: %s", ctx.String())
		}
	})

	for _, tc := range []struct {
		name    string
		dialect string
		wantSQL string
	}{
		{name: "postgres", dialect: "postgres", wantSQL: `"users"."email" = $1 AND "users"."active" = $2 AND "users"."name" = $3`},
		{name: "mysql", dialect: "mysql", wantSQL: "`users`.`email` = ? AND `users`.`active` = ? AND `users`.`name` = ?"},
		{name: "sqlite", dialect: "sqlite", wantSQL: `"users"."email" = ? AND "users"."active" = ? AND "users"."name" = ?`},
	} {
		t.Run("named placeholder "+tc.name, func(t *testing.T) {
			ctx := newCompileContext(dialectForTest(t, tc.dialect))
			defer releaseCompileContext(ctx)
			expr := schema.And(
				users.Email.EqExpr(schema.Placeholder("email")),
				users.Active.EqExpr(schema.Placeholder("active")),
				users.Name.Eq("alice"),
			)
			if err := ctx.writeExpression(expr); err != nil {
				t.Fatalf("writeExpression failed: %v", err)
			}
			if ctx.String() != "("+tc.wantSQL+")" {
				t.Fatalf("unexpected SQL:\nwant: %s\ngot:  %s", "("+tc.wantSQL+")", ctx.String())
			}
			compiled := ctx.compiledQuery()
			if !compiled.hasNames {
				t.Fatalf("expected compiled query to track named placeholders")
			}
			if _, err := compiled.literalArgs(); !errors.Is(err, ErrPreparedArgsRequired) {
				t.Fatalf("expected literalArgs placeholder error, got %v", err)
			}
			bound, err := compiled.bind(PreparedArgs{
				"email":  "alice@example.com",
				"active": true,
			})
			if err != nil {
				t.Fatalf("bind failed: %v", err)
			}
			if !reflect.DeepEqual(bound, []any{"alice@example.com", true, "alice"}) {
				t.Fatalf("unexpected bound args: %#v", bound)
			}
		})
	}

	t.Run("RawUnusedArgsError", func(t *testing.T) {
		ctx := newCompileContext(dialectForTest(t, "postgres"))
		defer releaseCompileContext(ctx)
		if err := ctx.writeRaw(schema.Raw("?", 1, 2)); err == nil || !strings.Contains(err.Error(), "unused args") {
			t.Fatalf("expected raw unused args error, got %v", err)
		}
	})

	t.Run("RawPlaceholderMismatchError", func(t *testing.T) {
		ctx := newCompileContext(dialectForTest(t, "postgres"))
		defer releaseCompileContext(ctx)
		if err := ctx.writeRaw(schema.Raw("? ?", 1)); err == nil || !strings.Contains(err.Error(), "placeholder count") {
			t.Fatalf("expected raw placeholder mismatch error, got %v", err)
		}
	})

	t.Run("CoalesceArityError", func(t *testing.T) {
		ctx := newCompileContext(dialectForTest(t, "postgres"))
		defer releaseCompileContext(ctx)
		if err := ctx.writeExpression(schema.CoalesceExpr{Exprs: []schema.Expression{users.Email}}); err == nil || !strings.Contains(err.Error(), "at least two expressions") {
			t.Fatalf("expected COALESCE arity error, got %v", err)
		}
	})

	t.Run("CaseArityError", func(t *testing.T) {
		ctx := newCompileContext(dialectForTest(t, "postgres"))
		defer releaseCompileContext(ctx)
		if err := ctx.writeExpression(schema.CaseExpr{}); err == nil || !strings.Contains(err.Error(), "CASE expression requires at least one WHEN clause") {
			t.Fatalf("expected CASE arity error, got %v", err)
		}
	})

	t.Run("EmptyInError", func(t *testing.T) {
		ctx := newCompileContext(dialectForTest(t, "postgres"))
		defer releaseCompileContext(ctx)
		if err := ctx.writeExpression(users.ID.In()); err == nil || !strings.Contains(err.Error(), "requires at least one value") {
			t.Fatalf("expected empty IN error, got %v", err)
		}
	})

	t.Run("UnsupportedExpressionError", func(t *testing.T) {
		ctx := newCompileContext(dialectForTest(t, "postgres"))
		defer releaseCompileContext(ctx)
		if err := ctx.writeExpression(nil); err == nil || !strings.Contains(err.Error(), "unsupported expression type") {
			t.Fatalf("expected unsupported expression error, got %v", err)
		}
	})

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

	t.Run("writeJoinedPredicates", func(t *testing.T) {
		ctx := newCompileContext(dialectForTest(t, "postgres"))
		defer releaseCompileContext(ctx)

		// Empty
		ctx.reset(dialectForTest(t, "postgres"))
		if err := ctx.writeJoinedPredicates(nil); err != nil {
			t.Fatalf("empty writeJoinedPredicates failed: %v", err)
		}
		if ctx.String() != "" {
			t.Fatalf("expected empty SQL for empty predicates, got %q", ctx.String())
		}

		// Single
		ctx.reset(dialectForTest(t, "postgres"))
		if err := ctx.writeJoinedPredicates([]schema.Predicate{users.Active.Eq(true)}); err != nil {
			t.Fatalf("single writeJoinedPredicates failed: %v", err)
		}
		if ctx.String() != `"users"."active" = $1` {
			t.Fatalf("unexpected single predicate SQL: %s", ctx.String())
		}

		// Multiple
		ctx.reset(dialectForTest(t, "postgres"))
		if err := ctx.writeJoinedPredicates([]schema.Predicate{users.Active.Eq(true), users.Email.Eq("alice@example.com")}); err != nil {
			t.Fatalf("multiple writeJoinedPredicates failed: %v", err)
		}
		if ctx.String() != `("users"."active" = $1 AND "users"."email" = $2)` {
			t.Fatalf("unexpected multiple predicates SQL: %s", ctx.String())
		}
	})
}

func TestCompiledQueryBindValidation(t *testing.T) {
	t.Parallel()

	users, _ := defineInternalQueryTables()
	compiled, err := (&SelectQuery{
		dialect: dialectForTest(t, "postgres"),
		table:   tableDefSource{table: users.TableDef()},
		where: []schema.Predicate{
			schema.And(
				users.Email.EqExpr(schema.Placeholder("email")),
				users.Name.EqExpr(schema.Placeholder("email")),
			),
		},
	}).compile()
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	bound, err := compiled.bind(PreparedArgs{"email": "alice@example.com"})
	if err != nil {
		t.Fatalf("bind repeated placeholder failed: %v", err)
	}
	if !reflect.DeepEqual(bound, []any{"alice@example.com", "alice@example.com"}) {
		t.Fatalf("unexpected repeated placeholder binding: %#v", bound)
	}

	if _, err := compiled.bind(PreparedArgs{}); err == nil || !strings.Contains(err.Error(), "missing prepared arg") {
		t.Fatalf("expected missing arg error, got %v", err)
	}
	if _, err := compiled.bind(PreparedArgs{"email": "alice@example.com", "extra": 1}); err == nil || !strings.Contains(err.Error(), "unexpected prepared arg") {
		t.Fatalf("expected extra arg error, got %v", err)
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
	if len(assignments) != 4 {
		t.Fatalf("expected 4 assignments with explicit zero-value writes, got %d", len(assignments))
	}
	if assignments[0].column.ColumnDef().Name != "email" || assignments[1].column.ColumnDef().Name != "name" || assignments[2].column.ColumnDef().Name != "active" || assignments[3].column.ColumnDef().Name != "nickname" {
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
	if value, include := fieldValueForInsert(users.Name.ColumnDef(), reflect.ValueOf(""), true); !include || value != "" {
		t.Fatalf("expected default-backed zero string to be included, got %#v include=%v", value, include)
	}
	if _, include := fieldValueForInsert(users.Nickname.ColumnDef(), reflect.ValueOf((*string)(nil)), true); include {
		t.Fatalf("expected nil pointer to be skipped")
	}
	if _, include := fieldValueForInsert(users.Active.ColumnDef(), reflect.ValueOf(Set[bool]{}), true); include {
		t.Fatalf("expected invalid set value to be skipped")
	}
	if value, include := fieldValueForInsert(users.Active.ColumnDef(), reflect.ValueOf(Set[bool]{Value: false, Valid: true}), true); !include || value != false {
		t.Fatalf("expected explicit false set value to be included, got %#v include=%v", value, include)
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

func TestNewOperatorsSQL(t *testing.T) {
	t.Parallel()

	users, _ := defineInternalQueryTables()
	d := dialectForTest(t, "postgres")

	for _, tc := range []struct {
		name     string
		expr     schema.Expression
		wantSQL  string
		wantArgs []any
	}{
		{
			name:     "NotIn",
			expr:     users.ID.NotIn(1, 2, 3),
			wantSQL:  `"users"."id" NOT IN ($1, $2, $3)`,
			wantArgs: []any{int64(1), int64(2), int64(3)},
		},
		{
			name:     "Like",
			expr:     users.Email.Like("%@example.com"),
			wantSQL:  `"users"."email" LIKE $1`,
			wantArgs: []any{"%@example.com"},
		},
		{
			name:     "NotLike",
			expr:     users.Email.NotLike("%@example.com"),
			wantSQL:  `"users"."email" NOT LIKE $1`,
			wantArgs: []any{"%@example.com"},
		},
		{
			name:     "ILike",
			expr:     users.Email.ILike("%@EXAMPLE.COM"),
			wantSQL:  `"users"."email" ILIKE $1`,
			wantArgs: []any{"%@EXAMPLE.COM"},
		},
		{
			name:     "NotILike",
			expr:     users.Email.NotILike("%@EXAMPLE.COM"),
			wantSQL:  `"users"."email" NOT ILIKE $1`,
			wantArgs: []any{"%@EXAMPLE.COM"},
		},
		{
			name:     "Between",
			expr:     users.ID.Between(10, 20),
			wantSQL:  `"users"."id" BETWEEN $1 AND $2`,
			wantArgs: []any{int64(10), int64(20)},
		},
		{
			name:     "NotBetween",
			expr:     users.ID.NotBetween(10, 20),
			wantSQL:  `"users"."id" NOT BETWEEN $1 AND $2`,
			wantArgs: []any{int64(10), int64(20)},
		},
		{
			name:     "LogicalNot",
			expr:     schema.Not(users.Active.Eq(true)),
			wantSQL:  `NOT ("users"."active" = $1)`,
			wantArgs: []any{true},
		},
		{
			name: "Exists",
			expr: schema.Exists(&SelectQuery{
				dialect: d,
				table:   tableDefSource{table: users.TableDef()},
				where:   []schema.Predicate{users.ID.Eq(1)},
			}),
			wantSQL:  `EXISTS (SELECT * FROM "users" WHERE "users"."id" = $1)`,
			wantArgs: []any{int64(1)},
		},
		{
			name: "NotExists",
			expr: schema.NotExists(&SelectQuery{
				dialect: d,
				table:   tableDefSource{table: users.TableDef()},
				where:   []schema.Predicate{users.ID.Eq(1)},
			}),
			wantSQL:  `NOT EXISTS (SELECT * FROM "users" WHERE "users"."id" = $1)`,
			wantArgs: []any{int64(1)},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := newCompileContext(d)
			defer releaseCompileContext(ctx)
			if err := ctx.writeExpression(tc.expr); err != nil {
				t.Fatalf("writeExpression failed: %v", err)
			}
			if ctx.String() != tc.wantSQL {
				t.Fatalf("unexpected SQL:\nwant: %s\ngot:  %s", tc.wantSQL, ctx.String())
			}
			compiled := ctx.compiledQuery()
			args, err := compiled.literalArgs()
			if err != nil {
				t.Fatalf("literalArgs failed: %v", err)
			}
			if !reflect.DeepEqual(args, tc.wantArgs) {
				t.Fatalf("unexpected args:\nwant: %#v\ngot:  %#v", tc.wantArgs, args)
			}
		})
	}
}

func TestCompoundQueryInternals(t *testing.T) {
	t.Parallel()

	users, _ := defineInternalQueryTables()
	d := dialectForTest(t, "postgres")

	t.Run("cacheOptions preserved in non-flattening wrapSetOp", func(t *testing.T) {
		q1 := &SelectQuery{
			dialect:      d,
			table:        tableDefSource{table: users.TableDef()},
			cacheOptions: &queryCacheOptions{ttl: 5 * time.Minute},
		}
		q2 := &SelectQuery{dialect: d, table: tableDefSource{table: users.TableDef()}}
		union := q1.Union(q2)
		if union.cacheOptions == nil || union.cacheOptions.ttl != 5*time.Minute {
			t.Fatalf("expected cacheOptions to propagate, got %#v", union.cacheOptions)
		}
	})

	t.Run("cacheOptions preserved in flattening wrapSetOp", func(t *testing.T) {
		q1 := &SelectQuery{dialect: d, table: tableDefSource{table: users.TableDef()}}
		q2 := &SelectQuery{dialect: d, table: tableDefSource{table: users.TableDef()}}
		q3 := &SelectQuery{dialect: d, table: tableDefSource{table: users.TableDef()}}
		base := q1.Union(q2)
		base.cacheOptions = &queryCacheOptions{ttl: 5 * time.Minute}
		union := base.Union(q3)
		if union.cacheOptions == nil || union.cacheOptions.ttl != 5*time.Minute {
			t.Fatalf("expected cacheOptions to propagate through flatten, got %#v", union.cacheOptions)
		}
	})

	t.Run("compileExists on compound query", func(t *testing.T) {
		q1 := &SelectQuery{
			dialect: d,
			table:   tableDefSource{table: users.TableDef()},
			where:   []schema.Predicate{users.ID.Eq(1)},
		}
		q2 := &SelectQuery{
			dialect: d,
			table:   tableDefSource{table: users.TableDef()},
			where:   []schema.Predicate{users.ID.Eq(2)},
		}
		union := q1.Union(q2)
		compiled, err := union.compileExists()
		if err != nil {
			t.Fatalf("compileExists failed: %v", err)
		}
		want := `SELECT EXISTS(SELECT * FROM "users" WHERE "users"."id" = $1 UNION SELECT * FROM "users" WHERE "users"."id" = $2)`
		if compiled.sql != want {
			t.Fatalf("unexpected exists SQL:\nwant: %s\ngot:  %s", want, compiled.sql)
		}
	})

	t.Run("isBareCompound", func(t *testing.T) {
		op := &SelectQuery{dialect: d, table: tableDefSource{table: users.TableDef()}}
		bare := &SelectQuery{dialect: d, firstOperand: op}
		if !bare.isBareCompound() {
			t.Fatalf("expected bare compound")
		}
		withOrder := &SelectQuery{dialect: d, firstOperand: op, order: []schema.OrderExpr{{}}}
		if withOrder.isBareCompound() {
			t.Fatalf("expected non-bare with order")
		}
	})
}

func dialectForTest(t *testing.T, driver string) dialect.Dialect {
	t.Helper()

	db, err := OpenDialect(driver)
	if err != nil {
		t.Fatalf("OpenDialect returned error: %v", err)
	}

	return db.Dialect()
}
