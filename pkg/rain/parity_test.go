package rain_test

import (
	"strings"
	"testing"
	"time"

	"github.com/hyperlocalise/rain-orm/pkg/rain"
	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

func TestDrizzleParityDDL(t *testing.T) {
	t.Parallel()

	type parityTable struct {
		schema.TableModel
		ShortCode *schema.Column[string]
		DailyTime *schema.Column[time.Time]
	}

	table := schema.Define("parity", func(t *parityTable) {
		t.ShortCode = t.Char("short_code", 3).NotNull()
		t.DailyTime = t.TimePrecision("daily_time", 3).NotNull()

		t.Index("partial_idx").On(t.ShortCode).Where(t.ShortCode.IsNotNull())
	})

	// Separate table for testing Nulls Order as it's not supported by MySQL
	nullsOrderTable := schema.Define("nulls_order_parity", func(t *parityTable) {
		t.ShortCode = t.Char("short_code", 3).NotNull()
		t.Index("ordered_nulls_idx").On(t.ShortCode.Asc().NullsFirst())
	})

	cases := []struct {
		name      string
		dialect   string
		fragments []string
	}{
		{
			name:    "postgres parity",
			dialect: "postgres",
			fragments: []string{
				`"short_code" CHAR(3) NOT NULL`,
				`"daily_time" TIME(3) NOT NULL`,
				`CREATE INDEX "partial_idx" ON "parity" ("short_code" ASC) WHERE "short_code" IS NOT NULL`,
			},
		},
		{
			name:    "mysql parity",
			dialect: "mysql",
			fragments: []string{
				"`short_code` CHAR(3) NOT NULL",
				"`daily_time` TIME(3) NOT NULL",
			},
		},
		{
			name:    "sqlite parity",
			dialect: "sqlite",
			fragments: []string{
				`"short_code" TEXT NOT NULL`,
				`"daily_time" TEXT NOT NULL`,
				`CREATE INDEX "partial_idx" ON "parity" ("short_code" ASC) WHERE "short_code" IS NOT NULL`,
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			db, err := rain.OpenDialect(tc.dialect)
			if err != nil {
				t.Fatalf("OpenDialect(%q): %v", tc.dialect, err)
			}

			tableSQL, err := db.CreateTableSQL(table)
			if err != nil {
				t.Fatalf("CreateTableSQL: %v", err)
			}

			indexSQLs, err := db.CreateIndexesSQL(table)
			if tc.dialect == "mysql" {
				if err == nil || !strings.Contains(err.Error(), "partial indexes are not supported") {
					t.Fatalf("expected error for partial index on MySQL, got: %v", err)
				}
				// Skip index fragments check for MySQL as index creation failed as expected
				indexSQLs = nil
			} else if err != nil {
				t.Fatalf("CreateIndexesSQL: %v", err)
			}

			allSQL := tableSQL + "\n" + strings.Join(indexSQLs, ";\n")

			for _, fragment := range tc.fragments {
				if !strings.Contains(allSQL, fragment) {
					t.Fatalf("expected SQL to contain %q, got:\n%s", fragment, allSQL)
				}
			}

			// Test Nulls Order where supported
			if tc.dialect != "mysql" {
				nullsSQLs, err := db.CreateIndexesSQL(nullsOrderTable)
				if err != nil {
					t.Fatalf("CreateIndexesSQL (NullsOrder): %v", err)
				}
				found := false
				for _, s := range nullsSQLs {
					if strings.Contains(s, "NULLS FIRST") {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("expected NULLS FIRST in index SQL, got: %v", nullsSQLs)
				}
			} else {
				_, err := db.CreateIndexesSQL(nullsOrderTable)
				if err == nil || !strings.Contains(err.Error(), "uses NULLS FIRST/LAST which is not supported by mysql dialect") {
					t.Fatalf("expected error for NULLS FIRST on MySQL, got: %v", err)
				}
			}
		})
	}
}

func TestUpdateQueryParity(t *testing.T) {
	t.Parallel()

	db, _ := rain.OpenDialect("postgres")
	users, _ := defineTables()

	t.Run("Model assignment", func(t *testing.T) {
		user := &userModel{
			Name:   "Updated Name",
			Active: true,
		}
		sql, args, err := db.Update().
			Table(users).
			Model(user).
			Where(users.ID.Eq(int64(1))).
			ToSQL()
		if err != nil {
			t.Fatalf("ToSQL: %v", err)
		}

		// id and email are Zero/empty, but Model() for UPDATE skips auto-increment (ID).
		// Email is not auto-increment, so it should be included as empty string.
		wantSQL := `UPDATE "users" SET "email" = $1, "name" = $2, "active" = $3 WHERE "users"."id" = $4`
		if sql != wantSQL {
			t.Fatalf("unexpected SQL:\nwant: %s\ngot:  %s", wantSQL, sql)
		}
		if len(args) != 4 || args[0] != "" || args[1] != "Updated Name" || args[2] != true || args[3] != int64(1) {
			t.Fatalf("unexpected args: %#v", args)
		}
	})

	t.Run("Values assignment", func(t *testing.T) {
		sql, args, err := db.Update().
			Table(users).
			Values(map[schema.ColumnReference]any{
				users.Name: "Values Name",
			}).
			Where(users.ID.Eq(int64(1))).
			ToSQL()
		if err != nil {
			t.Fatalf("ToSQL: %v", err)
		}

		wantSQL := `UPDATE "users" SET "name" = $1 WHERE "users"."id" = $2`
		if sql != wantSQL {
			t.Fatalf("unexpected SQL: %s", sql)
		}
		if len(args) != 2 || args[0] != "Values Name" || args[1] != int64(1) {
			t.Fatalf("unexpected args: %#v", args)
		}
	})

	t.Run("Merge Model and Set", func(t *testing.T) {
		user := &userModel{
			Name: "Should be overridden",
		}
		sql, args, err := db.Update().
			Table(users).
			Model(user).
			Set(users.Name, "Overridden").
			Where(users.ID.Eq(int64(1))).
			ToSQL()
		if err != nil {
			t.Fatalf("ToSQL: %v", err)
		}

		if !strings.Contains(sql, `"name" = $2`) || args[1] != "Overridden" {
			t.Fatalf("Set() did not override Model(): %s args=%#v", sql, args)
		}
	})
}
