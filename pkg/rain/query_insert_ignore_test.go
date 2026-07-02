package rain_test

import (
	"testing"

	"github.com/hyperlocalise/rain-orm/pkg/rain"
	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

func TestInsertIgnore(t *testing.T) {
	type UsersTable struct {
		schema.TableModel
		ID   *schema.Column[int64]
		Name *schema.Column[string]
	}
	users := schema.Define("users", func(t *UsersTable) {
		t.ID = t.BigInt("id").PrimaryKey()
		t.Name = t.Text("name").NotNull()
	})

	tests := []struct {
		name    string
		dialect string
		builder func(db *rain.DB) *rain.InsertQuery
		wantSQL string
	}{
		{
			name:    "PostgreSQL Ignore",
			dialect: "postgres",
			builder: func(db *rain.DB) *rain.InsertQuery {
				return db.Insert().Table(users).Set(users.ID, 1).Set(users.Name, "Alice").Ignore()
			},
			wantSQL: `INSERT INTO "users" ("id", "name") VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		},
		{
			name:    "MySQL Ignore",
			dialect: "mysql",
			builder: func(db *rain.DB) *rain.InsertQuery {
				return db.Insert().Table(users).Set(users.ID, 1).Set(users.Name, "Alice").Ignore()
			},
			wantSQL: "INSERT IGNORE INTO `users` (`id`, `name`) VALUES (?, ?)",
		},
		{
			name:    "SQLite Ignore",
			dialect: "sqlite",
			builder: func(db *rain.DB) *rain.InsertQuery {
				return db.Insert().Table(users).Set(users.ID, 1).Set(users.Name, "Alice").Ignore()
			},
			wantSQL: `INSERT OR IGNORE INTO "users" ("id", "name") VALUES (?, ?)`,
		},
		{
			name:    "PostgreSQL Targetless DoNothing",
			dialect: "postgres",
			builder: func(db *rain.DB) *rain.InsertQuery {
				return db.Insert().Table(users).Set(users.ID, 1).Set(users.Name, "Alice").OnConflict().DoNothing()
			},
			wantSQL: `INSERT INTO "users" ("id", "name") VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		},
		{
			name:    "MySQL Targetless DoNothing",
			dialect: "mysql",
			builder: func(db *rain.DB) *rain.InsertQuery {
				return db.Insert().Table(users).Set(users.ID, 1).Set(users.Name, "Alice").OnConflict().DoNothing()
			},
			wantSQL: "INSERT IGNORE INTO `users` (`id`, `name`) VALUES (?, ?)",
		},
		{
			name:    "SQLite Targetless DoNothing",
			dialect: "sqlite",
			builder: func(db *rain.DB) *rain.InsertQuery {
				return db.Insert().Table(users).Set(users.ID, 1).Set(users.Name, "Alice").OnConflict().DoNothing()
			},
			wantSQL: `INSERT OR IGNORE INTO "users" ("id", "name") VALUES (?, ?)`,
		},
		{
			name:    "PostgreSQL Select Ignore",
			dialect: "postgres",
			builder: func(db *rain.DB) *rain.InsertQuery {
				return db.Insert().Table(users).Columns(users.ID, users.Name).Select(db.Select(users.ID, users.Name).Table(users)).Ignore()
			},
			wantSQL: `INSERT INTO "users" ("id", "name") SELECT "users"."id", "users"."name" FROM "users" ON CONFLICT DO NOTHING`,
		},
		{
			name:    "MySQL Select Ignore",
			dialect: "mysql",
			builder: func(db *rain.DB) *rain.InsertQuery {
				return db.Insert().Table(users).Columns(users.ID, users.Name).Select(db.Select(users.ID, users.Name).Table(users)).Ignore()
			},
			wantSQL: "INSERT IGNORE INTO `users` (`id`, `name`) SELECT `users`.`id`, `users`.`name` FROM `users`",
		},
		{
			name:    "SQLite Select Ignore",
			dialect: "sqlite",
			builder: func(db *rain.DB) *rain.InsertQuery {
				return db.Insert().Table(users).Columns(users.ID, users.Name).Select(db.Select(users.ID, users.Name).Table(users)).Ignore()
			},
			wantSQL: `INSERT OR IGNORE INTO "users" ("id", "name") SELECT "users"."id", "users"."name" FROM "users"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, _ := rain.OpenDialect(tt.dialect)
			gotSQL, _, err := tt.builder(db).ToSQL()
			if err != nil {
				t.Fatalf("ToSQL() error: %v", err)
			}
			if gotSQL != tt.wantSQL {
				t.Errorf("ToSQL() got = %q, want %q", gotSQL, tt.wantSQL)
			}
		})
	}
}
