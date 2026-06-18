package rain

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

func TestQueryCommonHelpers(t *testing.T) {
	t.Parallel()

	users, _ := defineInternalQueryTables()

	t.Run("SubqueryAliasValidation", func(t *testing.T) {
		ctx := newCompileContext(dialectForTest(t, "postgres"))
		defer releaseCompileContext(ctx)
		if err := (tableSource{alias: "   ", subquery: &SelectQuery{dialect: ctx.dialect, table: users.TableDef()}}).writeSQL(ctx); err == nil || !strings.Contains(err.Error(), "non-empty alias") {
			t.Fatalf("expected empty alias error, got %v", err)
		}
	})

	t.Run("SubqueryNilQueryValidation", func(t *testing.T) {
		ctx := newCompileContext(dialectForTest(t, "postgres"))
		defer releaseCompileContext(ctx)
		if err := (tableSource{alias: "u", subquery: nil}).writeSQL(ctx); err == nil || !strings.Contains(err.Error(), "non-nil query") {
			t.Fatalf("expected nil query error, got %v", err)
		}
	})

	t.Run("SubqueryWriteSQL", func(t *testing.T) {
		ctx := newCompileContext(dialectForTest(t, "postgres"))
		defer releaseCompileContext(ctx)
		err := (tableSource{
			alias: "u",
			subquery: &SelectQuery{
				dialect: ctx.dialect,
				table:   users.TableDef(),
				cols:    []schema.Expression{users.ID},
			},
		}).writeSQL(ctx)
		if err != nil {
			t.Fatalf("tableSource.writeSQL returned error: %v", err)
		}
		if !strings.Contains(ctx.String(), `AS "u"`) {
			t.Fatalf("expected compiled subquery alias, got %q", ctx.String())
		}
	})

	t.Run("NestedQueryError", func(t *testing.T) {
		ctx := newCompileContext(dialectForTest(t, "postgres"))
		defer releaseCompileContext(ctx)
		if err := (tableSource{
			alias:    "broken",
			subquery: &SelectQuery{dialect: ctx.dialect},
		}).writeSQL(ctx); err == nil || !strings.Contains(err.Error(), "requires a table") {
			t.Fatalf("expected nested query error, got %v", err)
		}
	})
}

func TestCloseRows(t *testing.T) {
	t.Parallel()

	rows := openCloseRows(t, errors.New("close failed"))
	err := error(nil)
	closeRows(rows, &err)
	if err == nil || err.Error() != "close failed" {
		t.Fatalf("expected close error to be surfaced, got %v", err)
	}

	rows = openCloseRows(t, errors.New("close failed again"))
	existingErr := errors.New("existing")
	err = existingErr
	closeRows(rows, &err)
	if !errors.Is(err, existingErr) {
		t.Fatalf("expected existing error to be preserved, got %v", err)
	}
}

func openCloseRows(t *testing.T, closeErr error) *sql.Rows {
	t.Helper()

	name := fmt.Sprintf("rain-query-common-%d", atomic.AddUint64(&closeRowsDriverCounter, 1))
	sql.Register(name, closeRowsDriver{closeErr: closeErr})

	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatalf("sql.Open(%q): %v", name, err)
	}
	t.Cleanup(func() { _ = db.Close() })

	rows, err := db.QueryContext(context.Background(), "SELECT 1")
	if err != nil {
		t.Fatalf("db.QueryContext: %v", err)
	}
	return rows
}

type closeRowsDriver struct {
	closeErr error
}

type closeRowsConn struct {
	closeErr error
}

type closeRowsResult struct {
	closeErr error
	closed   bool
}

func (d closeRowsDriver) Open(name string) (driver.Conn, error) {
	return &closeRowsConn{closeErr: d.closeErr}, nil
}

func (c *closeRowsConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("not implemented")
}
func (c *closeRowsConn) Close() error              { return nil }
func (c *closeRowsConn) Begin() (driver.Tx, error) { return nil, errors.New("not implemented") }

func (c *closeRowsConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	return &closeRowsResult{closeErr: c.closeErr}, nil
}

func (r *closeRowsResult) Columns() []string { return []string{"value"} }

func (r *closeRowsResult) Close() error {
	r.closed = true
	return r.closeErr
}

func (r *closeRowsResult) Next(dest []driver.Value) error {
	if !r.closed {
		r.closed = true
		return io.EOF
	}
	return io.EOF
}

var closeRowsDriverCounter uint64

var (
	_ driver.Driver         = closeRowsDriver{}
	_ driver.Conn           = (*closeRowsConn)(nil)
	_ driver.QueryerContext = (*closeRowsConn)(nil)
)
