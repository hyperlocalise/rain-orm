package rain

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hyperlocalise/rain-orm/pkg/dialect"
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
	ID       int64   `db:"id"`
	Email    string  `db:"email"`
	Name     string  `db:"name"`
	Active   bool    `db:"active"`
	Nickname *string `db:"nickname"`
}

type internalUserRow struct {
	ID       int64   `db:"id"`
	Email    string  `db:"email"`
	Name     string  `db:"name"`
	Nickname *string `db:"nickname"`
}

type internalPostWithAuthorRow struct {
	ID     int64           `db:"id"`
	UserID int64           `db:"user_id"`
	Title  string          `db:"title"`
	Author internalUserRow `rain:"relation:author"`
}

type internalUserWithPostsRow struct {
	ID    int64                 `db:"id"`
	Email string                `db:"email"`
	Name  string                `db:"name"`
	Posts []internalPostOnlyRow `rain:"relation:posts"`
}

type internalPostOnlyRow struct {
	ID     int64  `db:"id"`
	UserID int64  `db:"user_id"`
	Title  string `db:"title"`
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

func TestQueryExecutionPaths(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openInternalQueryDB(t)
	users, posts := defineInternalQueryTables()
	createInternalQuerySchema(t, ctx, db)

	insert := db.Insert().
		Table(users).
		Model(&internalInsertModel{Email: "alice@example.com", Name: "Alice"})
	result, err := insert.Exec(ctx)
	if err != nil {
		t.Fatalf("insert exec failed: %v", err)
	}
	insertedID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id failed: %v", err)
	}

	if _, err := db.Insert().
		Table(posts).
		Set(posts.UserID, insertedID).
		Set(posts.Title, "Hello").
		Exec(ctx); err != nil {
		t.Fatalf("insert post failed: %v", err)
	}

	count, err := db.Select().Table(users).Where(users.Active.Eq(true)).Count(ctx)
	if err != nil {
		t.Fatalf("count failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected count 1, got %d", count)
	}

	exists, err := db.Select().Table(users).Where(users.Email.Eq("alice@example.com")).Exists(ctx)
	if err != nil {
		t.Fatalf("exists failed: %v", err)
	}
	if !exists {
		t.Fatalf("expected row to exist")
	}

	var row internalUserRow
	if err := db.Select().
		Table(users).
		Where(users.ID.Eq(insertedID)).
		Scan(ctx, &row); err != nil {
		t.Fatalf("scan select failed: %v", err)
	}
	if row.Email != "alice@example.com" || row.Name != "Alice" {
		t.Fatalf("unexpected row: %#v", row)
	}

	if err := db.Update().
		Table(users).
		Set(users.Name, "Alice Updated").
		Where(users.ID.Eq(insertedID)).
		Returning(users.ID, users.Name).
		Scan(ctx, &row); err != nil {
		t.Fatalf("update returning scan failed: %v", err)
	}
	if row.ID != insertedID || row.Name != "Alice Updated" {
		t.Fatalf("unexpected updated row: %#v", row)
	}

	if err := db.Delete().
		Table(users).
		Where(users.ID.Eq(insertedID)).
		Returning(users.ID, users.Email).
		Scan(ctx, &row); err != nil {
		t.Fatalf("delete returning scan failed: %v", err)
	}
	if row.ID != insertedID || row.Email != "alice@example.com" {
		t.Fatalf("unexpected deleted row: %#v", row)
	}

	if _, err := db.Select().Table(users).Where(users.ID.Eq(insertedID)).Count(ctx); !errors.Is(err, sql.ErrNoRows) && err != nil {
		t.Fatalf("unexpected count error after delete: %v", err)
	}
}

