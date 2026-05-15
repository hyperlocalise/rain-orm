package rain_test

import (
	"strings"
	"testing"

	"github.com/hyperlocalise/rain-orm/pkg/rain"
	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

type generatedTable struct {
	schema.TableModel
	ID        *schema.Column[int64]
	FirstName *schema.Column[string]
	LastName  *schema.Column[string]
	FullName  *schema.Column[string]
}

func defineGeneratedTable() *generatedTable {
	return schema.Define("users", func(t *generatedTable) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.FirstName = t.VarChar("first_name", 100).NotNull()
		t.LastName = t.VarChar("last_name", 100).NotNull()
		// FullName = FirstName || ' ' || LastName
		t.FullName = t.VarChar("full_name", 201).GeneratedAlwaysAs(
			schema.Raw("first_name || ' ' || last_name"),
			true,
		)
	})
}

func TestGeneratedColumnDDL(t *testing.T) {
	t.Parallel()
	users := defineGeneratedTable()

	cases := []struct {
		dialect   string
		fragments []string
		expectErr bool
	}{
		{
			dialect: "postgres",
			fragments: []string{
				`"full_name" VARCHAR(201) GENERATED ALWAYS AS (first_name || ' ' || last_name) STORED`,
			},
		},
		{
			dialect: "mysql",
			fragments: []string{
				"`full_name` VARCHAR(201) GENERATED ALWAYS AS (first_name || ' ' || last_name) STORED",
			},
		},
		{
			dialect: "sqlite",
			fragments: []string{
				`"full_name" TEXT GENERATED ALWAYS AS (first_name || ' ' || last_name) STORED`,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.dialect, func(t *testing.T) {
			db, err := rain.OpenDialect(tc.dialect)
			if err != nil {
				t.Fatalf("OpenDialect(%q): %v", tc.dialect, err)
			}

			sql, err := db.CreateTableSQL(users)
			if tc.expectErr {
				if err == nil {
					t.Fatal("expected error but got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("CreateTableSQL: %v", err)
			}

			for _, fragment := range tc.fragments {
				if !strings.Contains(sql, fragment) {
					t.Errorf("expected SQL to contain %q, got:\n%s", fragment, sql)
				}
			}
		})
	}
}

func TestGeneratedColumnPostgresVirtualErr(t *testing.T) {
	t.Parallel()
	users := schema.Define("users", func(t *generatedTable) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.FullName = t.VarChar("full_name", 201).GeneratedAlwaysAs(
			schema.Raw("first_name || ' ' || last_name"),
			false, // Virtual
		)
	})

	db, _ := rain.OpenDialect("postgres")
	_, err := db.CreateTableSQL(users)
	if err == nil || !strings.Contains(err.Error(), "postgres: generated columns must be STORED") {
		t.Fatalf("expected error about STORED for postgres, got: %v", err)
	}
}

func TestGeneratedColumnInsertSkip(t *testing.T) {
	t.Parallel()
	users := defineGeneratedTable()

	db, _ := rain.OpenDialect("postgres")

	type User struct {
		FirstName string `db:"first_name"`
		LastName  string `db:"last_name"`
		FullName  string `db:"full_name"`
	}

	sql, args, err := db.Insert().
		Table(users).
		Model(&User{FirstName: "John", LastName: "Doe", FullName: "SHOULD BE SKIPPED"}).
		ToSQL()

	if err != nil {
		t.Fatalf("ToSQL failed: %v", err)
	}

	if strings.Contains(sql, "full_name") {
		t.Errorf("generated column should not be in INSERT SQL: %s", sql)
	}
	if len(args) != 2 {
		t.Errorf("expected 2 args (first_name, last_name), got %d", len(args))
	}
}

func TestGeneratedColumnManualAssignmentErr(t *testing.T) {
	t.Parallel()
	users := defineGeneratedTable()
	db, _ := rain.OpenDialect("postgres")

	_, _, err := db.Insert().
		Table(users).
		Set(users.FullName, "Manual").
		ToSQL()

	if err == nil || !strings.Contains(err.Error(), "cannot assign to generated column full_name") {
		t.Fatalf("expected error when manually assigning to generated column, got: %v", err)
	}

	_, _, err = db.Update().
		Table(users).
		Set(users.FullName, "Manual").
		Where(users.ID.Eq(int64(1))).
		ToSQL()

	if err == nil || !strings.Contains(err.Error(), "cannot assign to generated column full_name") {
		t.Fatalf("expected error when manually assigning to generated column in update, got: %v", err)
	}
}
