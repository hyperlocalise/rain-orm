package rain_test

import (
	"testing"

	"github.com/hyperlocalise/rain-orm/pkg/rain"
	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

func TestTablelessSelect(t *testing.T) {
	db, _ := rain.OpenDialect("postgres")

	cases := []struct {
		name string
		q    *rain.SelectQuery
		want string
	}{
		{
			name: "simple select 1",
			q:    db.Select(schema.Raw("1")),
			want: "SELECT 1",
		},
		{
			name: "select multiple constants",
			q:    db.Select(schema.Raw("1"), schema.Raw("2"), schema.Raw("'foo'")),
			want: "SELECT 1, 2, 'foo'",
		},
		{
			name: "select with where (though unusual)",
			q:    db.Select(schema.Raw("1")).Where(schema.Raw("1 = 1")),
			want: "SELECT 1 WHERE 1 = 1",
		},
		{
			name: "select with limit",
			q:    db.Select(schema.Raw("1")).Limit(1),
			want: "SELECT 1 LIMIT 1",
		},
		{
			name: "select with alias",
			q:    db.Select(schema.As(schema.Raw("1"), "one")),
			want: `SELECT 1 AS "one"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sql, _, err := tc.q.ToSQL()
			if err != nil {
				t.Fatalf("ToSQL failed: %v", err)
			}
			if sql != tc.want {
				t.Errorf("got %q, want %q", sql, tc.want)
			}
		})
	}
}

func TestSelectEmptyFails(t *testing.T) {
	db, _ := rain.OpenDialect("postgres")
	_, _, err := db.Select().ToSQL()
	if err == nil {
		t.Fatal("expected error for empty SELECT without table, got nil")
	}
	want := "rain: select query requires a table"
	if err.Error() != want {
		t.Errorf("got error %q, want %q", err.Error(), want)
	}
}