func TestSelectQueryCacheHitMissExpiryAndBypass(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openInternalQueryDB(t)
	users, _ := defineInternalQueryTables()
	createInternalQuerySchema(t, ctx, db)

	cache := NewMemoryQueryCache()
	cache.now = func() time.Time { return time.Date(2026, 3, 29, 12, 0, 0, 0, time.UTC) }
	db.WithQueryCache(cache)

	if _, err := db.Insert().Table(users).Model(&internalInsertModel{Email: "cache@example.com", Name: "Cache"}).Exec(ctx); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	counter := &countingRunner{base: db}
	q := (&SelectQuery{runner: counter, dialect: db.Dialect(), cache: cache}).
		Table(users).
		Where(users.Email.Eq("cache@example.com")).
		Cache(QueryCacheOptions{TTL: time.Minute, Tags: []string{"users"}})

	var first []internalUserRow
	if err := q.Scan(ctx, &first); err != nil {
		t.Fatalf("first scan: %v", err)
	}
	if counter.queryCount != 1 {
		t.Fatalf("expected first query to hit DB once, got %d", counter.queryCount)
	}

	var second []internalUserRow
	if err := q.Scan(ctx, &second); err != nil {
		t.Fatalf("second scan: %v", err)
	}
	if counter.queryCount != 1 {
		t.Fatalf("expected second query to hit cache, got query count %d", counter.queryCount)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("cached scan mismatch:\nfirst=%#v\nsecond=%#v", first, second)
	}

	cache.now = func() time.Time { return time.Date(2026, 3, 29, 12, 2, 0, 0, time.UTC) }
	var third []internalUserRow
	if err := q.Scan(ctx, &third); err != nil {
		t.Fatalf("third scan after expiry: %v", err)
	}
	if counter.queryCount != 2 {
		t.Fatalf("expected expiry to force DB query, got %d", counter.queryCount)
	}

	var bypassed []internalUserRow
	if err := q.Cache(QueryCacheOptions{TTL: time.Minute, Tags: []string{"users"}, Bypass: true}).Scan(ctx, &bypassed); err != nil {
		t.Fatalf("bypass scan: %v", err)
	}
	if counter.queryCount != 3 {
		t.Fatalf("expected bypass to force DB query, got %d", counter.queryCount)
	}
}

func TestSelectQueryCacheArgsAndManualInvalidation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openInternalQueryDB(t)
	users, _ := defineInternalQueryTables()
	createInternalQuerySchema(t, ctx, db)
	db.WithQueryCache(NewMemoryQueryCache())

	for _, item := range []internalInsertModel{
		{Email: "alice@example.com", Name: "Alice"},
		{Email: "bob@example.com", Name: "Bob"},
	} {
		if _, err := db.Insert().Table(users).Model(&item).Exec(ctx); err != nil {
			t.Fatalf("insert user %s: %v", item.Email, err)
		}
	}

	counter := &countingRunner{base: db}
	queryFor := func(email string) *SelectQuery {
		return (&SelectQuery{runner: counter, dialect: db.Dialect(), cache: db.queryCache}).
			Table(users).
			Where(users.Email.Eq(email)).
			Cache(QueryCacheOptions{TTL: 5 * time.Minute, Tags: []string{"users"}})
	}

	var alice []internalUserRow
	if err := queryFor("alice@example.com").Scan(ctx, &alice); err != nil {
		t.Fatalf("alice query first run: %v", err)
	}
	if err := queryFor("alice@example.com").Scan(ctx, &alice); err != nil {
		t.Fatalf("alice query second run: %v", err)
	}
	if counter.queryCount != 1 {
		t.Fatalf("expected repeated identical args to hit cache, query count %d", counter.queryCount)
	}

	var bob []internalUserRow
	if err := queryFor("bob@example.com").Scan(ctx, &bob); err != nil {
		t.Fatalf("bob query first run: %v", err)
	}
	if counter.queryCount != 2 {
		t.Fatalf("expected different args to use different entry, query count %d", counter.queryCount)
	}

	if err := db.InvalidateQueryCache(ctx, "users"); err != nil {
		t.Fatalf("invalidate tag: %v", err)
	}
	if err := queryFor("alice@example.com").Scan(ctx, &alice); err != nil {
		t.Fatalf("alice query after invalidation: %v", err)
	}
	if counter.queryCount != 3 {
		t.Fatalf("expected invalidation miss, query count %d", counter.queryCount)
	}
}

func TestSelectQueryCacheDisabledKeepsNormalBehavior(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openInternalQueryDB(t)
	users, _ := defineInternalQueryTables()
	createInternalQuerySchema(t, ctx, db)
	if _, err := db.Insert().Table(users).Model(&internalInsertModel{Email: "nocache@example.com", Name: "No Cache"}).Exec(ctx); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	counter := &countingRunner{base: db}
	q := (&SelectQuery{runner: counter, dialect: db.Dialect()}).
		Table(users).
		Where(users.Email.Eq("nocache@example.com")).
		Cache(QueryCacheOptions{TTL: time.Minute, Tags: []string{"users"}})

	var rows []internalUserRow
	if err := q.Scan(ctx, &rows); err != nil {
		t.Fatalf("first uncached scan: %v", err)
	}
	if err := q.Scan(ctx, &rows); err != nil {
		t.Fatalf("second uncached scan: %v", err)
	}
	if counter.queryCount != 2 {
		t.Fatalf("expected uncached behavior without backend, query count %d", counter.queryCount)
	}
}

