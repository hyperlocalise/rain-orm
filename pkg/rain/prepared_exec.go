package rain

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"sync"

	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

// PreparedInsertQuery is a prepared INSERT query with reusable named argument binding.
type PreparedInsertQuery struct {
	table    *schema.TableDef
	compiled compiledQuery
	stmt     *sql.Stmt
	// OPTIMIZATION: Local cache for scan plans to bypass rows.Columns() and
	// global cache lookups on every execution.
	planCache sync.Map
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

	// OPTIMIZATION: Use the local plan cache to bypass rows.Columns() and
	// global cache lookups.
	destVal := reflect.ValueOf(dest)
	if destVal.Kind() != reflect.Pointer || destVal.IsNil() {
		return fmt.Errorf("rain: destination must be a non-nil pointer")
	}
	target := destVal.Elem()
	destType := target.Type()
	if cached, ok := p.planCache.Load(destType); ok {
		return scanRowsWithPlan(rows, dest, cached.(*rowScanPlan))
	}

	cols, err := rows.Columns()
	if err != nil {
		return err
	}

	structType := destType
	if target.Kind() == reflect.Slice {
		structType, _, err = sliceElementStructType(destType.Elem())
		if err != nil {
			return err
		}
	}

	plan, err := newRowScanPlanForColumns(cols, structType, p.table)
	if err != nil {
		return err
	}
	p.planCache.Store(destType, plan)
	return scanRowsWithPlan(rows, dest, plan)
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
	table    *schema.TableDef
	compiled compiledQuery
	stmt     *sql.Stmt
	// OPTIMIZATION: Local cache for scan plans to bypass rows.Columns() and
	// global cache lookups on every execution.
	planCache sync.Map
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

	// OPTIMIZATION: Use the local plan cache to bypass rows.Columns() and
	// global cache lookups.
	destVal := reflect.ValueOf(dest)
	if destVal.Kind() != reflect.Pointer || destVal.IsNil() {
		return fmt.Errorf("rain: destination must be a non-nil pointer")
	}
	target := destVal.Elem()
	destType := target.Type()
	if cached, ok := p.planCache.Load(destType); ok {
		return scanRowsWithPlan(rows, dest, cached.(*rowScanPlan))
	}

	cols, err := rows.Columns()
	if err != nil {
		return err
	}

	structType := destType
	if target.Kind() == reflect.Slice {
		structType, _, err = sliceElementStructType(destType.Elem())
		if err != nil {
			return err
		}
	}

	plan, err := newRowScanPlanForColumns(cols, structType, p.table)
	if err != nil {
		return err
	}
	p.planCache.Store(destType, plan)
	return scanRowsWithPlan(rows, dest, plan)
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
	table    *schema.TableDef
	compiled compiledQuery
	stmt     *sql.Stmt
	// OPTIMIZATION: Local cache for scan plans to bypass rows.Columns() and
	// global cache lookups on every execution.
	planCache sync.Map
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

	// OPTIMIZATION: Use the local plan cache to bypass rows.Columns() and
	// global cache lookups.
	destVal := reflect.ValueOf(dest)
	if destVal.Kind() != reflect.Pointer || destVal.IsNil() {
		return fmt.Errorf("rain: destination must be a non-nil pointer")
	}
	target := destVal.Elem()
	destType := target.Type()
	if cached, ok := p.planCache.Load(destType); ok {
		return scanRowsWithPlan(rows, dest, cached.(*rowScanPlan))
	}

	cols, err := rows.Columns()
	if err != nil {
		return err
	}

	structType := destType
	if target.Kind() == reflect.Slice {
		structType, _, err = sliceElementStructType(destType.Elem())
		if err != nil {
			return err
		}
	}

	plan, err := newRowScanPlanForColumns(cols, structType, p.table)
	if err != nil {
		return err
	}
	p.planCache.Store(destType, plan)
	return scanRowsWithPlan(rows, dest, plan)
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
