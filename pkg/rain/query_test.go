package rain_test

import (
	"strings"
	"testing"
	"time"

	"github.com/hyperlocalise/rain-orm/pkg/dialect"
	"github.com/hyperlocalise/rain-orm/pkg/rain"
	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

type usersTable struct {
	schema.TableModel
	ID        *schema.Column[int64]
	Email     *schema.Column[string]
	Name      *schema.Column[string]
	Active    *schema.Column[bool]
	CreatedAt *schema.Column[time.Time]
}

type postsTable struct {
	schema.TableModel
	ID     *schema.Column[int64]
	UserID *schema.Column[int64]
	Title  *schema.Column[string]
}

type expandedTypesTable struct {
	schema.TableModel
	ID          *schema.Column[int64]
	SmallCount  *schema.Column[int16]
	Count       *schema.Column[int32]
	Score       *schema.Column[float32]
	Precise     *schema.Column[float64]
	Amount      *schema.Column[string]
	Meta        *schema.Column[any]
	MetaBin     *schema.Column[any]
	ExternalID  *schema.Column[string]
	Payload     *schema.Column[[]byte]
	PublishedOn *schema.Column[time.Time]
	ProcessedAt *schema.Column[time.Time]
	Category    *schema.Column[string]
}

type userModel struct {
	ID     int64  `db:"id"`
	Email  string `db:"email"`
	Name   string `db:"name"`
	Active bool   `db:"active"`
}

func defineTables() (*usersTable, *postsTable) {
	users := schema.Define("users", func(t *usersTable) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.Email = t.VarChar("email", 255).NotNull().Unique()
		t.Name = t.Text("name").NotNull()
		t.Active = t.Boolean("active").NotNull().Default(true)
		t.CreatedAt = t.TimestampTZ("created_at").NotNull().DefaultNow()
	})

	posts := schema.Define("posts", func(t *postsTable) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.UserID = t.BigInt("user_id").NotNull().References(users.ID)
		t.Title = t.Text("title").NotNull()
	})

	return users, posts
}

func defineExpandedTypesTable() *expandedTypesTable {
	return schema.Define("expanded_types", func(t *expandedTypesTable) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.SmallCount = t.SmallInt("small_count").NotNull()
		t.Count = t.Integer("count").NotNull()
		t.Score = t.Real("score").NotNull()
		t.Precise = t.Double("precise").NotNull()
		t.Amount = t.Decimal("amount", 12, 2).NotNull()
		t.Meta = t.JSON("meta").NotNull()
		t.MetaBin = t.JSONB("meta_bin").NotNull()
		t.ExternalID = t.UUID("external_id").NotNull()
		t.Payload = t.Bytes("payload").NotNull()
		t.PublishedOn = t.Date("published_on").NotNull()
		t.ProcessedAt = t.Timestamp("processed_at").NotNull()
		t.Category = t.Enum("category", "alpha", "beta").NotNull()
	})
}