func TestBuildQueryCacheKeyIsStableForEquivalentArgs(t *testing.T) {
	t.Parallel()

	opts := normalizeQueryCacheOptions(QueryCacheOptions{TTL: time.Minute, Tags: []string{"users", "lookup"}, Namespace: "by-id"})
	keyOne, err := buildQueryCacheKey("sqlite", "SELECT * FROM users WHERE id = ?", []any{int64(1)}, nil, opts)
	if err != nil {
		t.Fatalf("build key one: %v", err)
	}
	keyTwo, err := buildQueryCacheKey("sqlite", "SELECT * FROM users WHERE id = ?", []any{int64(1)}, nil, opts)
	if err != nil {
		t.Fatalf("build key two: %v", err)
	}
	if keyOne != keyTwo {
		t.Fatalf("expected stable key, got %q and %q", keyOne, keyTwo)
	}
}

func TestSelectAggregateCacheForCountAndExists(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openInternalQueryDB(t)
	users, _ := defineInternalQueryTables()
	createInternalQuerySchema(t, ctx, db)
	db.WithQueryCache(NewMemoryQueryCache())

	if _, err := db.Insert().Table(users).Model(&internalInsertModel{Email: "agg@example.com", Name: "Agg"}).Exec(ctx); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	counter := &countingRunner{base: db}
	query := (&SelectQuery{runner: counter, dialect: db.Dialect(), cache: db.queryCache}).
		Table(users).
		Where(users.Email.Eq("agg@example.com")).
		Cache(QueryCacheOptions{TTL: time.Minute, Tags: []string{"users"}})

	count, err := query.Count(ctx)
	if err != nil {
		t.Fatalf("count first: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected count 1, got %d", count)
	}
	if _, err := query.Count(ctx); err != nil {
		t.Fatalf("count second: %v", err)
	}
	if counter.queryCount != 1 {
		t.Fatalf("expected second count to hit cache, query count %d", counter.queryCount)
	}

	exists, err := query.Exists(ctx)
	if err != nil {
		t.Fatalf("exists first: %v", err)
	}
	if !exists {
		t.Fatalf("expected exists=true")
	}
	if _, err := query.Exists(ctx); err != nil {
		t.Fatalf("exists second: %v", err)
	}
	if counter.queryCount != 2 {
		t.Fatalf("expected second exists to hit cache, query count %d", counter.queryCount)
	}
}

