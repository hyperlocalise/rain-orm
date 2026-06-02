package rain

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/hyperlocalise/rain-orm/pkg/dialect"
	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

type compiledArgKind uint8

const (
	compiledArgLiteral compiledArgKind = iota
	compiledArgNamedPlaceholder
)

type compiledArg struct {
	kind  compiledArgKind
	value any
	name  string
}

type compiledQuery struct {
	sql      string
	argPlan  []compiledArg
	hasNames bool
	args     []any
}

func (q compiledQuery) literalArgs() ([]any, error) {
	if q.hasNames {
		return nil, ErrPreparedArgsRequired
	}
	// OPTIMIZATION: Return the pre-calculated arguments directly. Since q.args
	// is a fresh slice created during compilation and query runners do not
	// modify it, we can safely avoid the redundant copy.
	return q.args, nil
}

func (q compiledQuery) bind(args PreparedArgs) ([]any, error) {
	if !q.hasNames {
		if len(args) > 0 {
			return nil, fmt.Errorf("rain: unexpected prepared args for query without placeholders")
		}
		// OPTIMIZATION: Return the pre-calculated arguments directly when there
		// are no named placeholders to bind.
		return q.args, nil
	}

	seen := make(map[string]struct{}, len(args))
	bound := append([]any(nil), q.args...)
	for i, arg := range q.argPlan {
		if arg.kind == compiledArgLiteral {
			continue
		}
		value, ok := args[arg.name]
		if !ok {
			return nil, fmt.Errorf("rain: missing prepared arg %q", arg.name)
		}
		seen[arg.name] = struct{}{}
		bound[i] = value
	}
	for name := range args {
		if _, ok := seen[name]; ok {
			continue
		}
		return nil, fmt.Errorf("rain: unexpected prepared arg %q", name)
	}
	return bound, nil
}

type compileContext struct {
	builder     strings.Builder
	dialect     dialect.Dialect
	argPlan     []compiledArg
	err         error
	skipCTEs    bool
	useLiterals bool
	quotedCache *sync.Map
}

var compileContextPool = sync.Pool{
	New: func() any {
		return &compileContext{
			argPlan: make([]compiledArg, 0, 8),
		}
	},
}

var (
	// OPTIMIZATION: Cache quoted identifiers per-dialect to avoid redundant
	// string allocations and escaping logic during query compilation.
	postgresQuotedCache sync.Map
	mysqlQuotedCache    sync.Map
	sqliteQuotedCache   sync.Map
)

func newCompileContext(d dialect.Dialect) *compileContext {
	ctx := compileContextPool.Get().(*compileContext)
	ctx.reset(d)
	return ctx
}

func releaseCompileContext(ctx *compileContext) {
	compileContextPool.Put(ctx)
}

func (c *compileContext) reset(d dialect.Dialect) {
	c.builder.Reset()
	c.dialect = d
	// Clear the argPlan slice to ensure any values it contains can be
	// garbage collected before we reset its length for reuse.
	clear(c.argPlan)
	c.argPlan = c.argPlan[:0]
	c.err = nil
	c.skipCTEs = false
	c.useLiterals = false

	// OPTIMIZATION: Pre-select the quoted identifier cache to avoid repeated
	// name lookups or type assertions in the hot compilation loop.
	switch d.(type) {
	case *dialect.PostgresDialect:
		c.quotedCache = &postgresQuotedCache
	case *dialect.MySQLDialect:
		c.quotedCache = &mysqlQuotedCache
	case *dialect.SQLiteDialect:
		c.quotedCache = &sqliteQuotedCache
	default:
		c.quotedCache = nil
	}
}

func (c *compileContext) String() string {
	return c.builder.String()
}

func (c *compileContext) compiledQuery() compiledQuery {
	var hasNames bool
	for _, arg := range c.argPlan {
		if arg.kind == compiledArgNamedPlaceholder {
			hasNames = true
			break
		}
	}

	// OPTIMIZATION: Only copy the argPlan if the query has named placeholders.
	// Queries with only literals can use the pre-calculated args slice.
	var argPlan []compiledArg
	if hasNames {
		argPlan = append([]compiledArg(nil), c.argPlan...)
	}

	compiled := compiledQuery{
		sql:      c.String(),
		argPlan:  argPlan,
		hasNames: hasNames,
	}
	if len(c.argPlan) > 0 {
		compiled.args = make([]any, len(c.argPlan))
		for i, arg := range c.argPlan {
			if arg.kind == compiledArgLiteral {
				compiled.args[i] = arg.value
			}
		}
	}
	return compiled
}

func (c *compileContext) nextPlaceholderIndex() int {
	return len(c.argPlan) + 1
}

func (c *compileContext) writeByte(ch byte) {
	c.builder.WriteByte(ch)
}

func (c *compileContext) writeString(value string) {
	c.builder.WriteString(value)
}

