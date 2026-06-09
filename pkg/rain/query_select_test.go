package rain_test

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hyperlocalise/rain-orm/pkg/rain"
	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

func TestSelectToSQL(t *testing.T) {
	t.Parallel()

	db, err := rain.OpenDialect("postgres")
	if err != nil {
		t.Fatalf("OpenDialect returned error: %v", err)
	}
	users, posts := defineTables()
	u := schema.Alias(users, "u")
	p := schema.Alias(posts, "p")

	sqlText, args, err := db.Select().
		Table(p).
		Column(p.ID, p.Title, u.Email).
		Join(u, p.UserID.EqCol(u.ID)).
		Where(u.Active.Eq(true)).
		OrderBy(p.ID.Desc()).
		Limit(10).
		ToSQL()
	if err != nil {
		t.Fatalf("ToSQL returned error: %v", err)
	}

	wantSQL := `SELECT "p"."id", "p"."title", "u"."email" FROM "posts" AS "p" INNER JOIN "users" AS "u" ON "p"."user_id" = "u"."id" WHERE "u"."active" = $1 ORDER BY "p"."id" DESC LIMIT 10`
	if sqlText != wantSQL {
		t.Fatalf("unexpected SQL:\nwant: %s\ngot:  %s", wantSQL, sqlText)
	}
	if len(args) != 1 || args[0] != true {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestSelectErgonomicsToSQL(t *testing.T) {
	t.Parallel()

	db, err := rain.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	users, _ := defineTables()

	type tc struct {
		name     string
		build    func(*rain.DB) *rain.SelectQuery
		wantSQL  string
		wantArgs []any
	}

	cases := []tc{
		{
			name: "variadic select and from",
			build: func(db *rain.DB) *rain.SelectQuery {
				return db.Select(users.ID, users.Email).From(users)
			},
			wantSQL: `SELECT "users"."id", "users"."email" FROM "users"`,
		},
		{
			name: "tx variadic select and from",
			build: func(db *rain.DB) *rain.SelectQuery {
				tx, err := db.Begin(context.Background())
				if err != nil {
					panic(err)
				}
				// We don't rollback here because we are returning a query built from it,
				// and the test runner will call ToSQL on it after this function returns.
				// In a real app, you'd use a closure or defer rollback.
				return tx.Select(users.ID, users.Email).From(users)
			},
			wantSQL: `SELECT "users"."id", "users"."email" FROM "users"`,
		},
		{
			name: "select distinct and from",
			build: func(db *rain.DB) *rain.SelectQuery {
				return db.SelectDistinct(users.Email).From(users)
			},
			wantSQL: `SELECT DISTINCT "users"."email" FROM "users"`,
		},
		{
			name: "variadic select then column appends",
			build: func(db *rain.DB) *rain.SelectQuery {
				return db.Select(users.ID).From(users).Column(users.Email)
			},
			wantSQL: `SELECT "users"."id", "users"."email" FROM "users"`,
		},
	}

	for _, tt := range cases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sqlText, args, err := tt.build(db).ToSQL()
			if err != nil {
				t.Fatalf("ToSQL returned error: %v", err)
			}
			if sqlText != tt.wantSQL {
				t.Fatalf("unexpected SQL:\nwant: %s\ngot:  %s", tt.wantSQL, sqlText)
			}
			if len(args) != len(tt.wantArgs) {
				t.Fatalf("unexpected arg count: want %d got %d (%#v)", len(tt.wantArgs), len(args), args)
			}
			for idx := range tt.wantArgs {
				if args[idx] != tt.wantArgs[idx] {
					t.Fatalf("unexpected arg[%d]: want %#v got %#v", idx, tt.wantArgs[idx], args[idx])
				}
			}
		})
	}
}

func TestSelectJoinsToSQL(t *testing.T) {
	t.Parallel()

	users, posts := defineTables()

	type tc struct {
		name     string
		dialect  string
		build    func(*rain.DB) *rain.SelectQuery
		wantSQL  string
		wantArgs []any
		wantErr  string
	}

	cases := []tc{
		{
			name:    "right join postgres",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				return db.Select().Table(users).RightJoin(posts, posts.UserID.EqCol(users.ID))
			},
			wantSQL: `SELECT * FROM "users" RIGHT JOIN "posts" ON "posts"."user_id" = "users"."id"`,
		},
		{
			name:    "full join postgres",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				return db.Select().Table(users).FullJoin(posts, posts.UserID.EqCol(users.ID))
			},
			wantSQL: `SELECT * FROM "users" FULL JOIN "posts" ON "posts"."user_id" = "users"."id"`,
		},
		{
			name:    "cross join mysql",
			dialect: "mysql",
			build: func(db *rain.DB) *rain.SelectQuery {
				return db.Select().Table(users).CrossJoin(posts)
			},
			wantSQL: "SELECT * FROM `users` CROSS JOIN `posts`",
		},
		{
			name:    "right join subquery postgres",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				subquery := db.Select().Table(posts).Where(posts.ID.Gt(int64(100)))
				return db.Select().Table(users).RightJoinSubquery(subquery, "p", schema.Raw(`"p"."user_id" = "users"."id"`))
			},
			wantSQL:  `SELECT * FROM "users" RIGHT JOIN (SELECT * FROM "posts" WHERE "posts"."id" > $1) AS "p" ON "p"."user_id" = "users"."id"`,
			wantArgs: []any{int64(100)},
		},
		{
			name:    "full join subquery postgres",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				subquery := db.Select().Table(posts).Where(posts.ID.Gt(int64(100)))
				return db.Select().Table(users).FullJoinSubquery(subquery, "p", schema.Raw(`"p"."user_id" = "users"."id"`))
			},
			wantSQL:  `SELECT * FROM "users" FULL JOIN (SELECT * FROM "posts" WHERE "posts"."id" > $1) AS "p" ON "p"."user_id" = "users"."id"`,
			wantArgs: []any{int64(100)},
		},
		{
			name:    "cross join subquery sqlite",
			dialect: "sqlite",
			build: func(db *rain.DB) *rain.SelectQuery {
				subquery := db.Select().Table(posts)
				return db.Select().Table(users).CrossJoinSubquery(subquery, "p")
			},
			wantSQL: `SELECT * FROM "users" CROSS JOIN (SELECT * FROM "posts") AS "p"`,
		},
	}

	for _, tt := range cases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			db, err := rain.OpenDialect(tt.dialect)
			if err != nil {
				t.Fatalf("OpenDialect returned error: %v", err)
			}

			sqlText, args, err := tt.build(db).ToSQL()
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ToSQL returned error: %v", err)
			}
			if sqlText != tt.wantSQL {
				t.Fatalf("unexpected SQL:\nwant: %s\ngot:  %s", tt.wantSQL, sqlText)
			}
			if len(args) != len(tt.wantArgs) {
				t.Fatalf("unexpected arg count: want %d got %d (%#v)", len(tt.wantArgs), len(args), args)
			}
			for idx := range tt.wantArgs {
				if args[idx] != tt.wantArgs[idx] {
					t.Fatalf("unexpected arg[%d]: want %#v got %#v", idx, tt.wantArgs[idx], args[idx])
				}
			}
		})
	}
}

