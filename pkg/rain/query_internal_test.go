package rain

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/hyperlocalise/rain-orm/pkg/schema"
	_ "modernc.org/sqlite"
)

type internalQueryUsersTable struct {
	schema.TableModel
	ID        *schema.Column[int64]
	Email     *schema.Column[string]
	Name      *schema.Column[string]
	Active    *schema.Column[bool]
	Nickname  *schema.Column[string]
	CreatedAt *schema.Column[time.Time]
}

type internalQueryPostsTable struct {
	schema.TableModel
	ID     *schema.Column[int64]
	UserID *schema.Column[int64]
	Title  *schema.Column[string]
}

type internalInsertModel struct {
	ID       int64
	Email    string
	Name     string
	Active   bool
	Nickname *string
}

type internalUserRow struct {
	ID       int64
	Email    string
	Name     string
	Nickname *string
}

type internalPostWithAuthorRow struct {
	ID     int64
	UserID int64
	Title  string
	Author internalUserRow `rain:"relation:author"`
}

type internalUserWithPostsRow struct {
	ID    int64
	Email string
	Name  string
	Posts []internalPostOnlyRow `rain:"relation:posts"`
}

type internalUserWithPostPointersRow struct {
	ID    int64
	Posts []*internalPostOnlyRow `rain:"relation:posts"`
}

type internalPostWithAuthorPointerRow struct {
	ID     int64
	UserID int64
	Title  string
	Author *internalUserRow `rain:"relation:author"`
}

type internalUserWithPostsAndAuthorsRow struct {
	ID    int64
	Email string
	Posts []internalPostWithAuthorPtrRow `rain:"relation:posts"`
}

type internalPostWithAuthorPtrRow struct {
	ID     int64
	UserID int64
	Title  string
	Author *internalUserRow `rain:"relation:author"`
}

type internalPostOnlyRow struct {
	ID     int64
	UserID int64
	Title  string
}

type countingRunner struct {
	base        queryRunner
	queryCount  int
	execCount   int
	lastQueries []string
}

func (r *countingRunner) execContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	r.execCount++
	return r.base.execContext(ctx, query, args...)
}

func (r *countingRunner) queryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	r.queryCount++
	r.lastQueries = append(r.lastQueries, query)
	return r.base.queryContext(ctx, query, args...)
}

func defineInternalQueryTables() (*internalQueryUsersTable, *internalQueryPostsTable) {
	users := schema.Define("users", func(t *internalQueryUsersTable) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.Email = t.VarChar("email", 255).NotNull()
		t.Name = t.Text("name").NotNull().Default("guest")
		t.Active = t.Boolean("active").NotNull().Default(true)
		t.Nickname = t.Text("nickname").Nullable().Default("buddy")
		t.CreatedAt = t.TimestampTZ("created_at").NotNull().DefaultNow()
	})

	posts := schema.Define("posts", func(t *internalQueryPostsTable) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.UserID = t.BigInt("user_id").NotNull().References(users.ID)
		t.Title = t.Text("title").NotNull()
		t.BelongsTo("author", t.UserID, users.ID)
	})
	users.HasMany("posts", users.ID, posts.UserID)

	return users, posts
}

func openInternalQueryDB(t *testing.T) *DB {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "query-internal.sqlite")
	db, err := Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	return db
}

func createInternalQuerySchema(t *testing.T, ctx context.Context, db *DB) {
	t.Helper()

	users, posts := defineInternalQueryTables()

	for _, table := range []schema.TableReference{users, posts} {
		statement, err := db.CreateTableSQL(table)
		if err != nil {
			t.Fatalf("compile schema for %q: %v", table.TableDef().Name, err)
		}
		if _, err := db.Exec(ctx, statement); err != nil {
			t.Fatalf("exec schema statement %q: %v", statement, err)
		}
	}
}
