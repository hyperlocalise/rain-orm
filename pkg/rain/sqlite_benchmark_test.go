package rain_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/hyperlocalise/rain-orm/pkg/rain"
	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

type benchmarkDataset struct {
	name  string
	users int
	posts int
}

type benchmarkFixture struct {
	db     *rain.DB
	users  *sqliteUsersTable
	posts  *sqlitePostsTable
	target int64
}

type benchmarkRichFixture struct {
	db          *rain.DB
	users       *sqliteRichUsersTable
	categories  *sqliteRichCategoriesTable
	posts       *sqliteRichPostsTable
	targetID    int64
	targetEmail string
	baseTime    time.Time
}

type benchmarkUserRow struct {
	ID       int64   `db:"id"`
	Email    string  `db:"email"`
	Name     string  `db:"name"`
	Active   bool    `db:"active"`
	Nickname *string `db:"nickname"`
}

type benchmarkJoinRow struct {
	Title string `db:"title"`
	Email string `db:"email"`
}

type benchmarkUserWithPostsRow struct {
	ID    int64                  `db:"id"`
	Posts []benchmarkPostOnlyRow `rain:"relation:posts"`
}

type benchmarkPostOnlyRow struct {
	ID     int64  `db:"id"`
	UserID int64  `db:"user_id"`
	Title  string `db:"title"`
}

type benchmarkRichGroupedRow struct {
	Status        string `db:"status"`
	PublishedPost int64  `db:"published_post_count"`
}

type benchmarkRichSummaryRow struct {
	Email     string `db:"email"`
	Status    string `db:"status"`
	PostCount int64  `db:"post_count"`
}

var benchmarkDatasets = []benchmarkDataset{
	{name: "small", users: 100, posts: 1000},
	{name: "medium", users: 1000, posts: 10000},
	{name: "large", users: 10000, posts: 100000},
}

func BenchmarkSQLiteInsertModel(b *testing.B) {
	runSQLiteBenchmarkDatasets(b, func(b *testing.B, fixture *benchmarkFixture, dataset benchmarkDataset) {
		ctx := context.Background()
		b.ReportAllocs()
		b.ResetTimer()

		for idx := range b.N {
			nickname := fmt.Sprintf("nickname-%s-%d", dataset.name, idx)
			if _, err := fixture.db.Insert().
				Table(fixture.users).
				Model(&sqliteInsertModel{
					Email:    fmt.Sprintf("model-%s-%d@example.com", dataset.name, idx),
					Name:     rain.Set[string]{Value: fmt.Sprintf("Model User %d", idx), Valid: true},
					Active:   rain.Set[bool]{Value: idx%2 == 0, Valid: true},
					Nickname: &nickname,
				}).
				Exec(ctx); err != nil {
				b.Fatalf("insert model: %v", err)
			}
		}
	})
}

func BenchmarkSQLiteInsertSet(b *testing.B) {
	runSQLiteBenchmarkDatasets(b, func(b *testing.B, fixture *benchmarkFixture, dataset benchmarkDataset) {
		ctx := context.Background()
		b.ReportAllocs()
		b.ResetTimer()

		for idx := range b.N {
			if _, err := fixture.db.Insert().
				Table(fixture.users).
				Set(fixture.users.Email, fmt.Sprintf("set-%s-%d@example.com", dataset.name, idx)).
				Set(fixture.users.Name, fmt.Sprintf("Set User %d", idx)).
				Set(fixture.users.Active, idx%2 == 0).
				Set(fixture.users.Nickname, fmt.Sprintf("set-nick-%d", idx)).
				Exec(ctx); err != nil {
				b.Fatalf("insert set: %v", err)
			}
		}
	})
}

func BenchmarkSQLiteSelectPointLookup(b *testing.B) {
	runSQLiteBenchmarkDatasets(b, func(b *testing.B, fixture *benchmarkFixture, _ benchmarkDataset) {
		ctx := context.Background()
		b.ReportAllocs()
		b.ResetTimer()

		for range b.N {
			var row benchmarkUserRow
			if err := fixture.db.Select().
				Table(fixture.users).
				Where(fixture.users.ID.Eq(fixture.target)).
				Scan(ctx, &row); err != nil {
				b.Fatalf("point lookup scan: %v", err)
			}
		}
	})
}