func TestSelectAdvancedPredicatesAndOrderToSQL(t *testing.T) {
	t.Parallel()

	users, posts := defineTables()

	type tc struct {
		name     string
		dialect  string
		build    func(*rain.DB) *rain.SelectQuery
		wantSQL  string
		wantArgs []any
		wantErr  string
	}

	cases := []tc{
		{
			name:    "searched case expression postgres",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				caseExpr := schema.Case().
					When(users.Active.Eq(true), schema.ValueExpr{Value: "active"}).
					When(users.Active.Eq(false), schema.ValueExpr{Value: "inactive"}).
					Else(schema.ValueExpr{Value: "unknown"}).
					End()
				return db.Select().Table(users).Column(caseExpr.As("status"))
			},
			wantSQL:  `SELECT CASE WHEN "users"."active" = $1 THEN $2 WHEN "users"."active" = $3 THEN $4 ELSE $5 END AS "status" FROM "users"`,
			wantArgs: []any{true, "active", false, "inactive", "unknown"},
		},
		{
			name:    "simple case expression mysql",
			dialect: "mysql",
			build: func(db *rain.DB) *rain.SelectQuery {
				caseExpr := schema.Case(users.ID).
					When(schema.ValueExpr{Value: int64(1)}, schema.ValueExpr{Value: "one"}).
					Else(schema.ValueExpr{Value: "other"}).
					End()
				return db.Select().Table(users).Column(caseExpr)
			},
			wantSQL:  "SELECT CASE `users`.`id` WHEN ? THEN ? ELSE ? END FROM `users`",
			wantArgs: []any{int64(1), "one", "other"},
		},
		{
			name:    "in subquery postgres",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				subquery := db.Select().Table(posts).Column(posts.UserID).Where(posts.ID.Gt(int64(100)))
				return db.Select().Table(users).Where(users.ID.InSubquery(subquery))
			},
			wantSQL:  `SELECT * FROM "users" WHERE "users"."id" IN (SELECT "posts"."user_id" FROM "posts" WHERE "posts"."id" > $1)`,
			wantArgs: []any{int64(100)},
		},
		{
			name:    "in array of expressions postgres",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				return db.Select().Table(users).Where(schema.Ref(users.ID.ColumnDef()).In(int64(1), schema.Raw("10 + 10")))
			},
			wantSQL:  `SELECT * FROM "users" WHERE "users"."id" IN ($1, 10 + 10)`,
			wantArgs: []any{int64(1)},
		},
		{
			name:    "order by nulls first postgres",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				return db.Select().Table(users).OrderBy(users.ID.Asc().NullsFirst())
			},
			wantSQL: `SELECT * FROM "users" ORDER BY "users"."id" ASC NULLS FIRST`,
		},
		{
			name:    "order by nulls last sqlite",
			dialect: "sqlite",
			build: func(db *rain.DB) *rain.SelectQuery {
				return db.Select().Table(users).OrderBy(users.ID.Desc().NullsLast())
			},
			wantSQL: `SELECT * FROM "users" ORDER BY "users"."id" DESC NULLS LAST`,
		},
		{
			name:    "nulls order unsupported mysql",
			dialect: "mysql",
			build: func(db *rain.DB) *rain.SelectQuery {
				return db.Select().Table(users).OrderBy(users.ID.Asc().NullsFirst())
			},
			wantErr: "NULLS FIRST/LAST is not supported by mysql dialect",
		},
	}

	for _, tt := range cases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			db, err := rain.OpenDialect(tt.dialect)
			if err != nil {
				t.Fatalf("OpenDialect returned error: %v", err)
			}

			sqlText, args, err := tt.build(db).ToSQL()
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ToSQL returned error: %v", err)
			}
			if sqlText != tt.wantSQL {
				t.Fatalf("unexpected SQL:\nwant: %s\ngot:  %s", tt.wantSQL, sqlText)
			}
			if len(args) != len(tt.wantArgs) {
				t.Fatalf("unexpected arg count: want %d got %d (%#v)", len(tt.wantArgs), len(args), args)
			}
			for idx := range tt.wantArgs {
				if args[idx] != tt.wantArgs[idx] {
					t.Fatalf("unexpected arg[%d]: want %#v got %#v", idx, tt.wantArgs[idx], args[idx])
				}
			}
		})
	}
}

