package rain_test

import (
	"testing"

	"github.com/hyperlocalise/rain-orm/pkg/rain"
	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

func TestPreparedToSQLReturnsErrorWithPlaceholders(t *testing.T) {
	t.Parallel()

	db, err := rain.OpenDialect("postgres")
	if err != nil {
		t.Fatalf("OpenDialect returned error: %v", err)
	}
	users, _ := defineTables()

	t.Run("insert", func(t *testing.T) {
		_, _, err := db.Insert().
			Table(users).
			Set(users.Email, schema.Placeholder("email")).
			ToSQL()
		if err != rain.ErrPreparedArgsRequired {
			t.Errorf("expected ErrPreparedArgsRequired, got %v", err)
		}
	})

	t.Run("update", func(t *testing.T) {
		_, _, err := db.Update().
			Table(users).
			Set(users.Name, schema.Placeholder("name")).
			Where(users.ID.Eq(int64(1))).
			ToSQL()
		if err != rain.ErrPreparedArgsRequired {
			t.Errorf("expected ErrPreparedArgsRequired, got %v", err)
		}
	})

	t.Run("delete", func(t *testing.T) {
		_, _, err := db.Delete().
			Table(users).
			Where(users.ID.EqExpr(schema.Placeholder("id"))).
			ToSQL()
		if err != rain.ErrPreparedArgsRequired {
			t.Errorf("expected ErrPreparedArgsRequired, got %v", err)
		}
	})
}
