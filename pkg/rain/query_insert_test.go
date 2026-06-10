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

	t.Run("on constraint", func(t *testing.T) {
		sqlText, _, err := db.Insert().
			Table(users).
			Set(users.Email, "alice@example.com").
			Set(users.Name, "Alice").
			OnConflict().
			OnConstraint("users_email_key").
			DoNothing().
			ToSQL()
		if err != nil {
			t.Fatalf("ToSQL returned error: %v", err)
		}

		wantSQL := `INSERT INTO "users" ("email", "name") VALUES ($1, $2) ON CONFLICT ON CONSTRAINT "users_email_key" DO NOTHING`
		if sqlText != wantSQL {
			t.Fatalf("unexpected SQL:\nwant: %s\ngot:  %s", wantSQL, sqlText)
		}
	})

	t.Run("conflict target where", func(t *testing.T) {
		sqlText, args, err := db.Insert().
			Table(users).
			Set(users.Email, "alice@example.com").
			Set(users.Name, "Alice").
			OnConflict(users.Email).
			Where(users.Active.Eq(true)).
			DoNothing().
			ToSQL()
		if err != nil {
			t.Fatalf("ToSQL returned error: %v", err)
		}

		wantSQL := `INSERT INTO "users" ("email", "name") VALUES ($1, $2) ON CONFLICT ("email") WHERE "users"."active" = $3 DO NOTHING`
		if sqlText != wantSQL {
			t.Fatalf("unexpected SQL:\nwant: %s\ngot:  %s", wantSQL, sqlText)
		}
		if len(args) != 3 || args[2] != true {
			t.Fatalf("unexpected args: %#v", args)
		}
	})

	t.Run("custom do update set and update where", func(t *testing.T) {
		sqlText, args, err := db.Insert().
			Table(users).
			Set(users.Email, "alice@example.com").
			Set(users.Name, "Alice").
			OnConflict(users.Email).
			DoUpdateSet().
			Set(users.Name, rain.Excluded(users.Name)).
			Set(users.Active, true).
			Where(users.Active.Eq(false)).
			ToSQL()
		if err != nil {
			t.Fatalf("ToSQL returned error: %v", err)
		}

		wantSQL := `INSERT INTO "users" ("email", "name") VALUES ($1, $2) ON CONFLICT ("email") DO UPDATE SET "name" = EXCLUDED."name", "active" = $3 WHERE "users"."active" = $4`
		if sqlText != wantSQL {
			t.Fatalf("unexpected SQL:\nwant: %s\ngot:  %s", wantSQL, sqlText)
		}
		if len(args) != 4 || args[2] != true || args[3] != false {
			t.Fatalf("unexpected args: %#v", args)
		}
	})
}