func BenchmarkSQLitePreparedSelectPointLookup(b *testing.B) {
	runSQLiteBenchmarkDatasets(b, func(b *testing.B, fixture *benchmarkFixture, _ benchmarkDataset) {
		ctx := context.Background()
		prepared, err := fixture.db.Select().
			Table(fixture.users).
			Where(fixture.users.ID.EqExpr(schema.Placeholder("id"))).
			Prepare(ctx)
		if err != nil {
			b.Fatalf("prepare point lookup: %v", err)
		}
		b.Cleanup(func() {
			if err := prepared.Close(); err != nil {
				b.Fatalf("close prepared point lookup: %v", err)
			}
		})

		b.ReportAllocs()
		b.ResetTimer()

		for range b.N {
			var row benchmarkUserRow
			if err := prepared.Scan(ctx, rain.PreparedArgs{"id": fixture.target}, &row); err != nil {
				b.Fatalf("prepared point lookup scan: %v", err)
			}
		}
	})
}

func BenchmarkSQLiteSelectFilteredSlice(b *testing.B) {
	runSQLiteBenchmarkDatasets(b, func(b *testing.B, fixture *benchmarkFixture, dataset benchmarkDataset) {
		ctx := context.Background()
		limit := min(dataset.users/2, 500)
		b.ReportAllocs()
		b.ResetTimer()

		for range b.N {
			rows := make([]benchmarkUserRow, 0, limit)
			if err := fixture.db.Select().
				Table(fixture.users).
				Where(fixture.users.Active.Eq(true)).
				OrderBy(fixture.users.ID.Asc()).
				Limit(limit).
				Scan(ctx, &rows); err != nil {
				b.Fatalf("filtered slice scan: %v", err)
			}
		}
	})
}

func BenchmarkSQLiteSelectBulkScan(b *testing.B) {
	runSQLiteBenchmarkDatasets(b, func(b *testing.B, fixture *benchmarkFixture, dataset benchmarkDataset) {
		ctx := context.Background()
		b.ReportAllocs()
		b.ResetTimer()

		for range b.N {
			rows := make([]benchmarkUserRow, 0, dataset.users)
			if err := fixture.db.Select().
				Table(fixture.users).
				OrderBy(fixture.users.ID.Asc()).
				Scan(ctx, &rows); err != nil {
				b.Fatalf("bulk scan: %v", err)
			}
		}
	})
}

func BenchmarkSQLiteSelectJoinScan(b *testing.B) {
	runSQLiteBenchmarkDatasets(b, func(b *testing.B, fixture *benchmarkFixture, dataset benchmarkDataset) {
		ctx := context.Background()
		u := schema.Alias(fixture.users, "u")
		p := schema.Alias(fixture.posts, "p")
		expectedRows := min(dataset.posts/2, 1000)
		b.ReportAllocs()
		b.ResetTimer()

		for range b.N {
			rows := make([]benchmarkJoinRow, 0, expectedRows)
			if err := fixture.db.Select().
				Table(p).
				Column(p.Title, u.Email).
				Join(u, p.UserID.EqCol(u.ID)).
				Where(u.Active.Eq(true)).
				OrderBy(p.ID.Asc()).
				Limit(expectedRows).
				Scan(ctx, &rows); err != nil {
				b.Fatalf("join scan: %v", err)
			}
		}
	})
}

func BenchmarkSQLiteSelectWithRelations(b *testing.B) {
	runSQLiteBenchmarkDatasets(b, func(b *testing.B, fixture *benchmarkFixture, dataset benchmarkDataset) {
		ctx := context.Background()
		limit := max(min(dataset.users/10, 100), 1)
		b.ReportAllocs()
		b.ResetTimer()

		for range b.N {
			rows := make([]benchmarkUserWithPostsRow, 0, limit)
			if err := fixture.db.Select().
				Table(fixture.users).
				OrderBy(fixture.users.ID.Asc()).
				Limit(limit).
				WithRelations("posts").
				Scan(ctx, &rows); err != nil {
				b.Fatalf("relation scan: %v", err)
			}
		}
	})
}

