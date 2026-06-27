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

type ddlMembershipsTable struct {
	schema.TableModel
	UserID    *schema.Column[int64]
	AccountID *schema.Column[int64]
	Status    *schema.Column[string]
	Active    *schema.Column[bool]
}

func defineDDLTables() (*ddlUsersTable, *ddlPostsTable, *ddlMembershipsTable) {
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

	memberships := schema.Define("memberships", func(t *ddlMembershipsTable) {
		t.UserID = t.BigInt("user_id").NotNull()
		t.AccountID = t.BigInt("account_id").NotNull()
		t.Status = t.Enum("status", "active", "disabled").NotNull().Default("active")
		t.Active = t.Boolean("active").NotNull().Default(true)
		t.PrimaryKey("memberships_pkey").On(t.UserID, t.AccountID)
		t.Unique("memberships_user_status_key").On(t.UserID, t.Status)
		t.Check("memberships_active_check", schema.Or(t.Active.Eq(true), t.Status.Eq("disabled")))
		t.ForeignKey("memberships_user_fk").On(t.UserID).References(users.ID).OnDelete(schema.ForeignKeyActionCascade).OnUpdate(schema.ForeignKeyActionRestrict)
		t.Index("memberships_status_idx").On(t.Status, t.AccountID.Desc())
	})

	return users, posts, memberships
}

type ddlDefaultRawTable struct {
	schema.TableModel
	ID        *schema.Column[int64]
	CreatedAt *schema.Column[time.Time]
	Random    *schema.Column[float64]
}

