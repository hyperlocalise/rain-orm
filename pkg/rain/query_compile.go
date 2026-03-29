package rain

import (
	"errors"
	"fmt"
	"strings"

	"github.com/hyperlocalise/rain-orm/pkg/dialect"
	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

type compileContext struct {
	builder strings.Builder
	dialect dialect.Dialect
	args    []any
	err     error
}

func newCompileContext(d dialect.Dialect) *compileContext {
	return &compileContext{
		dialect: d,
		args:    make([]any, 0, 8),
	}
}

func (c *compileContext) String() string {
	return c.builder.String()
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
		c.args = append(c.args, value.Value)
		c.writeString(c.dialect.Placeholder(len(c.args)))
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
		c.args = append(c.args, raw.Args[argIndex])
		c.writeString(c.dialect.Placeholder(len(c.args)))
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