func BenchmarkSQLiteRichGroupedAggregateScan(b *testing.B) {
	runSQLiteRichBenchmarkDatasets(b, func(b *testing.B, fixture *benchmarkRichFixture, _ benchmarkDataset) {
		ctx := context.Background()
		b.ReportAllocs()
		b.ResetTimer()

		for range b.N {
			rows := make([]benchmarkRichGroupedRow, 0, 3)
			if err := fixture.db.Select().
				Table(fixture.users).
				Column(fixture.users.Status, schema.Count().As("published_post_count")).
				Join(fixture.posts, fixture.users.ID.EqCol(fixture.posts.UserID)).
				Where(fixture.posts.Published.Eq(true)).
				GroupBy(fixture.users.Status).
				OrderBy(fixture.users.Status.Asc()).
				Scan(ctx, &rows); err != nil {
				b.Fatalf("rich grouped aggregate scan: %v", err)
			}
		}
	})
}

func BenchmarkSQLiteRichSubqueryJoinScan(b *testing.B) {
	runSQLiteRichBenchmarkDatasets(b, func(b *testing.B, fixture *benchmarkRichFixture, dataset benchmarkDataset) {
		ctx := context.Background()
		postCounts := fixture.db.Select().
			Table(fixture.posts).
			Column(fixture.posts.UserID.As("user_id"), schema.Count().As("post_count")).
			GroupBy(fixture.posts.UserID)

		limit := max(min(dataset.users/10, 200), 1)
		b.ReportAllocs()
		b.ResetTimer()

		for range b.N {
			rows := make([]benchmarkRichSummaryRow, 0, limit)
			if err := fixture.db.Select().
				Table(fixture.users).
				Column(fixture.users.Email, fixture.users.Status, schema.Raw("pc.post_count").As("post_count")).
				JoinSubquery(postCounts, "pc", schema.ComparisonExpr{
					Left:     fixture.users.ID,
					Operator: "=",
					Right:    schema.Raw("pc.user_id"),
				}).
				OrderBy(fixture.users.ID.Asc()).
				Limit(limit).
				Scan(ctx, &rows); err != nil {
				b.Fatalf("rich subquery join scan: %v", err)
			}
		}
	})
}

func BenchmarkSQLiteRichSelectWithNestedRelations(b *testing.B) {
	runSQLiteRichBenchmarkDatasets(b, func(b *testing.B, fixture *benchmarkRichFixture, dataset benchmarkDataset) {
		ctx := context.Background()
		limit := max(min(dataset.users/10, 100), 1)
		b.ReportAllocs()
		b.ResetTimer()

		for range b.N {
			rows := make([]sqliteRichUserWithPostsRow, 0, limit)
			if err := fixture.db.Select().
				Table(fixture.users).
				OrderBy(fixture.users.ID.Asc()).
				Limit(limit).
				WithRelations("posts.author", "posts.category").
				Scan(ctx, &rows); err != nil {
				b.Fatalf("rich nested relation scan: %v", err)
			}
		}
	})
}

func BenchmarkSQLiteRichUpsert(b *testing.B) {
	runSQLiteRichBenchmarkDatasets(b, func(b *testing.B, fixture *benchmarkRichFixture, _ benchmarkDataset) {
		ctx := context.Background()
		b.ReportAllocs()
		b.ResetTimer()

		for idx := range b.N {
			status := [...]string{"trial", "active", "disabled"}[idx%3]
			if _, err := fixture.db.Insert().
				Table(fixture.users).
				Set(fixture.users.Email, fixture.targetEmail).
				Set(fixture.users.Name, fmt.Sprintf("Benchmark User %d", idx)).
				Set(fixture.users.Active, idx%2 == 0).
				Set(fixture.users.ExternalID, fmt.Sprintf("bench-upsert-%d", idx)).
				Set(fixture.users.Status, status).
				Set(fixture.users.UpdatedAt, fixture.baseTime.Add(time.Duration(idx)*time.Second)).
				OnConflict(fixture.users.Email).
				DoUpdateSet(
					fixture.users.Name,
					fixture.users.Active,
					fixture.users.ExternalID,
					fixture.users.Status,
					fixture.users.UpdatedAt,
				).
				Exec(ctx); err != nil {
				b.Fatalf("rich upsert exec: %v", err)
			}
		}
	})
}

