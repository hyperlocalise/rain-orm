package rain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

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

func TestPreparedSelectQueryExecutionPaths(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openInternalQueryDB(t)
	users, posts := defineInternalQueryTables()
	createInternalQuerySchema(t, ctx, db)

	for _, item := range []internalInsertModel{
		{Email: "alice@example.com", Name: "Alice", Active: true},
		{Email: "bob@example.com", Name: "Bob", Active: false},
	} {
		result, err := db.Insert().Table(users).Model(&item).Exec(ctx)
		if err != nil {
			t.Fatalf("insert user %s: %v", item.Email, err)
		}
		userID, err := result.LastInsertId()
		if err != nil {
			t.Fatalf("last insert id for %s: %v", item.Email, err)
		}
		if _, err := db.Insert().Table(posts).Set(posts.UserID, userID).Set(posts.Title, "post-"+item.Name).Exec(ctx); err != nil {
			t.Fatalf("insert post for %s: %v", item.Email, err)
		}
	}

	query := db.Select().
		Table(users).
		Where(users.Email.EqExpr(schema.Placeholder("email"))).
		Where(users.Active.EqExpr(schema.Placeholder("active"))).
		WithRelations("posts")

	prepared, err := query.Prepare(ctx)
	if err != nil {
		t.Fatalf("prepare select failed: %v", err)
	}
	defer func() {
		if err := prepared.Close(); err != nil {
			t.Fatalf("close prepared query failed: %v", err)
		}
	}()

	var rows []internalUserWithPostsRow
	if err := prepared.Scan(ctx, PreparedArgs{
		"email":  "alice@example.com",
		"active": true,
	}, &rows); err != nil {
		t.Fatalf("prepared scan failed: %v", err)
	}
	if len(rows) != 1 || len(rows[0].Posts) != 1 {
		t.Fatalf("unexpected prepared scan rows: %#v", rows)
	}

	count, err := prepared.Count(ctx, PreparedArgs{
		"email":  "alice@example.com",
		"active": true,
	})
	if err != nil {
		t.Fatalf("prepared count failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected prepared count 1, got %d", count)
	}

	exists, err := prepared.Exists(ctx, PreparedArgs{
		"email":  "alice@example.com",
		"active": true,
	})
	if err != nil {
		t.Fatalf("prepared exists failed: %v", err)
	}
	if !exists {
		t.Fatalf("expected prepared exists to be true")
	}

	if err := prepared.Scan(ctx, PreparedArgs{"email": "alice@example.com"}, &rows); err == nil || !strings.Contains(err.Error(), "missing prepared arg") {
		t.Fatalf("expected missing prepared arg error, got %v", err)
	}
	if err := prepared.Scan(ctx, PreparedArgs{"email": "alice@example.com", "active": true, "extra": 1}, &rows); err == nil || !strings.Contains(err.Error(), "unexpected prepared arg") {
		t.Fatalf("expected unexpected prepared arg error, got %v", err)
	}
}

func TestPreparedSelectQueryInTransactionLifecycle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openInternalQueryDB(t)
	users, _ := defineInternalQueryTables()
	createInternalQuerySchema(t, ctx, db)

	if _, err := db.Insert().Table(users).Model(&internalInsertModel{Email: "tx@example.com", Name: "Tx"}).Exec(ctx); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}

	prepared, err := tx.Select().
		Table(users).
		Where(users.Email.EqExpr(schema.Placeholder("email"))).
		Prepare(ctx)
	if err != nil {
		t.Fatalf("prepare in tx failed: %v", err)
	}

	var row internalUserRow
	if err := prepared.Scan(ctx, PreparedArgs{"email": "tx@example.com"}, &row); err != nil {
		t.Fatalf("prepared scan in tx failed: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit tx: %v", err)
	}
	if err := prepared.Scan(ctx, PreparedArgs{"email": "tx@example.com"}, &row); err == nil {
		t.Fatalf("expected prepared statement on committed tx to fail")
	}
	if err := prepared.Close(); err != nil {
		t.Fatalf("close after tx commit failed: %v", err)
	}
	if err := prepared.Close(); err != nil {
		t.Fatalf("second close failed: %v", err)
	}
}