func TestSelectSetOperationsToSQL(t *testing.T) {
	t.Parallel()

	users, posts := defineTables()

	type tc struct {
		name           string
		dialect        string
		build          func(*rain.DB) *rain.SelectQuery
		wantSQL        string
		wantArgs       []any
		wantErr        string
		checkAggregate bool
	}

	cases := []tc{
		{
			name:    "simple union postgres",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				q1 := db.Select().Table(users).Column(users.ID).Where(users.ID.Eq(int64(1)))
				q2 := db.Select().Table(users).Column(users.ID).Where(users.ID.Eq(int64(2)))
				return q1.Union(q2)
			},
			wantSQL:  `SELECT "users"."id" FROM "users" WHERE "users"."id" = $1 UNION SELECT "users"."id" FROM "users" WHERE "users"."id" = $2`,
			wantArgs: []any{int64(1), int64(2)},
		},
		{
			name:    "union all with multiple operands mysql",
			dialect: "mysql",
			build: func(db *rain.DB) *rain.SelectQuery {
				q1 := db.Select().Table(users).Column(users.ID).Where(users.ID.Eq(int64(1)))
				q2 := db.Select().Table(users).Column(users.ID).Where(users.ID.Eq(int64(2)))
				q3 := db.Select().Table(users).Column(users.ID).Where(users.ID.Eq(int64(3)))
				return q1.UnionAll(q2).UnionAll(q3)
			},
			wantSQL:  "SELECT `users`.`id` FROM `users` WHERE `users`.`id` = ? UNION ALL SELECT `users`.`id` FROM `users` WHERE `users`.`id` = ? UNION ALL SELECT `users`.`id` FROM `users` WHERE `users`.`id` = ?",
			wantArgs: []any{int64(1), int64(2), int64(3)},
		},
		{
			name:    "intersect and except postgres",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				q1 := db.Select().Table(users).Column(users.ID)
				q2 := db.Select().Table(users).Column(users.ID).Where(users.ID.Gt(int64(5)))
				q3 := db.Select().Table(users).Column(users.ID).Where(users.ID.Lt(int64(10)))
				return q1.Intersect(q2).Except(q3)
			},
			wantSQL:  `SELECT "users"."id" FROM "users" INTERSECT SELECT "users"."id" FROM "users" WHERE "users"."id" > $1 EXCEPT SELECT "users"."id" FROM "users" WHERE "users"."id" < $2`,
			wantArgs: []any{int64(5), int64(10)},
		},
		{
			name:    "set operation with root order and limit postgres",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				q1 := db.Select().Table(users).Column(users.ID)
				q2 := db.Select().Table(posts).Column(posts.UserID)
				return q1.Union(q2).OrderBy(users.ID.Desc()).Limit(5)
			},
			wantSQL: `SELECT "users"."id" FROM "users" UNION SELECT "posts"."user_id" FROM "posts" ORDER BY "users"."id" DESC LIMIT 5`,
		},
		{
			name:    "set operation with operand order and limit postgres",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				q1 := db.Select().Table(users).Column(users.ID).OrderBy(users.ID.Asc()).Limit(10)
				q2 := db.Select().Table(posts).Column(posts.UserID).OrderBy(posts.UserID.Desc()).Limit(10)
				return q1.Union(q2)
			},
			wantSQL: `(SELECT "users"."id" FROM "users" ORDER BY "users"."id" ASC LIMIT 10) UNION (SELECT "posts"."user_id" FROM "posts" ORDER BY "posts"."user_id" DESC LIMIT 10)`,
		},
		{
			name:    "set_operation_with_nested_modifiers_postgres",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				q1 := db.Select().Table(users).Column(users.ID).OrderBy(users.ID.Asc()).Limit(10)
				q2 := db.Select().Table(users).Column(users.ID).Where(users.ID.Gt(int64(100)))
				return q1.Union(q2).OrderBy(users.ID.Desc()).Limit(5)
			},
			wantSQL:  `(SELECT "users"."id" FROM "users" ORDER BY "users"."id" ASC LIMIT 10) UNION SELECT "users"."id" FROM "users" WHERE "users"."id" > $1 ORDER BY "users"."id" DESC LIMIT 5`,
			wantArgs: []any{int64(100)},
		},
		{
			name:    "chained set operations with root modifiers postgres",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				q1 := db.Select().Table(users).Column(users.ID).Where(users.ID.Eq(int64(1)))
				q2 := db.Select().Table(users).Column(users.ID).Where(users.ID.Eq(int64(2)))
				q3 := db.Select().Table(users).Column(users.ID).Where(users.ID.Eq(int64(3)))
				return q1.Union(q2).Union(q3).OrderBy(users.ID.Desc())
			},
			wantSQL:  `SELECT "users"."id" FROM "users" WHERE "users"."id" = $1 UNION SELECT "users"."id" FROM "users" WHERE "users"."id" = $2 UNION SELECT "users"."id" FROM "users" WHERE "users"."id" = $3 ORDER BY "users"."id" DESC`,
			wantArgs: []any{int64(1), int64(2), int64(3)},
		},
		{
			name:    "nested set operations (union of unions) postgres",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				q1 := db.Select().Table(users).Column(users.ID).Where(users.ID.Eq(int64(1)))
				q2 := db.Select().Table(users).Column(users.ID).Where(users.ID.Eq(int64(2)))
				sub := q1.Union(q2)

				q3 := db.Select().Table(users).Column(users.ID).Where(users.ID.Eq(int64(3)))
				return sub.Union(q3)
			},
			wantSQL:  `SELECT "users"."id" FROM "users" WHERE "users"."id" = $1 UNION SELECT "users"."id" FROM "users" WHERE "users"."id" = $2 UNION SELECT "users"."id" FROM "users" WHERE "users"."id" = $3`,
			wantArgs: []any{int64(1), int64(2), int64(3)},
		},
		{
			name:    "intersect all and except all postgres",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				q1 := db.Select().Table(users).Column(users.ID)
				q2 := db.Select().Table(users).Column(users.ID).Where(users.ID.Gt(int64(5)))
				q3 := db.Select().Table(users).Column(users.ID).Where(users.ID.Lt(int64(10)))
				return q1.IntersectAll(q2).ExceptAll(q3)
			},
			wantSQL:  `SELECT "users"."id" FROM "users" INTERSECT ALL SELECT "users"."id" FROM "users" WHERE "users"."id" > $1 EXCEPT ALL SELECT "users"."id" FROM "users" WHERE "users"."id" < $2`,
			wantArgs: []any{int64(5), int64(10)},
		},
		{
			name:    "compound query as subquery postgres",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				q1 := db.Select().Table(users).Column(users.ID).Where(users.ID.Eq(int64(1)))
				q2 := db.Select().Table(users).Column(users.ID).Where(users.ID.Eq(int64(2)))
				unionQ := q1.Union(q2)
				return db.Select().TableSubquery(unionQ, "u").Column(schema.Raw("u.id"))
			},
			wantSQL:  `SELECT u.id FROM (SELECT "users"."id" FROM "users" WHERE "users"."id" = $1 UNION SELECT "users"."id" FROM "users" WHERE "users"."id" = $2) AS "u"`,
			wantArgs: []any{int64(1), int64(2)},
		},
		{
			name:    "cte on operand error postgres",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				cte := db.Select().Table(users).Column(users.ID)
				q1 := db.Select().With("active", cte).Table(users).Column(users.ID)
				q2 := db.Select().Table(users).Column(users.ID)
				return q1.Union(q2)
			},
			wantErr: "compound query operand cannot contain CTEs",
		},
		{
			name:    "cte on second operand error postgres",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				cte := db.Select().Table(users).Column(users.ID)
				q1 := db.Select().Table(users).Column(users.ID)
				q2 := db.Select().With("active", cte).Table(users).Column(users.ID)
				return q1.Union(q2)
			},
			wantErr: "compound query operand cannot contain CTEs",
		},
		{
			name:    "simple union sqlite",
			dialect: "sqlite",
			build: func(db *rain.DB) *rain.SelectQuery {
				q1 := db.Select().Table(users).Column(users.ID).Where(users.ID.Eq(int64(1)))
				q2 := db.Select().Table(users).Column(users.ID).Where(users.ID.Eq(int64(2)))
				return q1.Union(q2)
			},
			wantSQL:  `SELECT "users"."id" FROM "users" WHERE "users"."id" = ? UNION SELECT "users"."id" FROM "users" WHERE "users"."id" = ?`,
			wantArgs: []any{int64(1), int64(2)},
		},
		{
			name:    "aggregate helper fails with set operations",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				q1 := db.Select().Table(users)
				q2 := db.Select().Table(users)
				return q1.Union(q2)
			},
			wantErr:        "aggregate helpers do not support compound queries",
			checkAggregate: true,
		},
	}

	for _, tt := range cases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			db, err := rain.OpenDialect(tt.dialect)
			if err != nil {
				t.Fatalf("OpenDialect returned error: %v", err)
			}

			q := tt.build(db)

			if tt.checkAggregate {
				_, err := q.Count(context.Background())
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}

			sqlText, args, err := q.ToSQL()
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ToSQL returned error: %v", err)
			}
			if sqlText != tt.wantSQL {
				t.Fatalf("unexpected SQL:\nwant: %s\ngot:  %s", tt.wantSQL, sqlText)
			}
			if len(args) != len(tt.wantArgs) {
				t.Fatalf("unexpected arg count: want %d got %d (%#v)", len(tt.wantArgs), len(args), args)
			}
			for idx := range tt.wantArgs {
				if args[idx] != tt.wantArgs[idx] {
					t.Fatalf("unexpected arg[%d]: want %#v got %#v", idx, tt.wantArgs[idx], args[idx])
				}
			}
		})
	}
}