func TestQueryBuilderAndHelperErrors(t *testing.T) {
	t.Parallel()

	db, err := OpenDialect("postgres")
	if err != nil {
		t.Fatalf("OpenDialect returned error: %v", err)
	}
	users, posts := defineInternalQueryTables()

	if _, _, err := db.Select().ToSQL(); err == nil || !strings.Contains(err.Error(), "requires a table") {
		t.Fatalf("expected select table error, got %v", err)
	}
	selectNoRunner := &SelectQuery{dialect: db.Dialect(), table: tableDefSource{table: users.TableDef()}}
	if err := selectNoRunner.Scan(context.Background(), &internalUserRow{}); !errors.Is(err, ErrNoConnection) {
		t.Fatalf("expected select scan ErrNoConnection, got %v", err)
	}
	if _, err := (&SelectQuery{dialect: db.Dialect()}).Count(context.Background()); !errors.Is(err, ErrNoConnection) {
		t.Fatalf("expected select count ErrNoConnection, got %v", err)
	}
	if _, err := (&SelectQuery{dialect: db.Dialect()}).Exists(context.Background()); !errors.Is(err, ErrNoConnection) {
		t.Fatalf("expected select exists ErrNoConnection, got %v", err)
	}

	if _, _, err := db.Insert().ToSQL(); err == nil || !strings.Contains(err.Error(), "requires a table") {
		t.Fatalf("expected insert table error, got %v", err)
	}
	if _, _, err := db.Insert().Table(users).ToSQL(); err == nil || !strings.Contains(err.Error(), "requires either explicit values or a model") {
		t.Fatalf("expected insert values error, got %v", err)
	}
	if err := (&InsertQuery{dialect: db.Dialect(), table: users.TableDef(), returning: []schema.Expression{users.ID}}).Scan(context.Background(), &internalUserRow{}); !errors.Is(err, ErrNoConnection) {
		t.Fatalf("expected insert scan ErrNoConnection, got %v", err)
	}

	insertNoRunner := &InsertQuery{dialect: db.Dialect(), table: users.TableDef(), returning: []schema.Expression{users.ID}}
	if err := insertNoRunner.Scan(context.Background(), &internalUserRow{}); !errors.Is(err, ErrNoConnection) {
		t.Fatalf("expected insert returning scan ErrNoConnection, got %v", err)
	}
	insertNoReturning := &InsertQuery{runner: db, dialect: db.Dialect(), table: users.TableDef()}
	if err := insertNoReturning.Scan(context.Background(), &internalUserRow{}); err == nil || !strings.Contains(err.Error(), "requires RETURNING") {
		t.Fatalf("expected insert returning error, got %v", err)
	}

	if _, _, err := db.Update().ToSQL(); err == nil || !strings.Contains(err.Error(), "requires a table") {
		t.Fatalf("expected update table error, got %v", err)
	}
	if _, _, err := db.Update().Table(users).ToSQL(); err == nil || !strings.Contains(err.Error(), "requires at least one assignment") {
		t.Fatalf("expected update assignment error, got %v", err)
	}
	if _, _, err := db.Update().Table(users).Set(users.Name, "Alice").ToSQL(); err == nil || !strings.Contains(err.Error(), "requires at least one WHERE predicate") {
		t.Fatalf("expected update WHERE guard error, got %v", err)
	}
	if _, _, err := db.Update().Table(users).Set(users.Name, "Alice").Unbounded().ToSQL(); err != nil {
		t.Fatalf("expected unbounded update to succeed, got %v", err)
	}
	updateNoRunner := &UpdateQuery{dialect: db.Dialect(), table: users.TableDef(), values: []assignment{{column: users.Name, value: schema.ValueExpr{Value: "Alice"}}}, returning: []schema.Expression{users.ID}}
	if err := updateNoRunner.Scan(context.Background(), &internalUserRow{}); !errors.Is(err, ErrNoConnection) {
		t.Fatalf("expected update scan ErrNoConnection, got %v", err)
	}
	updateNoReturning := &UpdateQuery{runner: db, dialect: db.Dialect(), table: users.TableDef(), values: []assignment{{column: users.Name, value: schema.ValueExpr{Value: "Alice"}}}}
	if err := updateNoReturning.Scan(context.Background(), &internalUserRow{}); err == nil || !strings.Contains(err.Error(), "requires RETURNING") {
		t.Fatalf("expected update returning error, got %v", err)
	}

	if _, _, err := db.Delete().ToSQL(); err == nil || !strings.Contains(err.Error(), "requires a table") {
		t.Fatalf("expected delete table error, got %v", err)
	}
	if _, _, err := db.Delete().Table(users).ToSQL(); err == nil || !strings.Contains(err.Error(), "requires at least one WHERE predicate") {
		t.Fatalf("expected delete WHERE guard error, got %v", err)
	}
	if _, _, err := db.Delete().Table(users).Unbounded().ToSQL(); err != nil {
		t.Fatalf("expected unbounded delete to succeed, got %v", err)
	}
	deleteNoRunner := &DeleteQuery{dialect: db.Dialect(), table: users.TableDef(), returning: []schema.Expression{users.ID}}
	if err := deleteNoRunner.Scan(context.Background(), &internalUserRow{}); !errors.Is(err, ErrNoConnection) {
		t.Fatalf("expected delete scan ErrNoConnection, got %v", err)
	}
	deleteNoReturning := &DeleteQuery{runner: db, dialect: db.Dialect(), table: users.TableDef()}
	if err := deleteNoReturning.Scan(context.Background(), &internalUserRow{}); err == nil || !strings.Contains(err.Error(), "requires RETURNING") {
		t.Fatalf("expected delete returning error, got %v", err)
	}

	leftJoinSQL, _, err := db.Select().
		Table(users).
		Column(users.ID).
		LeftJoin(posts, users.ID.EqCol(posts.UserID)).
		Where(users.Active.Eq(true)).
		Where(users.Email.Eq("alice@example.com")).
		OrderBy(users.ID.Asc(), users.Email.Desc()).
		Limit(5).
		Offset(10).
		ToSQL()
	if err != nil {
		t.Fatalf("left join ToSQL failed: %v", err)
	}
	if !strings.Contains(leftJoinSQL, "LEFT JOIN") || !strings.Contains(leftJoinSQL, "OFFSET 10") {
		t.Fatalf("unexpected left join SQL: %s", leftJoinSQL)
	}
}

