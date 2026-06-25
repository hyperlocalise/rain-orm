package rain

import (
	"strings"
	"testing"

	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

func TestBinaryAndConcatExpressionsToSQL(t *testing.T) {
	type UsersTable struct {
		schema.TableModel
		ID    *schema.Column[int64]
		Age   *schema.Column[int32]
		Name  *schema.Column[string]
		Score *schema.Column[float64]
	}

	Users := schema.Define("users", func(t *UsersTable) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.Age = t.Integer("age").NotNull()
		t.Name = t.Text("name").NotNull()
		t.Score = t.Double("score").NotNull()
	})

	t.Run("Arithmetic", func(t *testing.T) {
		db := MustOpenDialect("postgres")

		tests := []struct {
			name     string
			expr     schema.Expression
			wantSQL  string
			wantArgs []any
		}{
			{
				name:     "Add",
				expr:     Users.Age.Add(int32(10)),
				wantSQL:  `("users"."age" + $1)`,
				wantArgs: []any{int32(10)},
			},
			{
				name:     "Sub",
				expr:     Users.Age.Sub(int32(5)),
				wantSQL:  `("users"."age" - $1)`,
				wantArgs: []any{int32(5)},
			},
			{
				name:     "Mul",
				expr:     Users.Score.Mul(1.5),
				wantSQL:  `("users"."score" * $1)`,
				wantArgs: []any{1.5},
			},
			{
				name:     "Div",
				expr:     Users.Score.Div(2.0),
				wantSQL:  `("users"."score" / $1)`,
				wantArgs: []any{2.0},
			},
			{
				name:     "Mod",
				expr:     Users.Age.Mod(int32(2)),
				wantSQL:  `("users"."age" % $1)`,
				wantArgs: []any{int32(2)},
			},
			{
				name:     "NestedArithmetic",
				expr:     Users.Age.Add(int32(10)).Mul(int32(2)),
				wantSQL:  `(("users"."age" + $1) * $2)`,
				wantArgs: []any{int32(10), int32(2)},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				query := db.Select(tt.expr).From(Users)
				gotSQL, gotArgs, err := query.ToSQL()
				if err != nil {
					t.Fatalf("ToSQL() error = %v", err)
				}
				expectedSQL := "SELECT " + tt.wantSQL + " FROM \"users\""
				if gotSQL != expectedSQL {
					t.Errorf("got SQL %q, want %q", gotSQL, expectedSQL)
				}
				if len(gotArgs) != len(tt.wantArgs) {
					t.Errorf("got %d args, want %d", len(gotArgs), len(tt.wantArgs))
				}
				for i := range gotArgs {
					if gotArgs[i] != tt.wantArgs[i] {
						t.Errorf("arg %d: got %v, want %v", i, gotArgs[i], tt.wantArgs[i])
					}
				}
			})
		}
	})

	t.Run("Concat", func(t *testing.T) {
		postgres := MustOpenDialect("postgres")
		mysql := MustOpenDialect("mysql")

		tests := []struct {
			name    string
			db      *DB
			expr    schema.Expression
			wantSQL string
		}{
			{
				name:    "PostgresConcat",
				db:      postgres,
				expr:    schema.Concat(Users.Name, " (", Users.Age, ")"),
				wantSQL: `SELECT ("users"."name" || $1 || "users"."age" || $2) FROM "users"`,
			},
			{
				name:    "MySQLConcat",
				db:      mysql,
				expr:    schema.Concat(Users.Name, " (", Users.Age, ")"),
				wantSQL: "SELECT CONCAT(`users`.`name`, ?, `users`.`age`, ?) FROM `users`",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				query := tt.db.Select(tt.expr).From(Users)
				gotSQL, _, err := query.ToSQL()
				if err != nil {
					t.Fatalf("ToSQL() error = %v", err)
				}
				if gotSQL != tt.wantSQL {
					t.Errorf("got SQL %q, want %q", gotSQL, tt.wantSQL)
				}
			})
		}
	})
}

func TestFluentAndStandaloneExpressionsToSQL(t *testing.T) {
	type UsersTable struct {
		schema.TableModel
		ID   *schema.Column[int64]
		Name *schema.Column[string]
	}

	Users := schema.Define("users", func(t *UsersTable) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.Name = t.Text("name").NotNull()
	})

	db := MustOpenDialect("postgres")

	t.Run("Standalone functions", func(t *testing.T) {
		tests := []struct {
			name    string
			expr    schema.Predicate
			wantSQL string
		}{
			{"Eq", schema.Eq(Users.ID, 1), `"users"."id" = $1`},
			{"Ne", schema.Ne(Users.ID, 1), `"users"."id" <> $1`},
			{"Gt", schema.Gt(Users.ID, 1), `"users"."id" > $1`},
			{"Gte", schema.Gte(Users.ID, 1), `"users"."id" >= $1`},
			{"Lt", schema.Lt(Users.ID, 1), `"users"."id" < $1`},
			{"Lte", schema.Lte(Users.ID, 1), `"users"."id" <= $1`},
			{"Like", schema.Like(Users.Name, "A%"), `"users"."name" LIKE $1`},
			{"In", schema.In(Users.ID, 1, 2, 3), `"users"."id" IN ($1, $2, $3)`},
			{"Between", schema.Between(Users.ID, 1, 10), `"users"."id" BETWEEN $1 AND $2`},
			{"IsNull", schema.IsNull(Users.ID), `"users"."id" IS NULL`},
			{"IsNotNull", schema.IsNotNull(Users.ID), `"users"."id" IS NOT NULL`},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				sql, _, err := db.Select().From(Users).Where(tt.expr).ToSQL()
				if err != nil {
					t.Fatalf("ToSQL failed: %v", err)
				}
				if !strings.Contains(sql, "WHERE "+tt.wantSQL) {
					t.Fatalf("expected SQL to contain %q, got %q", tt.wantSQL, sql)
				}
			})
		}
	})

	t.Run("Fluent methods", func(t *testing.T) {
		tests := []struct {
			name    string
			expr    any
			wantSQL string
		}{
			{"BinaryExpr.Gt", Users.ID.Add(1).Gt(10), `("users"."id" + $1) > $2`},
			{"AggregateExpr.Lt", schema.Count(Users.ID).Lt(5), `COUNT("users"."id") < $1`},
			{"CaseExpr.Eq", schema.Case().When(Users.ID.Eq(int64(1)), "one").End().Eq("one"), `(CASE WHEN "users"."id" = $1 THEN $2 END) = $3`},
			{"CoalesceExpr.IsNotNull", schema.Coalesce(Users.Name, "N/A").IsNotNull(), `COALESCE("users"."name", $1) IS NOT NULL`},
			{"SQL.Asc", schema.SQL("random()").Asc(), `ORDER BY random() ASC`},
			{"SQL.Embedded", schema.SQL("LOWER(?)", Users.Name).Eq("alice"), `LOWER("users"."name") = $1`},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				var sql string
				var err error
				if strings.HasSuffix(tt.name, ".Asc") {
					sql, _, err = db.Select().From(Users).OrderBy(tt.expr.(schema.OrderExpr)).ToSQL()
				} else {
					sql, _, err = db.Select().From(Users).Where(tt.expr.(schema.Predicate)).ToSQL()
				}

				if err != nil {
					t.Fatalf("ToSQL failed: %v", err)
				}
				if !strings.Contains(sql, tt.wantSQL) {
					t.Fatalf("expected SQL to contain %q, got %q", tt.wantSQL, sql)
				}
			})
		}
	})
}