func TestExpandedTypesCompileToSQL(t *testing.T) {
	t.Parallel()

	db, err := rain.OpenDialect("postgres")
	if err != nil {
		t.Fatalf("OpenDialect returned error: %v", err)
	}
	expanded := defineExpandedTypesTable()
	processedAt := time.Date(2026, 3, 28, 10, 30, 0, 0, time.UTC)
	publishedOn := time.Date(2026, 3, 28, 0, 0, 0, 0, time.UTC)

	sqlText, args, err := db.Select().
		Table(expanded).
		Column(
			expanded.SmallCount,
			expanded.Count,
			expanded.Score,
			expanded.Precise,
			expanded.Amount,
			expanded.Meta,
			expanded.MetaBin,
			expanded.ExternalID,
			expanded.Payload,
			expanded.PublishedOn,
			expanded.ProcessedAt,
			expanded.Category,
		).
		Where(schema.And(
			expanded.SmallCount.Eq(3),
			expanded.Count.Eq(11),
			expanded.Score.Gt(1.5),
			expanded.Precise.Lte(7.25),
			expanded.Amount.Eq("42.10"),
			expanded.Meta.Eq(map[string]any{"enabled": true}),
			expanded.MetaBin.Eq(map[string]any{"raw": "yes"}),
			expanded.ExternalID.Eq("00000000-0000-0000-0000-000000000042"),
			expanded.Payload.Eq([]byte{0xCA, 0xFE}),
			expanded.PublishedOn.Eq(publishedOn),
			expanded.ProcessedAt.Eq(processedAt),
			expanded.Category.Eq("alpha"),
		)).
		ToSQL()
	if err != nil {
		t.Fatalf("ToSQL returned error: %v", err)
	}

	wantSQL := `SELECT "expanded_types"."small_count", "expanded_types"."count", "expanded_types"."score", "expanded_types"."precise", "expanded_types"."amount", "expanded_types"."meta", "expanded_types"."meta_bin", "expanded_types"."external_id", "expanded_types"."payload", "expanded_types"."published_on", "expanded_types"."processed_at", "expanded_types"."category" FROM "expanded_types" WHERE ("expanded_types"."small_count" = $1 AND "expanded_types"."count" = $2 AND "expanded_types"."score" > $3 AND "expanded_types"."precise" <= $4 AND "expanded_types"."amount" = $5 AND "expanded_types"."meta" = $6 AND "expanded_types"."meta_bin" = $7 AND "expanded_types"."external_id" = $8 AND "expanded_types"."payload" = $9 AND "expanded_types"."published_on" = $10 AND "expanded_types"."processed_at" = $11 AND "expanded_types"."category" = $12)`
	if sqlText != wantSQL {
		t.Fatalf("unexpected SQL:\nwant: %s\ngot:  %s", wantSQL, sqlText)
	}
	if len(args) != 12 {
		t.Fatalf("unexpected args length: %d", len(args))
	}
}