func TestSelectWithRelations(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openInternalQueryDB(t)
	users, posts := defineInternalQueryTables()
	createInternalQuerySchema(t, ctx, db)

	aliceResult, err := db.Insert().Table(users).Set(users.Email, "alice@example.com").Set(users.Name, "Alice").Exec(ctx)
	if err != nil {
		t.Fatalf("insert alice failed: %v", err)
	}
	aliceID, err := aliceResult.LastInsertId()
	if err != nil {
		t.Fatalf("alice last insert id failed: %v", err)
	}
	bobResult, err := db.Insert().Table(users).Set(users.Email, "bob@example.com").Set(users.Name, "Bob").Exec(ctx)
	if err != nil {
		t.Fatalf("insert bob failed: %v", err)
	}
	bobID, err := bobResult.LastInsertId()
	if err != nil {
		t.Fatalf("bob last insert id failed: %v", err)
	}

	if _, err := db.Insert().Table(posts).Set(posts.UserID, aliceID).Set(posts.Title, "Hello from Alice").Exec(ctx); err != nil {
		t.Fatalf("insert alice post failed: %v", err)
	}
	if _, err := db.Insert().Table(posts).Set(posts.UserID, aliceID).Set(posts.Title, "Second Alice Post").Exec(ctx); err != nil {
		t.Fatalf("insert alice post 2 failed: %v", err)
	}
	if _, err := db.Insert().Table(posts).Set(posts.UserID, bobID).Set(posts.Title, "Bob Post").Exec(ctx); err != nil {
		t.Fatalf("insert bob post failed: %v", err)
	}

	var postsWithAuthor []internalPostWithAuthorRow
	if err := db.Select().
		Table(posts).
		Where(posts.Title.Eq("Hello from Alice")).
		WithRelations("author").
		Scan(ctx, &postsWithAuthor); err != nil {
		t.Fatalf("select with author relation failed: %v", err)
	}
	if len(postsWithAuthor) != 1 {
		t.Fatalf("expected one post row, got %d", len(postsWithAuthor))
	}
	if postsWithAuthor[0].Author.Email != "alice@example.com" {
		t.Fatalf("expected author alice@example.com, got %#v", postsWithAuthor[0].Author)
	}

	var usersWithPosts []internalUserWithPostsRow
	if err := db.Select().
		Table(users).
		Where(users.ID.Eq(aliceID)).
		WithRelations("posts").
		Scan(ctx, &usersWithPosts); err != nil {
		t.Fatalf("select with posts relation failed: %v", err)
	}
	if len(usersWithPosts) != 1 {
		t.Fatalf("expected one user row, got %d", len(usersWithPosts))
	}
	if len(usersWithPosts[0].Posts) != 2 {
		t.Fatalf("expected two posts for alice, got %d", len(usersWithPosts[0].Posts))
	}

	var bad []internalUserRow
	err = db.Select().Table(users).WithRelations("does_not_exist").Scan(ctx, &bad)
	if err == nil || !strings.Contains(err.Error(), "unknown relation") {
		t.Fatalf("expected unknown relation error, got %v", err)
	}

	var empty []internalUserRow
	err = db.Select().Table(users).Where(users.ID.Eq(-999)).WithRelations("does_not_exist").Scan(ctx, &empty)
	if err == nil || !strings.Contains(err.Error(), "unknown relation") {
		t.Fatalf("expected unknown relation error for empty result, got %v", err)
	}
}

