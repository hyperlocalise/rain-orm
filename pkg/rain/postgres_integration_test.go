package rain_test

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hyperlocalise/rain-orm/pkg/rain"
	"github.com/hyperlocalise/rain-orm/pkg/schema"
	"github.com/jackc/pgx/v5/stdlib"
)

type postgresUsersTable struct {
	schema.TableModel
	ID        *schema.Column[int64]
	Email     *schema.Column[string]
	Name      *schema.Column[string]
	Active    *schema.Column[bool]
	Nickname  *schema.Column[string]
	CreatedAt *schema.Column[time.Time]
}

type postgresPostsTable struct {
	schema.TableModel
	ID     *schema.Column[int64]
	UserID *schema.Column[int64]
	Title  *schema.Column[string]
}

type postgresInsertModel struct {
	Email    string  `db:"email"`
	Name     string  `db:"name"`
	Active   bool    `db:"active"`
	Nickname *string `db:"nickname"`
}

func registerPostgresDriverForTests(tb testing.TB) {
	tb.Helper()

	if slices.Contains(sql.Drivers(), "postgres") {
		return
	}

	sql.Register("postgres", stdlib.GetDefaultDriver())
}

type postgresUserRow struct {
	ID        int64      `db:"id"`
	Email     string     `db:"email"`
	Name      string     `db:"name"`
	Active    bool       `db:"active"`
	Nickname  *string    `db:"nickname"`
	CreatedAt *time.Time `db:"created_at"`
}

func TestPostgresIntegrationInsertSelectAndJoin(t *testing.T) {
	t.Parallel()

	dsn, ok := postgresIntegrationDSN()
	if !ok {
		t.Skip("set RAIN_POSTGRES_DSN or RAIN_POSTGRES_HOST/RAIN_POSTGRES_USER/RAIN_POSTGRES_DB to run postgres integration tests")
	}

	ctx := context.Background()
	registerPostgresDriverForTests(t)
	db, err := rain.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open postgres db: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	users, posts := definePostgresTables(suffix)
	createPostgresSchema(t, ctx, db, users, posts)

	if _, err := db.Insert().
		Table(users).
		Model(&postgresInsertModel{Email: "defaults@example.com"}).
		Exec(ctx); err != nil {
		t.Fatalf("insert user with defaults: %v", err)
	}

	if _, err := db.Insert().
		Table(users).
		Model(&postgresInsertModel{Email: "override@example.com"}).
		Set(users.Name, "Alice").
		Set(users.Active, false).
		Set(users.Nickname, "ali").
		Exec(ctx); err != nil {
		t.Fatalf("insert user with explicit values: %v", err)
	}

	var first postgresUserRow
	if err := db.Select().
		Table(users).
		Where(users.Email.Eq("defaults@example.com")).
		Scan(ctx, &first); err != nil {
		t.Fatalf("scan default user: %v", err)
	}
	if first.Name != "guest" || !first.Active {
		t.Fatalf("unexpected default user values: %#v", first)
	}
	if first.Nickname == nil || *first.Nickname != "buddy" {
		t.Fatalf("unexpected default nickname: %#v", first.Nickname)
	}
	if first.CreatedAt == nil || first.CreatedAt.IsZero() {
		t.Fatalf("expected created_at to be populated")
	}

	var second postgresUserRow
	if err := db.Select().
		Table(users).
		Where(users.Email.Eq("override@example.com")).
		Scan(ctx, &second); err != nil {
		t.Fatalf("scan override user: %v", err)
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

	if _, err := db.Insert().
		Table(posts).
		Set(posts.UserID, first.ID).
		Set(posts.Title, "Hello").
		Exec(ctx); err != nil {
		t.Fatalf("insert post: %v", err)
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
		t.Fatalf("scan joined rows: %v", err)
	}
	if len(joined) != 1 || joined[0].Title != "Hello" || joined[0].Email != "defaults@example.com" {
		t.Fatalf("unexpected joined rows: %#v", joined)
	}
}

func postgresIntegrationDSN() (string, bool) {
	if dsn := strings.TrimSpace(os.Getenv("RAIN_POSTGRES_DSN")); dsn != "" {
		return dsn, true
	}

	host := strings.TrimSpace(os.Getenv("RAIN_POSTGRES_HOST"))
	user := strings.TrimSpace(os.Getenv("RAIN_POSTGRES_USER"))
	dbName := strings.TrimSpace(os.Getenv("RAIN_POSTGRES_DB"))
	if host == "" || user == "" || dbName == "" {
		return "", false
	}

	port := strings.TrimSpace(os.Getenv("RAIN_POSTGRES_PORT"))
	if port == "" {
		port = "5432"
	}

	sslmode := strings.TrimSpace(os.Getenv("RAIN_POSTGRES_SSLMODE"))
	if sslmode == "" {
		sslmode = "disable"
	}

	password := strings.TrimSpace(os.Getenv("RAIN_POSTGRES_PASSWORD"))
	query := url.Values{"sslmode": []string{sslmode}}
	dsn := &url.URL{
		Scheme:   "postgres",
		Host:     net.JoinHostPort(host, port),
		Path:     "/" + dbName,
		RawQuery: query.Encode(),
	}

	if password == "" {
		dsn.User = url.User(user)
		return dsn.String(), true
	}

	dsn.User = url.UserPassword(user, password)
	return dsn.String(), true
}

func definePostgresTables(suffix string) (*postgresUsersTable, *postgresPostsTable) {
	users := schema.Define("users_"+suffix, func(t *postgresUsersTable) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.Email = t.VarChar("email", 255).NotNull()
		t.Name = t.Text("name").NotNull().Default("guest")
		t.Active = t.Boolean("active").NotNull().Default(true)
		t.Nickname = t.Text("nickname").Nullable().Default("buddy")
		t.CreatedAt = t.TimestampTZ("created_at").NotNull().DefaultNow()
	})

	posts := schema.Define("posts_"+suffix, func(t *postgresPostsTable) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.UserID = t.BigInt("user_id").NotNull().References(users.ID)
		t.Title = t.Text("title").NotNull()
	})

	return users, posts
}

func createPostgresSchema(tb testing.TB, ctx context.Context, db *rain.DB, users *postgresUsersTable, posts *postgresPostsTable) {
	tb.Helper()

	for _, table := range []schema.TableReference{users, posts} {
		statement, err := db.CreateTableSQL(table)
		if err != nil {
			tb.Fatalf("compile schema for %q: %v", table.TableDef().Name, err)
		}
		if _, err := db.Exec(ctx, statement); err != nil {
			tb.Fatalf("exec schema statement %q: %v", statement, err)
		}
	}

	tb.Cleanup(func() {
		for _, table := range []schema.TableReference{posts, users} {
			_, _ = db.Exec(ctx, fmt.Sprintf(`DROP TABLE IF EXISTS "%s" CASCADE`, table.TableDef().Name))
		}
	})
}
