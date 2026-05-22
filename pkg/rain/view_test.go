package rain

import (
	"strings"
	"testing"

	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

func TestViewSQL(t *testing.T) {
	type UsersTable struct {
		schema.TableModel
		ID    *schema.Column[int64]
		Email *schema.Column[string]
	}
	Users := schema.Define("users", func(t *UsersTable) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.Email = t.VarChar("email", 255).NotNull()
	})

	db, _ := OpenDialect("postgres")
	query := db.Select().Table(Users).Column(Users.ID, Users.Email).Where(Users.ID.Gt(100))

	type UsersView struct {
		schema.TableModel
		ID    *schema.Column[int64]
		Email *schema.Column[string]
	}
	UsersOver100 := schema.DefineView("users_over_100", query, func(t *UsersView) {
		t.ID = t.BigInt("id")
		t.Email = t.VarChar("email", 255)
	})

	sql, err := db.CreateTableSQL(UsersOver100)
	if err != nil {
		t.Fatal(err)
	}

	expected := `CREATE VIEW "users_over_100" AS SELECT "users"."id", "users"."email" FROM "users" WHERE "users"."id" > $1`
	if sql != expected {
		t.Errorf("expected %q, got %q", expected, sql)
	}
}

func TestSerialTypes(t *testing.T) {
	type SerialsTable struct {
		schema.TableModel
		ID    *schema.Column[int32]
		Small *schema.Column[int16]
		Big   *schema.Column[int64]
	}
	Serials := schema.Define("serials", func(t *SerialsTable) {
		t.ID = t.Serial("id").PrimaryKey()
		t.Small = t.SmallSerial("small").NotNull()
		t.Big = t.BigSerial("big").NotNull()
	})

	db, _ := OpenDialect("postgres")
	sql, err := db.CreateTableSQL(Serials)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(sql, `"id" SERIAL PRIMARY KEY`) {
		t.Errorf("expected SERIAL for id, got %q", sql)
	}
	if !strings.Contains(sql, `"small" SMALLSERIAL NOT NULL`) {
		t.Errorf("expected SMALLSERIAL for small, got %q", sql)
	}
	if !strings.Contains(sql, `"big" BIGSERIAL NOT NULL`) {
		t.Errorf("expected BIGSERIAL for big, got %q", sql)
	}
}
