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
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				// We use a manual compile context or just build a dummy query that uses these.
				// Chaining on AggregateExpr usually goes in HAVING.
				q := db.Select(schema.Count()).From(Users).GroupBy(Users.ID)
				if strings.Contains(tt.wantSQL, "COUNT") {
					q.Having(tt.pred)
				} else {
					q.Where(tt.pred)
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
		})

		sqls, err := db.CreateIndexesSQL(UsersWithIndex)
		if err != nil {
			t.Fatalf("CreateIndexesSQL() error = %v", err)
		}

		found := false
		want := `CREATE INDEX "idx_active_adults" ON "users_idx" ("id" ASC) WHERE "active" = TRUE AND "age" > 18`
		for _, sql := range sqls {
			if sql == want {
				found = true
				break
			}
		}

		if !found {
			t.Errorf("expected index SQL not found. Got: %v", sqls)
		}
	})
}