func TestInsertWithCTEToSQL(t *testing.T) {
	t.Parallel()

	users, _ := defineTables()

	t.Run("insert values with cte", func(t *testing.T) {
		db, _ := rain.OpenDialect("postgres")
		cte := db.Select().Table(users).Where(users.Active.Eq(true))

		sqlText, args, err := db.Insert().
			With("active_users", cte).
			Table(users).
			Set(users.Email, "new@example.com").
			Set(users.Name, "New User").
			ToSQL()
		if err != nil {
			t.Fatalf("ToSQL returned error: %v", err)
		}

		wantSQL := `WITH "active_users" AS (SELECT * FROM "users" WHERE "users"."active" = $1) INSERT INTO "users" ("email", "name") VALUES ($2, $3)`
		if sqlText != wantSQL {
			t.Fatalf("unexpected SQL:\nwant: %s\ngot:  %s", wantSQL, sqlText)
		}
		if len(args) != 3 || args[0] != true || args[1] != "new@example.com" || args[2] != "New User" {
			t.Fatalf("unexpected args: %#v", args)
		}
	})

	t.Run("insert select with cte", func(t *testing.T) {
		db, _ := rain.OpenDialect("postgres")
		// The CTE itself has a predicate (arg $1)
		cte := db.Select().Table(users).Where(users.Active.Eq(true))

		// The SELECT query uses a CTE from its own level, which should be suppressed
		// when passed to INSERT to avoid double WITH.
		innerSelect := db.Select().
			With("active_users", cte).
			TableSubquery(db.Select().Table(users), "active_users").
			Column(schema.Raw("*")).
			Where(schema.Raw("1=?", 1)) // arg $2

		sqlText, args, err := db.Insert().
			With("active_users", cte).
			Table(users).
			Columns(users.Email, users.Name).
			Select(innerSelect).
			ToSQL()
		if err != nil {
			t.Fatalf("ToSQL returned error: %v", err)
		}

		// Verify WITH appears once at the top, and the inner SELECT does not repeat it.
		wantSQL := `WITH "active_users" AS (SELECT * FROM "users" WHERE "users"."active" = $1) INSERT INTO "users" ("email", "name") SELECT * FROM (SELECT * FROM "users") AS "active_users" WHERE 1=$2`
		if sqlText != wantSQL {
			t.Fatalf("unexpected SQL:\nwant: %s\ngot:  %s", wantSQL, sqlText)
		}
		if len(args) != 2 || args[0] != true || args[1] != 1 {
			t.Fatalf("unexpected args: %#v", args)
		}
	})

	t.Run("upsert with cte", func(t *testing.T) {
		db, _ := rain.OpenDialect("postgres")
		cte := db.Select().Table(users).Where(users.Active.Eq(true))

		sqlText, args, err := db.Insert().
			With("active_users", cte).
			Table(users).
			Set(users.Email, "alice@example.com").
			OnConflict(users.Email).
			DoUpdateSet(users.Name).
			ToSQL()
		if err != nil {
			t.Fatalf("ToSQL returned error: %v", err)
		}

		wantSQL := `WITH "active_users" AS (SELECT * FROM "users" WHERE "users"."active" = $1) INSERT INTO "users" ("email") VALUES ($2) ON CONFLICT ("email") DO UPDATE SET "name" = EXCLUDED."name"`
		if sqlText != wantSQL {
			t.Fatalf("unexpected SQL:\nwant: %s\ngot:  %s", wantSQL, sqlText)
		}
		if len(args) != 2 || args[0] != true || args[1] != "alice@example.com" {
			t.Fatalf("unexpected args: %#v", args)
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

	t.Run("sqlite conflict without select where adds parser guard", func(t *testing.T) {
		db, _ := rain.OpenDialect("sqlite")
		subquery := db.Select().
			Table(users).
			Column(users.ID, users.Name)

		sqlText, args, err := db.Insert().
			Table(users).
			Columns(users.ID, users.Name).
			Select(subquery).
			OnConflict(users.ID).
			DoNothing().
			ToSQL()
		if err != nil {
			t.Fatalf("ToSQL returned error: %v", err)
		}

		wantSQL := `INSERT INTO "users" ("id", "name") SELECT "users"."id", "users"."name" FROM "users" WHERE 1 = 1 ON CONFLICT ("id") DO NOTHING`
		if sqlText != wantSQL {
			t.Fatalf("unexpected SQL:\nwant: %s\ngot:  %s", wantSQL, sqlText)
		}
		if len(args) != 0 {
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

	t.Run("mysql select with do update set (on duplicate key update)", func(t *testing.T) {
		db, _ := rain.OpenDialect("mysql")
		subquery := db.Select().
			Table(users).
			Column(users.ID, users.Name).
			Where(users.Active.Eq(true))

		sqlText, args, err := db.Insert().
			Table(users).
			Columns(users.ID, users.Name).
			Select(subquery).
			OnConflict().
			DoUpdateSet(users.Name).
			ToSQL()

		if err != nil {
			t.Fatalf("ToSQL returned error: %v", err)
		}

		wantSQL := "INSERT INTO `users` (`id`, `name`) SELECT `users`.`id`, `users`.`name` FROM `users` WHERE `users`.`active` = ? ON DUPLICATE KEY UPDATE `name` = VALUES(`name`)"
		if sqlText != wantSQL {
			t.Fatalf("unexpected SQL:\nwant: %s\ngot:  %s", wantSQL, sqlText)
		}
		if len(args) != 1 || args[0] != true {
			t.Fatalf("unexpected args: %#v", args)
		}
	})

	t.Run("target columns must belong to insert table", func(t *testing.T) {
		db, _ := rain.OpenDialect("postgres")
		subquery := db.Select().
			Table(users).
			Column(users.ID)

		_, _, err := db.Insert().
			Table(users).
			Columns(posts.UserID).
			Select(subquery).
			ToSQL()

		if err == nil || !strings.Contains(err.Error(), "belongs to table posts, not users") {
			t.Fatalf("expected insert-select target table error, got %v", err)
		}
	})

	t.Run("generated target columns are rejected", func(t *testing.T) {
		db, _ := rain.OpenDialect("postgres")
		generatedUsers := defineGeneratedTable()
		subquery := db.Select().
			Table(generatedUsers).
			Column(generatedUsers.FullName)

		_, _, err := db.Insert().
			Table(generatedUsers).
			Columns(generatedUsers.FullName).
			Select(subquery).
			ToSQL()

		if err == nil || !strings.Contains(err.Error(), "cannot assign to generated column full_name") {
			t.Fatalf("expected generated insert-select target error, got %v", err)
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

func TestInsertQuery_DefaultValues(t *testing.T) {
	t.Parallel()

	users, _ := defineTables()

	t.Run("postgres", func(t *testing.T) {
		db, _ := rain.OpenDialect("postgres")
		sqlText, args, err := db.Insert().
			Table(users).
			DefaultValues().
			ToSQL()
		if err != nil {
			t.Fatalf("ToSQL returned error: %v", err)
		}

		wantSQL := `INSERT INTO "users" DEFAULT VALUES`
		if sqlText != wantSQL {
			t.Fatalf("unexpected SQL:\nwant: %s\ngot:  %s", wantSQL, sqlText)
		}
		if len(args) != 0 {
			t.Fatalf("unexpected args: %#v", args)
		}
	})

	t.Run("sqlite", func(t *testing.T) {
		db, _ := rain.OpenDialect("sqlite")
		sqlText, args, err := db.Insert().
			Table(users).
			DefaultValues().
			ToSQL()
		if err != nil {
			t.Fatalf("ToSQL returned error: %v", err)
		}

		wantSQL := `INSERT INTO "users" DEFAULT VALUES`
		if sqlText != wantSQL {
			t.Fatalf("unexpected SQL:\nwant: %s\ngot:  %s", wantSQL, sqlText)
		}
		if len(args) != 0 {
			t.Fatalf("unexpected args: %#v", args)
		}
	})

	t.Run("mysql", func(t *testing.T) {
		db, _ := rain.OpenDialect("mysql")
		sqlText, args, err := db.Insert().
			Table(users).
			DefaultValues().
			ToSQL()
		if err != nil {
			t.Fatalf("ToSQL returned error: %v", err)
		}

		wantSQL := "INSERT INTO `users` () VALUES ()"
		if sqlText != wantSQL {
			t.Fatalf("unexpected SQL:\nwant: %s\ngot:  %s", wantSQL, sqlText)
		}
		if len(args) != 0 {
			t.Fatalf("unexpected args: %#v", args)
		}
	})

	t.Run("with returning", func(t *testing.T) {
		db, _ := rain.OpenDialect("postgres")
		sqlText, _, err := db.Insert().
			Table(users).
			DefaultValues().
			Returning(users.ID).
			ToSQL()
		if err != nil {
			t.Fatalf("ToSQL returned error: %v", err)
		}

		wantSQL := `INSERT INTO "users" DEFAULT VALUES RETURNING "users"."id"`
		if sqlText != wantSQL {
			t.Fatalf("unexpected SQL:\nwant: %s\ngot:  %s", wantSQL, sqlText)
		}
	})

	t.Run("with conflict", func(t *testing.T) {
		db, _ := rain.OpenDialect("postgres")
		sqlText, _, err := db.Insert().
			Table(users).
			DefaultValues().
			OnConflict(users.Email).
			DoNothing().
			ToSQL()
		if err != nil {
			t.Fatalf("ToSQL returned error: %v", err)
		}

		wantSQL := `INSERT INTO "users" DEFAULT VALUES ON CONFLICT ("email") DO NOTHING`
		if sqlText != wantSQL {
			t.Fatalf("unexpected SQL:\nwant: %s\ngot:  %s", wantSQL, sqlText)
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
		if err == nil || !strings.Contains(err.Error(), "does not support conflict targets") {
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
		if err == nil || !strings.Contains(err.Error(), "does not support conflict targets") {
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
