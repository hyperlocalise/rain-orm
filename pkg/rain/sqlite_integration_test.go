package rain_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hyperlocalise/rain-orm/pkg/dialect"
	"github.com/hyperlocalise/rain-orm/pkg/rain"
	"github.com/hyperlocalise/rain-orm/pkg/schema"
	_ "modernc.org/sqlite"
)

type sqliteUsersTable struct {
	schema.TableModel
	ID        *schema.Column[int64]
	Email     *schema.Column[string]
	Name      *schema.Column[string]
	Active    *schema.Column[bool]
	Nickname  *schema.Column[string]
	CreatedAt *schema.Column[time.Time]
}

type sqlitePostsTable struct {
	schema.TableModel
	ID     *schema.Column[int64]
	UserID *schema.Column[int64]
	Title  *schema.Column[string]
}

type sqliteInsertModel struct {
	Email    string  `db:"email"`
	Name     string  `db:"name"`
	Active   bool    `db:"active"`
	Nickname *string `db:"nickname"`
}

type sqliteUserRow struct {
	ID        int64   `db:"id"`
	Email     string  `db:"email"`
	Name      string  `db:"name"`
	Active    bool    `db:"active"`
	Nickname  *string `db:"nickname"`
	CreatedAt string  `db:"created_at"`
}

type joinedPostRow struct {
	Title string `db:"title"`
	Email string `db:"email"`
}

func defineSQLiteTables() (*sqliteUsersTable, *sqlitePostsTable) {
	users := schema.Define("users", func(t *sqliteUsersTable) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.Email = t.VarChar("email", 255).NotNull()
		t.Name = t.Text("name").NotNull().Default("guest")
		t.Active = t.Boolean("active").NotNull().Default(true)
		t.Nickname = t.Text("nickname").Nullable().Default("buddy")
		t.CreatedAt = t.TimestampTZ("created_at").NotNull().DefaultNow()
	})

	posts := schema.Define("posts", func(t *sqlitePostsTable) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.UserID = t.BigInt("user_id").NotNull().References(users.ID)
		t.Title = t.Text("title").NotNull()
	})

	return users, posts
}

func TestOpenUnknownDriverReturnsError(t *testing.T) {
	t.Parallel()

	db, err := rain.Open("definitely-missing-driver", "dsn")
	if err == nil {
		t.Fatalf("expected unknown driver error, got nil")
	}
	if db != nil {
		t.Fatalf("expected nil db when open fails")
	}
}

func TestOpenPostgresAliasReturnsHelpfulDriverError(t *testing.T) {
	t.Parallel()

	db, err := rain.Open("postgresql", "dsn")
	if err == nil {
		t.Fatalf("expected alias driver error, got nil")
	}
	if db != nil {
		t.Fatalf("expected nil db when open fails")
	}
	if !strings.Contains(err.Error(), `dialect "postgresql" maps to "postgres"`) {
		t.Fatalf("expected helpful alias message, got %v", err)
	}
}

func TestSQLiteIntegrationInsertDefaultsOverridesAndScan(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openSQLiteTestDB(t)
	users, posts := defineSQLiteTables()

	createSQLiteSchema(t, ctx, db)

	if _, err := db.Insert().
		Table(users).
		Model(&sqliteInsertModel{Email: "defaults@example.com"}).
		Exec(ctx); err != nil {
		t.Fatalf("default-backed insert failed: %v", err)
	}

	if _, err := db.Insert().
		Table(users).
		Model(&sqliteInsertModel{Email: "override@example.com"}).
		Set(users.Name, "Alice").
		Set(users.Active, false).
		Set(users.Nickname, "ali").
		Exec(ctx); err != nil {
		t.Fatalf("override insert failed: %v", err)
	}

	var first sqliteUserRow
	if err := db.Select().
		Table(users).
		Where(users.Email.Eq("defaults@example.com")).
		Scan(ctx, &first); err != nil {
		t.Fatalf("select first row failed: %v", err)
	}
	if first.Name != "guest" {
		t.Fatalf("expected default name guest, got %q", first.Name)
	}
	if !first.Active {
		t.Fatalf("expected default active=true")
	}
	if first.Nickname == nil || *first.Nickname != "buddy" {
		t.Fatalf("expected default nickname buddy, got %#v", first.Nickname)
	}
	if first.CreatedAt == "" {
		t.Fatalf("expected created_at to be populated")
	}

	var second sqliteUserRow
	if err := db.Select().
		Table(users).
		Where(users.Email.Eq("override@example.com")).
		Scan(ctx, &second); err != nil {
		t.Fatalf("select override row failed: %v", err)
	}
	if second.Name != "Alice" {
		t.Fatalf("expected override name Alice, got %q", second.Name)
	}
	if second.Active {
		t.Fatalf("expected explicit active=false override")
	}
	if second.Nickname == nil || *second.Nickname != "ali" {
		t.Fatalf("expected explicit nickname ali, got %#v", second.Nickname)
	}

	var allUsers []sqliteUserRow
	if err := db.Select().
		Table(users).
		OrderBy(users.ID.Asc()).
		Scan(ctx, &allUsers); err != nil {
		t.Fatalf("scan users slice failed: %v", err)
	}
	if len(allUsers) != 2 {
		t.Fatalf("expected 2 users, got %d", len(allUsers))
	}

	if _, err := db.Insert().
		Table(posts).
		Set(posts.UserID, first.ID).
		Set(posts.Title, "Hello").
		Exec(ctx); err != nil {
		t.Fatalf("insert post failed: %v", err)
	}

	u := schema.Alias(users, "u")
	p := schema.Alias(posts, "p")
	var joined []joinedPostRow
	if err := db.Select().
		Table(p).
		Column(p.Title, u.Email).
		Join(u, p.UserID.EqCol(u.ID)).
		Where(u.ID.Eq(first.ID)).
		Scan(ctx, &joined); err != nil {
		t.Fatalf("aliased join scan failed: %v", err)
	}
	if len(joined) != 1 || joined[0].Title != "Hello" || joined[0].Email != "defaults@example.com" {
		t.Fatalf("unexpected joined rows: %#v", joined)
	}
}

