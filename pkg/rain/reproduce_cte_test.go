package rain_test

import (
	"strings"
	"testing"

	"github.com/hyperlocalise/rain-orm/pkg/rain"
	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

func TestInsertSelectWithCTEToSQL(t *testing.T) {
	t.Parallel()

	users, posts := defineTables()
	db, _ := rain.OpenDialect("postgres")

	subquery := db.Select().
		With("active_ids", db.Select().Table(users).Column(users.ID).Where(users.Active.Eq(true))).
		Table(users).
		Column(users.ID, schema.Raw("'Migrated'")).
        Join(schema.Alias(users, "active_ids"), users.ID.EqCol(schema.Alias(users, "active_ids").ID))

	sqlText, _, err := db.Insert().
		Table(posts).
		Columns(posts.UserID, posts.Title).
		Select(subquery).
		ToSQL()

	if err != nil {
		t.Fatalf("ToSQL returned error: %v", err)
	}

	// Correct SQL should have WITH at the beginning
	if !strings.HasPrefix(sqlText, "WITH") {
		t.Errorf("Expected SQL to start with WITH, got: %s", sqlText)
	}
}