func TestCreateTableSQLWithDefaultRaw(t *testing.T) {
	t.Parallel()

	table := schema.Define("default_raw_test", func(t *ddlDefaultRawTable) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.CreatedAt = t.TimestampTZ("created_at").NotNull().DefaultRaw(schema.Raw("now()"))
		t.Random = t.Double("random").NotNull().DefaultRaw(schema.Raw("random()"))
	})

	cases := []struct {
		name      string
		dialect   string
		fragments []string
	}{
		{
			name:    "postgres default raw",
			dialect: "postgres",
			fragments: []string{
				`"created_at" TIMESTAMPTZ NOT NULL DEFAULT now()`,
				`"random" DOUBLE PRECISION NOT NULL DEFAULT random()`,
			},
		},
		{
			name:    "mysql default raw",
			dialect: "mysql",
			fragments: []string{
				"`created_at` DATETIME NOT NULL DEFAULT now()",
				"`random` DOUBLE NOT NULL DEFAULT random()",
			},
		},
		{
			name:    "sqlite default raw",
			dialect: "sqlite",
			fragments: []string{
				`"created_at" TEXT NOT NULL DEFAULT now()`,
				`"random" REAL NOT NULL DEFAULT random()`,
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

			sql, err := db.CreateTableSQL(table)
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

func TestCreateTableSQLAcrossDialects(t *testing.T) {
	t.Parallel()

	users, posts, memberships := defineDDLTables()

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
		{
			name:    "postgres memberships constraints",
			dialect: "postgres",
			table:   memberships,
			fragments: []string{
				`CONSTRAINT "memberships_pkey" PRIMARY KEY ("user_id", "account_id")`,
				`CONSTRAINT "memberships_user_status_key" UNIQUE ("user_id", "status")`,
				`CONSTRAINT "memberships_active_check" CHECK (("active" = TRUE OR "status" = 'disabled'))`,
				`CONSTRAINT "memberships_user_fk" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON DELETE CASCADE ON UPDATE RESTRICT`,
			},
		},
		{
			name:    "mysql memberships constraints",
			dialect: "mysql",
			table:   memberships,
			fragments: []string{
				"CONSTRAINT `memberships_pkey` PRIMARY KEY (`user_id`, `account_id`)",
				"CONSTRAINT `memberships_user_status_key` UNIQUE (`user_id`, `status`)",
				"CONSTRAINT `memberships_active_check` CHECK ((`active` = 1 OR `status` = 'disabled'))",
				"CONSTRAINT `memberships_user_fk` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON DELETE CASCADE ON UPDATE RESTRICT",
			},
		},
		{
			name:    "sqlite memberships constraints",
			dialect: "sqlite",
			table:   memberships,
			fragments: []string{
				`CONSTRAINT "memberships_pkey" PRIMARY KEY ("user_id", "account_id")`,
				`CONSTRAINT "memberships_user_status_key" UNIQUE ("user_id", "status")`,
				`CONSTRAINT "memberships_active_check" CHECK (("active" = 1 OR "status" = 'disabled'))`,
				`CONSTRAINT "memberships_user_fk" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON DELETE CASCADE ON UPDATE RESTRICT`,
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
	users, _, _ := defineDDLTables()
	if _, err := nilDB.CreateTableSQL(users); err == nil {
		t.Fatalf("expected nil DB to fail")
	}

	ambiguous := schema.Define("ambiguous_keys", func(t *struct {
		schema.TableModel
		ID    *schema.Column[int64]
		Other *schema.Column[int64]
	},
	) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.Other = t.BigInt("other").NotNull()
		t.PrimaryKey("ambiguous_keys_pkey").On(t.ID, t.Other)
	})
	if _, err := db.CreateTableSQL(ambiguous); err == nil {
		t.Fatalf("expected mixed column and table primary keys to fail")
	}

	invalidAction := schema.Define("invalid_fk_action", func(t *struct {
		schema.TableModel
		ID     *schema.Column[int64]
		UserID *schema.Column[int64]
	},
	) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.UserID = t.BigInt("user_id").NotNull()
		t.ForeignKey("invalid_fk_action_user_fk").On(t.UserID).References(users.ID).OnDelete(schema.ForeignKeyAction("INVALID"))
	})
	if _, err := db.CreateTableSQL(invalidAction); err == nil {
		t.Fatalf("expected invalid foreign key action to fail")
	}

	missingReference := schema.Define("missing_fk_reference", func(t *struct {
		schema.TableModel
		ID     *schema.Column[int64]
		UserID *schema.Column[int64]
	},
	) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.UserID = t.BigInt("user_id").NotNull()
		t.ForeignKey("missing_fk_reference_user_fk").On(t.UserID)
	})
	if _, err := db.CreateTableSQL(missingReference); err == nil {
		t.Fatalf("expected missing foreign key reference columns to fail")
	}

	mismatchedReference := schema.Define("mismatched_fk_reference", func(t *struct {
		schema.TableModel
		ID        *schema.Column[int64]
		UserID    *schema.Column[int64]
		AccountID *schema.Column[int64]
	},
	) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.UserID = t.BigInt("user_id").NotNull()
		t.AccountID = t.BigInt("account_id").NotNull()
		t.ForeignKey("mismatched_fk_reference_user_fk").On(t.UserID, t.AccountID).References(users.ID)
	})
	if _, err := db.CreateTableSQL(mismatchedReference); err == nil {
		t.Fatalf("expected mismatched foreign key arity to fail")
	}

	unsupportedCheck := schema.Define("unsupported_check", func(t *struct {
		schema.TableModel
		ID *schema.Column[int64]
	},
	) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.Check("unsupported_check_expr", schema.ComparisonExpr{
			Left:     schema.Raw("lower(?)", "value", "extra"),
			Operator: "=",
			Right:    schema.ValueExpr{Value: "x"},
		})
	})
	if _, err := db.CreateTableSQL(unsupportedCheck); err == nil || !strings.Contains(err.Error(), "unused args") {
		t.Fatalf("expected raw CHECK args mismatch to fail, got: %v", err)
	}

	crossTableCheck := schema.Define("cross_table_check", func(t *struct {
		schema.TableModel
		ID *schema.Column[int64]
	},
	) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.Check("cross_table_check_expr", users.ID.Eq(int64(1)))
	})
	if _, err := db.CreateTableSQL(crossTableCheck); err == nil {
		t.Fatalf("expected cross-table check expression to fail")
	}
}

func TestCreateTableSQLWithComplexCheckExpressions(t *testing.T) {
	t.Parallel()

	type ComplexCheckTable struct {
		schema.TableModel
		ID   *schema.Column[int64]
		Val1 *schema.Column[int32]
		Val2 *schema.Column[int32]
	}

	table := schema.Define("complex_check_test", func(t *ComplexCheckTable) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.Val1 = t.Integer("val1").NotNull()
		t.Val2 = t.Integer("val2").NotNull()
		t.Check("check1", t.Val1.AddExpr(t.Val2).Gt(int32(100)))
		t.Check("check2", schema.Case().When(t.Val1.Gt(int32(10)), t.Val2).Else(int32(0)).End().Lt(int32(50)))
		t.Check("check3", schema.Raw("ABS(?) < 10", t.Val1))
	})

	db, _ := rain.OpenDialect("postgres")
	sql, err := db.CreateTableSQL(table)
	if err != nil {
		t.Fatalf("CreateTableSQL: %v", err)
	}

	fragments := []string{
		`CONSTRAINT "check1" CHECK (("val1" + "val2") > 100)`,
		`CONSTRAINT "check2" CHECK ((CASE WHEN "val1" > 10 THEN "val2" ELSE 0 END) < 50)`,
		`CONSTRAINT "check3" CHECK (ABS("val1") < 10)`,
	}

	for _, fragment := range fragments {
		if !strings.Contains(sql, fragment) {
			t.Errorf("expected SQL to contain %q, got:\n%s", fragment, sql)
		}
	}
}

