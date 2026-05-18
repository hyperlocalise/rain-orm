package rain_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/hyperlocalise/rain-orm/pkg/rain"
	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

func TestInsertModelAndSetMergeToSQL(t *testing.T) {
	t.Parallel()

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

func TestInsertIncludesDefaultBackedZeroValues(t *testing.T) {
	t.Parallel()

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
		t.Fatalf("insert default inclusion ToSQL returned error: %v", err)
	}

	wantSQL := `INSERT INTO "users" ("email", "name", "active") VALUES ($1, $2, $3)`
	if sqlText != wantSQL {
		t.Fatalf("unexpected default-including insert SQL:\nwant: %s\ngot:  %s", wantSQL, sqlText)
	}
	if len(args) != 3 || args[0] != "alice@example.com" || args[1] != "" || args[2] != false {
		t.Fatalf("unexpected default-including insert args: %#v", args)
	}
}

func TestInsertMultiRowModelsToSQL(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

	db, err := rain.OpenDialect("postgres")
	if err != nil {
		t.Fatalf("OpenDialect returned error: %v", err)
	}
	users, _ := defineTables()

	type partialUserModel struct {
		Email  string         `db:"email"`
		Name   string         `db:"name"`
		Active rain.Set[bool] `db:"active"`
	}

	_, _, err = db.Insert().
		Table(users).
		Models([]partialUserModel{
			{Email: "alice@example.com", Name: "Alice", Active: rain.Set[bool]{Value: true, Valid: true}},
			{Email: "bob@example.com", Name: ""},
		}).
		ToSQL()
	if err == nil || !strings.Contains(err.Error(), "targets 2 columns, expected 3") {
		t.Fatalf("expected column mismatch error, got %v", err)
	}
}