func TestSelectAdvancedComposition(t *testing.T) {
	t.Parallel()

	users, posts := defineTables()
	cteSales := schema.Define("sales_by_user", func(t *struct {
		schema.TableModel
		UserID *schema.Column[int64]
		Total  *schema.Column[int64]
	},
	) {
		t.UserID = t.BigInt("user_id")
		t.Total = t.BigInt("total")
	})
	cteFiltered := schema.Define("filtered_sales", func(t *struct {
		schema.TableModel
		UserID *schema.Column[int64]
		Total  *schema.Column[int64]
	},
	) {
		t.UserID = t.BigInt("user_id")
		t.Total = t.BigInt("total")
	})

	type tc struct {
		name     string
		dialect  string
		build    func(*rain.DB) *rain.SelectQuery
		wantSQL  string
		wantArgs []any
		wantErr  string
	}

	cases := []tc{
		{
			name:    "distinct rendering postgres",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				return db.Select().Distinct().Table(users).Column(users.ID)
			},
			wantSQL: `SELECT DISTINCT "users"."id" FROM "users"`,
		},
		{
			name:    "group by without having mysql",
			dialect: "mysql",
			build: func(db *rain.DB) *rain.SelectQuery {
				return db.Select().
					Table(posts).
					Column(posts.UserID, schema.Raw("COUNT(*)")).
					GroupBy(posts.UserID)
			},
			wantSQL: "SELECT `posts`.`user_id`, COUNT(*) FROM `posts` GROUP BY `posts`.`user_id`",
		},
		{
			name:    "aggregate helpers in select postgres",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				return db.Select().
					Table(posts).
					Column(
						posts.UserID,
						schema.Count().As("post_count"),
						schema.Sum(posts.ID).As("id_sum"),
						schema.Avg(posts.ID).As("id_avg"),
						schema.Min(posts.ID).As("id_min"),
						schema.Max(posts.ID).As("id_max"),
					).
					GroupBy(posts.UserID)
			},
			wantSQL: `SELECT "posts"."user_id", COUNT(*) AS "post_count", SUM("posts"."id") AS "id_sum", AVG("posts"."id") AS "id_avg", MIN("posts"."id") AS "id_min", MAX("posts"."id") AS "id_max" FROM "posts" GROUP BY "posts"."user_id"`,
		},
		{
			name:    "alias helper in where placeholder ordering postgres",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				return db.Select().
					Table(posts).
					Column(posts.UserID, schema.Count().As("post_count")).
					Where(posts.Title.Eq("hello")).
					GroupBy(posts.UserID).
					Having(schema.ComparisonExpr{Left: schema.Count(), Operator: ">", Right: schema.ValueExpr{Value: 3}})
			},
			wantSQL:  `SELECT "posts"."user_id", COUNT(*) AS "post_count" FROM "posts" WHERE "posts"."title" = $1 GROUP BY "posts"."user_id" HAVING COUNT(*) > $2`,
			wantArgs: []any{"hello", 3},
		},
		{
			name:    "aggregate helper mixed with raw placeholders mysql",
			dialect: "mysql",
			build: func(db *rain.DB) *rain.SelectQuery {
				return db.Select().
					Table(posts).
					Column(schema.Sum(posts.ID).As("total_id")).
					Where(schema.ComparisonExpr{Left: schema.Raw("COALESCE(?, 0)", 10), Operator: "<", Right: schema.ValueExpr{Value: 50}})
			},
			wantSQL:  "SELECT SUM(`posts`.`id`) AS `total_id` FROM `posts` WHERE COALESCE(?, 0) < ?",
			wantArgs: []any{10, 50},
		},
		{
			name:    "coalesce helper in select postgres",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				return db.Select().
					Table(users).
					Column(schema.Coalesce(users.Email, schema.ValueExpr{Value: ""}).As("safe_email"))
			},
			wantSQL:  `SELECT COALESCE("users"."email", $1) AS "safe_email" FROM "users"`,
			wantArgs: []any{""},
		},
		{
			name:    "column alias helper in select postgres",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				return db.Select().
					Table(users).
					Column(users.Email.As("user_email"))
			},
			wantSQL: `SELECT "users"."email" AS "user_email" FROM "users"`,
		},
		{
			name:    "aggregate distinct star is invalid",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				return db.Select().
					Table(posts).
					Column(schema.AggregateExpr{
						Function: "COUNT",
						Distinct: true,
						Star:     true,
					})
			},
			wantErr: "cannot combine DISTINCT with *",
		},
		{
			name:    "aggregate missing function is invalid",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				return db.Select().
					Table(posts).
					Column(schema.AggregateExpr{Expr: posts.ID})
			},
			wantErr: "function name cannot be empty",
		},
		{
			name:    "alias in group by is invalid",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				return db.Select().
					Table(posts).
					Column(posts.UserID).
					GroupBy(schema.As(posts.UserID, "uid"))
			},
			wantErr: "aliased expressions are only supported in SELECT columns",
		},
		{
			name:    "group by with having postgres",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				return db.Select().
					Table(posts).
					Column(posts.UserID, schema.Raw("COUNT(*)")).
					GroupBy(posts.UserID).
					Having(schema.ComparisonExpr{
						Left:     schema.Raw("COUNT(*)"),
						Operator: ">",
						Right:    schema.ValueExpr{Value: 2},
					})
			},
			wantSQL:  `SELECT "posts"."user_id", COUNT(*) FROM "posts" GROUP BY "posts"."user_id" HAVING COUNT(*) > $1`,
			wantArgs: []any{2},
		},
		{
			name:    "single cte postgres",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				salesByUser := db.Select().
					Table(posts).
					Column(posts.UserID, schema.Raw("SUM(amount) AS total")).
					GroupBy(posts.UserID)

				return db.Select().
					With("sales_by_user", salesByUser).
					Table(cteSales).
					Column(cteSales.UserID, cteSales.Total)
			},
			wantSQL: `WITH "sales_by_user" AS (SELECT "posts"."user_id", SUM(amount) AS total FROM "posts" GROUP BY "posts"."user_id") SELECT "sales_by_user"."user_id", "sales_by_user"."total" FROM "sales_by_user"`,
		},
		{
			name:    "multiple ctes postgres",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				salesByUser := db.Select().
					Table(posts).
					Column(posts.UserID, schema.Raw("SUM(amount) AS total")).
					GroupBy(posts.UserID)
				filtered := db.Select().
					Table(cteSales).
					Column(cteSales.UserID, cteSales.Total).
					Where(schema.ComparisonExpr{
						Left:     schema.Raw("total"),
						Operator: ">",
						Right:    schema.ValueExpr{Value: 100},
					})

				return db.Select().
					With("sales_by_user", salesByUser).
					With("filtered_sales", filtered).
					Table(cteFiltered).
					Column(cteFiltered.UserID, cteFiltered.Total)
			},
			wantSQL:  `WITH "sales_by_user" AS (SELECT "posts"."user_id", SUM(amount) AS total FROM "posts" GROUP BY "posts"."user_id"), "filtered_sales" AS (SELECT "sales_by_user"."user_id", "sales_by_user"."total" FROM "sales_by_user" WHERE total > $1) SELECT "filtered_sales"."user_id", "filtered_sales"."total" FROM "filtered_sales"`,
			wantArgs: []any{100},
		},
		{
			name:    "subquery in from placeholder numbering postgres",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				postsByUser := db.Select().
					Table(posts).
					Column(posts.UserID, schema.Raw("COUNT(*)").As("post_count")).
					Where(posts.Title.Eq("hello")).
					GroupBy(posts.UserID)

				return db.Select().
					TableSubquery(postsByUser, "pbu").
					Column(schema.Raw("pbu.user_id"), schema.Raw("pbu.post_count")).
					Where(schema.ComparisonExpr{
						Left:     schema.Raw("pbu.post_count"),
						Operator: ">",
						Right:    schema.ValueExpr{Value: 3},
					})
			},
			wantSQL:  `SELECT pbu.user_id, pbu.post_count FROM (SELECT "posts"."user_id", COUNT(*) AS "post_count" FROM "posts" WHERE "posts"."title" = $1 GROUP BY "posts"."user_id") AS "pbu" WHERE pbu.post_count > $2`,
			wantArgs: []any{"hello", 3},
		},
		{
			name:    "subquery in join mysql",
			dialect: "mysql",
			build: func(db *rain.DB) *rain.SelectQuery {
				userPosts := db.Select().
					Table(posts).
					Column(posts.UserID, schema.Raw("COUNT(*)").As("post_count")).
					GroupBy(posts.UserID)

				return db.Select().
					Table(users).
					Column(users.ID, schema.Raw("up.post_count")).
					JoinSubquery(userPosts, "up", schema.ComparisonExpr{
						Left:     users.ID,
						Operator: "=",
						Right:    schema.Raw("up.user_id"),
					})
			},
			wantSQL: "SELECT `users`.`id`, up.post_count FROM `users` INNER JOIN (SELECT `posts`.`user_id`, COUNT(*) AS `post_count` FROM `posts` GROUP BY `posts`.`user_id`) AS `up` ON `users`.`id` = up.user_id",
		},
		{
			name:    "left join subquery postgres",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				userPosts := db.Select().
					Table(posts).
					Column(posts.UserID, schema.Raw("COUNT(*)").As("post_count")).
					GroupBy(posts.UserID)

				return db.Select().
					Table(users).
					Column(users.ID, schema.Raw("up.post_count")).
					LeftJoinSubquery(userPosts, "up", schema.ComparisonExpr{
						Left:     users.ID,
						Operator: "=",
						Right:    schema.Raw("up.user_id"),
					})
			},
			wantSQL: `SELECT "users"."id", up.post_count FROM "users" LEFT JOIN (SELECT "posts"."user_id", COUNT(*) AS "post_count" FROM "posts" GROUP BY "posts"."user_id") AS "up" ON "users"."id" = up.user_id`,
		},
		{
			name:    "subquery without alias is invalid",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				return db.Select().
					TableSubquery(db.Select().Table(users), "").
					Column(schema.Raw("id"))
			},
			wantErr: "requires a non-empty alias",
		},
		{
			name:    "subquery without query is invalid",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				return db.Select().
					TableSubquery(nil, "sq").
					Column(schema.Raw("id"))
			},
			wantErr: "requires a non-nil query",
		},
		{
			name:    "cte supported on mysql",
			dialect: "mysql",
			build: func(db *rain.DB) *rain.SelectQuery {
				base := db.Select().Table(users)
				return db.Select().With("u", base).Table(users)
			},
			wantSQL: "WITH `u` AS (SELECT * FROM `users`) SELECT * FROM `users`",
		},
		{
			name:    "nested cte body is invalid",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				inner := db.Select().Table(users).Column(users.ID)
				outerBody := db.Select().
					With("inner", inner).
					Table(users).
					Column(users.ID)

				return db.Select().
					With("outer", outerBody).
					Table(users).
					Column(users.ID)
			},
			wantErr: `CTE "outer" body cannot itself contain CTEs`,
		},
	}

	for _, tt := range cases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			db, err := rain.OpenDialect(tt.dialect)
			if err != nil {
				t.Fatalf("OpenDialect returned error: %v", err)
			}

			sqlText, args, err := tt.build(db).ToSQL()
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ToSQL returned error: %v", err)
			}
			if sqlText != tt.wantSQL {
				t.Fatalf("unexpected SQL:\nwant: %s\ngot:  %s", tt.wantSQL, sqlText)
			}
			if len(args) != len(tt.wantArgs) {
				t.Fatalf("unexpected arg count: want %d got %d (%#v)", len(tt.wantArgs), len(args), args)
			}
			for idx := range tt.wantArgs {
				if args[idx] != tt.wantArgs[idx] {
					t.Fatalf("unexpected arg[%d]: want %#v got %#v", idx, tt.wantArgs[idx], args[idx])
				}
			}
		})
	}
}

