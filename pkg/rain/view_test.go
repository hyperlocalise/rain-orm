package rain_test

import (
	"strings"
	"testing"

	"github.com/hyperlocalise/rain-orm/pkg/rain"
	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

func TestCreateViewSQL(t *testing.T) {
	t.Parallel()

	users := schema.Define("users", func(t *struct {
		schema.TableModel
		ID    *schema.Column[int64]
		Email *schema.Column[string]
	},
	) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.Email = t.VarChar("email", 255).NotNull()
	})

	activeUsers := schema.DefineView("active_users", func(t *struct {
		schema.TableModel
		ID    *schema.Column[int64]
		Email *schema.Column[string]
	},
	) {
		t.ID = t.BigInt("id")
		t.Email = t.VarChar("email", 255)
	}, rain.OpenDialectSelect("sqlite").Select().Table(users).Column(users.ID, users.Email).Where(users.ID.Gt(int64(0))))

	cases := []struct {
		name    string
		dialect string
		want    string
	}{
		{
			name:    "postgres view",
			dialect: "postgres",
			want:    `CREATE VIEW "active_users" AS (SELECT "users"."id", "users"."email" FROM "users" WHERE "users"."id" > 0)`,
		},
		{
			name:    "mysql view",
			dialect: "mysql",
			want:    "CREATE VIEW `active_users` AS (SELECT `users`.`id`, `users`.`email` FROM `users` WHERE `users`.`id` > 0)",
		},
		{
			name:    "sqlite view",
			dialect: "sqlite",
			want:    `CREATE VIEW "active_users" AS SELECT "users"."id", "users"."email" FROM "users" WHERE "users"."id" > 0`,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			db, _ := rain.OpenDialect(tc.dialect)
			got, err := db.CreateTableSQL(activeUsers)
			if err != nil {
				t.Fatalf("CreateTableSQL: %v", err)
			}
			if got != tc.want {
				t.Fatalf("unexpected SQL:\nwant: %s\ngot:  %s", tc.want, got)
			}
		})
	}
}

func TestViewRejectsDDL(t *testing.T) {
	t.Parallel()

	v := schema.DefineView("v", func(t *struct{ schema.TableModel }) {}, nil)
	db, _ := rain.OpenDialect("sqlite")

	if _, err := db.CreateIndexesSQL(v); err != nil {
		t.Fatalf("CreateIndexesSQL: %v", err)
	}

	if _, err := db.ColumnDefinitionSQL(v, "id"); err == nil || !strings.Contains(err.Error(), "does not support column definition SQL") {
		t.Fatalf("expected ColumnDefinitionSQL to fail for view, got %v", err)
	}

	if _, err := db.AddConstraintSQL(v, "c"); err == nil || !strings.Contains(err.Error(), "does not support constraints") {
		t.Fatalf("expected AddConstraintSQL to fail for view, got %v", err)
	}

	if _, err := db.AddForeignKeySQL(v, "fk"); err == nil || !strings.Contains(err.Error(), "does not support foreign keys") {
		t.Fatalf("expected AddForeignKeySQL to fail for view, got %v", err)
	}
}

func TestViewRejectsWrites(t *testing.T) {
	t.Parallel()

	v := schema.DefineView("v", func(t *struct {
		schema.TableModel
		ID *schema.Column[int64]
	},
	) {
		t.ID = t.BigInt("id")
	}, nil)
	db, _ := rain.OpenDialect("sqlite")

	if _, _, err := db.Insert().Table(v).Set(v.ID, 1).ToSQL(); err == nil || !strings.Contains(err.Error(), "cannot insert into view") {
		t.Fatalf("expected Insert to fail for view, got %v", err)
	}

	if _, _, err := db.Update().Table(v).Set(v.ID, 1).Where(v.ID.Eq(int64(1))).ToSQL(); err == nil || !strings.Contains(err.Error(), "cannot update view") {
		t.Fatalf("expected Update to fail for view, got %v", err)
	}

	if _, _, err := db.Delete().Table(v).Where(v.ID.Eq(int64(1))).ToSQL(); err == nil || !strings.Contains(err.Error(), "cannot delete from view") {
		t.Fatalf("expected Delete to fail for view, got %v", err)
	}
}
