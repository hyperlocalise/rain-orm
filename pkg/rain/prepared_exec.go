package rain

import (
	"context"
	"database/sql"
	"sync"

	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

// PreparedInsertQuery is a prepared INSERT query with reusable named argument binding.
type PreparedInsertQuery struct {
	table     *schema.TableDef
	compiled  compiledQuery
	stmt      *sql.Stmt
	closeOnce sync.Once
	closeErr  error
}

// Exec executes the prepared INSERT query.
func (p *PreparedInsertQuery) Exec(ctx context.Context, args PreparedArgs) (sql.Result, error) {
	bound, err := p.compiled.bind(args)
	if err != nil {
		return nil, err
	}

	return p.stmt.ExecContext(ctx, bound...)
}

// Scan executes the prepared INSERT ... RETURNING query and scans results into dest.
func (p *PreparedInsertQuery) Scan(ctx context.Context, args PreparedArgs, dest any) (err error) {
	bound, err := p.compiled.bind(args)
	if err != nil {
		return err
	}

	rows, err := p.stmt.QueryContext(ctx, bound...)
	if err != nil {
		return err
	}
	defer closeRows(rows, &err)

	return scanRowsAgainstTable(rows, dest, p.table)
}

// Close closes the prepared statement.
func (p *PreparedInsertQuery) Close() error {
	p.closeOnce.Do(func() {
		if p.stmt != nil {
			p.closeErr = p.stmt.Close()
		}
	})
	return p.closeErr
}

// PreparedUpdateQuery is a prepared UPDATE query with reusable named argument binding.
type PreparedUpdateQuery struct {
	table     *schema.TableDef
	compiled  compiledQuery
	stmt      *sql.Stmt
	closeOnce sync.Once
	closeErr  error
}

// Exec executes the prepared UPDATE query.
func (p *PreparedUpdateQuery) Exec(ctx context.Context, args PreparedArgs) (sql.Result, error) {
	bound, err := p.compiled.bind(args)
	if err != nil {
		return nil, err
	}

	return p.stmt.ExecContext(ctx, bound...)
}

// Scan executes the prepared UPDATE ... RETURNING query and scans results into dest.
func (p *PreparedUpdateQuery) Scan(ctx context.Context, args PreparedArgs, dest any) (err error) {
	bound, err := p.compiled.bind(args)
	if err != nil {
		return err
	}

	rows, err := p.stmt.QueryContext(ctx, bound...)
	if err != nil {
		return err
	}
	defer closeRows(rows, &err)

	return scanRowsAgainstTable(rows, dest, p.table)
}

// Close closes the prepared statement.
func (p *PreparedUpdateQuery) Close() error {
	p.closeOnce.Do(func() {
		if p.stmt != nil {
			p.closeErr = p.stmt.Close()
		}
	})
	return p.closeErr
}

// PreparedDeleteQuery is a prepared DELETE query with reusable named argument binding.
type PreparedDeleteQuery struct {
	table     *schema.TableDef
	compiled  compiledQuery
	stmt      *sql.Stmt
	closeOnce sync.Once
	closeErr  error
}

// Exec executes the prepared DELETE query.
func (p *PreparedDeleteQuery) Exec(ctx context.Context, args PreparedArgs) (sql.Result, error) {
	bound, err := p.compiled.bind(args)
	if err != nil {
		return nil, err
	}

	return p.stmt.ExecContext(ctx, bound...)
}

// Scan executes the prepared DELETE ... RETURNING query and scans results into dest.
func (p *PreparedDeleteQuery) Scan(ctx context.Context, args PreparedArgs, dest any) (err error) {
	bound, err := p.compiled.bind(args)
	if err != nil {
		return err
	}

	rows, err := p.stmt.QueryContext(ctx, bound...)
	if err != nil {
		return err
	}
	defer closeRows(rows, &err)

	return scanRowsAgainstTable(rows, dest, p.table)
}

// Close closes the prepared statement.
func (p *PreparedDeleteQuery) Close() error {
	p.closeOnce.Do(func() {
		if p.stmt != nil {
			p.closeErr = p.stmt.Close()
		}
	})
	return p.closeErr
}
