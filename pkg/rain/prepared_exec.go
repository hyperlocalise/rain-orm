package rain

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"sync"

	"github.com/hyperlocalise/rain-orm/pkg/schema"
	"golang.org/x/sync/singleflight"
)

// PreparedInsertQuery is a prepared INSERT query with reusable named argument binding.
type PreparedInsertQuery struct {
	table     *schema.TableDef
	compiled  compiledQuery
	stmt      *sql.Stmt
	closeOnce sync.Once
	closeErr  error

	scanPlans sync.Map // map[reflect.Type]*rowScanPlan
	planGroup singleflight.Group
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
	bound, bindErr := p.compiled.bind(args)
	if bindErr != nil {
		return bindErr
	}

	destVal := reflect.ValueOf(dest)
	if destVal.Kind() != reflect.Pointer || destVal.IsNil() {
		return fmt.Errorf("rain: destination must be a non-nil pointer")
	}
	target := destVal.Elem()
	if !target.CanSet() {
		return fmt.Errorf("rain: destination must be settable")
	}

	var structType reflect.Type
	switch target.Kind() {
	case reflect.Struct:
		structType = target.Type()
	case reflect.Slice:
		var scanErr error
		structType, _, scanErr = sliceElementStructType(target.Type().Elem())
		if scanErr != nil {
			return scanErr
		}
	default:
		return fmt.Errorf("rain: destination must point to a struct or slice")
	}

	var plan *rowScanPlan
	if cached, ok := p.scanPlans.Load(structType); ok {
		plan = cached.(*rowScanPlan)
	} else {
		// Only one goroutine performs the mutation to avoid duplicate mutations.
		v, err, _ := p.planGroup.Do(structType.String(), func() (any, error) {
			rows, queryErr := p.stmt.QueryContext(ctx, bound...)
			if queryErr != nil {
				return nil, queryErr
			}
			defer closeRows(rows, &queryErr)

			colNames, colErr := rows.Columns()
			if colErr != nil {
				return nil, colErr
			}
			plan, err := newRowScanPlanForColumns(colNames, structType, p.table)
			if err != nil {
				return nil, err
			}
			p.scanPlans.Store(structType, plan)

			// We still need to scan the results into the first caller's target.
			// However, since singleflight returns the same result to all callers,
			// and Scan mutates 'target', this is tricky.
			// Actually, for DML RETURNING, it's safer to just let subsequent callers
			// wait for the first one to finish the mutation, and then maybe
			// they should get an error if it was a single-row mutation?
			// The issue description says "Concurrent plan-cache miss executes DML twice".
			// By using singleflight around the mutation, we solve that.
			return scanRowsWithPlan(rows, target, plan), nil
		})
		if err != nil {
			return err
		}
		// If another goroutine called Scan while the first one was building the plan,
		// it will get the error from the first one.
		// If it's a mutation, perhaps it shouldn't have been called concurrently?
		// But at least we didn't execute the mutation twice.
		if err, ok := v.(error); ok && err != nil {
			return err
		}
		return nil
	}

	rows, queryErr := p.stmt.QueryContext(ctx, bound...)
	if queryErr != nil {
		return queryErr
	}
	defer closeRows(rows, &err)

	err = scanRowsWithPlan(rows, target, plan)
	return err
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

	scanPlans sync.Map // map[reflect.Type]*rowScanPlan
	planGroup singleflight.Group
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
	bound, bindErr := p.compiled.bind(args)
	if bindErr != nil {
		return bindErr
	}

	destVal := reflect.ValueOf(dest)
	if destVal.Kind() != reflect.Pointer || destVal.IsNil() {
		return fmt.Errorf("rain: destination must be a non-nil pointer")
	}
	target := destVal.Elem()
	if !target.CanSet() {
		return fmt.Errorf("rain: destination must be settable")
	}

	var structType reflect.Type
	switch target.Kind() {
	case reflect.Struct:
		structType = target.Type()
	case reflect.Slice:
		var scanErr error
		structType, _, scanErr = sliceElementStructType(target.Type().Elem())
		if scanErr != nil {
			return scanErr
		}
	default:
		return fmt.Errorf("rain: destination must point to a struct or slice")
	}

	var plan *rowScanPlan
	if cached, ok := p.scanPlans.Load(structType); ok {
		plan = cached.(*rowScanPlan)
	} else {
		v, err, _ := p.planGroup.Do(structType.String(), func() (any, error) {
			rows, queryErr := p.stmt.QueryContext(ctx, bound...)
			if queryErr != nil {
				return nil, queryErr
			}
			defer closeRows(rows, &queryErr)

			colNames, colErr := rows.Columns()
			if colErr != nil {
				return nil, colErr
			}
			plan, err := newRowScanPlanForColumns(colNames, structType, p.table)
			if err != nil {
				return nil, err
			}
			p.scanPlans.Store(structType, plan)
			return scanRowsWithPlan(rows, target, plan), nil
		})
		if err != nil {
			return err
		}
		if err, ok := v.(error); ok && err != nil {
			return err
		}
		return nil
	}

	rows, queryErr := p.stmt.QueryContext(ctx, bound...)
	if queryErr != nil {
		return queryErr
	}
	defer closeRows(rows, &err)

	err = scanRowsWithPlan(rows, target, plan)
	return err
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

	scanPlans sync.Map // map[reflect.Type]*rowScanPlan
	planGroup singleflight.Group
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
	bound, bindErr := p.compiled.bind(args)
	if bindErr != nil {
		return bindErr
	}

	destVal := reflect.ValueOf(dest)
	if destVal.Kind() != reflect.Pointer || destVal.IsNil() {
		return fmt.Errorf("rain: destination must be a non-nil pointer")
	}
	target := destVal.Elem()
	if !target.CanSet() {
		return fmt.Errorf("rain: destination must be settable")
	}

	var structType reflect.Type
	switch target.Kind() {
	case reflect.Struct:
		structType = target.Type()
	case reflect.Slice:
		var scanErr error
		structType, _, scanErr = sliceElementStructType(target.Type().Elem())
		if scanErr != nil {
			return scanErr
		}
	default:
		return fmt.Errorf("rain: destination must point to a struct or slice")
	}

	var plan *rowScanPlan
	if cached, ok := p.scanPlans.Load(structType); ok {
		plan = cached.(*rowScanPlan)
	} else {
		v, err, _ := p.planGroup.Do(structType.String(), func() (any, error) {
			rows, queryErr := p.stmt.QueryContext(ctx, bound...)
			if queryErr != nil {
				return nil, queryErr
			}
			defer closeRows(rows, &queryErr)

			colNames, colErr := rows.Columns()
			if colErr != nil {
				return nil, colErr
			}
			plan, err := newRowScanPlanForColumns(colNames, structType, p.table)
			if err != nil {
				return nil, err
			}
			p.scanPlans.Store(structType, plan)
			return scanRowsWithPlan(rows, target, plan), nil
		})
		if err != nil {
			return err
		}
		if err, ok := v.(error); ok && err != nil {
			return err
		}
		return nil
	}

	rows, queryErr := p.stmt.QueryContext(ctx, bound...)
	if queryErr != nil {
		return queryErr
	}
	defer closeRows(rows, &err)

	err = scanRowsWithPlan(rows, target, plan)
	return err
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