func TestSelectToSQL(t *testing.T) {
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
					Column(posts.UserID, schema.Raw("COUNT(*) AS post_count")).
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
			wantSQL:  `SELECT pbu.user_id, pbu.post_count FROM (SELECT "posts"."user_id", COUNT(*) AS post_count FROM "posts" WHERE "posts"."title" = $1 GROUP BY "posts"."user_id") AS "pbu" WHERE pbu.post_count > $2`,
			wantArgs: []any{"hello", 3},
		},
		{
			name:    "subquery in join mysql",
			dialect: "mysql",
			build: func(db *rain.DB) *rain.SelectQuery {
				userPosts := db.Select().
					Table(posts).
					Column(posts.UserID, schema.Raw("COUNT(*) AS post_count")).
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
			wantSQL: "SELECT `users`.`id`, up.post_count FROM `users` INNER JOIN (SELECT `posts`.`user_id`, COUNT(*) AS post_count FROM `posts` GROUP BY `posts`.`user_id`) AS `up` ON `users`.`id` = up.user_id",
		},
		{
			name:    "left join subquery postgres",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				userPosts := db.Select().
					Table(posts).
					Column(posts.UserID, schema.Raw("COUNT(*) AS post_count")).
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
			wantSQL: `SELECT "users"."id", up.post_count FROM "users" LEFT JOIN (SELECT "posts"."user_id", COUNT(*) AS post_count FROM "posts" GROUP BY "posts"."user_id") AS "up" ON "users"."id" = up.user_id`,
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
			name:    "cte unsupported on mysql",
			dialect: "mysql",
			build: func(db *rain.DB) *rain.SelectQuery {
				base := db.Select().Table(users)
				return db.Select().With("u", base).Table(users)
			},
			wantErr: "do not support CTEs",
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

func TestInsertUpdateDeleteToSQL(t *testing.T) {
	db, err := rain.OpenDialect("postgres")
	if err != nil {
		t.Fatalf("OpenDialect returned error: %v", err)
	}
	users, _ := defineTables()

	insertSQL, insertArgs, err := db.Insert().
		Table(users).
		Model(&userModel{Email: "alice@example.com", Name: "Alice", Active: true}).
		Returning(users.ID).
		ToSQL()
	if err != nil {
		t.Fatalf("insert ToSQL returned error: %v", err)
	}
	wantInsert := `INSERT INTO "users" ("email", "name", "active") VALUES ($1, $2, $3) RETURNING "users"."id"`
	if insertSQL != wantInsert {
		t.Fatalf("unexpected insert SQL:\nwant: %s\ngot:  %s", wantInsert, insertSQL)
	}
	if len(insertArgs) != 3 {
		t.Fatalf("unexpected insert args: %#v", insertArgs)
	}

	updateSQL, updateArgs, err := db.Update().
		Table(users).
		Set(users.Name, "Alice Smith").
		Where(users.ID.Eq(int64(1))).
		ToSQL()
	if err != nil {
		t.Fatalf("update ToSQL returned error: %v", err)
	}
	wantUpdate := `UPDATE "users" SET "name" = $1 WHERE "users"."id" = $2`
	if updateSQL != wantUpdate {
		t.Fatalf("unexpected update SQL:\nwant: %s\ngot:  %s", wantUpdate, updateSQL)
	}
	if len(updateArgs) != 2 {
		t.Fatalf("unexpected update args: %#v", updateArgs)
	}

	deleteSQL, deleteArgs, err := db.Delete().
		Table(users).
		Where(users.ID.Eq(int64(99))).
		ToSQL()
	if err != nil {
		t.Fatalf("delete ToSQL returned error: %v", err)
	}
	wantDelete := `DELETE FROM "users" WHERE "users"."id" = $1`
	if deleteSQL != wantDelete {
		t.Fatalf("unexpected delete SQL:\nwant: %s\ngot:  %s", wantDelete, deleteSQL)
	}
	if len(deleteArgs) != 1 || deleteArgs[0] != int64(99) {
		t.Fatalf("unexpected delete args: %#v", deleteArgs)
	}
}

func TestDialectFeatures(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		dialect  string
		features dialect.Feature
		missing  []dialect.Feature
	}{
		{
			name:    "postgres",
			dialect: "postgres",
			features: dialect.FeatureInsertReturning |
				dialect.FeatureUpdateReturning |
				dialect.FeatureDeleteReturning |
				dialect.FeatureOffset |
				dialect.FeatureUpsert |
				dialect.FeatureCTE |
				dialect.FeatureDefaultPlaceholder,
		},
		{
			name:     "mysql",
			dialect:  "mysql",
			features: dialect.FeatureOffset | dialect.FeatureUpsert,
			missing: []dialect.Feature{
				dialect.FeatureInsertReturning,
				dialect.FeatureUpdateReturning,
				dialect.FeatureDeleteReturning,
				dialect.FeatureCTE,
				dialect.FeatureDefaultPlaceholder,
			},
		},
		{
			name:    "sqlite",
			dialect: "sqlite",
			features: dialect.FeatureInsertReturning |
				dialect.FeatureUpdateReturning |
				dialect.FeatureDeleteReturning |
				dialect.FeatureOffset |
				dialect.FeatureUpsert,
			missing: []dialect.Feature{
				dialect.FeatureCTE,
				dialect.FeatureDefaultPlaceholder,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			db, err := rain.OpenDialect(tc.dialect)
			if err != nil {
				t.Fatalf("OpenDialect returned error: %v", err)
			}
			got := db.Dialect().Features()
			if got != tc.features {
				t.Fatalf("unexpected features: want %b got %b", tc.features, got)
			}
			for _, feature := range tc.missing {
				if dialect.HasFeature(got, feature) {
					t.Fatalf("expected feature %b to be absent from %b", feature, got)
				}
			}
		})
	}
}

func TestOpenDialectUnknownDialectReturnsError(t *testing.T) {
	t.Parallel()

	db, err := rain.OpenDialect("postres")
	if err == nil {
		t.Fatalf("expected unsupported dialect error, got nil")
	}
	if db != nil {
		t.Fatalf("expected nil db for unsupported dialect")
	}
}

func TestReturningUnsupportedDialect(t *testing.T) {
	db, err := rain.OpenDialect("mysql")
	if err != nil {
		t.Fatalf("OpenDialect returned error: %v", err)
	}
	users, _ := defineTables()

	_, _, err = db.Insert().
		Table(users).
		Set(users.Name, "Alice").
		Returning(users.ID).
		ToSQL()
	if err == nil || !strings.Contains(err.Error(), "insert queries do not support RETURNING") {
		t.Fatalf("expected insert RETURNING to fail on mysql dialect, got %v", err)
	}

	_, _, err = db.Update().
		Table(users).
		Set(users.Name, "Alice").
		Where(users.ID.Eq(int64(1))).
		Returning(users.ID).
		ToSQL()
	if err == nil || !strings.Contains(err.Error(), "update queries do not support RETURNING") {
		t.Fatalf("expected update RETURNING to fail on mysql dialect, got %v", err)
	}

	_, _, err = db.Delete().
		Table(users).
		Where(users.ID.Eq(int64(1))).
		Returning(users.ID).
		ToSQL()
	if err == nil || !strings.Contains(err.Error(), "delete queries do not support RETURNING") {
		t.Fatalf("expected delete RETURNING to fail on mysql dialect, got %v", err)
	}
}

func TestReturningSupportedOperations(t *testing.T) {
	db, err := rain.OpenDialect("postgres")
	if err != nil {
		t.Fatalf("OpenDialect returned error: %v", err)
	}
	users, _ := defineTables()

	insertSQL, _, err := db.Insert().
		Table(users).
		Set(users.Name, "Alice").
		Returning(users.ID).
		ToSQL()
	if err != nil || !strings.Contains(insertSQL, "RETURNING") {
		t.Fatalf("expected insert RETURNING to compile, got sql=%q err=%v", insertSQL, err)
	}

	updateSQL, _, err := db.Update().
		Table(users).
		Set(users.Name, "Alice").
		Where(users.ID.Eq(int64(1))).
		Returning(users.ID).
		ToSQL()
	if err != nil || !strings.Contains(updateSQL, "RETURNING") {
		t.Fatalf("expected update RETURNING to compile, got sql=%q err=%v", updateSQL, err)
	}

	deleteSQL, _, err := db.Delete().
		Table(users).
		Where(users.ID.Eq(int64(1))).
		Returning(users.ID).
		ToSQL()
	if err != nil || !strings.Contains(deleteSQL, "RETURNING") {
		t.Fatalf("expected delete RETURNING to compile, got sql=%q err=%v", deleteSQL, err)
	}
}

func TestInsertModelAndSetMergeToSQL(t *testing.T) {
	db, err := rain.OpenDialect("postgres")
	if err != nil {
		t.Fatalf("OpenDialect returned error: %v", err)
	}
	users, _ := defineTables()

	sqlText, args, err := db.Insert().
		Table(users).
		Model(&userModel{Email: "alice@example.com", Name: "", Active: false}).
		Set(users.Name, "Alice").
		Set(users.Active, false).
		ToSQL()
	if err != nil {
		t.Fatalf("insert merge ToSQL returned error: %v", err)
	}

	wantSQL := `INSERT INTO "users" ("email", "name", "active") VALUES ($1, $2, $3)`
	if sqlText != wantSQL {
		t.Fatalf("unexpected merged insert SQL:\nwant: %s\ngot:  %s", wantSQL, sqlText)
	}
	if len(args) != 3 || args[0] != "alice@example.com" || args[1] != "Alice" || args[2] != false {
		t.Fatalf("unexpected merged insert args: %#v", args)
	}
}

func TestInsertOmitDefaultBackedZeroValues(t *testing.T) {
	db, err := rain.OpenDialect("postgres")
	if err != nil {
		t.Fatalf("OpenDialect returned error: %v", err)
	}
	users, _ := defineTables()

	sqlText, args, err := db.Insert().
		Table(users).
		Model(&userModel{Email: "alice@example.com"}).
		ToSQL()
	if err != nil {
		t.Fatalf("insert default omission ToSQL returned error: %v", err)
	}

	wantSQL := `INSERT INTO "users" ("email", "name") VALUES ($1, $2)`
	if sqlText != wantSQL {
		t.Fatalf("unexpected default-omitting insert SQL:\nwant: %s\ngot:  %s", wantSQL, sqlText)
	}
	if len(args) != 2 || args[0] != "alice@example.com" || args[1] != "" {
		t.Fatalf("unexpected default-omitting insert args: %#v", args)
	}
}