func TestInsertOnConflictPostgres(t *testing.T) {
	t.Parallel()

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

func TestInsertSelectToSQL(t *testing.T) {
	t.Parallel()

	users, posts := defineTables()

	t.Run("postgres basic select", func(t *testing.T) {
		db, _ := rain.OpenDialect("postgres")
		subquery := db.Select().
			Table(users).
			Column(users.ID, users.Email).
			Where(users.Active.Eq(true))

		sqlText, args, err := db.Insert().
			Table(posts).
			Columns(posts.UserID, posts.Title).
			Select(subquery).
			ToSQL()
		if err != nil {
			t.Fatalf("ToSQL returned error: %v", err)
		}

		wantSQL := `INSERT INTO "posts" ("user_id", "title") SELECT "users"."id", "users"."email" FROM "users" WHERE "users"."active" = $1`
		if sqlText != wantSQL {
			t.Fatalf("unexpected SQL:\nwant: %s\ngot:  %s", wantSQL, sqlText)
		}
		if len(args) != 1 || args[0] != true {
			t.Fatalf("unexpected args: %#v", args)
		}
	})

	t.Run("sqlite with conflict and returning", func(t *testing.T) {
		db, _ := rain.OpenDialect("sqlite")
		subquery := db.Select().
			Table(users).
			Column(users.ID, users.Name).
			Where(users.Active.Eq(true))

		sqlText, args, err := db.Insert().
			Table(users).
			Columns(users.ID, users.Name).
			Select(subquery).
			OnConflict(users.ID).
			DoNothing().
			Returning(users.ID).
			ToSQL()
		if err != nil {
			t.Fatalf("ToSQL returned error: %v", err)
		}

		wantSQL := `INSERT INTO "users" ("id", "name") SELECT "users"."id", "users"."name" FROM "users" WHERE "users"."active" = ? ON CONFLICT ("id") DO NOTHING RETURNING "users"."id"`
		if sqlText != wantSQL {
			t.Fatalf("unexpected SQL:\nwant: %s\ngot:  %s", wantSQL, sqlText)
		}
		if len(args) != 1 || args[0] != true {
			t.Fatalf("unexpected args: %#v", args)
		}
	})

	t.Run("mysql select", func(t *testing.T) {
		db, _ := rain.OpenDialect("mysql")
		subquery := db.Select().
			Table(users).
			Column(users.ID, users.Name).
			Where(users.Active.Eq(true))

		sqlText, args, err := db.Insert().
			Table(users).
			Columns(users.ID, users.Name).
			Select(subquery).
			ToSQL()
		if err != nil {
			t.Fatalf("ToSQL returned error: %v", err)
		}

		wantSQL := "INSERT INTO `users` (`id`, `name`) SELECT `users`.`id`, `users`.`name` FROM `users` WHERE `users`.`active` = ?"
		if sqlText != wantSQL {
			t.Fatalf("unexpected SQL:\nwant: %s\ngot:  %s", wantSQL, sqlText)
		}
		if len(args) != 1 || args[0] != true {
			t.Fatalf("unexpected args: %#v", args)
		}
	})

	t.Run("error when multiple sources provided", func(t *testing.T) {
		db, _ := rain.OpenDialect("postgres")
		subquery := db.Select().Table(users)

		_, _, err := db.Insert().
			Table(posts).
			Model(&userModel{Email: "alice@example.com"}).
			Select(subquery).
			ToSQL()

		if err == nil || !strings.Contains(err.Error(), "requires exactly one data source") {
			t.Fatalf("expected multiple source error, got %v", err)
		}
	})
}

func TestInsertOnConflictSQLite(t *testing.T) {
	t.Parallel()

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

func TestInsertOnConflictMySQL(t *testing.T) {
	t.Parallel()

	db, err := rain.OpenDialect("mysql")
	if err != nil {
		t.Fatalf("OpenDialect returned error: %v", err)
	}
	users, _ := defineTables()

	t.Run("do nothing (no-op update)", func(t *testing.T) {
		sqlText, args, err := db.Insert().
			Table(users).
			Set(users.Email, "alice@example.com").
			Set(users.Name, "Alice").
			OnConflict().
			DoNothing().
			ToSQL()
		if err != nil {
			t.Fatalf("insert on conflict mysql do nothing ToSQL returned error: %v", err)
		}

		wantSQL := "INSERT INTO `users` (`email`, `name`) VALUES (?, ?) ON DUPLICATE KEY UPDATE `id` = `id`"
		if sqlText != wantSQL {
			t.Fatalf("unexpected mysql do nothing SQL:\nwant: %s\ngot:  %s", wantSQL, sqlText)
		}
		if len(args) != 2 {
			t.Fatalf("unexpected mysql do nothing args: %#v", args)
		}
	})

	t.Run("target columns are rejected for do nothing", func(t *testing.T) {
		_, _, err := db.Insert().
			Table(users).
			Set(users.Email, "alice@example.com").
			Set(users.Name, "Alice").
			OnConflict(users.Email).
			DoNothing().
			ToSQL()
		if err == nil || !strings.Contains(err.Error(), "cannot target specific conflict columns") {
			t.Fatalf("expected mysql conflict target error, got %v", err)
		}
	})

	t.Run("target columns are rejected for do update set", func(t *testing.T) {
		_, _, err := db.Insert().
			Table(users).
			Set(users.Email, "alice@example.com").
			Set(users.Name, "Alice").
			OnConflict(users.Email).
			DoUpdateSet(users.Name).
			ToSQL()
		if err == nil || !strings.Contains(err.Error(), "cannot target specific conflict columns") {
			t.Fatalf("expected mysql conflict target error, got %v", err)
		}
	})

	t.Run("do update set (on duplicate key update)", func(t *testing.T) {
		sqlText, args, err := db.Insert().
			Table(users).
			Set(users.Email, "alice@example.com").
			Set(users.Name, "Alice").
			Set(users.Active, true).
			OnConflict().
			DoUpdateSet(users.Name, users.Active).
			ToSQL()
		if err != nil {
			t.Fatalf("insert on conflict mysql do update ToSQL returned error: %v", err)
		}

		wantSQL := "INSERT INTO `users` (`email`, `name`, `active`) VALUES (?, ?, ?) ON DUPLICATE KEY UPDATE `name` = VALUES(`name`), `active` = VALUES(`active`)"
		if sqlText != wantSQL {
			t.Fatalf("unexpected mysql do update SQL:\nwant: %s\ngot:  %s", wantSQL, sqlText)
		}
		if len(args) != 3 {
			t.Fatalf("unexpected mysql do update args: %#v", args)
		}
	})
}