func (c *compileContext) writeQuotedIdentifier(name string) {
	if c.quotedCache == nil {
		// Fallback for custom or less common dialects that don't have a dedicated cache.
		c.writeString(c.dialect.QuoteIdentifier(name))
		return
	}

	if cached, ok := c.quotedCache.Load(name); ok {
		c.writeString(cached.(string))
		return
	}

	quoted := c.dialect.QuoteIdentifier(name)
	c.quotedCache.Store(name, quoted)
	c.writeString(quoted)
}

func (c *compileContext) writeTableName(table *schema.TableDef) {
	c.writeQuotedIdentifier(table.Name)
}

func (c *compileContext) writeTable(table *schema.TableDef) {
	c.writeTableName(table)
	if table.Alias != "" {
		c.writeString(" AS ")
		c.writeQuotedIdentifier(table.Alias)
	}
}

func (c *compileContext) writeReturning(exprs []schema.Expression, clause returningClause) error {
	if len(exprs) == 0 {
		return nil
	}
	if !dialect.HasFeature(c.dialect.Features(), clause.feature) {
		return fmt.Errorf("rain: %s queries do not support RETURNING for %s dialect", clause.label, c.dialect.Name())
	}

	c.writeString(" RETURNING ")
	for idx, expr := range exprs {
		if idx > 0 {
			c.writeString(", ")
		}
		if err := c.writeExpression(expr); err != nil {
			return err
		}
	}

	return nil
}

func (c *compileContext) writePredicate(predicate schema.Predicate) error {
	return c.writeExpression(predicate)
}

type expressionContext struct {
	allowAlias  bool
	noParens    bool
	unqualified bool
}

func (c *compileContext) writeExpression(expr schema.Expression) error {
	return c.writeExpressionInContext(expr, expressionContext{})
}

func (c *compileContext) writeSelectExpression(expr schema.Expression) error {
	return c.writeExpressionInContext(expr, expressionContext{allowAlias: true})
}