func BenchmarkSQLiteRichUpdateReturningScan(b *testing.B) {
	runSQLiteRichBenchmarkDatasets(b, func(b *testing.B, fixture *benchmarkRichFixture, _ benchmarkDataset) {
		ctx := context.Background()
		b.ReportAllocs()
		b.ResetTimer()

		for idx := range b.N {
			var row sqliteRichUserMutationRow
			if err := fixture.db.Update().
				Table(fixture.users).
				Set(fixture.users.Name, fmt.Sprintf("Updated Benchmark User %d", idx)).
				Set(fixture.users.Status, [...]string{"trial", "active", "disabled"}[idx%3]).
				Set(fixture.users.UpdatedAt, fixture.baseTime.Add(time.Duration(idx)*time.Minute)).
				Where(fixture.users.ID.Eq(fixture.targetID)).
				Returning(
					fixture.users.ID,
					fixture.users.Email,
					fixture.users.Name,
					fixture.users.ExternalID,
					fixture.users.Status,
				).
				Scan(ctx, &row); err != nil {
				b.Fatalf("rich update returning scan: %v", err)
			}
		}
	})
}

func runSQLiteBenchmarkDatasets(
	b *testing.B,
	run func(b *testing.B, fixture *benchmarkFixture, dataset benchmarkDataset),
) {
	b.Helper()

	for _, dataset := range benchmarkDatasets {
		b.Run(dataset.name, func(b *testing.B) {
			fixture := newSQLiteBenchmarkFixture(b, dataset)
			run(b, fixture, dataset)
		})
	}
}

func runSQLiteRichBenchmarkDatasets(
	b *testing.B,
	run func(b *testing.B, fixture *benchmarkRichFixture, dataset benchmarkDataset),
) {
	b.Helper()

	for _, dataset := range benchmarkDatasets {
		b.Run(dataset.name, func(b *testing.B) {
			fixture := newSQLiteRichBenchmarkFixture(b, dataset)
			run(b, fixture, dataset)
		})
	}
}

func newSQLiteBenchmarkFixture(b *testing.B, dataset benchmarkDataset) *benchmarkFixture {
	b.Helper()

	ctx := context.Background()
	db := openSQLiteTestDB(b)
	users, posts, _ := defineSQLiteTables()
	createSQLiteSchema(b, ctx, db)
	seedSQLiteBenchmarkData(b, ctx, db, users, posts, dataset)
	validateSQLiteBenchmarkData(b, ctx, db, users, posts, dataset)

	return &benchmarkFixture{
		db:     db,
		users:  users,
		posts:  posts,
		target: int64(dataset.users/2 + 1),
	}
}

func newSQLiteRichBenchmarkFixture(b *testing.B, dataset benchmarkDataset) *benchmarkRichFixture {
	b.Helper()

	ctx := context.Background()
	db := openSQLiteTestDB(b)
	rich := defineSQLiteRichTables()
	createSQLiteRichSchema(b, ctx, db, rich)
	baseTime := time.Date(2026, time.March, 29, 0, 0, 0, 0, time.UTC)
	seedSQLiteRichBenchmarkData(b, ctx, db, rich, dataset, baseTime)
	validateSQLiteRichBenchmarkData(b, ctx, db, rich, dataset)

	return &benchmarkRichFixture{
		db:          db,
		users:       rich.users,
		categories:  rich.categories,
		posts:       rich.posts,
		targetID:    int64(dataset.users/2 + 1),
		targetEmail: fmt.Sprintf("rich-user-%06d@example.com", dataset.users/2+1),
		baseTime:    baseTime,
	}
}

