package rain_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/hyperlocalise/rain-orm/pkg/rain"
	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

type ddlUsersTable struct {
	schema.TableModel
	ID         *schema.Column[int64]
	Email      *schema.Column[string]
	Score      *schema.Column[float32]
	Precise    *schema.Column[float64]
	Amount     *schema.Column[string]
	Metadata   *schema.Column[any]
	MetadataB  *schema.Column[any]
	ExternalID *schema.Column[string]
	Payload    *schema.Column[[]byte]
	CreatedAt  *schema.Column[time.Time]
	Status     *schema.Column[string]
}

type ddlPostsTable struct {
	schema.TableModel
	ID     *schema.Column[int64]
	UserID *schema.Column[int64]
	Title  *schema.Column[string]
}

func defineDDLTables() (*ddlUsersTable, *ddlPostsTable) {
	users := schema.Define("users", func(t *ddlUsersTable) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.Email = t.VarChar("email", 255).NotNull().Unique()
		t.Score = t.Real("score").NotNull().Default(float32(0.5))
		t.Precise = t.Double("precise").NotNull()
		t.Amount = t.Decimal("amount", 12, 2).NotNull().Default("0.00")
		t.Metadata = t.JSON("metadata").Nullable()
		t.MetadataB = t.JSONB("metadata_b").Nullable()
		t.ExternalID = t.UUID("external_id").Nullable()
		t.Payload = t.Bytes("payload").Nullable()
		t.CreatedAt = t.TimestampTZPrecision("created_at", 6).NotNull().DefaultNow()
		t.Status = t.Enum("status", "draft", "published").NotNull().Default("draft")
		t.UniqueIndex("users_email_idx").On(t.Email)
		t.Index("users_created_status_idx").On(t.CreatedAt.Desc(), t.Status)
	})

	posts := schema.Define("posts", func(t *ddlPostsTable) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.UserID = t.BigInt("user_id").NotNull().References(users.ID)
		t.Title = t.Text("title").NotNull()
	})

	return users, posts
}

func TestCreateTableSQLAcrossDialects(t *testing.T) {
	t.Parallel()

	users, posts := defineDDLTables()

	cases := []struct {
		name      string
		dialect   string
		table     schema.TableReference
		fragments []string
	}{
		{
			name:    "postgres users",
			dialect: "postgres",
			table:   users,
			fragments: []string{
				`CREATE TABLE "users" (`,
				`"id" BIGSERIAL PRIMARY KEY`,
				`"email" VARCHAR(255) NOT NULL UNIQUE`,
				`"score" REAL NOT NULL DEFAULT 0.5`,
				`"precise" DOUBLE PRECISION NOT NULL`,
				`"amount" NUMERIC(12,2) NOT NULL DEFAULT '0.00'`,
				`"metadata_b" JSONB`,
				`"external_id" UUID`,
				`"payload" BYTEA`,
				`"created_at" TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP`,
				`"status" TEXT NOT NULL DEFAULT 'draft' CHECK ("status" IN ('draft', 'published'))`,
			},
		},
		{
			name:    "mysql users",
			dialect: "mysql",
			table:   users,
			fragments: []string{
				"CREATE TABLE `users` (",
				"`id` BIGINT PRIMARY KEY AUTO_INCREMENT",
				"`email` VARCHAR(255) NOT NULL UNIQUE",
				"`score` FLOAT NOT NULL DEFAULT 0.5",
				"`precise` DOUBLE NOT NULL",
				"`amount` DECIMAL(12,2) NOT NULL DEFAULT '0.00'",
				"`metadata_b` JSON",
				"`external_id` CHAR(36)",
				"`payload` BLOB",
				"`created_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP",
				"`status` VARCHAR(255) NOT NULL DEFAULT 'draft' CHECK (`status` IN ('draft', 'published'))",
			},
		},
		{
			name:    "sqlite users",
			dialect: "sqlite",
			table:   users,
			fragments: []string{
				`CREATE TABLE "users" (`,
				`"id" INTEGER PRIMARY KEY AUTOINCREMENT`,
				`"email" TEXT NOT NULL UNIQUE`,
				`"score" REAL NOT NULL DEFAULT 0.5`,
				`"precise" REAL NOT NULL`,
				`"amount" REAL NOT NULL DEFAULT '0.00'`,
				`"metadata_b" TEXT`,
				`"external_id" TEXT`,
				`"payload" BLOB`,
				`"created_at" TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP`,
				`"status" TEXT NOT NULL DEFAULT 'draft' CHECK ("status" IN ('draft', 'published'))`,
			},
		},
		{
			name:    "sqlite posts foreign key",
			dialect: "sqlite",
			table:   posts,
			fragments: []string{
				`FOREIGN KEY ("user_id") REFERENCES "users" ("id")`,
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			db, err := rain.OpenDialect(tc.dialect)
			if err != nil {
				t.Fatalf("OpenDialect(%q): %v", tc.dialect, err)
			}

			sql, err := db.CreateTableSQL(tc.table)
			if err != nil {
				t.Fatalf("CreateTableSQL: %v", err)
			}

			for _, fragment := range tc.fragments {
				if !strings.Contains(sql, fragment) {
					t.Fatalf("expected SQL to contain %q, got:\n%s", fragment, sql)
				}
			}
		})
	}
}

