package rain_test

import (
	"context"
	"testing"
)

func BenchmarkSQLiteSelectPrimitiveScan(b *testing.B) {
	runSQLiteBenchmarkDatasets(b, func(b *testing.B, fixture *benchmarkFixture, dataset benchmarkDataset) {
		ctx := context.Background()
		b.Run("PrimitiveSlice", func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()

			for range b.N {
				var ids []int64
				if err := fixture.db.Select().
					Table(fixture.users).
					Column(fixture.users.ID).
					OrderBy(fixture.users.ID.Asc()).
					Scan(ctx, &ids); err != nil {
					b.Fatalf("primitive slice scan: %v", err)
				}
				if len(ids) != dataset.users {
					b.Fatalf("expected %d rows, got %d", dataset.users, len(ids))
				}
			}
		})

		b.Run("StructSlice", func(b *testing.B) {
			type userOnlyID struct {
				ID int64 `db:"id"`
			}
			b.ReportAllocs()
			b.ResetTimer()

			for range b.N {
				var rows []userOnlyID
				if err := fixture.db.Select().
					Table(fixture.users).
					Column(fixture.users.ID).
					OrderBy(fixture.users.ID.Asc()).
					Scan(ctx, &rows); err != nil {
					b.Fatalf("struct slice scan: %v", err)
				}
				if len(rows) != dataset.users {
					b.Fatalf("expected %d rows, got %d", dataset.users, len(rows))
				}
			}
		})
	})
}