func TestRelationLoadingBatchesQueriesPerRelation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openInternalQueryDB(t)
	users, posts := defineInternalQueryTables()
	createInternalQuerySchema(t, ctx, db)

	aliceResult, err := db.Insert().Table(users).Set(users.Email, "alice@example.com").Set(users.Name, "Alice").Exec(ctx)
	if err != nil {
		t.Fatalf("insert alice failed: %v", err)
	}
	aliceID, err := aliceResult.LastInsertId()
	if err != nil {
		t.Fatalf("alice last insert id failed: %v", err)
	}
	bobResult, err := db.Insert().Table(users).Set(users.Email, "bob@example.com").Set(users.Name, "Bob").Exec(ctx)
	if err != nil {
		t.Fatalf("insert bob failed: %v", err)
	}
	bobID, err := bobResult.LastInsertId()
	if err != nil {
		t.Fatalf("bob last insert id failed: %v", err)
	}

	for _, row := range []struct {
		userID int64
		title  string
	}{
		{userID: aliceID, title: "Alice 1"},
		{userID: aliceID, title: "Alice 2"},
		{userID: bobID, title: "Bob 1"},
	} {
		if _, err := db.Insert().Table(posts).Set(posts.UserID, row.userID).Set(posts.Title, row.title).Exec(ctx); err != nil {
			t.Fatalf("insert post %q failed: %v", row.title, err)
		}
	}

	runner := &countingRunner{base: db}
	query := &SelectQuery{runner: runner, dialect: db.Dialect()}

	var rows []internalUserWithPostsRow
	if err := query.Table(users).WithRelations("posts").Scan(ctx, &rows); err != nil {
		t.Fatalf("relation batch scan failed: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 users, got %d", len(rows))
	}
	if runner.queryCount != 2 {
		t.Fatalf("expected 2 query executions (base + relation batch), got %d", runner.queryCount)
	}
	if len(runner.lastQueries) != 2 || !strings.Contains(runner.lastQueries[1], `IN (`) {
		t.Fatalf("expected relation load query with IN clause, got %#v", runner.lastQueries)
	}
}

func TestCompileContextAndAssignmentsHelpers(t *testing.T) {
	t.Parallel()

	users, posts := defineInternalQueryTables()

	ctx := newCompileContext(dialectForTest(t, "postgres"))
	if err := ctx.writeRaw(schema.Raw("NOW()")); err != nil {
		t.Fatalf("writeRaw without args failed: %v", err)
	}
	if ctx.String() != "NOW()" {
		t.Fatalf("unexpected raw SQL: %s", ctx.String())
	}

	ctx = newCompileContext(dialectForTest(t, "postgres"))
	if err := ctx.writeRaw(schema.Raw("? + ?", 1, 2)); err != nil {
		t.Fatalf("writeRaw placeholders failed: %v", err)
	}
	if ctx.String() != "$1 + $2" {
		t.Fatalf("unexpected placeholder SQL: %s", ctx.String())
	}

	if err := newCompileContext(dialectForTest(t, "postgres")).writeRaw(schema.Raw("?", 1, 2)); err == nil || !strings.Contains(err.Error(), "unused args") {
		t.Fatalf("expected raw unused args error, got %v", err)
	}
	if err := newCompileContext(dialectForTest(t, "postgres")).writeRaw(schema.Raw("? ?", 1)); err == nil || !strings.Contains(err.Error(), "placeholder count") {
		t.Fatalf("expected raw placeholder mismatch error, got %v", err)
	}
	if err := newCompileContext(dialectForTest(t, "postgres")).writeExpression(users.ID.In()); err == nil || !strings.Contains(err.Error(), "requires at least one value") {
		t.Fatalf("expected empty IN error, got %v", err)
	}
	if err := newCompileContext(dialectForTest(t, "postgres")).writeExpression(nil); err == nil || !strings.Contains(err.Error(), "unsupported expression type") {
		t.Fatalf("expected unsupported expression error, got %v", err)
	}

	merged, err := mergeAssignments(users.TableDef(),
		[]assignment{
			{column: users.Email, value: schema.ValueExpr{Value: "base@example.com"}},
			{column: users.Name, value: schema.ValueExpr{Value: "Base"}},
		},
		[]assignment{
			{column: users.Name, value: schema.ValueExpr{Value: "Override"}},
			{column: users.Active, value: schema.ValueExpr{Value: true}},
		},
	)
	if err != nil {
		t.Fatalf("mergeAssignments failed: %v", err)
	}
	if len(merged) != 3 {
		t.Fatalf("expected 3 merged assignments, got %d", len(merged))
	}
	if merged[1].column.ColumnDef().Name != "name" || merged[2].column.ColumnDef().Name != "active" {
		t.Fatalf("unexpected merged order: %#v", merged)
	}
	if merged[1].value.(schema.ValueExpr).Value != "Override" {
		t.Fatalf("expected override assignment to win, got %#v", merged[1].value)
	}

	if _, err := mergeAssignments(users.TableDef(), nil, []assignment{{column: posts.Title, value: schema.ValueExpr{Value: "bad"}}}); err == nil || !strings.Contains(err.Error(), "belongs to table posts") {
		t.Fatalf("expected foreign table assignment error, got %v", err)
	}

	ghostColumn := schema.Ref(&schema.ColumnDef{Table: users.TableDef(), Name: "ghost"})
	if _, err := mergeAssignments(users.TableDef(), nil, []assignment{{column: ghostColumn, value: schema.ValueExpr{Value: "bad"}}}); err == nil || !strings.Contains(err.Error(), "unknown column ghost") {
		t.Fatalf("expected unknown column assignment error, got %v", err)
	}

	if got := joinPredicates([]schema.Predicate{users.Active.Eq(true)}); got != users.Active.Eq(true) {
		t.Fatalf("expected single predicate to pass through")
	}
	if _, ok := joinPredicates([]schema.Predicate{users.Active.Eq(true), users.Email.Eq("alice@example.com")}).(schema.LogicalExpr); !ok {
		t.Fatalf("expected multiple predicates to produce logical expression")
	}
}

