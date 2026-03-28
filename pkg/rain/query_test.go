package rain_test

import (
	"testing"
	"time"

	"github.com/hyperlocalise/rain-orm/pkg/rain"
	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

type usersTable struct {
	schema.TableModel
	ID        *schema.Column[int64]
	Email     *schema.Column[string]
	Name      *schema.Column[string]
	Active    *schema.Column[bool]
	CreatedAt *schema.Column[time.Time]
}

type postsTable struct {
	schema.TableModel
	ID     *schema.Column[int64]
	UserID *schema.Column[int64]
	Title  *schema.Column[string]
}

type userModel struct {
	ID     int64  `db:"id"`
	Email  string `db:"email"`
	Name   string `db:"name"`
	Active bool   `db:"active"`
}

func defineTables() (*usersTable, *postsTable) {
	users := schema.Define("users", func(t *usersTable) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.Email = t.VarChar("email", 255).NotNull().Unique()
		t.Name = t.Text("name").NotNull()
		t.Active = t.Boolean("active").NotNull().Default(true)
		t.CreatedAt = t.TimestampTZ("created_at").NotNull().DefaultNow()
	})

	posts := schema.Define("posts", func(t *postsTable) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.UserID = t.BigInt("user_id").NotNull().References(users.ID)
		t.Title = t.Text("title").NotNull()
	})

	return users, posts
}

func TestSelectToSQL(t *testing.T) {
	db := rain.OpenDialect("postgres")
	users, posts := defineTables()
	u := schema.Alias(users, "u")
	p := schema.Alias(posts, "p")

	sqlText, args, err := db.Select().
		Table(p).
		Column(p.ID, p.Title, u.Email).
		Join(u, p.UserID.EqCol(u.ID)).
		Where(u.Active.Eq(true)).
		OrderBy(p.ID.Desc()).
		Limit(10).
		ToSQL()
	if err != nil {
		t.Fatalf("ToSQL returned error: %v", err)
	}

	wantSQL := `SELECT "p"."id", "p"."title", "u"."email" FROM "posts" AS "p" INNER JOIN "users" AS "u" ON "p"."user_id" = "u"."id" WHERE "u"."active" = $1 ORDER BY "p"."id" DESC LIMIT 10`
	if sqlText != wantSQL {
		t.Fatalf("unexpected SQL:\nwant: %s\ngot:  %s", wantSQL, sqlText)
	}
	if len(args) != 1 || args[0] != true {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestInsertUpdateDeleteToSQL(t *testing.T) {
	db := rain.OpenDialect("postgres")
	users, _ := defineTables()

	insertSQL, insertArgs, err := db.Insert().
		Table(users).
		Model(&userModel{Email: "alice@example.com", Name: "Alice", Active: true}).
		Returning(users.ID).
		ToSQL()
	if err != nil {
		t.Fatalf("insert ToSQL returned error: %v", err)
	}
	wantInsert := `INSERT INTO "users" ("email", "name", "active") VALUES ($1, $2, $3) RETURNING "users"."id"`
	if insertSQL != wantInsert {
		t.Fatalf("unexpected insert SQL:\nwant: %s\ngot:  %s", wantInsert, insertSQL)
	}
	if len(insertArgs) != 3 {
		t.Fatalf("unexpected insert args: %#v", insertArgs)
	}

	updateSQL, updateArgs, err := db.Update().
		Table(users).
		Set(users.Name, "Alice Smith").
		Where(users.ID.Eq(int64(1))).
		ToSQL()
	if err != nil {
		t.Fatalf("update ToSQL returned error: %v", err)
	}
	wantUpdate := `UPDATE "users" SET "name" = $1 WHERE "users"."id" = $2`
	if updateSQL != wantUpdate {
		t.Fatalf("unexpected update SQL:\nwant: %s\ngot:  %s", wantUpdate, updateSQL)
	}
	if len(updateArgs) != 2 {
		t.Fatalf("unexpected update args: %#v", updateArgs)
	}

	deleteSQL, deleteArgs, err := db.Delete().
		Table(users).
		Where(users.ID.Eq(int64(99))).
		ToSQL()
	if err != nil {
		t.Fatalf("delete ToSQL returned error: %v", err)
	}
	wantDelete := `DELETE FROM "users" WHERE "users"."id" = $1`
	if deleteSQL != wantDelete {
		t.Fatalf("unexpected delete SQL:\nwant: %s\ngot:  %s", wantDelete, deleteSQL)
	}
	if len(deleteArgs) != 1 || deleteArgs[0] != int64(99) {
		t.Fatalf("unexpected delete args: %#v", deleteArgs)
	}
}

func TestReturningUnsupportedDialect(t *testing.T) {
	db := rain.OpenDialect("mysql")
	users, _ := defineTables()

	_, _, err := db.Insert().
		Table(users).
		Set(users.Name, "Alice").
		Returning(users.ID).
		ToSQL()
	if err == nil {
		t.Fatalf("expected RETURNING to fail on mysql dialect")
	}
}

func TestInsertModelAndSetMergeToSQL(t *testing.T) {
	db := rain.OpenDialect("postgres")
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
	db := rain.OpenDialect("postgres")
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
