package rain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"sync"

	"golang.org/x/sync/singleflight"
)

// ErrPreparedArgsRequired is returned when a query with named placeholders is executed without prepared binding.
var ErrPreparedArgsRequired = errors.New("rain: query contains named placeholders; call Prepare and execute with PreparedArgs")

// ErrPrepareNotSupported is returned when the current query runner cannot prepare statements.
var ErrPrepareNotSupported = errors.New("rain: query runner does not support prepared statements")

// PreparedArgs provides runtime values for a prepared query's named placeholders.
type PreparedArgs map[string]any

// PreparedSelectQuery is a prepared SELECT query with reusable named argument binding.
type PreparedSelectQuery struct {
	query       *SelectQuery
	selectQuery compiledQuery
	countQuery  compiledQuery
	existsQuery compiledQuery
	selectStmt  *sql.Stmt
	countStmt   *sql.Stmt
	existsStmt  *sql.Stmt
	countErr    error
	closeOnce   sync.Once
	closeErr    error

	scanPlans sync.Map // map[reflect.Type]*rowScanPlan
	planGroup singleflight.Group
}

// Prepare compiles and prepares the SELECT query and derived aggregate statements.
func (q *SelectQuery) Prepare(ctx context.Context) (*PreparedSelectQuery, error) {
	if q.runner == nil {
		return nil, ErrNoConnection
	}

	runner, ok := q.runner.(preparingQueryRunner)
	if !ok {
		return nil, ErrPrepareNotSupported
	}

	selectQuery, err := q.compile()
	if err != nil {
		return nil, err
	}
	existsQuery, err := q.compileExists()
	if err != nil {
		return nil, err
	}

	prepared := &PreparedSelectQuery{
		query:       q,
		selectQuery: selectQuery,
		existsQuery: existsQuery,
	}
	countQuery, err := q.compileAggregate("COUNT(*)")
	if err != nil {
		prepared.countErr = err
	} else {
		prepared.countQuery = countQuery
	}

	selectStmt, err := runner.prepareContext(ctx, selectQuery.sql)
	if err != nil {
		return nil, err
	}
	prepared.selectStmt = selectStmt

	if prepared.countErr == nil {
		countStmt, err := runner.prepareContext(ctx, countQuery.sql)
		if err != nil {
			_ = prepared.Close()
			return nil, err
		}
		prepared.countStmt = countStmt
	}

	existsStmt, err := runner.prepareContext(ctx, existsQuery.sql)
	if err != nil {
		_ = prepared.Close()
		return nil, err
	}
	prepared.existsStmt = existsStmt

	return prepared, nil
}

// Scan executes the prepared SELECT query and scans results into dest.
func (p *PreparedSelectQuery) Scan(ctx context.Context, args PreparedArgs, dest any) (err error) {
	bound, err := p.selectQuery.bind(args)
	if err != nil {
		return err
	}

	cacheKey, cacheOptions, err := p.query.resolveCacheKey(p.selectQuery.sql, bound)
	if err != nil {
		return err
	}
	table := p.query.scanValidationTable()
	if len(p.query.relationNames) == 0 {
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
		}

		if cacheOptions != nil && !cacheOptions.bypass && p.query.locking == nil {
			cached, ok, cacheErr := p.query.cache.Get(ctx, cacheKey)
			if cacheErr != nil {
				return cacheErr
			}
			if ok {
				if result, decodeErr := decodeCachedSelectRows(cached); decodeErr == nil {
					if plan != nil {
						return scanCachedRowsWithPlan(result, target, plan)
					}
					// Populate plan cache on application cache hit if missing
					v, err, _ := p.planGroup.Do(structType.String(), func() (any, error) {
						return newRowScanPlanForColumns(result.Columns, structType, table)
					})
					if err != nil {
						return err
					}
					plan = v.(*rowScanPlan)
					p.scanPlans.Store(structType, plan)
					return scanCachedRowsWithPlan(result, target, plan)
				}
			}
		}

		rows, queryErr := p.selectStmt.QueryContext(ctx, bound...)
		if queryErr != nil {
			return queryErr
		}
		defer closeRows(rows, &err)

		if plan == nil {
			v, err, _ := p.planGroup.Do(structType.String(), func() (any, error) {
				colNames, colErr := rows.Columns()
				if colErr != nil {
					return nil, colErr
				}
				return newRowScanPlanForColumns(colNames, structType, table)
			})
			if err != nil {
				return err
			}
			plan = v.(*rowScanPlan)
			p.scanPlans.Store(structType, plan)
		}

		if cacheKey != "" && cacheOptions != nil && !cacheOptions.bypass && p.query.locking == nil {
			result, readErr := readCachedSelectRows(rows)
			if readErr != nil {
				return readErr
			}
			err = scanCachedRowsWithPlan(result, target, plan)
			if err != nil {
				return err
			}
			err = p.query.writeCachedSelectResult(ctx, cacheKey, cacheOptions, result)
			return err
		}

		err = scanRowsWithPlan(rows, target, plan)
		return err
	}

	rows, queryErr := p.selectStmt.QueryContext(ctx, bound...)
	if queryErr != nil {
		return queryErr
	}
	defer closeRows(rows, &err)

	if len(p.query.relationNames) == 0 {
		if cacheKey != "" && cacheOptions != nil && !cacheOptions.bypass && p.query.locking == nil {
			result, readErr := readCachedSelectRows(rows)
			if readErr != nil {
				return readErr
			}
			err = scanCachedRowsAgainstTable(result, dest, table)
			if err != nil {
				return err
			}
			err = p.query.writeCachedSelectResult(ctx, cacheKey, cacheOptions, result)
			return err
		}
		err = scanRowsAgainstTableDirect(rows, dest, table)
	} else {
		err = p.query.scanRowsWithRelations(ctx, rows, dest)
	}
	return err
}