func TestSQLiteIntegrationDialectTypeRendering(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openSQLiteTestDB(t)

	sqliteDialect, err := dialect.GetDialect("sqlite")
	if err != nil {
		t.Fatalf("get sqlite dialect: %v", err)
	}

	statement := `CREATE TABLE dialect_types (
		ratio ` + sqliteDialect.DataType(schema.ColumnType{DataType: schema.TypeReal}) + ` NOT NULL,
		precise ` + sqliteDialect.DataType(schema.ColumnType{DataType: schema.TypeDouble}) + ` NOT NULL,
		amount ` + sqliteDialect.DataType(schema.ColumnType{DataType: schema.TypeDecimal, Precision: 12, Scale: 2}) + ` NOT NULL,
		created_at ` + sqliteDialect.DataType(schema.ColumnType{DataType: schema.TypeTimestampTZ}) + ` NOT NULL,
		metadata ` + sqliteDialect.DataType(schema.ColumnType{DataType: schema.TypeJSONB}) + `,
		external_id ` + sqliteDialect.DataType(schema.ColumnType{DataType: schema.TypeUUID}) + `,
		status ` + sqliteDialect.DataType(schema.ColumnType{DataType: schema.TypeEnum, EnumValues: []string{"draft", "published"}}) + `,
		payload ` + sqliteDialect.DataType(schema.ColumnType{DataType: schema.TypeBytes}) + `
	)`

	if _, err := db.Exec(ctx, statement); err != nil {
		t.Fatalf("create dialect_types table failed: %v", err)
	}

	rows, err := db.Query(ctx, `PRAGMA table_info(dialect_types)`)
	if err != nil {
		t.Fatalf("query pragma table_info: %v", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			t.Errorf("close pragma table_info rows: %v", err)
		}
	}()

	got := map[string]string{}
	for rows.Next() {
		var (
			cid        int
			name       string
			declared   string
			notNull    int
			defaultSQL any
			primaryKey int
		)
		if err := rows.Scan(&cid, &name, &declared, &notNull, &defaultSQL, &primaryKey); err != nil {
			t.Fatalf("scan pragma table_info row: %v", err)
		}
		got[name] = declared
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate pragma table_info rows: %v", err)
	}

	want := map[string]string{
		"ratio":       "REAL",
		"precise":     "REAL",
		"amount":      "REAL",
		"created_at":  "TEXT",
		"metadata":    "TEXT",
		"external_id": "TEXT",
		"status":      "TEXT",
		"payload":     "BLOB",
	}

	if len(got) != len(want) {
		t.Fatalf("expected %d columns, got %d: %#v", len(want), len(got), got)
	}
	for name, expectedType := range want {
		if got[name] != expectedType {
			t.Fatalf("column %q: want declared type %q got %q", name, expectedType, got[name])
		}
	}
}

func openSQLiteTestDB(tb testing.TB) *rain.DB {
	tb.Helper()

	dbPath := filepath.Join(tb.TempDir(), "rain.sqlite")
	db, err := rain.Open("sqlite", dbPath)
	if err != nil {
		tb.Fatalf("open sqlite db: %v", err)
	}
	tb.Cleanup(func() {
		_ = db.Close()
	})

	return db
}

func createSQLiteSchema(tb testing.TB, ctx context.Context, db *rain.DB) {
	tb.Helper()

	users, posts := defineSQLiteTables()

	for _, table := range []schema.TableReference{users, posts} {
		statement, err := db.CreateTableSQL(table)
		if err != nil {
			tb.Fatalf("compile schema for %q: %v", table.TableDef().Name, err)
		}
		if _, err := db.Exec(ctx, statement); err != nil {
			tb.Fatalf("exec schema statement %q: %v", statement, err)
		}
	}
}
