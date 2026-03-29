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
