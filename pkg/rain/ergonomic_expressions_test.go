package rain

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

func TestErgonomicExpressionsToSQL(t *testing.T) {
	type UsersTable struct {
		schema.TableModel
		ID     *schema.Column[int64]
		Age    *schema.Column[int32]
		Name   *schema.Column[string]
		Active *schema.Column[bool]
	}

	Users := schema.Define("users", func(t *UsersTable) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.Age = t.Integer("age").NotNull()
		t.Name = t.Text("name").NotNull()
		t.Active = t.Boolean("active").NotNull()
	})

	db := MustOpenDialect("postgres")

	t.Run("StandaloneFunctions", func(t *testing.T) {
		tests := []struct {
			name     string
			pred     schema.Predicate
			wantSQL  string
			wantArgs []any
		}{
			{
				name:     "Eq",
				pred:     schema.Eq(Users.ID, int64(1)),
				wantSQL:  `"users"."id" = $1`,
				wantArgs: []any{int64(1)},
			},
			{
				name:     "Gt",
				pred:     schema.Gt(Users.Age, 18),
				wantSQL:  `"users"."age" > $1`,
				wantArgs: []any{18},
			},
			{
				name:     "InArray",
				pred:     schema.In(Users.ID, int64(1), int64(2), int64(3)),
				wantSQL:  `"users"."id" IN ($1, $2, $3)`,
				wantArgs: []any{int64(1), int64(2), int64(3)},
			},
			{
				name:    "IsNull",
				pred:    schema.IsNull(Users.Name),
				wantSQL: `"users"."name" IS NULL`,
			},
			{
				name:    "IsNotNull",
				pred:    schema.IsNotNull(Users.Name),
				wantSQL: `"users"."name" IS NOT NULL`,
			},
			{
				name:     "Ne",
				pred:     schema.Ne(Users.ID, int64(1)),
				wantSQL:  `"users"."id" <> $1`,
				wantArgs: []any{int64(1)},
			},
			{
				name:     "Gte",
				pred:     schema.Gte(Users.Age, 18),
				wantSQL:  `"users"."age" >= $1`,
				wantArgs: []any{18},
			},
			{
				name:     "Lt",
				pred:     schema.Lt(Users.Age, 18),
				wantSQL:  `"users"."age" < $1`,
				wantArgs: []any{18},
			},
			{
				name:     "Lte",
				pred:     schema.Lte(Users.Age, 18),
				wantSQL:  `"users"."age" <= $1`,
				wantArgs: []any{18},
			},
			{
				name:     "Like",
				pred:     schema.Like(Users.Name, "A%"),
				wantSQL:  `"users"."name" LIKE $1`,
				wantArgs: []any{"A%"},
			},
			{
				name:     "NotLike",
				pred:     schema.NotLike(Users.Name, "A%"),
				wantSQL:  `"users"."name" NOT LIKE $1`,
				wantArgs: []any{"A%"},
			},
			{
				name:     "ILike",
				pred:     schema.ILike(Users.Name, "a%"),
				wantSQL:  `"users"."name" ILIKE $1`,
				wantArgs: []any{"a%"},
			},
			{
				name:     "NotILike",
				pred:     schema.NotILike(Users.Name, "a%"),
				wantSQL:  `"users"."name" NOT ILIKE $1`,
				wantArgs: []any{"a%"},
			},
			{
				name:     "NotIn",
				pred:     schema.NotIn(Users.ID, int64(1), int64(2)),
				wantSQL:  `"users"."id" NOT IN ($1, $2)`,
				wantArgs: []any{int64(1), int64(2)},
			},
			{
				name:     "Between",
				pred:     schema.Between(Users.Age, 18, 30),
				wantSQL:  `"users"."age" BETWEEN $1 AND $2`,
				wantArgs: []any{18, 30},
			},
			{
				name:     "NotBetween",
				pred:     schema.NotBetween(Users.Age, 18, 30),
				wantSQL:  `"users"."age" NOT BETWEEN $1 AND $2`,
				wantArgs: []any{18, 30},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				sqlText, args, err := db.Select().From(Users).Where(tt.pred).ToSQL()
				if err != nil {
					t.Fatalf("ToSQL() error = %v", err)
				}
				expectedSQL := "SELECT * FROM \"users\" WHERE " + tt.wantSQL
				if sqlText != expectedSQL {
					t.Errorf("got SQL %q, want %q", sqlText, expectedSQL)
				}
				if !reflect.DeepEqual(args, tt.wantArgs) && (len(args) != 0 || len(tt.wantArgs) != 0) {
					t.Errorf("got args %v, want %v", args, tt.wantArgs)
				}
			})
		}
	})

	t.Run("FluentChaining", func(t *testing.T) {
		tests := []struct {
			name     string
			pred     schema.Predicate
			wantSQL  string
			wantArgs []any
		}{
			{
				name:     "AggregateComparison",
				pred:     schema.Count().Gt(5),
				wantSQL:  `COUNT(*) > $1`,
				wantArgs: []any{5},
			},
			{
				name:     "ArithmeticComparison",
				pred:     Users.Age.Add(10).Lte(50),
				wantSQL:  `("users"."age" + $1) <= $2`,
				wantArgs: []any{10, 50},
			},
			{
				name:     "AnyColumnEq",
				pred:     Users.C("name").Eq("Alice"),
				wantSQL:  `"users"."name" = $1`,
				wantArgs: []any{"Alice"},
			},
			{
				name:     "AnyColumnNe",
				pred:     Users.C("name").Ne("Alice"),
				wantSQL:  `"users"."name" <> $1`,
				wantArgs: []any{"Alice"},
			},
			{
				name:     "AnyColumnGt",
				pred:     Users.C("age").Gt(18),
				wantSQL:  `"users"."age" > $1`,
				wantArgs: []any{18},
			},
			{
				name:     "AnyColumnGte",
				pred:     Users.C("age").Gte(18),
				wantSQL:  `"users"."age" >= $1`,
				wantArgs: []any{18},
			},
			{
				name:     "AnyColumnLt",
				pred:     Users.C("age").Lt(18),
				wantSQL:  `"users"."age" < $1`,
				wantArgs: []any{18},
			},
			{
				name:     "AnyColumnLte",
				pred:     Users.C("age").Lte(18),
				wantSQL:  `"users"."age" <= $1`,
				wantArgs: []any{18},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				// We use a manual compile context or just build a dummy query that uses these.
				// Chaining on AggregateExpr usually goes in HAVING.
				q := db.Select(schema.Count()).From(Users).GroupBy(Users.ID)
				if strings.Contains(tt.wantSQL, "COUNT") {
					q = q.Having(tt.pred)
				} else {
					q = q.Where(tt.pred)
				}

				sqlText, args, err := q.ToSQL()
				if err != nil {
					t.Fatalf("ToSQL() error = %v", err)
				}
				if !strings.Contains(sqlText, tt.wantSQL) {
					t.Errorf("SQL %q does not contain %q", sqlText, tt.wantSQL)
				}
				// Verify args are present
				for _, arg := range tt.wantArgs {
					found := false
					for _, got := range args {
						// Match by value string to handle potential type differences in literals.
						if fmt.Sprintf("%v", got) == fmt.Sprintf("%v", arg) {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("arg %v not found in %v", arg, args)
					}
				}
			})
		}
	})

	t.Run("FluentChainingExtra", func(t *testing.T) {
		tests := []struct {
			name     string
			pred     schema.Predicate
			wantSQL  string
			wantArgs []any
		}{
			{
				name:     "AggregateNe",
				pred:     schema.Count().Ne(0),
				wantSQL:  `COUNT(*) <> $1`,
				wantArgs: []any{0},
			},
			{
				name:     "AggregateGte",
				pred:     schema.Count().Gte(1),
				wantSQL:  `COUNT(*) >= $1`,
				wantArgs: []any{1},
			},
			{
				name:     "AggregateLt",
				pred:     schema.Count().Lt(100),
				wantSQL:  `COUNT(*) < $1`,
				wantArgs: []any{100},
			},
			{
				name:     "AggregateLte",
				pred:     schema.Count().Lte(10),
				wantSQL:  `COUNT(*) <= $1`,
				wantArgs: []any{10},
			},
			{
				name:     "ArithmeticNe",
				pred:     Users.Age.Add(1).Ne(21),
				wantSQL:  `("users"."age" + $1) <> $2`,
				wantArgs: []any{1, 21},
			},
			{
				name:     "ArithmeticGt",
				pred:     Users.Age.Sub(1).Gt(17),
				wantSQL:  `("users"."age" - $1) > $2`,
				wantArgs: []any{1, 17},
			},
			{
				name:     "ArithmeticGte",
				pred:     Users.Age.Mul(2).Gte(40),
				wantSQL:  `("users"."age" * $1) >= $2`,
				wantArgs: []any{2, 40},
			},
			{
				name:     "ArithmeticLt",
				pred:     Users.Age.Div(2).Lt(10),
				wantSQL:  `("users"."age" / $1) < $2`,
				wantArgs: []any{2, 10},
			},
			{
				name:     "CaseEq",
				pred:     schema.Case(Users.ID).When(schema.ValueExpr{Value: int64(1)}, schema.ValueExpr{Value: "one"}).Else(schema.ValueExpr{Value: "other"}).End().Eq("one"),
				wantSQL:  `CASE "users"."id" WHEN $1 THEN $2 ELSE $3 END = $4`,
				wantArgs: []any{int64(1), "one", "other", "one"},
			},
			{
				name:     "CoalesceEq",
				pred:     schema.Coalesce(Users.Name, schema.ValueExpr{Value: "N/A"}).Eq("Alice"),
				wantSQL:  `COALESCE("users"."name", $1) = $2`,
				wantArgs: []any{"N/A", "Alice"},
			},
			{
				name:     "RawEq",
				pred:     schema.Raw("LOWER(?)", Users.Name).Eq("alice"),
				wantSQL:  `LOWER("users"."name") = $1`,
				wantArgs: []any{"alice"},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				q := db.Select(schema.Count()).From(Users).GroupBy(Users.ID)
				if strings.Contains(tt.wantSQL, "COUNT") {
					q = q.Having(tt.pred)
				} else {
					q = q.Where(tt.pred)
				}

				sqlText, args, err := q.ToSQL()
				if err != nil {
					t.Fatalf("ToSQL() error = %v", err)
				}
				if !strings.Contains(sqlText, tt.wantSQL) {
					t.Errorf("SQL %q does not contain %q", sqlText, tt.wantSQL)
				}
				for _, arg := range tt.wantArgs {
					found := false
					for _, got := range args {
						if fmt.Sprintf("%v", got) == fmt.Sprintf("%v", arg) {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("arg %v not found in %v", arg, args)
					}
				}
			})
		}
	})

	t.Run("Ordering", func(t *testing.T) {
		tests := []struct {
			name    string
			order   schema.OrderExpr
			wantSQL string
		}{
			{
				name:    "StandaloneAsc",
				order:   schema.Asc(Users.Name),
				wantSQL: `"users"."name" ASC`,
			},
			{
				name:    "StandaloneDesc",
				order:   schema.Desc(Users.Name),
				wantSQL: `"users"."name" DESC`,
			},
			{
				name:    "AnyColumnAsc",
				order:   Users.C("name").Asc(),
				wantSQL: `"users"."name" ASC`,
			},
			{
				name:    "AnyColumnDesc",
				order:   Users.C("name").Desc(),
				wantSQL: `"users"."name" DESC`,
			},
			{
				name:    "BinaryAsc",
				order:   Users.Age.Add(1).Asc(),
				wantSQL: `("users"."age" + $1) ASC`,
			},
			{
				name:    "AggregateDesc",
				order:   schema.Count().Desc(),
				wantSQL: `COUNT(*) DESC`,
			},
			{
				name:    "CaseAsc",
				order:   schema.Case(Users.ID).When(schema.ValueExpr{Value: int64(1)}, schema.ValueExpr{Value: 1}).Else(schema.ValueExpr{Value: 2}).End().Asc(),
				wantSQL: `CASE "users"."id" WHEN $1 THEN $2 ELSE $3 END ASC`,
			},
			{
				name:    "CoalesceDesc",
				order:   schema.Coalesce(Users.Name, schema.ValueExpr{Value: ""}).Desc(),
				wantSQL: `COALESCE("users"."name", $1) DESC`,
			},
			{
				name:    "RawAsc",
				order:   schema.Raw("random()").Asc(),
				wantSQL: `random() ASC`,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				sqlText, _, err := db.Select().From(Users).OrderBy(tt.order).ToSQL()
				if err != nil {
					t.Fatalf("ToSQL() error = %v", err)
				}
				if !strings.Contains(sqlText, tt.wantSQL) {
					t.Errorf("SQL %q does not contain %q", sqlText, tt.wantSQL)
				}
			})
		}
	})

	t.Run("RawWithEmbeddedExpressions", func(t *testing.T) {
		tests := []struct {
			name     string
			expr     schema.Expression
			wantSQL  string
			wantArgs []any
		}{
			{
				name:     "ColumnInRaw",
				expr:     schema.Raw("LOWER(?)", Users.Name),
				wantSQL:  `LOWER("users"."name")`,
				wantArgs: nil,
			},
			{
				name:     "MixedRaw",
				expr:     schema.Raw("? || ' ' || ?", Users.Name, "Suffix"),
				wantSQL:  `"users"."name" || ' ' || $1`,
				wantArgs: []any{"Suffix"},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				sqlText, args, err := db.Select(tt.expr).From(Users).ToSQL()
				if err != nil {
					t.Fatalf("ToSQL() error = %v", err)
				}
				if !strings.Contains(sqlText, tt.wantSQL) {
					t.Errorf("SQL %q does not contain %q", sqlText, tt.wantSQL)
				}
				if !reflect.DeepEqual(args, tt.wantArgs) && (len(args) != 0 || len(tt.wantArgs) != 0) {
					t.Errorf("got args %v, want %v", args, tt.wantArgs)
				}
			})
		}
	})

	t.Run("DDLRawWithEmbeddedExpressions", func(t *testing.T) {
		// Verify that CreateIndexesSQL correctly renders embedded expressions in index WHERE clauses.
		UsersWithIndex := schema.Define("users_idx", func(t *UsersTable) {
			t.ID = t.BigSerial("id").PrimaryKey()
			t.Age = t.Integer("age").NotNull()
			t.Name = t.Text("name").NotNull()
			t.Active = t.Boolean("active").NotNull()

			t.Index("idx_active_adults").On(t.ID).Where(schema.Raw("? = TRUE AND ? > 18", t.Active, t.Age))
			t.Index("idx_complex_raw").On(t.ID).Where(schema.Raw("? > 0", t.Age.Add(1)))
		})

		sqls, err := db.CreateIndexesSQL(UsersWithIndex)
		if err != nil {
			t.Fatalf("CreateIndexesSQL() error = %v", err)
		}

		want1 := `CREATE INDEX "idx_active_adults" ON "users_idx" ("id" ASC) WHERE "active" = TRUE AND "age" > 18`
		want2 := `CREATE INDEX "idx_complex_raw" ON "users_idx" ("id" ASC) WHERE ("age" + 1) > 0`
		found1, found2 := false, false
		for _, sql := range sqls {
			if sql == want1 {
				found1 = true
			}
			if sql == want2 {
				found2 = true
			}
		}

		if !found1 {
			t.Errorf("expected index SQL not found: %q. Got: %v", want1, sqls)
		}
		if !found2 {
			t.Errorf("expected index SQL not found: %q. Got: %v", want2, sqls)
		}
	})
}