func TestSelectInPredicateToSQL(t *testing.T) {
	t.Parallel()

	db, err := rain.OpenDialect("postgres")
	if err != nil {
		t.Fatalf("OpenDialect returned error: %v", err)
	}
	users, _ := defineTables()

	sqlText, args, err := db.Select().
		Table(users).
		Where(users.ID.In(int64(3), int64(5), int64(8))).
		ToSQL()
	if err != nil {
		t.Fatalf("ToSQL returned error: %v", err)
	}

	wantSQL := `SELECT * FROM "users" WHERE "users"."id" IN ($1, $2, $3)`
	if sqlText != wantSQL {
		t.Fatalf("unexpected SQL:\nwant: %s\ngot:  %s", wantSQL, sqlText)
	}
	if !reflect.DeepEqual(args, []any{int64(3), int64(5), int64(8)}) {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestSelectLockingToSQL(t *testing.T) {
	t.Parallel()

	users, posts := defineTables()

	type tc struct {
		name    string
		dialect string
		op      string // "count", "exists", or "" for ToSQL
		build   func(*rain.DB) *rain.SelectQuery
		wantSQL string
		wantErr string
	}

	cases := []tc{
		{
			name:    "for update postgres",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				return db.Select().Table(users).ForUpdate()
			},
			wantSQL: `SELECT * FROM "users" FOR UPDATE`,
		},
		{
			name:    "for share mysql",
			dialect: "mysql",
			build: func(db *rain.DB) *rain.SelectQuery {
				return db.Select().Table(users).ForShare()
			},
			wantSQL: "SELECT * FROM `users` FOR SHARE",
		},
		{
			name:    "for update nowait postgres",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				return db.Select().Table(users).ForUpdate(rain.LockConfig{NoWait: true})
			},
			wantSQL: `SELECT * FROM "users" FOR UPDATE NOWAIT`,
		},
		{
			name:    "for update skip locked mysql",
			dialect: "mysql",
			build: func(db *rain.DB) *rain.SelectQuery {
				return db.Select().Table(users).ForUpdate(rain.LockConfig{SkipLocked: true})
			},
			wantSQL: "SELECT * FROM `users` FOR UPDATE SKIP LOCKED",
		},
		{
			name:    "for update of tables postgres",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				return db.Select().Table(users).Join(posts, posts.UserID.EqCol(users.ID)).ForUpdate(rain.LockConfig{Of: []schema.TableReference{users, posts}})
			},
			wantSQL: `SELECT * FROM "users" INNER JOIN "posts" ON "posts"."user_id" = "users"."id" FOR UPDATE OF "users", "posts"`,
		},
		{
			name:    "for update of aliased tables postgres",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				u := schema.Alias(users, "u")
				p := schema.Alias(posts, "p")
				return db.Select().Table(u).Join(p, p.UserID.EqCol(u.ID)).ForUpdate(rain.LockConfig{Of: []schema.TableReference{u, p}})
			},
			wantSQL: `SELECT * FROM "users" AS "u" INNER JOIN "posts" AS "p" ON "p"."user_id" = "u"."id" FOR UPDATE OF "u", "p"`,
		},
		{
			name:    "for no key update postgres",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				return db.Select().Table(users).For(rain.LockNoKeyUpdate)
			},
			wantSQL: `SELECT * FROM "users" FOR NO KEY UPDATE`,
		},
		{
			name:    "locking unsupported on sqlite",
			dialect: "sqlite",
			build: func(db *rain.DB) *rain.SelectQuery {
				return db.Select().Table(users).ForUpdate()
			},
			wantErr: "select locking is not supported by sqlite dialect",
		},
		{
			name:    "locking with compound query error",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				q1 := db.Select().Table(users)
				q2 := db.Select().Table(users)
				return q1.Union(q2).ForUpdate()
			},
			wantErr: "compound queries do not support FOR locking clauses",
		},
		{
			name:    "locking with count error",
			dialect: "postgres",
			op:      "count",
			build: func(db *rain.DB) *rain.SelectQuery {
				return db.Select().Table(users).ForUpdate()
			},
			wantErr: "aggregate helpers do not support FOR locking clauses",
		},
		{
			name:    "locking with exists error",
			dialect: "postgres",
			op:      "exists",
			build: func(db *rain.DB) *rain.SelectQuery {
				return db.Select().Table(users).ForUpdate()
			},
			wantErr: "exists checks do not support FOR locking clauses",
		},
	}

	for _, tt := range cases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			db, err := rain.OpenDialect(tt.dialect)
			if err != nil {
				t.Fatalf("OpenDialect returned error: %v", err)
			}

			q := tt.build(db)

			switch tt.op {
			case "count":
				_, err := q.Count(context.Background())
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			case "exists":
				_, err := q.Exists(context.Background())
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			case "":
			default:
				t.Fatalf("unknown select test operation %q", tt.op)
			}

			sqlText, _, err := q.ToSQL()
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ToSQL returned error: %v", err)
			}
			if sqlText != tt.wantSQL {
				t.Fatalf("unexpected SQL:\nwant: %s\ngot:  %s", tt.wantSQL, sqlText)
			}
		})
	}
}