func TestPreparedSelectQueryAllowsScanWhenPreparedCountIsUnsupported(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openInternalQueryDB(t)
	users, _ := defineInternalQueryTables()
	createInternalQuerySchema(t, ctx, db)

	for _, item := range []internalInsertModel{
		{Email: "group@example.com", Name: "Group", Active: true},
		{Email: "group-2@example.com", Name: "Group", Active: true},
	} {
		if _, err := db.Insert().Table(users).Model(&item).Exec(ctx); err != nil {
			t.Fatalf("insert user: %v", err)
		}
	}

	query := db.Select().
		Table(users).
		Column(users.Name, schema.Count().As("user_count")).
		Where(users.Active.EqExpr(schema.Placeholder("active"))).
		GroupBy(users.Name)

	prepared, err := query.Prepare(ctx)
	if err != nil {
		t.Fatalf("prepare grouped query failed: %v", err)
	}
	defer func() {
		if err := prepared.Close(); err != nil {
			t.Fatalf("close grouped prepared query failed: %v", err)
		}
	}()

	var rows []struct {
		Name      string `db:"name"`
		UserCount int64  `db:"user_count"`
	}
	if err := prepared.Scan(ctx, PreparedArgs{"active": true}, &rows); err != nil {
		t.Fatalf("prepared grouped scan failed: %v", err)
	}
	if len(rows) != 1 || rows[0].UserCount != 2 {
		t.Fatalf("unexpected grouped rows: %#v", rows)
	}
	if _, err := prepared.Count(ctx, PreparedArgs{"active": true}); err == nil || !strings.Contains(err.Error(), "aggregate helpers do not support DISTINCT, GROUP BY, or HAVING clauses") {
		t.Fatalf("expected prepared count grouped-query error, got %v", err)
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

	var postsWithAuthorPtr []internalPostWithAuthorPointerRow
	if err := db.Select().
		Table(posts).
		Where(posts.Title.Eq("Hello from Alice")).
		WithRelations("author").
		Scan(ctx, &postsWithAuthorPtr); err != nil {
		t.Fatalf("select with pointer author relation failed: %v", err)
	}
	if len(postsWithAuthorPtr) != 1 || postsWithAuthorPtr[0].Author == nil || postsWithAuthorPtr[0].Author.Email != "alice@example.com" {
		t.Fatalf("expected pointer author alice@example.com, got %#v", postsWithAuthorPtr)
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

	var usersWithPostPointers []internalUserWithPostPointersRow
	if err := db.Select().
		Table(users).
		Where(users.ID.Eq(aliceID)).
		WithRelations("posts").
		Scan(ctx, &usersWithPostPointers); err != nil {
		t.Fatalf("select with pointer posts relation failed: %v", err)
	}
	if len(usersWithPostPointers) != 1 || len(usersWithPostPointers[0].Posts) != 2 || usersWithPostPointers[0].Posts[0] == nil {
		t.Fatalf("expected pointer posts relation to populate, got %#v", usersWithPostPointers)
	}

	var nested []internalUserWithPostsAndAuthorsRow
	if err := db.Select().
		Table(users).
		Where(users.ID.Eq(aliceID)).
		WithRelations("posts.author").
		Scan(ctx, &nested); err != nil {
		t.Fatalf("select with nested relations failed: %v", err)
	}
	if len(nested) != 1 || len(nested[0].Posts) != 2 {
		t.Fatalf("expected nested relation rows, got %#v", nested)
	}
	for _, post := range nested[0].Posts {
		if post.Author == nil || post.Author.Email != "alice@example.com" {
			t.Fatalf("expected nested author alice@example.com, got %#v", post.Author)
		}
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

	err = db.Select().Table(users).WithRelations("posts.does_not_exist").Scan(ctx, &bad)
	if err == nil || !strings.Contains(err.Error(), "unknown relation") {
		t.Fatalf("expected unknown nested relation error, got %v", err)
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

func TestRelationLoadingChunksLargeINQueries(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openInternalQueryDB(t)
	users, posts := defineInternalQueryTables()
	createInternalQuerySchema(t, ctx, db)

	for idx := range relationBatchSize + 5 {
		result, err := db.Insert().
			Table(users).
			Set(users.Email, fmt.Sprintf("user-%d@example.com", idx)).
			Set(users.Name, fmt.Sprintf("User %d", idx)).
			Exec(ctx)
		if err != nil {
			t.Fatalf("insert user %d failed: %v", idx, err)
		}
		userID, err := result.LastInsertId()
		if err != nil {
			t.Fatalf("last insert id %d failed: %v", idx, err)
		}
		if _, err := db.Insert().Table(posts).Set(posts.UserID, userID).Set(posts.Title, fmt.Sprintf("Post %d", idx)).Exec(ctx); err != nil {
			t.Fatalf("insert post %d failed: %v", idx, err)
		}
	}

	runner := &countingRunner{base: db}
	query := &SelectQuery{runner: runner, dialect: db.Dialect()}

	var rows []internalUserWithPostsRow
	if err := query.Table(users).WithRelations("posts").Scan(ctx, &rows); err != nil {
		t.Fatalf("chunked relation load failed: %v", err)
	}
	if len(rows) != relationBatchSize+5 {
		t.Fatalf("expected %d users, got %d", relationBatchSize+5, len(rows))
	}
	if runner.queryCount != 3 {
		t.Fatalf("expected 3 query executions (base + 2 relation batches), got %d", runner.queryCount)
	}
}

func TestRelationElementTypeFromTypeHandlesPointerSlices(t *testing.T) {
	t.Parallel()

	users, _ := defineInternalQueryTables()
	db, err := OpenDialect("sqlite")
	if err != nil {
		t.Fatalf("OpenDialect(sqlite): %v", err)
	}

	parentsType := reflect.TypeFor[[]*internalUserWithPostPointersRow]()
	parentStructType, err := sliceParentStructType(parentsType)
	if err != nil {
		t.Fatalf("sliceParentStructType failed: %v", err)
	}

	relatedType, err := db.Select().relationElementTypeFromType(parentStructType, users.TableDef().Relations[0])
	if err != nil {
		t.Fatalf("relationElementTypeFromType failed: %v", err)
	}
	if relatedType != reflect.TypeFor[internalPostOnlyRow]() {
		t.Fatalf("expected related type %v, got %v", reflect.TypeFor[internalPostOnlyRow](), relatedType)
	}
}
