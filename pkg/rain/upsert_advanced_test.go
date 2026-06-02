package rain_test

import (
	"testing"

	"github.com/hyperlocalise/rain-orm/pkg/rain"
)

func TestInsertOnConflictAdvancedPostgres(t *testing.T) {
	t.Parallel()

	db, err := rain.OpenDialect("postgres")
	if err != nil {
		t.Fatalf("OpenDialect returned error: %v", err)
	}
	users, _ := defineTables()

	t.Run("do nothing without targets", func(t *testing.T) {
		sqlText, _, err := db.Insert().
			Table(users).
			Set(users.Email, "alice@example.com").
			OnConflict().
			DoNothing().
			ToSQL()
		if err != nil {
			t.Fatalf("ToSQL returned error: %v", err)
		}

		wantSQL := `INSERT INTO "users" ("email") VALUES ($1) ON CONFLICT DO NOTHING`
		if sqlText != wantSQL {
			t.Fatalf("unexpected SQL:\nwant: %s\ngot:  %s", wantSQL, sqlText)
		}
	})

	t.Run("on constraint", func(t *testing.T) {
		sqlText, _, err := db.Insert().
			Table(users).
			Set(users.Email, "alice@example.com").
			OnConflict().
			OnConstraint("users_email_key").
			DoNothing().
			ToSQL()
		if err != nil {
			t.Fatalf("ToSQL returned error: %v", err)
		}

		wantSQL := `INSERT INTO "users" ("email") VALUES ($1) ON CONFLICT ON CONSTRAINT "users_email_key" DO NOTHING`
		if sqlText != wantSQL {
			t.Fatalf("unexpected SQL:\nwant: %s\ngot:  %s", wantSQL, sqlText)
		}
	})

	t.Run("target where", func(t *testing.T) {
		sqlText, args, err := db.Insert().
			Table(users).
			Set(users.Email, "alice@example.com").
			OnConflict(users.Email).
			TargetWhere(users.Active.Eq(true)).
			DoNothing().
			ToSQL()
		if err != nil {
			t.Fatalf("ToSQL returned error: %v", err)
		}

		wantSQL := `INSERT INTO "users" ("email") VALUES ($1) ON CONFLICT ("email") WHERE "active" = TRUE DO NOTHING`
		if sqlText != wantSQL {
			t.Fatalf("unexpected SQL:\nwant: %s\ngot:  %s", wantSQL, sqlText)
		}
		if len(args) != 1 {
			t.Fatalf("unexpected args: %#v", args)
		}
	})

	t.Run("do update set with custom values and where", func(t *testing.T) {
		sqlText, args, err := db.Insert().
			Table(users).
			Set(users.Email, "alice@example.com").
			Set(users.Name, "Alice").
			OnConflict(users.Email).
			Set(users.Active, true).
			Where(users.Active.Eq(false)).
			DoUpdateSet(users.Name).
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

	t.Run("do update with only custom sets", func(t *testing.T) {
		sqlText, args, err := db.Insert().
			Table(users).
			Set(users.Email, "alice@example.com").
			OnConflict(users.Email).
			Set(users.Name, "Conflicted").
			DoUpdate().
			ToSQL()
		if err != nil {
			t.Fatalf("ToSQL returned error: %v", err)
		}

		wantSQL := `INSERT INTO "users" ("email") VALUES ($1) ON CONFLICT ("email") DO UPDATE SET "name" = $2`
		if sqlText != wantSQL {
			t.Fatalf("unexpected SQL:\nwant: %s\ngot:  %s", wantSQL, sqlText)
		}
		if len(args) != 2 || args[1] != "Conflicted" {
			t.Fatalf("unexpected args: %#v", args)
		}
	})
}

func TestInsertOnConflictValidation(t *testing.T) {
	t.Parallel()

	db, _ := rain.OpenDialect("postgres")
	users, _ := defineTables()

	t.Run("targetWhere without targets returns error", func(t *testing.T) {
		_, _, err := db.Insert().
			Table(users).
			Set(users.Email, "a").
			OnConflict().
			TargetWhere(users.Active.Eq(true)).
			DoNothing().
			ToSQL()

		if err == nil || err.Error() != "rain: conflict targetWhere requires at least one conflict target column" {
			t.Fatalf("expected targetWhere validation error, got %v", err)
		}
	})

	t.Run("NullCheckExpr in targetWhere is unqualified", func(t *testing.T) {
		sqlText, _, err := db.Insert().
			Table(users).
			Set(users.Email, "a").
			OnConflict(users.Email).
			TargetWhere(users.Name.IsNull()).
			DoNothing().
			ToSQL()

		if err != nil {
			t.Fatalf("ToSQL returned error: %v", err)
		}

		wantSQL := `INSERT INTO "users" ("email") VALUES ($1) ON CONFLICT ("email") WHERE "name" IS NULL DO NOTHING`
		if sqlText != wantSQL {
			t.Fatalf("unexpected SQL:\nwant: %s\ngot:  %s", wantSQL, sqlText)
		}
	})
}

func TestInsertOnConflictAdvancedSQLite(t *testing.T) {
	t.Parallel()

	db, err := rain.OpenDialect("sqlite")
	if err != nil {
		t.Fatalf("OpenDialect returned error: %v", err)
	}
	users, _ := defineTables()

	t.Run("target where", func(t *testing.T) {
		sqlText, args, err := db.Insert().
			Table(users).
			Set(users.Email, "alice@example.com").
			OnConflict(users.Email).
			TargetWhere(users.Active.Eq(true)).
			DoNothing().
			ToSQL()
		if err != nil {
			t.Fatalf("ToSQL returned error: %v", err)
		}

		wantSQL := `INSERT INTO "users" ("email") VALUES (?) ON CONFLICT ("email") WHERE "active" = 1 DO NOTHING`
		if sqlText != wantSQL {
			t.Fatalf("unexpected SQL:\nwant: %s\ngot:  %s", wantSQL, sqlText)
		}
		if len(args) != 1 {
			t.Fatalf("unexpected args: %#v", args)
		}
	})
}

func TestInsertOnConflictAdvancedMySQL(t *testing.T) {
	t.Parallel()

	db, err := rain.OpenDialect("mysql")
	if err != nil {
		t.Fatalf("OpenDialect returned error: %v", err)
	}
	users, _ := defineTables()

	t.Run("do update set with custom values", func(t *testing.T) {
		sqlText, args, err := db.Insert().
			Table(users).
			Set(users.Email, "alice@example.com").
			Set(users.Name, "Alice").
			OnConflict().
			Set(users.Active, true).
			DoUpdateSet(users.Name).
			ToSQL()
		if err != nil {
			t.Fatalf("ToSQL returned error: %v", err)
		}

		wantSQL := "INSERT INTO `users` (`email`, `name`) VALUES (?, ?) ON DUPLICATE KEY UPDATE `name` = VALUES(`name`), `active` = ?"
		if sqlText != wantSQL {
			t.Fatalf("unexpected SQL:\nwant: %s\ngot:  %s", wantSQL, sqlText)
		}
		if len(args) != 3 || args[2] != true {
			t.Fatalf("unexpected args: %#v", args)
		}
	})

	t.Run("rejects postgres-only features", func(t *testing.T) {
		_, _, err := db.Insert().
			Table(users).
			Set(users.Email, "a").
			OnConflict().
			OnConstraint("foo").
			DoNothing().
			ToSQL()
		if err == nil {
			t.Fatal("expected error for ON CONSTRAINT on MySQL")
		}

		_, _, err = db.Insert().
			Table(users).
			Set(users.Email, "a").
			OnConflict().
			TargetWhere(users.Active.Eq(true)).
			DoNothing().
			ToSQL()
		if err == nil {
			t.Fatal("expected error for TargetWhere on MySQL")
		}

		_, _, err = db.Insert().
			Table(users).
			Set(users.Email, "a").
			OnConflict().
			Where(users.Active.Eq(true)).
			DoUpdateSet(users.Name).
			ToSQL()
		if err == nil {
			t.Fatal("expected error for Where on MySQL")
		}
	})
}