func TestSelectDistinctOnToSQL(t *testing.T) {
	t.Parallel()

	users, _ := defineTables()

	type tc struct {
		name    string
		dialect string
		build   func(*rain.DB) *rain.SelectQuery
		wantSQL string
		wantErr string
	}

	cases := []tc{
		{
			name:    "distinct on one column postgres",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				return db.Select().Table(users).DistinctOn(users.ID).Column(users.ID, users.Email)
			},
			wantSQL: `SELECT DISTINCT ON ("users"."id") "users"."id", "users"."email" FROM "users"`,
		},
		{
			name:    "distinct on multiple columns postgres",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				return db.Select().Table(users).DistinctOn(users.ID, users.Email).Column(users.ID)
			},
			wantSQL: `SELECT DISTINCT ON ("users"."id", "users"."email") "users"."id" FROM "users"`,
		},
		{
			name:    "distinct on with expression postgres",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				return db.Select().Table(users).DistinctOn(schema.Raw("LOWER(email)")).Column(users.ID)
			},
			wantSQL: `SELECT DISTINCT ON (LOWER(email)) "users"."id" FROM "users"`,
		},
		{
			name:    "distinct on unsupported on mysql",
			dialect: "mysql",
			build: func(db *rain.DB) *rain.SelectQuery {
				return db.Select().Table(users).DistinctOn(users.ID)
			},
			wantErr: "SELECT DISTINCT ON is not supported by mysql dialect",
		},
		{
			name:    "distinct on unsupported on sqlite",
			dialect: "sqlite",
			build: func(db *rain.DB) *rain.SelectQuery {
				return db.Select().Table(users).DistinctOn(users.ID)
			},
			wantErr: "SELECT DISTINCT ON is not supported by sqlite dialect",
		},
		{
			name:    "distinct and distinct on together error",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				return db.Select().Table(users).Distinct().DistinctOn(users.ID)
			},
			wantErr: "SELECT DISTINCT and DISTINCT ON cannot be used together",
		},
		{
			name:    "distinct on in compound query error",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				q1 := db.Select().Table(users)
				q2 := db.Select().Table(users)
				return q1.Union(q2).DistinctOn(users.ID)
			},
			wantErr: "compound queries do not support DISTINCT ON",
		},
		{
			name:    "aggregate helper fails with distinct on",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				return db.Select().Table(users).DistinctOn(users.ID)
			},
			wantErr: "aggregate helpers do not support DISTINCT, DISTINCT ON, GROUP BY, or HAVING clauses",
		},
	}

	for _, tt := range cases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			db, err := rain.OpenDialect(tt.dialect)
			if err != nil {
				t.Fatalf("OpenDialect returned error: %v", err)
			}

			q := tt.build(db)

			if strings.HasPrefix(tt.name, "aggregate helper fails") {
				_, err := q.Count(context.Background())
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}

			sqlText, _, err := q.ToSQL()
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ToSQL returned error: %v", err)
			}
			if sqlText != tt.wantSQL {
				t.Fatalf("unexpected SQL:\nwant: %s\ngot:  %s", tt.wantSQL, sqlText)
			}
		})
	}
}
