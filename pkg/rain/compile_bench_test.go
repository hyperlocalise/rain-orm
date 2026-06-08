package rain

import (
	"testing"

	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

func BenchmarkSelectToSQL(b *testing.B) {
	db, _ := OpenDialect("postgres")
	users, posts := defineInternalQueryTables()

	b.Run("Simple", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			_, _, _ = db.Select().
				Table(users).
				Where(users.ID.Eq(1)).
				ToSQL()
		}
	})

	b.Run("Complex", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			_, _, _ = db.Select().
				Table(users).
				Column(users.ID, users.Email, users.Name, users.Active).
				Join(posts, posts.UserID.EqCol(users.ID)).
				Where(users.Active.Eq(true)).
				Where(users.Email.Like("alice%")).
				OrderBy(users.CreatedAt.Desc()).
				Limit(10).
				ToSQL()
		}
	})

	b.Run("BulkColumns", func(b *testing.B) {
		// Simulate a table with many columns
		type LargeTable struct {
			schema.TableModel
			C1, C2, C3, C4, C5, C6, C7, C8, C9, C10 *schema.Column[int64]
			C11, C12, C13, C14, C15, C16, C17, C18, C19, C20 *schema.Column[int64]
		}
		lt := schema.Define("large_table", func(t *LargeTable) {
			t.C1 = t.BigInt("c1"); t.C2 = t.BigInt("c2"); t.C3 = t.BigInt("c3"); t.C4 = t.BigInt("c4"); t.C5 = t.BigInt("c5")
			t.C6 = t.BigInt("c6"); t.C7 = t.BigInt("c7"); t.C8 = t.BigInt("c8"); t.C9 = t.BigInt("c9"); t.C10 = t.BigInt("c10")
			t.C11 = t.BigInt("c11"); t.C12 = t.BigInt("c12"); t.C13 = t.BigInt("c13"); t.C14 = t.BigInt("c14"); t.C15 = t.BigInt("c15")
			t.C16 = t.BigInt("c16"); t.C17 = t.BigInt("c17"); t.C18 = t.BigInt("c18"); t.C19 = t.BigInt("c19"); t.C20 = t.BigInt("c20")
		})

		b.ReportAllocs()
		for range b.N {
			_, _, _ = db.Select().
				Table(lt).
				Column(lt.C1, lt.C2, lt.C3, lt.C4, lt.C5, lt.C6, lt.C7, lt.C8, lt.C9, lt.C10).
				Column(lt.C11, lt.C12, lt.C13, lt.C14, lt.C15, lt.C16, lt.C17, lt.C18, lt.C19, lt.C20).
				Where(lt.C1.Eq(1)).
				ToSQL()
		}
	})
}
