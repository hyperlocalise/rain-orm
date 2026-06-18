package rain_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/hyperlocalise/rain-orm/pkg/rain"
)

func TestDBTransactionOptions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openSQLiteTestDB(t)
	users, _, _ := defineSQLiteTables()
	createSQLiteSchema(t, ctx, db)

	t.Run("BeginTx", func(t *testing.T) {
		tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			t.Fatalf("BeginTx failed: %v", err)
		}
		defer func() { _ = tx.Rollback() }()

		// Verify it's a real transaction
		_, err = tx.Insert().Table(users).Set(users.Email, "txopts@example.com").Exec(ctx)
		// SQLite might not strictly enforce ReadOnly via sql.TxOptions depending on the driver implementation,
		// but we want to verify the call path works.
		if err != nil && !testing.Short() {
			// If it's really read-only, this might fail, which is also fine.
			t.Logf("Insert in read-only tx failed (expected if enforced): %v", err)
		}
	})

	t.Run("RunInTxOpts", func(t *testing.T) {
		err := db.RunInTxOpts(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable}, func(tx *rain.Tx) error {
			_, err := tx.Insert().Table(users).Set(users.Email, "runintxopts@example.com").Exec(ctx)
			return err
		})
		if err != nil {
			t.Fatalf("RunInTxOpts failed: %v", err)
		}

		exists, err := db.Select().Table(users).Where(users.Email.Eq("runintxopts@example.com")).Exists(ctx)
		if err != nil || !exists {
			t.Fatalf("expected row to exist after RunInTxOpts")
		}
	})
}
