package rain_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/hyperlocalise/rain-orm/pkg/rain"
	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

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

func TestInsertMultiRowModelsToSQL(t *testing.T) {
	db, err := rain.OpenDialect("postgres")
	if err != nil {
		t.Fatalf("OpenDialect returned error: %v", err)
	}
	users, _ := defineTables()

	sqlText, args, err := db.Insert().
		Table(users).
		Models([]userModel{
			{Email: "alice@example.com", Name: "Alice", Active: true},
			{Email: "bob@example.com", Name: "Bob", Active: true},
		}).
		Returning(users.ID).
		ToSQL()
	if err != nil {
		t.Fatalf("insert multi model ToSQL returned error: %v", err)
	}

	wantSQL := `INSERT INTO "users" ("email", "name", "active") VALUES ($1, $2, $3), ($4, $5, $6) RETURNING "users"."id"`
	if sqlText != wantSQL {
		t.Fatalf("unexpected multi model insert SQL:\nwant: %s\ngot:  %s", wantSQL, sqlText)
	}
	wantArgs := []any{"alice@example.com", "Alice", true, "bob@example.com", "Bob", true}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("unexpected multi model insert args: %#v", args)
	}
}

func TestInsertMultiRowValuesToSQL(t *testing.T) {
	db, err := rain.OpenDialect("postgres")
	if err != nil {
		t.Fatalf("OpenDialect returned error: %v", err)
	}
	users, _ := defineTables()

	sqlText, args, err := db.Insert().
		Table(users).
		Values(
			map[schema.ColumnReference]any{users.Email: "alice@example.com", users.Name: "Alice", users.Active: true},
			map[schema.ColumnReference]any{users.Email: "bob@example.com", users.Name: "Bob", users.Active: false},
		).
		ToSQL()
	if err != nil {
		t.Fatalf("insert multi values ToSQL returned error: %v", err)
	}

	wantSQL := `INSERT INTO "users" ("email", "name", "active") VALUES ($1, $2, $3), ($4, $5, $6)`
	if sqlText != wantSQL {
		t.Fatalf("unexpected multi values insert SQL:\nwant: %s\ngot:  %s", wantSQL, sqlText)
	}
	wantArgs := []any{"alice@example.com", "Alice", true, "bob@example.com", "Bob", false}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("unexpected multi values insert args: %#v", args)
	}
}

func TestInsertMultiRowColumnMismatchReturnsError(t *testing.T) {
	db, err := rain.OpenDialect("postgres")
	if err != nil {
		t.Fatalf("OpenDialect returned error: %v", err)
	}
	users, _ := defineTables()

	_, _, err = db.Insert().
		Table(users).
		Models([]userModel{
			{Email: "alice@example.com", Name: "Alice", Active: true},
			{Email: "bob@example.com", Name: "", Active: false},
		}).
		ToSQL()
	if err == nil || !strings.Contains(err.Error(), "targets 2 columns, expected 3") {
		t.Fatalf("expected column mismatch error, got %v", err)
	}
}

func TestInsertOnConflictPostgres(t *testing.T) {
	db, err := rain.OpenDialect("postgres")
	if err != nil {
		t.Fatalf("OpenDialect returned error: %v", err)
	}
	users, _ := defineTables()

	t.Run("do nothing", func(t *testing.T) {
		sqlText, args, err := db.Insert().
			Table(users).
			Set(users.Email, "alice@example.com").
			Set(users.Name, "Alice").
			OnConflict(users.Email).
			DoNothing().
			ToSQL()
		if err != nil {
			t.Fatalf("insert on conflict do nothing ToSQL returned error: %v", err)
		}

		wantSQL := `INSERT INTO "users" ("email", "name") VALUES ($1, $2) ON CONFLICT ("email") DO NOTHING`
		if sqlText != wantSQL {
			t.Fatalf("unexpected do nothing SQL:\nwant: %s\ngot:  %s", wantSQL, sqlText)
		}
		if len(args) != 2 {
			t.Fatalf("unexpected do nothing args: %#v", args)
		}
	})

	t.Run("do update set", func(t *testing.T) {
		sqlText, args, err := db.Insert().
			Table(users).
			Set(users.Email, "alice@example.com").
			Set(users.Name, "Alice").
			Set(users.Active, true).
			OnConflict(users.Email).
			DoUpdateSet(users.Name, users.Active).
			ToSQL()
		if err != nil {
			t.Fatalf("insert on conflict do update ToSQL returned error: %v", err)
		}

		wantSQL := `INSERT INTO "users" ("email", "name", "active") VALUES ($1, $2, $3) ON CONFLICT ("email") DO UPDATE SET "name" = EXCLUDED."name", "active" = EXCLUDED."active"`
		if sqlText != wantSQL {
			t.Fatalf("unexpected do update SQL:\nwant: %s\ngot:  %s", wantSQL, sqlText)
		}
		if len(args) != 3 {
			t.Fatalf("unexpected do update args: %#v", args)
		}
	})
}

func TestInsertOnConflictSQLite(t *testing.T) {
	db, err := rain.OpenDialect("sqlite")
	if err != nil {
		t.Fatalf("OpenDialect returned error: %v", err)
	}
	users, _ := defineTables()

	sqlText, args, err := db.Insert().
		Table(users).
		Set(users.Email, "alice@example.com").
		Set(users.Name, "Alice").
		Set(users.Active, true).
		OnConflict(users.Email).
		DoUpdateSet(users.Name, users.Active).
		ToSQL()
	if err != nil {
		t.Fatalf("insert on conflict sqlite ToSQL returned error: %v", err)
	}

	wantSQL := `INSERT INTO "users" ("email", "name", "active") VALUES (?, ?, ?) ON CONFLICT ("email") DO UPDATE SET "name" = EXCLUDED."name", "active" = EXCLUDED."active"`
	if sqlText != wantSQL {
		t.Fatalf("unexpected sqlite do update SQL:\nwant: %s\ngot:  %s", wantSQL, sqlText)
	}
	wantArgs := []any{"alice@example.com", "Alice", true}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("unexpected sqlite do update args: %#v", args)
	}
}

func TestInsertOnConflictUnsupportedDialectReturnsError(t *testing.T) {
	db, err := rain.OpenDialect("mysql")
	if err != nil {
		t.Fatalf("OpenDialect returned error: %v", err)
	}
	users, _ := defineTables()

	_, _, err = db.Insert().
		Table(users).
		Set(users.Email, "alice@example.com").
		Set(users.Name, "Alice").
		OnConflict(users.Email).
		DoUpdateSet(users.Name).
		ToSQL()
	if err == nil || !strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("expected unsupported dialect error, got %v", err)
	}
}