func TestCreateTableSQLValidation(t *testing.T) {
	t.Parallel()

	db, err := rain.OpenDialect("sqlite")
	if err != nil {
		t.Fatalf("OpenDialect(sqlite): %v", err)
	}

	if _, err := db.CreateTableSQL(nil); err == nil {
		t.Fatalf("expected nil table to fail")
	}

	var nilDB *rain.DB
	users, _ := defineDDLTables()
	if _, err := nilDB.CreateTableSQL(users); err == nil {
		t.Fatalf("expected nil DB to fail")
	}
}

func TestCreateTableSQLExecutesInSQLite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openSQLiteTestDB(t)
	users, posts := defineDDLTables()

	createUsersSQL, err := db.CreateTableSQL(users)
	if err != nil {
		t.Fatalf("CreateTableSQL(users): %v", err)
	}
	createPostsSQL, err := db.CreateTableSQL(posts)
	if err != nil {
		t.Fatalf("CreateTableSQL(posts): %v", err)
	}

	for _, statement := range []string{createUsersSQL, createPostsSQL} {
		if _, err := db.Exec(ctx, statement); err != nil {
			t.Fatalf("exec generated DDL failed: %v\nSQL:\n%s", err, statement)
		}
	}

	indexesSQL, err := db.CreateIndexesSQL(users)
	if err != nil {
		t.Fatalf("CreateIndexesSQL(users): %v", err)
	}
	for _, statement := range indexesSQL {
		if _, err := db.Exec(ctx, statement); err != nil {
			t.Fatalf("exec generated index DDL failed: %v\nSQL:\n%s", err, statement)
		}
	}
}

func TestCreateIndexesSQLAcrossDialects(t *testing.T) {
	t.Parallel()

	users, _ := defineDDLTables()

	cases := []struct {
		name      string
		dialect   string
		fragments []string
	}{
		{
			name:    "postgres indexes",
			dialect: "postgres",
			fragments: []string{
				`CREATE UNIQUE INDEX "users_email_idx" ON "users" ("email" ASC)`,
				`CREATE INDEX "users_created_status_idx" ON "users" ("created_at" DESC, "status" ASC)`,
			},
		},
		{
			name:    "mysql indexes",
			dialect: "mysql",
			fragments: []string{
				"CREATE UNIQUE INDEX `users_email_idx` ON `users` (`email` ASC)",
				"CREATE INDEX `users_created_status_idx` ON `users` (`created_at` DESC, `status` ASC)",
			},
		},
		{
			name:    "sqlite indexes",
			dialect: "sqlite",
			fragments: []string{
				`CREATE UNIQUE INDEX "users_email_idx" ON "users" ("email" ASC)`,
				`CREATE INDEX "users_created_status_idx" ON "users" ("created_at" DESC, "status" ASC)`,
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			db, err := rain.OpenDialect(tc.dialect)
			if err != nil {
				t.Fatalf("OpenDialect(%q): %v", tc.dialect, err)
			}

			statements, err := db.CreateIndexesSQL(users)
			if err != nil {
				t.Fatalf("CreateIndexesSQL: %v", err)
			}
			if len(statements) != len(tc.fragments) {
				t.Fatalf("expected %d index statements, got %d", len(tc.fragments), len(statements))
			}
			for idx, fragment := range tc.fragments {
				if statements[idx] != fragment {
					t.Fatalf("unexpected index SQL at %d:\nwant: %s\ngot:  %s", idx, fragment, statements[idx])
				}
			}
		})
	}
}
