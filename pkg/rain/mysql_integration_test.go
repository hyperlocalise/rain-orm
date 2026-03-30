package rain_test

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/hyperlocalise/rain-orm/pkg/rain"
	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

type mysqlUsersTable struct {
	schema.TableModel
	ID        *schema.Column[int64]
	Email     *schema.Column[string]
	Name      *schema.Column[string]
	Active    *schema.Column[bool]
	Nickname  *schema.Column[string]
	CreatedAt *schema.Column[time.Time]
}

type mysqlPostsTable struct {
	schema.TableModel
	ID     *schema.Column[int64]
	UserID *schema.Column[int64]
	Title  *schema.Column[string]
}

type mysqlInsertModel struct {
	Email    string
	Name     rain.Set[string]
	Active   rain.Set[bool]
	Nickname *string
}

type mysqlUserRow struct {
	ID        int64
	Email     string
	Name      string
	Active    bool
	Nickname  *string
	CreatedAt string
}

func TestMySQLIntegrationInsertSelectAndJoin(t *testing.T) {
	t.Parallel()

	dsn, ok := mysqlIntegrationDSN()
	if !ok {
		t.Skip("set RAIN_MYSQL_DSN or RAIN_MYSQL_HOST/RAIN_MYSQL_USER/RAIN_MYSQL_DB to run mysql integration tests")
	}

	ctx := context.Background()
	db, err := rain.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("open mysql db: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	users, posts := defineMySQLTables(suffix)
	createMySQLSchema(t, ctx, db, users, posts)

	if _, err := db.Insert().
		Table(users).
		Model(&mysqlInsertModel{Email: "defaults@example.com"}).
		Exec(ctx); err != nil {
		t.Fatalf("insert user with defaults: %v", err)
	}

	if _, err := db.Insert().
		Table(users).
		Model(&mysqlInsertModel{
			Email:  "override@example.com",
			Name:   rain.Set[string]{Value: "Alice", Valid: true},
			Active: rain.Set[bool]{Value: false, Valid: true},
		}).
		Set(users.Name, "Alice").
		Set(users.Active, false).
		Set(users.Nickname, "ali").
		Exec(ctx); err != nil {
		t.Fatalf("insert user with explicit values: %v", err)
	}

	var first mysqlUserRow
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
	if first.CreatedAt == "" {
		t.Fatalf("expected created_at to be populated")
	}

	var second mysqlUserRow
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

func mysqlIntegrationDSN() (string, bool) {
	if dsn := strings.TrimSpace(os.Getenv("RAIN_MYSQL_DSN")); dsn != "" {
		return dsn, true
	}

	host := strings.TrimSpace(os.Getenv("RAIN_MYSQL_HOST"))
	user := strings.TrimSpace(os.Getenv("RAIN_MYSQL_USER"))
	dbName := strings.TrimSpace(os.Getenv("RAIN_MYSQL_DB"))
	if host == "" || user == "" || dbName == "" {
		return "", false
	}

	port := strings.TrimSpace(os.Getenv("RAIN_MYSQL_PORT"))
	if port == "" {
		port = "3306"
	}

	cfg := mysql.Config{
		Net:       "tcp",
		Addr:      net.JoinHostPort(host, port),
		User:      user,
		Passwd:    strings.TrimSpace(os.Getenv("RAIN_MYSQL_PASSWORD")),
		DBName:    dbName,
		ParseTime: true,
	}

	return cfg.FormatDSN(), true
}

func defineMySQLTables(suffix string) (*mysqlUsersTable, *mysqlPostsTable) {
	users := schema.Define("users_"+suffix, func(t *mysqlUsersTable) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.Email = t.VarChar("email", 255).NotNull()
		t.Name = t.VarChar("name", 255).NotNull().Default("guest")
		t.Active = t.Boolean("active").NotNull().Default(true)
		t.Nickname = t.VarChar("nickname", 255).Nullable().Default("buddy")
		t.CreatedAt = t.TimestampTZ("created_at").NotNull().DefaultNow()
	})

	posts := schema.Define("posts_"+suffix, func(t *mysqlPostsTable) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.UserID = t.BigInt("user_id").NotNull().References(users.ID)
		t.Title = t.Text("title").NotNull()
	})

	return users, posts
}

func createMySQLSchema(tb testing.TB, ctx context.Context, db *rain.DB, users *mysqlUsersTable, posts *mysqlPostsTable) {
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
			_, _ = db.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS `%s`", table.TableDef().Name))
		}
	})
}