// Count executes the prepared COUNT(*) query.
func (p *PreparedSelectQuery) Count(ctx context.Context, args PreparedArgs) (int64, error) {
	if p.countErr != nil {
		return 0, p.countErr
	}

	bound, err := p.countQuery.bind(args)
	if err != nil {
		return 0, err
	}

	cacheKey, cacheOptions, err := p.query.resolveCacheKey(p.countQuery.sql, bound)
	if err != nil {
		return 0, err
	}
	if cacheOptions != nil && !cacheOptions.bypass {
		cached, ok, cacheErr := p.query.cache.Get(ctx, cacheKey)
		if cacheErr != nil {
			return 0, cacheErr
		}
		if ok {
			if count, err := decodeCachedInt64(cached); err == nil {
				return count, nil
			}
		}
	}
	rows, err := p.countStmt.QueryContext(ctx, bound...)
	if err != nil {
		return 0, err
	}
	defer closeRows(rows, &err)

	var count int64
	if !rows.Next() {
		err = sql.ErrNoRows
		return 0, err
	}
	if err := rows.Scan(&count); err != nil {
		return 0, err
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	return count, p.query.writeCachedInt64(ctx, cacheKey, cacheOptions, count)
}

// Exists executes the prepared SELECT EXISTS query.
func (p *PreparedSelectQuery) Exists(ctx context.Context, args PreparedArgs) (bool, error) {
	bound, err := p.existsQuery.bind(args)
	if err != nil {
		return false, err
	}

	cacheKey, cacheOptions, err := p.query.resolveCacheKey(p.existsQuery.sql, bound)
	if err != nil {
		return false, err
	}
	if cacheOptions != nil && !cacheOptions.bypass {
		cached, ok, cacheErr := p.query.cache.Get(ctx, cacheKey)
		if cacheErr != nil {
			return false, cacheErr
		}
		if ok {
			if exists, err := decodeCachedBool(cached); err == nil {
				return exists, nil
			}
		}
	}
	rows, err := p.existsStmt.QueryContext(ctx, bound...)
	if err != nil {
		return false, err
	}
	defer closeRows(rows, &err)

	var exists bool
	if !rows.Next() {
		err = sql.ErrNoRows
		return false, err
	}
	if err := rows.Scan(&exists); err != nil {
		return false, err
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	return exists, p.query.writeCachedBool(ctx, cacheKey, cacheOptions, exists)
}

// Close closes all prepared statements.
func (p *PreparedSelectQuery) Close() error {
	p.closeOnce.Do(func() {
		for _, stmt := range []*sql.Stmt{p.selectStmt, p.countStmt, p.existsStmt} {
			if stmt == nil {
				continue
			}
			if err := stmt.Close(); err != nil && p.closeErr == nil {
				p.closeErr = err
			}
		}
	})
	return p.closeErr
}
