package rain_test

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/hyperlocalise/rain-orm/pkg/rain"
)

func TestTransactionOptions(t *testing.T) {
	ctx := context.Background()
	db, err := rain.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Test BeginTx with options
	opts := &sql.TxOptions{
		Isolation: sql.LevelSerializable,
		ReadOnly:  true,
	}
	tx, err := db.BeginTx(ctx, opts)
	if err != nil {
		t.Fatalf("expected no error starting transaction, got %v", err)
	}
	_ = tx.Rollback()

	// Test RunInTxOpts
	err = db.RunInTxOpts(ctx, opts, func(tx *rain.Tx) error {
		return nil
	})
	if err != nil {
		t.Fatalf("expected no error in RunInTxOpts, got %v", err)
	}

	// Verify it still works with nil options (matches Begin/RunInTx)
	err = db.RunInTxOpts(ctx, nil, func(tx *rain.Tx) error {
		return nil
	})
	if err != nil {
		t.Fatalf("expected no error in RunInTxOpts with nil options, got %v", err)
	}
}
