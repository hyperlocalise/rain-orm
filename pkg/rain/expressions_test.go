package rain

import (
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