func TestModelAssignmentAndValueHelpers(t *testing.T) {
	t.Parallel()

	users, _ := defineInternalQueryTables()

	nickname := "ally"
	assignments, err := assignmentsFromModel(users.TableDef(), &internalInsertModel{
		ID:       0,
		Email:    "alice@example.com",
		Name:     "",
		Active:   false,
		Nickname: &nickname,
	}, true)
	if err != nil {
		t.Fatalf("assignmentsFromModel failed: %v", err)
	}
	if len(assignments) != 2 {
		t.Fatalf("expected 2 assignments after skipping default-backed zero values, got %d", len(assignments))
	}
	if assignments[0].column.ColumnDef().Name != "email" || assignments[1].column.ColumnDef().Name != "nickname" {
		t.Fatalf("unexpected assignments: %#v", assignments)
	}

	assignments, err = assignmentsFromModel(users.TableDef(), &internalInsertModel{
		ID:     42,
		Email:  "bob@example.com",
		Name:   "Bob",
		Active: true,
	}, false)
	if err != nil {
		t.Fatalf("assignmentsFromModel skipAuto=false failed: %v", err)
	}
	if len(assignments) != 4 {
		t.Fatalf("expected 4 assignments when auto id is retained, got %d", len(assignments))
	}

	if _, include := fieldValueForInsert(users.ID.ColumnDef(), reflect.ValueOf(int64(0)), true); include {
		t.Fatalf("expected zero auto-increment id to be skipped")
	}
	if _, include := fieldValueForInsert(users.Name.ColumnDef(), reflect.ValueOf(""), true); include {
		t.Fatalf("expected default-backed zero string to be skipped")
	}
	if _, include := fieldValueForInsert(users.Nickname.ColumnDef(), reflect.ValueOf((*string)(nil)), true); include {
		t.Fatalf("expected nil pointer to be skipped")
	}
	if value, include := fieldValueForInsert(users.Name.ColumnDef(), reflect.ValueOf("Alice"), true); !include || value != "Alice" {
		t.Fatalf("expected non-zero string to be included, got %#v include=%v", value, include)
	}

	type pointerHolder struct {
		Value **string
	}
	var nilStringPtr *string
	holder := pointerHolder{Value: &nilStringPtr}
	if _, isNil := dereferenceValue(reflect.ValueOf(holder).Field(0)); !isNil {
		t.Fatalf("expected nested nil pointer to be detected")
	}

	name := "Alice"
	namePtr := &name
	holder = pointerHolder{Value: &namePtr}
	resolved, isNil := dereferenceValue(reflect.ValueOf(holder).Field(0))
	if isNil || resolved.Kind() != reflect.String || resolved.String() != "Alice" {
		t.Fatalf("unexpected dereference result: %#v isNil=%v", resolved, isNil)
	}
}

func dialectForTest(t *testing.T, driver string) dialect.Dialect {
	t.Helper()

	db, err := OpenDialect(driver)
	if err != nil {
		t.Fatalf("OpenDialect returned error: %v", err)
	}

	return db.Dialect()
}