func TestCreateTableSQLCompositeBigSerialDoesNotEmitAutoIncrement(t *testing.T) {
	t.Parallel()

	table := schema.Define("composite_serials", func(t *struct {
		schema.TableModel
		ID       *schema.Column[int64]
		TenantID *schema.Column[int64]
	},
	) {
		t.ID = t.BigSerial("id")
		t.TenantID = t.BigInt("tenant_id").NotNull()
		t.PrimaryKey("composite_serials_pkey").On(t.ID, t.TenantID)
	})

	sqliteDB, err := rain.OpenDialect("sqlite")
	if err != nil {
		t.Fatalf("OpenDialect(sqlite): %v", err)
	}
	sqliteSQL, err := sqliteDB.CreateTableSQL(table)
	if err != nil {
		t.Fatalf("CreateTableSQL(sqlite): %v", err)
	}
	if strings.Contains(sqliteSQL, "AUTOINCREMENT") {
		t.Fatalf("expected composite SQLite primary key not to emit AUTOINCREMENT:\n%s", sqliteSQL)
	}
	execDB := openSQLiteTestDB(t)
	if _, err := execDB.Exec(context.Background(), sqliteSQL); err != nil {
		t.Fatalf("expected composite SQLite primary key DDL to execute: %v\nSQL:\n%s", err, sqliteSQL)
	}

	mysqlDB, err := rain.OpenDialect("mysql")
	if err != nil {
		t.Fatalf("OpenDialect(mysql): %v", err)
	}
	mysqlSQL, err := mysqlDB.CreateTableSQL(table)
	if err != nil {
		t.Fatalf("CreateTableSQL(mysql): %v", err)
	}
	if strings.Contains(mysqlSQL, "AUTO_INCREMENT") {
		t.Fatalf("expected composite MySQL primary key not to emit AUTO_INCREMENT:\n%s", mysqlSQL)
	}
}

func TestCreateTableSQLExecutesInSQLite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openSQLiteTestDB(t)
	users, posts, memberships := defineDDLTables()

	createUsersSQL, err := db.CreateTableSQL(users)
	if err != nil {
		t.Fatalf("CreateTableSQL(users): %v", err)
	}
	createPostsSQL, err := db.CreateTableSQL(posts)
	if err != nil {
		t.Fatalf("CreateTableSQL(posts): %v", err)
	}

	createMembershipsSQL, err := db.CreateTableSQL(memberships)
	if err != nil {
		t.Fatalf("CreateTableSQL(memberships): %v", err)
	}

	for _, statement := range []string{createUsersSQL, createPostsSQL, createMembershipsSQL} {
		if _, err := db.Exec(ctx, statement); err != nil {
			t.Fatalf("exec generated DDL failed: %v\nSQL:\n%s", err, statement)
		}
	}

	indexesSQL, err := db.CreateIndexesSQL(users)
	if err != nil {
		t.Fatalf("CreateIndexesSQL(users): %v", err)
	}
	membershipIndexesSQL, err := db.CreateIndexesSQL(memberships)
	if err != nil {
		t.Fatalf("CreateIndexesSQL(memberships): %v", err)
	}
	for _, statement := range append(indexesSQL, membershipIndexesSQL...) {
		if _, err := db.Exec(ctx, statement); err != nil {
			t.Fatalf("exec generated index DDL failed: %v\nSQL:\n%s", err, statement)
		}
	}
}

func TestCreateIndexesSQLAcrossDialects(t *testing.T) {
	t.Parallel()

	users, _, memberships := defineDDLTables()

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

	db, err := rain.OpenDialect("sqlite")
	if err != nil {
		t.Fatalf("OpenDialect(sqlite): %v", err)
	}
	statements, err := db.CreateIndexesSQL(memberships)
	if err != nil {
		t.Fatalf("CreateIndexesSQL(memberships): %v", err)
	}
	if len(statements) != 1 || statements[0] != `CREATE INDEX "memberships_status_idx" ON "memberships" ("status" ASC, "account_id" DESC)` {
		t.Fatalf("unexpected memberships index SQL: %#v", statements)
	}
}
