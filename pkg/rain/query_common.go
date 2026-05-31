package rain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/hyperlocalise/rain-orm/pkg/dialect"
	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

type queryRunner interface {
	execContext(context.Context, string, ...any) (sql.Result, error)
	queryContext(context.Context, string, ...any) (*sql.Rows, error)
}

type preparingQueryRunner interface {
	queryRunner
	prepareContext(context.Context, string) (*sql.Stmt, error)
}

type joinClause struct {
	kind  string
	table selectTableSource
	on    schema.Predicate
}

type assignment struct {
	column schema.ColumnReference
	value  schema.Expression
}

type returningClause struct {
	feature dialect.Feature
	label   string
}

type selectTableSource interface {
	writeSQL(*compileContext) error
}

type tableDefSource struct {
	table *schema.TableDef
}

func tableDefFromSelectSource(source selectTableSource) *schema.TableDef {
	if table, ok := source.(tableDefSource); ok {
		return table.table
	}
	return nil
}

func (s tableDefSource) writeSQL(ctx *compileContext) error {
	ctx.writeTable(s.table)
	return nil
}

type subqueryTableSource struct {
	query *SelectQuery
	alias string
}

func (s subqueryTableSource) writeSQL(ctx *compileContext) error {
	if strings.TrimSpace(s.alias) == "" {
		return errors.New("rain: subquery table source requires a non-empty alias")
	}
	if s.query == nil {
		return fmt.Errorf("rain: subquery table source %q requires a non-nil query", s.alias)
	}
	ctx.writeByte('(')
	if err := s.query.writeSQL(ctx); err != nil {
		return err
	}
	ctx.writeString(") AS ")
	ctx.writeQuotedIdentifier(s.alias)
	return nil
}

type cteDefinition struct {
	name  string
	query *SelectQuery
}

func closeRows(rows *sql.Rows, errp *error) {
	if err := rows.Close(); err != nil && *errp == nil {
		*errp = err
	}
}

func writeCTEs(ctx *compileContext, ctes []cteDefinition, label string) error {
	if len(ctes) == 0 || ctx.skipCTEs {
		return nil
	}
	if !dialect.HasFeature(ctx.dialect.Features(), dialect.FeatureCTE) {
		return fmt.Errorf("rain: %s queries do not support CTEs for %s dialect", label, ctx.dialect.Name())
	}
	ctx.writeString("WITH ")
	for idx, cte := range ctes {
		if idx > 0 {
			ctx.writeString(", ")
		}
		if strings.TrimSpace(cte.name) == "" {
			return errors.New("rain: CTE name cannot be empty")
		}
		if cte.query == nil {
			return fmt.Errorf("rain: CTE %q requires a query", cte.name)
		}
		if len(cte.query.ctes) > 0 {
			return fmt.Errorf("rain: CTE %q body cannot itself contain CTEs", cte.name)
		}
		ctx.writeQuotedIdentifier(cte.name)
		ctx.writeString(" AS (")
		if err := cte.query.writeSQL(ctx); err != nil {
			return err
		}
		ctx.writeByte(')')
	}
	ctx.writeByte(' ')
	return nil
}

func writeOrderLimit(ctx *compileContext, order []schema.OrderExpr, limit *int, offset *int, featureOrder, featureLimit dialect.Feature) error {
	if len(order) > 0 {
		if featureOrder != dialect.FeatureUnlimited && !dialect.HasFeature(ctx.dialect.Features(), featureOrder) {
			return fmt.Errorf("rain: ORDER BY is not supported for this query type in %s dialect", ctx.dialect.Name())
		}
		ctx.writeString(" ORDER BY ")
		for idx, item := range order {
			if idx > 0 {
				ctx.writeString(", ")
			}
			if err := ctx.writeExpression(item.Expr); err != nil {
				return err
			}
			ctx.writeByte(' ')
			ctx.writeString(string(item.Direction))
			if item.NullsOrder != "" {
				if !dialect.HasFeature(ctx.dialect.Features(), dialect.FeatureNullsOrder) {
					return fmt.Errorf("rain: NULLS FIRST/LAST is not supported by %s dialect", ctx.dialect.Name())
				}
				ctx.writeByte(' ')
				ctx.writeString(string(item.NullsOrder))
			}
		}
	}

	if limit != nil || (offset != nil && *offset > 0) {
		if featureLimit != dialect.FeatureUnlimited && !dialect.HasFeature(ctx.dialect.Features(), featureLimit) {
			return fmt.Errorf("rain: LIMIT/OFFSET is not supported for this query type in %s dialect", ctx.dialect.Name())
		}
		l := -1
		if limit != nil {
			l = *limit
			if l < 0 {
				return errors.New("rain: LIMIT must be non-negative")
			}
		}
		o := 0
		if offset != nil {
			o = *offset
			if o < 0 {
				return errors.New("rain: OFFSET must be non-negative")
			}
		}
		if clause := ctx.dialect.LimitOffset(l, o); clause != "" {
			ctx.writeByte(' ')
			ctx.writeString(clause)
		}
	}
	return nil
}
