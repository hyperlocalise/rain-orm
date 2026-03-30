package rain

import (
	"errors"
	"fmt"
	"strings"

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
}

func (q compiledQuery) literalArgs() ([]any, error) {
	if q.hasNames {
		return nil, ErrPreparedArgsRequired
	}

	args := make([]any, 0, len(q.argPlan))
	for _, arg := range q.argPlan {
		args = append(args, arg.value)
	}
	return args, nil
}

func (q compiledQuery) bind(args PreparedArgs) ([]any, error) {
	if !q.hasNames {
		bound := make([]any, 0, len(q.argPlan))
		for _, arg := range q.argPlan {
			bound = append(bound, arg.value)
		}
		if len(args) > 0 {
			return nil, fmt.Errorf("rain: unexpected prepared args for query without placeholders")
		}
		return bound, nil
	}

	seen := make(map[string]struct{}, len(args))
	bound := make([]any, 0, len(q.argPlan))
	for _, arg := range q.argPlan {
		if arg.kind == compiledArgLiteral {
			bound = append(bound, arg.value)
			continue
		}
		value, ok := args[arg.name]
		if !ok {
			return nil, fmt.Errorf("rain: missing prepared arg %q", arg.name)
		}
		seen[arg.name] = struct{}{}
		bound = append(bound, value)
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
	builder strings.Builder
	dialect dialect.Dialect
	argPlan []compiledArg
	err     error
}

func newCompileContext(d dialect.Dialect) *compileContext {
	return &compileContext{
		dialect: d,
		argPlan: make([]compiledArg, 0, 8),
	}
}

func (c *compileContext) String() string {
	return c.builder.String()
}

func (c *compileContext) compiledQuery() compiledQuery {
	compiled := compiledQuery{
		sql:     c.String(),
		argPlan: make([]compiledArg, len(c.argPlan)),
	}
	copy(compiled.argPlan, c.argPlan)
	for _, arg := range compiled.argPlan {
		if arg.kind == compiledArgNamedPlaceholder {
			compiled.hasNames = true
			break
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
	c.writeString(c.dialect.QuoteIdentifier(name))
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
	allowAlias bool
}

func (c *compileContext) writeExpression(expr schema.Expression) error {
	return c.writeExpressionInContext(expr, expressionContext{})
}

func (c *compileContext) writeSelectExpression(expr schema.Expression) error {
	return c.writeExpressionInContext(expr, expressionContext{allowAlias: true})
}

func (c *compileContext) writeExpressionInContext(expr schema.Expression, context expressionContext) error {
	switch value := expr.(type) {
	case schema.ColumnReference:
		c.writeColumn(value)
	case schema.ValueExpr:
		index := c.nextPlaceholderIndex()
		c.argPlan = append(c.argPlan, compiledArg{kind: compiledArgLiteral, value: value.Value})
		c.writeString(c.dialect.Placeholder(index))
	case schema.PlaceholderExpr:
		if strings.TrimSpace(value.Name) == "" {
			return errors.New("rain: placeholder name cannot be empty")
		}
		index := c.nextPlaceholderIndex()
		c.argPlan = append(c.argPlan, compiledArg{kind: compiledArgNamedPlaceholder, name: value.Name})
		c.writeString(c.dialect.Placeholder(index))
	case schema.ComparisonExpr:
		if err := c.writeExpression(value.Left); err != nil {
			return err
		}
		c.writeByte(' ')
		c.writeString(value.Operator)
		c.writeByte(' ')
		if err := c.writeExpression(value.Right); err != nil {
			return err
		}
	case schema.InExpr:
		if len(value.Values) == 0 {
			return errors.New("rain: IN predicate requires at least one value")
		}
		if err := c.writeExpression(value.Left); err != nil {
			return err
		}
		c.writeString(" IN (")
		for idx, item := range value.Values {
			if idx > 0 {
				c.writeString(", ")
			}
			if err := c.writeExpression(item); err != nil {
				return err
			}
		}
		c.writeByte(')')
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
			if err := c.writePredicate(part); err != nil {
				return err
			}
		}
		c.writeByte(')')
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
			if err := c.writeExpression(value.Expr); err != nil {
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
			if err := c.writeExpression(part); err != nil {
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
