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
					Name:     fmt.Sprintf("Model User %d", idx),
					Active:   idx%2 == 0,
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

func newSQLiteBenchmarkFixture(b *testing.B, dataset benchmarkDataset) *benchmarkFixture {
	b.Helper()

	ctx := context.Background()
	db := openSQLiteTestDB(b)
	users, posts := defineSQLiteTables()
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