func seedSQLiteBenchmarkData(
	b *testing.B,
	ctx context.Context,
	db *rain.DB,
	users *sqliteUsersTable,
	posts *sqlitePostsTable,
	dataset benchmarkDataset,
) {
	b.Helper()

	tx, err := db.Begin(ctx)
	if err != nil {
		b.Fatalf("begin seed transaction: %v", err)
	}
	defer func() {
		if tx == nil {
			return
		}
		if rollbackErr := tx.Rollback(); rollbackErr != nil && rollbackErr != rain.ErrNoConnection {
			b.Fatalf("rollback seed transaction: %v", rollbackErr)
		}
	}()

	const batchSize = 500
	createdAt := time.Date(2026, time.March, 29, 0, 0, 0, 0, time.UTC)

	for start := 0; start < dataset.users; start += batchSize {
		end := min(start+batchSize, dataset.users)
		rows := make([]map[schema.ColumnReference]any, 0, end-start)
		for idx := start; idx < end; idx++ {
			rows = append(rows, map[schema.ColumnReference]any{
				users.Email:     fmt.Sprintf("user-%06d@example.com", idx+1),
				users.Name:      fmt.Sprintf("User %06d", idx+1),
				users.Active:    idx%2 == 0,
				users.Nickname:  fmt.Sprintf("nick-%06d", idx+1),
				users.CreatedAt: createdAt.Add(time.Duration(idx) * time.Second),
			})
		}
		if _, err := tx.Insert().Table(users).Values(rows...).Exec(ctx); err != nil {
			b.Fatalf("seed users batch [%d:%d): %v", start, end, err)
		}
	}

	for start := 0; start < dataset.posts; start += batchSize {
		end := min(start+batchSize, dataset.posts)
		rows := make([]map[schema.ColumnReference]any, 0, end-start)
		for idx := start; idx < end; idx++ {
			userID := int64((idx % dataset.users) + 1)
			rows = append(rows, map[schema.ColumnReference]any{
				posts.UserID: userID,
				posts.Title:  fmt.Sprintf("Post %06d for user %06d", idx+1, userID),
			})
		}
		if _, err := tx.Insert().Table(posts).Values(rows...).Exec(ctx); err != nil {
			b.Fatalf("seed posts batch [%d:%d): %v", start, end, err)
		}
	}

	if err := tx.Commit(); err != nil {
		b.Fatalf("commit seed transaction: %v", err)
	}
	tx = nil
}