func (c *compileContext) writeExpressionInContext(expr schema.Expression, context expressionContext) error {
	switch value := expr.(type) {
	case excludedColumn:
		if c.dialect.Name() == "mysql" {
			c.writeString("VALUES(")
			c.writeQuotedIdentifier(value.column.ColumnDef().Name)
			c.writeByte(')')
		} else {
			c.writeString("EXCLUDED.")
			c.writeQuotedIdentifier(value.column.ColumnDef().Name)
		}
	case schema.ColumnReference:
		if context.unqualified {
			c.writeColumn(value)
		} else {
			c.writeQualifiedColumn(value)
		}
	case schema.ValueExpr:
		if c.useLiterals {
			sql, err := literalDDLSQL(c.dialect, value.Value)
			if err != nil {
				return err
			}
			c.writeString(sql)
		} else {
			index := c.nextPlaceholderIndex()
			c.argPlan = append(c.argPlan, compiledArg{kind: compiledArgLiteral, value: value.Value})
			c.writeString(c.dialect.Placeholder(index))
		}
	case schema.PlaceholderExpr:
		if strings.TrimSpace(value.Name) == "" {
			return errors.New("rain: placeholder name cannot be empty")
		}
		index := c.nextPlaceholderIndex()
		c.argPlan = append(c.argPlan, compiledArg{kind: compiledArgNamedPlaceholder, name: value.Name})
		c.writeString(c.dialect.Placeholder(index))
	case schema.ComparisonExpr:
		if err := c.writeExpressionInContext(value.Left, context); err != nil {
			return err
		}
		c.writeByte(' ')
		c.writeString(value.Operator)
		c.writeByte(' ')
		if err := c.writeExpressionInContext(value.Right, context); err != nil {
			return err
		}
	case schema.InExpr:
		if len(value.Values) == 0 {
			return errors.New("rain: IN predicate requires at least one value")
		}
		if err := c.writeExpressionInContext(value.Left, context); err != nil {
			return err
		}
		if value.Negated {
			c.writeString(" NOT IN (")
		} else {
			c.writeString(" IN (")
		}
		for idx, item := range value.Values {
			if idx > 0 {
				c.writeString(", ")
			}
			ctx := expressionContext{unqualified: context.unqualified}
			if len(value.Values) == 1 {
				ctx.noParens = true
			}
			if err := c.writeExpressionInContext(item, ctx); err != nil {
				return err
			}
		}
		c.writeByte(')')
	case schema.BetweenExpr:
		if err := c.writeExpressionInContext(value.Left, context); err != nil {
			return err
		}
		if value.Negated {
			c.writeString(" NOT BETWEEN ")
		} else {
			c.writeString(" BETWEEN ")
		}
		if err := c.writeExpressionInContext(value.Start, context); err != nil {
			return err
		}
		c.writeString(" AND ")
		if err := c.writeExpressionInContext(value.End, context); err != nil {
			return err
		}
	case schema.NotExpr:
		c.writeString("NOT (")
		if err := c.writeExpressionInContext(value.Expr, context); err != nil {
			return err
		}
		c.writeByte(')')
	case schema.ExistsExpr:
		if value.Negated {
			c.writeString("NOT ")
		}
		c.writeString("EXISTS (")
		if err := c.writeExpressionInContext(value.Subquery, expressionContext{noParens: true}); err != nil {
			return err
		}
		c.writeByte(')')
	case *SelectQuery:
		if !context.noParens {
			c.writeByte('(')
		}
		if err := value.writeSQL(c); err != nil {
			return err
		}
		if !context.noParens {
			c.writeByte(')')
		}
	case schema.NullCheckExpr:
		if err := c.writeExpression(value.Expr); err != nil {
			return err
		}
		if value.Negated {
			c.writeString(" IS NOT NULL")
		} else {
			c.writeString(" IS NULL")
		}
	case schema.LogicalExpr:
		c.writeByte('(')
		for idx, part := range value.Exprs {
			if idx > 0 {
				c.writeByte(' ')
				c.writeString(value.Operator)
				c.writeByte(' ')
			}
			if err := c.writeExpressionInContext(part, context); err != nil {
				return err
			}
		}
		c.writeByte(')')
	case schema.CaseExpr:
		if len(value.WhenThenPairs) == 0 {
			return errors.New("rain: CASE expression requires at least one WHEN clause")
		}
		c.writeString("CASE")
		if value.ValueExpression != nil {
			c.writeByte(' ')
			if err := c.writeExpressionInContext(value.ValueExpression, context); err != nil {
				return err
			}
		}
		for _, pair := range value.WhenThenPairs {
			c.writeString(" WHEN ")
			if err := c.writeExpressionInContext(pair.When, context); err != nil {
				return err
			}
			c.writeString(" THEN ")
			if err := c.writeExpressionInContext(pair.Then, context); err != nil {
				return err
			}
		}
		if value.ElseExpression != nil {
			c.writeString(" ELSE ")
			if err := c.writeExpressionInContext(value.ElseExpression, context); err != nil {
				return err
			}
		}
		c.writeString(" END")
	case schema.AggregateExpr:
		if value.Function == "" {
			return errors.New("rain: aggregate function name cannot be empty")
		}
		if value.Distinct && value.Star {
			return fmt.Errorf("rain: aggregate %s cannot combine DISTINCT with *", value.Function)
		}
		c.writeString(value.Function)
		c.writeByte('(')
		if value.Distinct {
			c.writeString("DISTINCT ")
		}
		switch {
		case value.Star:
			c.writeByte('*')
		case value.Expr != nil:
			if err := c.writeExpressionInContext(value.Expr, context); err != nil {
				return err
			}
		default:
			return fmt.Errorf("rain: aggregate %s requires an expression", value.Function)
		}
		c.writeByte(')')
	case schema.CoalesceExpr:
		if len(value.Exprs) < 2 {
			return errors.New("rain: COALESCE requires at least two expressions")
		}
		c.writeString("COALESCE(")
		for idx, part := range value.Exprs {
			if part == nil {
				return errors.New("rain: COALESCE requires non-nil expressions")
			}
			if idx > 0 {
				c.writeString(", ")
			}
			if err := c.writeExpressionInContext(part, context); err != nil {
				return err
			}
		}
		c.writeByte(')')
	case schema.AliasExpr:
		if !context.allowAlias {
			return errors.New("rain: aliased expressions are only supported in SELECT columns")
		}
		if err := c.writeExpressionInContext(value.Expr, expressionContext{}); err != nil {
			return err
		}
		c.writeString(" AS ")
		c.writeQuotedIdentifier(value.Alias)
	case schema.RawExpr:
		if err := c.writeRaw(value); err != nil {
			return err
		}
	default:
		return fmt.Errorf("rain: unsupported expression type %T", expr)
	}

	return nil
}

func (c *compileContext) writeRaw(raw schema.RawExpr) error {
	argIndex := 0
	for idx := range len(raw.SQL) {
		if raw.SQL[idx] != '?' {
			c.writeByte(raw.SQL[idx])
			continue
		}
		if argIndex >= len(raw.Args) {
			return errors.New("rain: raw SQL placeholder count does not match args")
		}
		index := c.nextPlaceholderIndex()
		c.argPlan = append(c.argPlan, compiledArg{kind: compiledArgLiteral, value: raw.Args[argIndex]})
		c.writeString(c.dialect.Placeholder(index))
		argIndex++
	}
	if argIndex != len(raw.Args) {
		return errors.New("rain: raw SQL has unused args")
	}

	return nil
}

func (c *compileContext) writeColumn(column schema.ColumnReference) {
	def := column.ColumnDef()
	c.writeQuotedIdentifier(def.Name)
}

func (c *compileContext) writeQualifiedColumn(column schema.ColumnReference) {
	def := column.ColumnDef()
	table := def.Table
	qualifier := table.Name
	if table.Alias != "" {
		qualifier = table.Alias
	}

	c.writeQuotedIdentifier(qualifier)
	c.writeByte('.')
	c.writeQuotedIdentifier(def.Name)
}

func joinPredicates(predicates []schema.Predicate) schema.Predicate {
	if len(predicates) == 1 {
		return predicates[0]
	}

	return schema.And(predicates...)
}
