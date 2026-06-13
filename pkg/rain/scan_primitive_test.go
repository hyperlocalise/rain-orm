package rain_test

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/hyperlocalise/rain-orm/pkg/rain"
	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

func TestScanPrimitive(t *testing.T) {
	ctx := context.Background()
	db, err := rain.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	type UsersTable struct {
		schema.TableModel
		ID    *schema.Column[int64]
		Name  *schema.Column[string]
		Score *schema.Column[int64]
	}
	Users := schema.Define("users", func(t *UsersTable) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.Name = t.Text("name").NotNull()
		t.Score = t.BigInt("score").NotNull().Default(0)
	})

	createSQL, _ := db.CreateTableSQL(Users)
	if _, err := db.Exec(ctx, createSQL); err != nil {
		t.Fatal(err)
	}

	// Test SELECT single primitive
	var count int64
	err = db.Select(schema.Count()).From(Users).Scan(ctx, &count)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if count != 0 {
		t.Errorf("expected count 0, got %d", count)
	}

	// Test INSERT ... RETURNING primitive
	var id int64
	err = db.Insert().Table(Users).Set(Users.Name, "Alice").Returning(Users.ID).Scan(ctx, &id)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if id != 1 {
		t.Errorf("expected id 1, got %d", id)
	}

	// Test SELECT slice of primitives
	_, _ = db.Insert().Table(Users).Set(Users.Name, "Bob").Exec(ctx)
	_, _ = db.Insert().Table(Users).Set(Users.Name, "Charlie").Exec(ctx)

	var names []string
	err = db.Select(Users.Name).From(Users).OrderBy(Users.ID.Asc()).Scan(ctx, &names)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if len(names) != 3 {
		t.Errorf("expected 3 names, got %d", len(names))
	}
	if names[0] != "Alice" || names[1] != "Bob" || names[2] != "Charlie" {
		t.Errorf("unexpected names: %v", names)
	}

	// Test UPDATE ... RETURNING primitive
	var newScore int64
	err = db.Update().Table(Users).Set(Users.Score, int64(100)).Where(Users.ID.Eq(id)).Returning(Users.Score).Scan(ctx, &newScore)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if newScore != 100 {
		t.Errorf("expected score 100, got %d", newScore)
	}

	// Test DELETE ... RETURNING primitive
	var deletedName string
	err = db.Delete().Table(Users).Where(Users.ID.Eq(id)).Returning(Users.Name).Scan(ctx, &deletedName)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if deletedName != "Alice" {
		t.Errorf("expected deleted name Alice, got %s", deletedName)
	}

	// Test ErrNoRows
	var dummy int64
	err = db.Select(Users.ID).From(Users).Where(Users.ID.Eq(int64(999))).Scan(ctx, &dummy)
	if err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}

	// Test caching
	cache := rain.NewMemoryQueryCache()
	db.WithQueryCache(cache)

	var cachedCount int64
	err = db.Select(schema.Count()).From(Users).Cache(rain.QueryCacheOptions{TTL: 1000000000}).Scan(ctx, &cachedCount)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if cachedCount != 2 {
		t.Errorf("expected count 2, got %d", cachedCount)
	}

	var cachedCount2 int64
	err = db.Select(schema.Count()).From(Users).Cache(rain.QueryCacheOptions{TTL: 1000000000}).Scan(ctx, &cachedCount2)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if cachedCount2 != 2 {
		t.Errorf("expected count 2, got %d", cachedCount2)
	}
}
