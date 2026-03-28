package rain_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

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

func openSQLiteTestDB(t *testing.T) *rain.DB {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "rain.sqlite")
	db, err := rain.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	return db
}

func createSQLiteSchema(t *testing.T, ctx context.Context, db *rain.DB) {
	t.Helper()

	statements := []string{
		`CREATE TABLE users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			email TEXT NOT NULL,
			name TEXT NOT NULL DEFAULT 'guest',
			active BOOLEAN NOT NULL DEFAULT 1,
			nickname TEXT DEFAULT 'buddy',
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE posts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			title TEXT NOT NULL
		)`,
	}

	for _, statement := range statements {
		if _, err := db.Exec(ctx, statement); err != nil {
			t.Fatalf("exec schema statement %q: %v", statement, err)
		}
	}
}