func seedSQLiteRichBenchmarkData(
	b *testing.B,
	ctx context.Context,
	db *rain.DB,
	fixture sqliteRichFixture,
	dataset benchmarkDataset,
	baseTime time.Time,
) {
	b.Helper()

	tx, err := db.Begin(ctx)
	if err != nil {
		b.Fatalf("begin rich seed transaction: %v", err)
	}
	defer func() {
		if tx == nil {
			return
		}
		if rollbackErr := tx.Rollback(); rollbackErr != nil && rollbackErr != rain.ErrNoConnection {
			b.Fatalf("rollback rich seed transaction: %v", rollbackErr)
		}
	}()

	const batchSize = 500
	categoryCount := max(min(dataset.users/20, 100), 5)
	statuses := [...]string{"trial", "active", "disabled"}

	categoryRows := make([]map[schema.ColumnReference]any, 0, categoryCount)
	for idx := 0; idx < categoryCount; idx++ {
		categoryRows = append(categoryRows, map[schema.ColumnReference]any{
			fixture.categories.Slug: fmt.Sprintf("category-%03d", idx+1),
			fixture.categories.Name: fmt.Sprintf("Category %03d", idx+1),
		})
	}
	if _, err := tx.Insert().Table(fixture.categories).Values(categoryRows...).Exec(ctx); err != nil {
		b.Fatalf("seed categories: %v", err)
	}

	for start := 0; start < dataset.users; start += batchSize {
		end := min(start+batchSize, dataset.users)
		rows := make([]map[schema.ColumnReference]any, 0, end-start)
		for idx := start; idx < end; idx++ {
			rows = append(rows, map[schema.ColumnReference]any{
				fixture.users.Email:      fmt.Sprintf("rich-user-%06d@example.com", idx+1),
				fixture.users.Name:       fmt.Sprintf("Rich User %06d", idx+1),
				fixture.users.Active:     idx%3 != 2,
				fixture.users.Nickname:   fmt.Sprintf("rich-nick-%06d", idx+1),
				fixture.users.ExternalID: fmt.Sprintf("external-%06d", idx+1),
				fixture.users.Status:     statuses[idx%len(statuses)],
				fixture.users.CreatedAt:  baseTime.Add(time.Duration(idx) * time.Minute),
				fixture.users.UpdatedAt:  baseTime.Add(time.Duration(idx) * time.Minute).Add(5 * time.Minute),
			})
		}
		if _, err := tx.Insert().Table(fixture.users).Values(rows...).Exec(ctx); err != nil {
			b.Fatalf("seed rich users batch [%d:%d): %v", start, end, err)
		}
	}

	for start := 0; start < dataset.posts; start += batchSize {
		end := min(start+batchSize, dataset.posts)
		rows := make([]map[schema.ColumnReference]any, 0, end-start)
		for idx := start; idx < end; idx++ {
			userID := int64((idx % dataset.users) + 1)
			categoryID := int64((idx % categoryCount) + 1)
			if idx%11 == 0 {
				categoryID = 0
			}
			row := map[schema.ColumnReference]any{
				fixture.posts.UserID:    userID,
				fixture.posts.Title:     fmt.Sprintf("Rich Post %06d", idx+1),
				fixture.posts.Body:      fmt.Sprintf("Body %06d with realistic payload text", idx+1),
				fixture.posts.Published: idx%2 == 0,
				fixture.posts.CreatedAt: baseTime.Add(time.Duration(idx) * time.Second),
			}
			if categoryID == 0 {
				row[fixture.posts.CategoryID] = nil
			} else {
				row[fixture.posts.CategoryID] = categoryID
			}
			rows = append(rows, row)
		}
		if _, err := tx.Insert().Table(fixture.posts).Values(rows...).Exec(ctx); err != nil {
			b.Fatalf("seed rich posts batch [%d:%d): %v", start, end, err)
		}
	}

	if err := tx.Commit(); err != nil {
		b.Fatalf("commit rich seed transaction: %v", err)
	}
	tx = nil
}

func validateSQLiteBenchmarkData(
	b *testing.B,
	ctx context.Context,
	db *rain.DB,
	users *sqliteUsersTable,
	posts *sqlitePostsTable,
	dataset benchmarkDataset,
) {
	b.Helper()

	userCount, err := db.Select().Table(users).Count(ctx)
	if err != nil {
		b.Fatalf("count users: %v", err)
	}
	if userCount != int64(dataset.users) {
		b.Fatalf("seeded users mismatch: got %d want %d", userCount, dataset.users)
	}

	postCount, err := db.Select().Table(posts).Count(ctx)
	if err != nil {
		b.Fatalf("count posts: %v", err)
	}
	if postCount != int64(dataset.posts) {
		b.Fatalf("seeded posts mismatch: got %d want %d", postCount, dataset.posts)
	}
}

func validateSQLiteRichBenchmarkData(
	b *testing.B,
	ctx context.Context,
	db *rain.DB,
	fixture sqliteRichFixture,
	dataset benchmarkDataset,
) {
	b.Helper()

	userCount, err := db.Select().Table(fixture.users).Count(ctx)
	if err != nil {
		b.Fatalf("count rich users: %v", err)
	}
	if userCount != int64(dataset.users) {
		b.Fatalf("seeded rich users mismatch: got %d want %d", userCount, dataset.users)
	}

	postCount, err := db.Select().Table(fixture.posts).Count(ctx)
	if err != nil {
		b.Fatalf("count rich posts: %v", err)
	}
	if postCount != int64(dataset.posts) {
		b.Fatalf("seeded rich posts mismatch: got %d want %d", postCount, dataset.posts)
	}

	categoryCount, err := db.Select().Table(fixture.categories).Count(ctx)
	if err != nil {
		b.Fatalf("count rich categories: %v", err)
	}
	expectedCategories := int64(max(min(dataset.users/20, 100), 5))
	if categoryCount != expectedCategories {
		b.Fatalf("seeded rich categories mismatch: got %d want %d", categoryCount, expectedCategories)
	}
}
