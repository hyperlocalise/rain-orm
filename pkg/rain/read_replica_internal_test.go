package rain

import (
	"context"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"github.com/hyperlocalise/rain-orm/pkg/schema"
	_ "modernc.org/sqlite"
)

func openReplicaTestDB(t *testing.T, name string) *DB {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), name+".sqlite")
	db, err := Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite db %q: %v", name, err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	return db
}

func createInternalQuerySchemaForTables(
	t *testing.T,
	ctx context.Context,
	db *DB,
	users *internalQueryUsersTable,
	posts *internalQueryPostsTable,
) {
	t.Helper()

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

func insertReplicaTestUser(t *testing.T, ctx context.Context, db *DB, users *internalQueryUsersTable, email, name string) int64 {
	t.Helper()

	result, err := db.Insert().
		Table(users).
		Model(&internalInsertModel{Email: email, Name: name}).
		Exec(ctx)
	if err != nil {
		t.Fatalf("insert user %q: %v", email, err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id for %q: %v", email, err)
	}

	return id
}

func insertReplicaTestPost(t *testing.T, ctx context.Context, db *DB, posts *internalQueryPostsTable, userID int64, title string) {
	t.Helper()

	if _, err := db.Insert().
		Table(posts).
		Set(posts.UserID, userID).
		Set(posts.Title, title).
		Exec(ctx); err != nil {
		t.Fatalf("insert post %q: %v", title, err)
	}
}

func TestWithReplicasValidation(t *testing.T) {
	t.Parallel()

	primary, err := OpenDialect("sqlite")
	if err != nil {
		t.Fatalf("OpenDialect(sqlite): %v", err)
	}
	replica, err := OpenDialect("sqlite")
	if err != nil {
		t.Fatalf("OpenDialect(sqlite): %v", err)
	}
	mysqlReplica, err := OpenDialect("mysql")
	if err != nil {
		t.Fatalf("OpenDialect(mysql): %v", err)
	}

	if _, err := WithReplicas(nil, []*DB{replica}, nil); err == nil {
		t.Fatalf("expected nil primary validation error")
	}
	if _, err := WithReplicas(primary, nil, nil); err == nil {
		t.Fatalf("expected empty replica validation error")
	}
	if _, err := WithReplicas(primary, []*DB{nil}, nil); err == nil {
		t.Fatalf("expected nil replica validation error")
	}
	if _, err := WithReplicas(primary, []*DB{mysqlReplica}, nil); err == nil {
		t.Fatalf("expected mixed dialect validation error")
	}
}

func TestWithReplicasDefaultSelectorUsesOnlyReplicas(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	users, posts := defineInternalQueryTables()
	primary := openReplicaTestDB(t, "primary-default")
	replica1 := openReplicaTestDB(t, "replica-default-1")
	replica2 := openReplicaTestDB(t, "replica-default-2")

	for _, db := range []*DB{primary, replica1, replica2} {
		createInternalQuerySchemaForTables(t, ctx, db, users, posts)
	}

	insertReplicaTestUser(t, ctx, primary, users, "primary@example.com", "Primary")
	insertReplicaTestUser(t, ctx, replica1, users, "replica1-a@example.com", "Replica 1A")
	insertReplicaTestUser(t, ctx, replica1, users, "replica1-b@example.com", "Replica 1B")
	insertReplicaTestUser(t, ctx, replica2, users, "replica2-a@example.com", "Replica 2A")
	insertReplicaTestUser(t, ctx, replica2, users, "replica2-b@example.com", "Replica 2B")
	insertReplicaTestUser(t, ctx, replica2, users, "replica2-c@example.com", "Replica 2C")

	routed, err := WithReplicas(primary, []*DB{replica1, replica2}, nil)
	if err != nil {
		t.Fatalf("WithReplicas: %v", err)
	}

	for range 24 {
		count, err := routed.Select().Table(users).Count(ctx)
		if err != nil {
			t.Fatalf("Count: %v", err)
		}
		if count == 1 {
			t.Fatalf("expected default selector to avoid primary row count, got %d", count)
		}
		if count != 2 && count != 3 {
			t.Fatalf("expected replica row count, got %d", count)
		}
	}
}

func TestWithReplicasCustomSelectorAndPrimaryView(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	users, posts := defineInternalQueryTables()
	primary := openReplicaTestDB(t, "primary-custom")
	replica1 := openReplicaTestDB(t, "replica-custom-1")
	replica2 := openReplicaTestDB(t, "replica-custom-2")

	for _, db := range []*DB{primary, replica1, replica2} {
		createInternalQuerySchemaForTables(t, ctx, db, users, posts)
	}

	insertReplicaTestUser(t, ctx, primary, users, "primary@example.com", "Primary")
	insertReplicaTestUser(t, ctx, replica1, users, "replica1@example.com", "Replica 1")
	insertReplicaTestUser(t, ctx, replica2, users, "replica2@example.com", "Replica 2")

	routed, err := WithReplicas(primary, []*DB{replica1, replica2}, func(replicas []*DB) *DB {
		return replicas[1]
	})
	if err != nil {
		t.Fatalf("WithReplicas: %v", err)
	}

	var row internalUserRow
	if err := routed.Select().
		Table(users).
		Scan(ctx, &row); err != nil {
		t.Fatalf("replica select scan: %v", err)
	}
	if row.Email != "replica2@example.com" {
		t.Fatalf("expected custom selector to use replica2, got %#v", row)
	}

	row = internalUserRow{}
	if err := routed.Primary().Select().
		Table(users).
		Scan(ctx, &row); err != nil {
		t.Fatalf("primary select scan: %v", err)
	}
	if row.Email != "primary@example.com" {
		t.Fatalf("expected primary view to use primary, got %#v", row)
	}
}

func TestWithReplicasFallsBackWhenSelectorReturnsUnknownHandle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	users, posts := defineInternalQueryTables()
	primary := openReplicaTestDB(t, "primary-selector-fallback")
	replica := openReplicaTestDB(t, "replica-selector-fallback")
	unknown := openReplicaTestDB(t, "unknown-selector-fallback")

	for _, db := range []*DB{primary, replica, unknown} {
		createInternalQuerySchemaForTables(t, ctx, db, users, posts)
	}

	insertReplicaTestUser(t, ctx, primary, users, "primary@example.com", "Primary")
	insertReplicaTestUser(t, ctx, replica, users, "replica-a@example.com", "Replica A")
	insertReplicaTestUser(t, ctx, replica, users, "replica-b@example.com", "Replica B")
	insertReplicaTestUser(t, ctx, unknown, users, "unknown@example.com", "Unknown")

	routed, err := WithReplicas(primary, []*DB{replica}, func(_ []*DB) *DB {
		return unknown
	})
	if err != nil {
		t.Fatalf("WithReplicas: %v", err)
	}

	for range 8 {
		count, err := routed.Select().Table(users).Count(ctx)
		if err != nil {
			t.Fatalf("Count: %v", err)
		}
		if count != 2 {
			t.Fatalf("expected fallback to configured replica count 2, got %d", count)
		}
	}
}

func TestWithReplicasRelationLoadingUsesSelectedReplica(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	users, posts := defineInternalQueryTables()
	primary := openReplicaTestDB(t, "primary-relations")
	replica := openReplicaTestDB(t, "replica-relations")

	for _, db := range []*DB{primary, replica} {
		createInternalQuerySchemaForTables(t, ctx, db, users, posts)
	}

	primaryUserID := insertReplicaTestUser(t, ctx, primary, users, "shared@example.com", "Primary User")
	insertReplicaTestPost(t, ctx, primary, posts, primaryUserID, "primary-post")

	replicaUserID := insertReplicaTestUser(t, ctx, replica, users, "shared@example.com", "Replica User")
	insertReplicaTestPost(t, ctx, replica, posts, replicaUserID, "replica-post")

	routed, err := WithReplicas(primary, []*DB{replica}, func(replicas []*DB) *DB {
		return replicas[0]
	})
	if err != nil {
		t.Fatalf("WithReplicas: %v", err)
	}

	var rows []internalUserWithPostsRow
	if err := routed.Select().
		Table(users).
		WithRelations("posts").
		Scan(ctx, &rows); err != nil {
		t.Fatalf("select with relations: %v", err)
	}
	if len(rows) != 1 || len(rows[0].Posts) != 1 {
		t.Fatalf("expected one related post from replica, got %#v", rows)
	}
	if rows[0].Posts[0].Title != "replica-post" {
		t.Fatalf("expected replica relation row, got %#v", rows[0].Posts[0])
	}
}

func TestWithReplicasWritesUsePrimary(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	users, posts := defineInternalQueryTables()
	primary := openReplicaTestDB(t, "primary-writes")
	replica := openReplicaTestDB(t, "replica-writes")

	for _, db := range []*DB{primary, replica} {
		createInternalQuerySchemaForTables(t, ctx, db, users, posts)
	}

	routed, err := WithReplicas(primary, []*DB{replica}, nil)
	if err != nil {
		t.Fatalf("WithReplicas: %v", err)
	}

	var inserted internalUserRow
	if err := routed.Insert().
		Table(users).
		Model(&internalInsertModel{Email: "inserted@example.com", Name: "Inserted"}).
		Returning(users.ID, users.Email, users.Name).
		Scan(ctx, &inserted); err != nil {
		t.Fatalf("insert returning scan: %v", err)
	}

	var updated internalUserRow
	if err := routed.Update().
		Table(users).
		Set(users.Name, "Updated").
		Where(users.ID.Eq(inserted.ID)).
		Returning(users.ID, users.Email, users.Name).
		Scan(ctx, &updated); err != nil {
		t.Fatalf("update returning scan: %v", err)
	}
	if updated.Name != "Updated" {
		t.Fatalf("expected updated row from primary, got %#v", updated)
	}

	var deleted internalUserRow
	if err := routed.Delete().
		Table(users).
		Where(users.ID.Eq(inserted.ID)).
		Returning(users.ID, users.Email).
		Scan(ctx, &deleted); err != nil {
		t.Fatalf("delete returning scan: %v", err)
	}
	if deleted.Email != "inserted@example.com" {
		t.Fatalf("expected deleted row from primary, got %#v", deleted)
	}

	primaryCount, err := primary.Select().Table(users).Count(ctx)
	if err != nil {
		t.Fatalf("primary count after delete: %v", err)
	}
	replicaCount, err := replica.Select().Table(users).Count(ctx)
	if err != nil {
		t.Fatalf("replica count after delete: %v", err)
	}
	if primaryCount != 0 || replicaCount != 0 {
		t.Fatalf("expected writes to affect only primary and leave no rows, got primary=%d replica=%d", primaryCount, replicaCount)
	}
}

func TestWithReplicasTransactionsAndRawSQLUsePrimary(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	users, posts := defineInternalQueryTables()
	primary := openReplicaTestDB(t, "primary-tx")
	replica := openReplicaTestDB(t, "replica-tx")

	for _, db := range []*DB{primary, replica} {
		createInternalQuerySchemaForTables(t, ctx, db, users, posts)
	}

	insertReplicaTestUser(t, ctx, primary, users, "primary@example.com", "Primary")
	insertReplicaTestUser(t, ctx, replica, users, "replica-a@example.com", "Replica A")
	insertReplicaTestUser(t, ctx, replica, users, "replica-b@example.com", "Replica B")

	routed, err := WithReplicas(primary, []*DB{replica}, nil)
	if err != nil {
		t.Fatalf("WithReplicas: %v", err)
	}

	tx, err := routed.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	count, err := tx.Select().Table(users).Count(ctx)
	if err != nil {
		t.Fatalf("tx count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected tx read to use primary, got %d", count)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}

	if err := routed.RunInTx(ctx, func(tx *Tx) error {
		txCount, err := tx.Select().Table(users).Count(ctx)
		if err != nil {
			return err
		}
		if txCount != 1 {
			t.Fatalf("expected RunInTx read to use primary, got %d", txCount)
		}
		_, err = tx.Insert().Table(users).Model(&internalInsertModel{
			Email: "tx-write@example.com",
			Name:  "Tx Write",
		}).Exec(ctx)
		return err
	}); err != nil {
		t.Fatalf("RunInTx: %v", err)
	}

	row := routed.QueryRow(ctx, `SELECT COUNT(*) FROM users`)
	if row == nil {
		t.Fatalf("QueryRow returned nil")
	}
	var queryRowCount int
	if err := row.Scan(&queryRowCount); err != nil {
		t.Fatalf("scan QueryRow count: %v", err)
	}
	if queryRowCount != 2 {
		t.Fatalf("expected QueryRow to use primary, got %d", queryRowCount)
	}

	rows, err := routed.Query(ctx, `SELECT email FROM users ORDER BY id`)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	var emails []string
	for rows.Next() {
		var email string
		if err := rows.Scan(&email); err != nil {
			t.Fatalf("scan Query row: %v", err)
		}
		emails = append(emails, email)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows err: %v", err)
	}
	if len(emails) != 2 || emails[0] != "primary@example.com" || emails[1] != "tx-write@example.com" {
		t.Fatalf("expected Query to use primary, got %#v", emails)
	}

	if _, err := routed.Exec(ctx, `INSERT INTO users (email, name, active, created_at) VALUES (?, ?, ?, CURRENT_TIMESTAMP)`, "exec-write@example.com", "Exec Write", true); err != nil {
		t.Fatalf("Exec: %v", err)
	}

	primaryCount, err := primary.Select().Table(users).Count(ctx)
	if err != nil {
		t.Fatalf("primary count: %v", err)
	}
	replicaCount, err := replica.Select().Table(users).Count(ctx)
	if err != nil {
		t.Fatalf("replica count: %v", err)
	}
	if primaryCount != 3 || replicaCount != 2 {
		t.Fatalf("expected raw SQL and tx writes on primary, got primary=%d replica=%d", primaryCount, replicaCount)
	}
}

func TestWithReplicasSharesQueryCacheAndPrimaryView(t *testing.T) {
	t.Parallel()

	primary, err := OpenDialect("sqlite")
	if err != nil {
		t.Fatalf("OpenDialect(sqlite): %v", err)
	}
	replica, err := OpenDialect("sqlite")
	if err != nil {
		t.Fatalf("OpenDialect(sqlite): %v", err)
	}

	routed, err := WithReplicas(primary, []*DB{replica}, nil)
	if err != nil {
		t.Fatalf("WithReplicas: %v", err)
	}

	cache := NewMemoryQueryCache()
	routed.WithQueryCache(cache)

	if routed.Select().cache != cache {
		t.Fatalf("expected routed Select cache to be shared")
	}
	if routed.Primary().Select().cache != cache {
		t.Fatalf("expected primary view Select cache to be shared")
	}
	if primary.Select().cache != cache {
		t.Fatalf("expected primary handle Select cache to be shared")
	}
	if replica.Select().cache != cache {
		t.Fatalf("expected replica handle Select cache to be shared")
	}

	primaryView := routed.Primary()
	if primaryView == nil {
		t.Fatalf("expected Primary view")
	}
	if primaryView == routed {
		t.Fatalf("expected Primary to return a distinct view")
	}
	if primaryView.Primary() == primaryView {
		t.Fatalf("expected repeated Primary calls to return a fresh primary view")
	}
	if !primaryView.forcePrimaryReads {
		t.Fatalf("expected primary view to force primary reads")
	}
	if primaryView.replicaRoute != routed.replicaRoute {
		t.Fatalf("expected primary view to share replica route")
	}
	if primaryView.shared != routed.shared {
		t.Fatalf("expected primary view to share query cache state")
	}
}

func TestWithReplicasPreservesPreconfiguredReplicaCache(t *testing.T) {
	t.Parallel()

	primary, err := OpenDialect("sqlite")
	if err != nil {
		t.Fatalf("OpenDialect(sqlite): %v", err)
	}
	replica, err := OpenDialect("sqlite")
	if err != nil {
		t.Fatalf("OpenDialect(sqlite): %v", err)
	}

	replicaCache := NewMemoryQueryCache()
	replica.WithQueryCache(replicaCache)

	routed, err := WithReplicas(primary, []*DB{replica}, nil)
	if err != nil {
		t.Fatalf("WithReplicas: %v", err)
	}

	if routed.Select().cache != replicaCache {
		t.Fatalf("expected routed Select cache to preserve preconfigured replica cache")
	}
	if routed.Primary().Select().cache != replicaCache {
		t.Fatalf("expected primary view Select cache to preserve preconfigured replica cache")
	}
	if primary.Select().cache != replicaCache {
		t.Fatalf("expected primary handle Select cache to preserve preconfigured replica cache")
	}
}

func TestWithReplicasCloseDeduplicatesUnderlyingHandles(t *testing.T) {
	t.Parallel()

	primary, err := OpenDialect("sqlite")
	if err != nil {
		t.Fatalf("OpenDialect(sqlite): %v", err)
	}
	replica, err := OpenDialect("sqlite")
	if err != nil {
		t.Fatalf("OpenDialect(sqlite): %v", err)
	}

	routed, err := WithReplicas(primary, []*DB{replica, replica}, nil)
	if err != nil {
		t.Fatalf("WithReplicas: %v", err)
	}

	if got := len(routed.replicaRoute.all); got != 2 {
		t.Fatalf("expected two unique underlying handles, got %d", got)
	}
	if err := routed.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := routed.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestWithReplicasCloseSharesDedupAcrossViews(t *testing.T) {
	t.Parallel()

	primary, err := OpenDialect("sqlite")
	if err != nil {
		t.Fatalf("OpenDialect(sqlite): %v", err)
	}
	replica, err := OpenDialect("sqlite")
	if err != nil {
		t.Fatalf("OpenDialect(sqlite): %v", err)
	}

	routed, err := WithReplicas(primary, []*DB{replica}, nil)
	if err != nil {
		t.Fatalf("WithReplicas: %v", err)
	}

	primaryView := routed.Primary()
	if err := primaryView.Close(); err != nil {
		t.Fatalf("Primary().Close: %v", err)
	}
	if err := routed.Close(); err != nil {
		t.Fatalf("Close after Primary().Close: %v", err)
	}
}

func TestWithReplicasConcurrentReadsStayOnReplicas(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	users, posts := defineInternalQueryTables()
	primary := openReplicaTestDB(t, "primary-concurrent")
	replica1 := openReplicaTestDB(t, "replica-concurrent-1")
	replica2 := openReplicaTestDB(t, "replica-concurrent-2")

	for _, db := range []*DB{primary, replica1, replica2} {
		createInternalQuerySchemaForTables(t, ctx, db, users, posts)
	}

	insertReplicaTestUser(t, ctx, primary, users, "primary@example.com", "Primary")
	insertReplicaTestUser(t, ctx, replica1, users, "replica1-a@example.com", "Replica 1A")
	insertReplicaTestUser(t, ctx, replica1, users, "replica1-b@example.com", "Replica 1B")
	insertReplicaTestUser(t, ctx, replica2, users, "replica2-a@example.com", "Replica 2A")
	insertReplicaTestUser(t, ctx, replica2, users, "replica2-b@example.com", "Replica 2B")
	insertReplicaTestUser(t, ctx, replica2, users, "replica2-c@example.com", "Replica 2C")

	routed, err := WithReplicas(primary, []*DB{replica1, replica2}, nil)
	if err != nil {
		t.Fatalf("WithReplicas: %v", err)
	}

	errCh := make(chan error, 32)
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			count, err := routed.Select().Table(users).Count(ctx)
			if err != nil {
				errCh <- err
				return
			}
			if count != 2 && count != 3 {
				errCh <- &replicaCountError{count: count}
			}
		}()
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("concurrent routed read failed: %v", err)
		}
	}
}

type replicaCountError struct {
	count int64
}

func (e *replicaCountError) Error() string {
	return "unexpected replica count: " + strconv.FormatInt(e.count, 10)
}
