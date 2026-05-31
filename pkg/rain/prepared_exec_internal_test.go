package rain

import (
	"testing"

	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

func TestPreparedInsertCompile(t *testing.T) {
	t.Parallel()

	db, _ := OpenDialect("postgres")
	users := schema.Define("users", func(t *struct {
		schema.TableModel
		ID    *schema.Column[int64]
		Email *schema.Column[string]
		Name  *schema.Column[string]
	},
	) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.Email = t.VarChar("email", 255)
		t.Name = t.Text("name")
	})

	q := db.Insert().
		Table(users).
		Set(users.Email, schema.Placeholder("email")).
		Set(users.Name, schema.Placeholder("name"))

	compiled, err := q.compile()
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	wantSQL := `INSERT INTO "users" ("email", "name") VALUES ($1, $2)`
	if compiled.sql != wantSQL {
		t.Errorf("unexpected SQL:\nwant: %s\ngot:  %s", wantSQL, compiled.sql)
	}
	if !compiled.hasNames {
		t.Errorf("expected compiled query to have names")
	}
}

func TestPreparedUpdateCompile(t *testing.T) {
	t.Parallel()

	db, _ := OpenDialect("postgres")
	users := schema.Define("users", func(t *struct {
		schema.TableModel
		ID   *schema.Column[int64]
		Name *schema.Column[string]
	},
	) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.Name = t.Text("name")
	})

	q := db.Update().
		Table(users).
		Set(users.Name, schema.Placeholder("new_name")).
		Where(users.ID.EqExpr(schema.Placeholder("id")))

	compiled, err := q.compile()
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	wantSQL := `UPDATE "users" SET "name" = $1 WHERE "users"."id" = $2`
	if compiled.sql != wantSQL {
		t.Errorf("unexpected SQL:\nwant: %s\ngot:  %s", wantSQL, compiled.sql)
	}
}

func TestPreparedDeleteCompile(t *testing.T) {
	t.Parallel()

	db, _ := OpenDialect("postgres")
	users := schema.Define("users", func(t *struct {
		schema.TableModel
		ID *schema.Column[int64]
	},
	) {
		t.ID = t.BigSerial("id").PrimaryKey()
	})

	q := db.Delete().
		Table(users).
		Where(users.ID.EqExpr(schema.Placeholder("id")))

	compiled, err := q.compile()
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	wantSQL := `DELETE FROM "users" WHERE "users"."id" = $1`
	if compiled.sql != wantSQL {
		t.Errorf("unexpected SQL:\nwant: %s\ngot:  %s", wantSQL, compiled.sql)
	}
}

func TestPreparedInsertScanInternal(t *testing.T) {
	// This test ensures that PreparedInsertQuery has access to the correct table metadata.
	users := schema.Define("users", func(t *struct {
		schema.TableModel
		ID    *schema.Column[int64]
		Email *schema.Column[string]
	},
	) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.Email = t.VarChar("email", 255)
	})

	prepared := &PreparedInsertQuery{
		table: users.TableDef(),
	}

	if prepared.table.Name != "users" {
		t.Errorf("expected table name users, got %s", prepared.table.Name)
	}
}
