package rain_test

import (
	"strings"
	"testing"

	"github.com/hyperlocalise/rain-orm/pkg/dialect"
	"github.com/hyperlocalise/rain-orm/pkg/rain"
	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

func TestInsertUpdateDeleteToSQL(t *testing.T) {
	t.Parallel()

	db, err := rain.OpenDialect("postgres")
	if err != nil {
		t.Fatalf("OpenDialect returned error: %v", err)
	}
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

func TestInsertUpdateSetExpressionToSQL(t *testing.T) {
	t.Parallel()

	db, err := rain.OpenDialect("postgres")
	if err != nil {
		t.Fatalf("OpenDialect returned error: %v", err)
	}
	users, _ := defineTables()

	// Test Update Set expression
	updateSQL, updateArgs, err := db.Update().
		Table(users).
		Set(users.Name, schema.Raw("UPPER(name)")).
		Where(users.ID.Eq(int64(1))).
		ToSQL()
	if err != nil {
		t.Fatalf("update ToSQL failed: %v", err)
	}
	wantUpdate := `UPDATE "users" SET "name" = UPPER(name) WHERE "users"."id" = $1`
	if updateSQL != wantUpdate {
		t.Errorf("unexpected update SQL:\nwant: %s\ngot:  %s", wantUpdate, updateSQL)
	}
	if len(updateArgs) != 1 || updateArgs[0] != int64(1) {
		t.Errorf("unexpected update args: %#v", updateArgs)
	}

	// Test Insert Set expression
	insertSQL, insertArgs, err := db.Insert().
		Table(users).
		Set(users.Email, "alice@example.com").
		Set(users.Name, schema.Raw("UPPER(?)", "alice")).
		ToSQL()
	if err != nil {
		t.Fatalf("insert ToSQL failed: %v", err)
	}
	wantInsert := `INSERT INTO "users" ("email", "name") VALUES ($1, UPPER($2))`
	if insertSQL != wantInsert {
		t.Errorf("unexpected insert SQL:\nwant: %s\ngot:  %s", wantInsert, insertSQL)
	}
	if len(insertArgs) != 2 || insertArgs[0] != "alice@example.com" || insertArgs[1] != "alice" {
		t.Errorf("unexpected insert args: %#v", insertArgs)
	}
}

type modelWithExpr struct {
	ID   int64
	Name any
}

func TestInsertModelExpressionToSQL(t *testing.T) {
	t.Parallel()

	db, err := rain.OpenDialect("postgres")
	if err != nil {
		t.Fatalf("OpenDialect returned error: %v", err)
	}
	users, _ := defineTables()

	sql, args, err := db.Insert().
		Table(users).
		Model(&modelWithExpr{Name: schema.Raw("UPPER(?)", "alice")}).
		ToSQL()
	if err != nil {
		t.Fatalf("ToSQL failed: %v", err)
	}

	wantSQL := `INSERT INTO "users" ("name") VALUES (UPPER($1))`
	if sql != wantSQL {
		t.Errorf("unexpected SQL:\nwant: %s\ngot:  %s", wantSQL, sql)
	}
	if len(args) != 1 || args[0] != "alice" {
		t.Errorf("expected args [alice], got %v", args)
	}
}

func TestDialectFeatures(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		dialect  string
		features dialect.Feature
		missing  []dialect.Feature
	}{
		{
			name:    "postgres",
			dialect: "postgres",
			features: dialect.FeatureInsertReturning |
				dialect.FeatureUpdateReturning |
				dialect.FeatureDeleteReturning |
				dialect.FeatureOffset |
				dialect.FeatureUpsert |
				dialect.FeatureCTE |
				dialect.FeatureDefaultPlaceholder |
				dialect.FeatureSavepoint |
				dialect.FeatureSelectLocking,
		},
		{
			name:     "mysql",
			dialect:  "mysql",
			features: dialect.FeatureOffset | dialect.FeatureUpsert | dialect.FeatureSavepoint | dialect.FeatureSelectLocking,
			missing: []dialect.Feature{
				dialect.FeatureInsertReturning,
				dialect.FeatureUpdateReturning,
				dialect.FeatureDeleteReturning,
				dialect.FeatureCTE,
				dialect.FeatureDefaultPlaceholder,
			},
		},
		{
			name:    "sqlite",
			dialect: "sqlite",
			features: dialect.FeatureInsertReturning |
				dialect.FeatureUpdateReturning |
				dialect.FeatureDeleteReturning |
				dialect.FeatureOffset |
				dialect.FeatureUpsert |
				dialect.FeatureSavepoint,
			missing: []dialect.Feature{
				dialect.FeatureCTE,
				dialect.FeatureDefaultPlaceholder,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			db, err := rain.OpenDialect(tc.dialect)
			if err != nil {
				t.Fatalf("OpenDialect returned error: %v", err)
			}
			got := db.Dialect().Features()
			if got != tc.features {
				t.Fatalf("unexpected features: want %b got %b", tc.features, got)
			}
			for _, feature := range tc.missing {
				if dialect.HasFeature(got, feature) {
					t.Fatalf("expected feature %b to be absent from %b", feature, got)
				}
			}
		})
	}
}

func TestOpenDialectUnknownDialectReturnsError(t *testing.T) {
	t.Parallel()

	db, err := rain.OpenDialect("postres")
	if err == nil {
		t.Fatalf("expected unsupported dialect error, got nil")
	}
	if db != nil {
		t.Fatalf("expected nil db for unsupported dialect")
	}
}

func TestReturningUnsupportedDialect(t *testing.T) {
	t.Parallel()

	db, err := rain.OpenDialect("mysql")
	if err != nil {
		t.Fatalf("OpenDialect returned error: %v", err)
	}
	users, _ := defineTables()

	_, _, err = db.Insert().
		Table(users).
		Set(users.Name, "Alice").
		Returning(users.ID).
		ToSQL()
	if err == nil || !strings.Contains(err.Error(), "insert queries do not support RETURNING") {
		t.Fatalf("expected insert RETURNING to fail on mysql dialect, got %v", err)
	}

	_, _, err = db.Update().
		Table(users).
		Set(users.Name, "Alice").
		Where(users.ID.Eq(int64(1))).
		Returning(users.ID).
		ToSQL()
	if err == nil || !strings.Contains(err.Error(), "update queries do not support RETURNING") {
		t.Fatalf("expected update RETURNING to fail on mysql dialect, got %v", err)
	}

	_, _, err = db.Delete().
		Table(users).
		Where(users.ID.Eq(int64(1))).
		Returning(users.ID).
		ToSQL()
	if err == nil || !strings.Contains(err.Error(), "delete queries do not support RETURNING") {
		t.Fatalf("expected delete RETURNING to fail on mysql dialect, got %v", err)
	}
}

func TestReturningSupportedOperations(t *testing.T) {
	t.Parallel()

	db, err := rain.OpenDialect("postgres")
	if err != nil {
		t.Fatalf("OpenDialect returned error: %v", err)
	}
	users, _ := defineTables()

	insertSQL, _, err := db.Insert().
		Table(users).
		Set(users.Name, "Alice").
		Returning(users.ID).
		ToSQL()
	if err != nil || !strings.Contains(insertSQL, "RETURNING") {
		t.Fatalf("expected insert RETURNING to compile, got sql=%q err=%v", insertSQL, err)
	}

	updateSQL, _, err := db.Update().
		Table(users).
		Set(users.Name, "Alice").
		Where(users.ID.Eq(int64(1))).
		Returning(users.ID).
		ToSQL()
	if err != nil || !strings.Contains(updateSQL, "RETURNING") {
		t.Fatalf("expected update RETURNING to compile, got sql=%q err=%v", updateSQL, err)
	}

	deleteSQL, _, err := db.Delete().
		Table(users).
		Where(users.ID.Eq(int64(1))).
		Returning(users.ID).
		ToSQL()
	if err != nil || !strings.Contains(deleteSQL, "RETURNING") {
		t.Fatalf("expected delete RETURNING to compile, got sql=%q err=%v", deleteSQL, err)
	}
}
