package rain_test

import (
	"reflect"
	"testing"

	"github.com/hyperlocalise/rain-orm/pkg/rain"
	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

func TestInsertSelectToSQL(t *testing.T) {
	t.Parallel()

	users, posts := defineTables()

	t.Run("postgres", func(t *testing.T) {
		db, _ := rain.OpenDialect("postgres")
		subquery := db.Select().
			Table(users).
			Column(users.ID, schema.Raw("'Migrated user: ' || name")).
			Where(users.Active.Eq(true))

		sqlText, args, err := db.Insert().
			Table(posts).
			Columns(posts.UserID, posts.Title).
			Select(subquery).
			ToSQL()
		if err != nil {
			t.Fatalf("ToSQL returned error: %v", err)
		}

		wantSQL := `INSERT INTO "posts" ("user_id", "title") SELECT "users"."id", 'Migrated user: ' || name FROM "users" WHERE "users"."active" = $1`
		if sqlText != wantSQL {
			t.Fatalf("unexpected SQL:\nwant: %s\ngot:  %s", wantSQL, sqlText)
		}
		if len(args) != 1 || args[0] != true {
			t.Fatalf("unexpected args: %#v", args)
		}
	})

	t.Run("mysql", func(t *testing.T) {
		db, _ := rain.OpenDialect("mysql")
		subquery := db.Select().
			Table(users).
			Column(users.ID, schema.Raw("CONCAT('Migrated user: ', name)")).
			Where(users.Active.Eq(true))

		sqlText, args, err := db.Insert().
			Table(posts).
			Columns(posts.UserID, posts.Title).
			Select(subquery).
			ToSQL()
		if err != nil {
			t.Fatalf("ToSQL returned error: %v", err)
		}

		wantSQL := "INSERT INTO `posts` (`user_id`, `title`) SELECT `users`.`id`, CONCAT('Migrated user: ', name) FROM `users` WHERE `users`.`active` = ?"
		if sqlText != wantSQL {
			t.Fatalf("unexpected SQL:\nwant: %s\ngot:  %s", wantSQL, sqlText)
		}
		if len(args) != 1 || args[0] != true {
			t.Fatalf("unexpected args: %#v", args)
		}
	})

	t.Run("sqlite", func(t *testing.T) {
		db, _ := rain.OpenDialect("sqlite")
		subquery := db.Select().
			Table(users).
			Column(users.ID, schema.Raw("'Migrated user: ' || name")).
			Where(users.Active.Eq(true))

		sqlText, args, err := db.Insert().
			Table(posts).
			Columns(posts.UserID, posts.Title).
			Select(subquery).
			ToSQL()
		if err != nil {
			t.Fatalf("ToSQL returned error: %v", err)
		}

		wantSQL := `INSERT INTO "posts" ("user_id", "title") SELECT "users"."id", 'Migrated user: ' || name FROM "users" WHERE "users"."active" = ?`
		if sqlText != wantSQL {
			t.Fatalf("unexpected SQL:\nwant: %s\ngot:  %s", wantSQL, sqlText)
		}
		if len(args) != 1 || args[0] != true {
			t.Fatalf("unexpected args: %#v", args)
		}
	})

	t.Run("without explicit columns", func(t *testing.T) {
		db, _ := rain.OpenDialect("postgres")
		subquery := db.Select().
			Table(users).
			Column(users.ID, users.Email, users.Name, users.Active, users.CreatedAt)

		sqlText, _, err := db.Insert().
			Table(users).
			Select(subquery).
			ToSQL()
		if err != nil {
			t.Fatalf("ToSQL returned error: %v", err)
		}

		wantSQL := `INSERT INTO "users" SELECT "users"."id", "users"."email", "users"."name", "users"."active", "users"."created_at" FROM "users"`
		if sqlText != wantSQL {
			t.Fatalf("unexpected SQL:\nwant: %s\ngot:  %s", wantSQL, sqlText)
		}
	})

	t.Run("with conflict and returning", func(t *testing.T) {
		db, _ := rain.OpenDialect("postgres")
		subquery := db.Select().
			Table(users).
			Column(users.Email, users.Name).
			Where(users.Active.Eq(true))

		sqlText, _, err := db.Insert().
			Table(users).
			Columns(users.Email, users.Name).
			Select(subquery).
			OnConflict(users.Email).
			DoNothing().
			Returning(users.ID).
			ToSQL()
		if err != nil {
			t.Fatalf("ToSQL returned error: %v", err)
		}

		wantSQL := `INSERT INTO "users" ("email", "name") SELECT "users"."email", "users"."name" FROM "users" WHERE "users"."active" = $1 ON CONFLICT ("email") DO NOTHING RETURNING "users"."id"`
		if sqlText != wantSQL {
			t.Fatalf("unexpected SQL:\nwant: %s\ngot:  %s", wantSQL, sqlText)
		}
	})
}

func TestInsertSelectValidation(t *testing.T) {
	t.Parallel()

	db, _ := rain.OpenDialect("postgres")
	users, _ := defineTables()
	subquery := db.Select().Table(users)

	t.Run("exactly one source - select and model", func(t *testing.T) {
		_, _, err := db.Insert().
			Table(users).
			Model(&userModel{Email: "alice@example.com"}).
			Select(subquery).
			ToSQL()
		if err == nil || !reflect.DeepEqual(err.Error(), "rain: insert query requires exactly one value source: Model/Set, Models, Values, or Select") {
			t.Fatalf("expected source exclusivity error, got %v", err)
		}
	})

	t.Run("exactly one source - none", func(t *testing.T) {
		_, _, err := db.Insert().
			Table(users).
			ToSQL()
		if err == nil || !reflect.DeepEqual(err.Error(), "rain: insert query requires either explicit values, a model, or a subquery") {
			t.Fatalf("expected missing source error, got %v", err)
		}
	})
}
