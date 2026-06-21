package rain

import (
	"bytes"
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
	buffer      bytes.Buffer
	dialect     dialect.Dialect
	argPlan     []compiledArg
	hasNames    bool
	args        []any
	err         error
	skipCTEs    bool
	useLiterals bool
	quotedCache *sync.Map
	columnCache *sync.Map
	tableCache  *sync.Map
}

var compileContextPool = sync.Pool{
	New: func() any {
		ctx := &compileContext{
			argPlan: make([]compiledArg, 0, 8),
			args:    make([]any, 0, 8),
		}
		// OPTIMIZATION: Pre-allocate a reasonable buffer capacity to reduce early
		// re-allocations during query building. bytes.Buffer preserves this
		// capacity across Reset() calls.
		ctx.buffer.Grow(512)
		return ctx
	},
}

var (
	// OPTIMIZATION: Cache quoted identifiers per-dialect to avoid redundant
	// string allocations and escaping logic during query compilation.
	postgresQuotedCache sync.Map
	mysqlQuotedCache    sync.Map
	sqliteQuotedCache   sync.Map

	// OPTIMIZATION: Cache qualified column and table names per-dialect to avoid
	// repeated string concatenations and identifier quoting.
	postgresColumnCache sync.Map
	mysqlColumnCache    sync.Map
	sqliteColumnCache   sync.Map

	postgresTableCache sync.Map
	mysqlTableCache    sync.Map
	sqliteTableCache   sync.Map
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
	c.buffer.Reset()
	c.dialect = d
	// Clear the argPlan slice to ensure any values it contains can be
	// garbage collected before we reset its length for reuse.
	clear(c.argPlan)
	c.argPlan = c.argPlan[:0]
	c.hasNames = false
	clear(c.args)
	c.args = c.args[:0]
	c.err = nil
	c.skipCTEs = false
	c.useLiterals = false

	// OPTIMIZATION: Pre-select the quoted identifier cache to avoid repeated
	// name lookups or type assertions in the hot compilation loop.
	switch d.(type) {
	case *dialect.PostgresDialect:
		c.quotedCache = &postgresQuotedCache
		c.columnCache = &postgresColumnCache
		c.tableCache = &postgresTableCache
	case *dialect.MySQLDialect:
		c.quotedCache = &mysqlQuotedCache
		c.columnCache = &mysqlColumnCache
		c.tableCache = &mysqlTableCache
	case *dialect.SQLiteDialect:
		c.quotedCache = &sqliteQuotedCache
		c.columnCache = &sqliteColumnCache
		c.tableCache = &sqliteTableCache
	default:
		c.quotedCache = nil
		c.columnCache = nil
		c.tableCache = nil
	}
}

func (c *compileContext) String() string {
	return c.buffer.String()
}

func (c *compileContext) compiledQuery() compiledQuery {
	// OPTIMIZATION: Only copy the argPlan if the query has named placeholders.
	// Queries with only literals can use the pre-calculated args slice.
	var argPlan []compiledArg
	if c.hasNames {
		argPlan = append([]compiledArg(nil), c.argPlan...)
	}

	compiled := compiledQuery{
		sql:      c.String(),
		argPlan:  argPlan,
		hasNames: c.hasNames,
	}

	if len(c.args) > 0 {
		compiled.args = append([]any(nil), c.args...)
	}

	return compiled
}

func (c *compileContext) nextPlaceholderIndex() int {
	return len(c.args) + 1
}

func (c *compileContext) writeByte(ch byte) {
	c.buffer.WriteByte(ch)
}

func (c *compileContext) writeString(value string) {
	c.buffer.WriteString(value)
}

func (c *compileContext) ensureArgsCapacity(n int) {
	if c.hasNames {
		if needed := len(c.argPlan) + n; cap(c.argPlan) < needed {
			newPlan := make([]compiledArg, len(c.argPlan), needed)
			copy(newPlan, c.argPlan)
			c.argPlan = newPlan
		}
	}
	if needed := len(c.args) + n; cap(c.args) < needed {
		newArgs := make([]any, len(c.args), needed)
		copy(newArgs, c.args)
		c.args = newArgs
	}
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
	if c.tableCache == nil {
		c.writeTableName(table)
		if table.Alias != "" {
			c.writeString(" AS ")
			c.writeQuotedIdentifier(table.Alias)
		}
		return
	}

	if cached, ok := c.tableCache.Load(table); ok {
		c.writeString(cached.(string))
		return
	}

	// Capture the current buffer length to derive the table reference string.
	// OPTIMIZATION: Extracting the substring from the byte slice avoids
	// allocating the entire buffer string via c.buffer.String().
	start := c.buffer.Len()
	c.writeTableName(table)
	if table.Alias != "" {
		c.writeString(" AS ")
		c.writeQuotedIdentifier(table.Alias)
	}
	ref := string(c.buffer.Bytes()[start:])
	c.tableCache.Store(table, ref)
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

// writeJoinedPredicates renders a slice of predicates joined by AND.
// If wrap is true and there are multiple predicates, they are wrapped in
// parentheses.
// OPTIMIZATION: This method iterates over the predicates directly to avoid
// allocating intermediate schema.LogicalExpr objects (which would otherwise
// be created by joinPredicates). This reduces heap pressure during query
// compilation, particularly for complex queries with multiple WHERE or
// HAVING conditions.
// Top-level clauses (WHERE, HAVING) use wrap=false to avoid redundant parentheses.
func (c *compileContext) writeJoinedPredicates(predicates []schema.Predicate, wrap bool) error {
	if len(predicates) == 0 {
		return nil
	}
	if len(predicates) == 1 {
		return c.writePredicate(predicates[0])
	}

	if wrap {
		c.writeByte('(')
	}
	for idx, p := range predicates {
		if idx > 0 {
			c.writeString(" AND ")
		}
		if err := c.writePredicate(p); err != nil {
			return err
		}
	}
	if wrap {
		c.writeByte(')')
	}
	return nil
}

type expressionContext struct {
	allowAlias bool
	noParens   bool
}

func (c *compileContext) writeExpression(expr schema.Expression) error {
	return c.writeExpressionInContext(expr, expressionContext{})
}

func (c *compileContext) writeSelectExpression(expr schema.Expression) error {
	return c.writeExpressionInContext(expr, expressionContext{allowAlias: true})
}

func (c *compileContext) writeAny(val any) error {
	if expr, ok := val.(schema.Expression); ok {
		return c.writeExpression(expr)
	}

	if c.useLiterals {
		sql, err := literalDDLSQL(c.dialect, val)
		if err != nil {
			return err
		}
		c.writeString(sql)
	} else {
		index := c.nextPlaceholderIndex()
		if c.hasNames {
			c.argPlan = append(c.argPlan, compiledArg{kind: compiledArgLiteral, value: val})
		}
		c.args = append(c.args, val)
		c.writeString(c.dialect.Placeholder(index))
	}
	return nil
}

func (c *compileContext) writeExpressionInContext(expr schema.Expression, context expressionContext) error {
	switch value := expr.(type) {
	case schema.ColumnReference:
		c.writeColumn(value)
	case schema.ValueExpr:
		return c.writeAny(value.Value)
	case schema.PlaceholderExpr:
		if strings.TrimSpace(value.Name) == "" {
			return errors.New("rain: placeholder name cannot be empty")
		}
		c.activateNamedPlaceholders()
		index := c.nextPlaceholderIndex()
		c.argPlan = append(c.argPlan, compiledArg{kind: compiledArgNamedPlaceholder, name: value.Name})
		c.args = append(c.args, nil)
		c.writeString(c.dialect.Placeholder(index))
	case schema.ComparisonExpr:
		switch value.Operator {
		case "=", "<>", ">", ">=", "<", "<=", "LIKE", "NOT LIKE", "ILIKE", "NOT ILIKE":
			// ok
		default:
			return fmt.Errorf("rain: invalid comparison operator %q", value.Operator)
		}
		if err := c.writeExpression(value.Left); err != nil {
			return err
		}
		c.writeByte(' ')
		c.writeString(value.Operator)
		c.writeByte(' ')
		if err := c.writeExpression(value.Right); err != nil {
			return err
		}
	case schema.BinaryExpr:
		switch value.Operator {
		case "+", "-", "*", "/", "%":
			// ok
		default:
			return fmt.Errorf("rain: invalid binary operator %q", value.Operator)
		}
		c.writeByte('(')
		if err := c.writeExpression(value.Left); err != nil {
			return err
		}
		c.writeByte(' ')
		c.writeString(value.Operator)
		c.writeByte(' ')
		if err := c.writeExpression(value.Right); err != nil {
			return err
		}
		c.writeByte(')')
	case schema.ConcatExpr:
		if len(value.Exprs) < 2 {
			return errors.New("rain: CONCAT requires at least two expressions")
		}
		switch c.dialect.Name() {
		case "postgres", "sqlite":
			c.writeByte('(')
			for idx, expr := range value.Exprs {
				if idx > 0 {
					c.writeString(" || ")
				}
				if err := c.writeExpression(expr); err != nil {
					return err
				}
			}
			c.writeByte(')')
		case "mysql":
			c.writeString("CONCAT(")
			for idx, expr := range value.Exprs {
				if idx > 0 {
					c.writeString(", ")
				}
				if err := c.writeExpression(expr); err != nil {
					return err
				}
			}
			c.writeByte(')')
		default:
			return fmt.Errorf("rain: CONCAT is not implemented for %s dialect", c.dialect.Name())
		}
	case schema.InExpr:
		if len(value.Values) == 0 {
			return errors.New("rain: IN predicate requires at least one value")
		}
		if err := c.writeExpression(value.Left); err != nil {
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
			ctx := expressionContext{}
			if len(value.Values) == 1 {
				ctx.noParens = true
			}
			if err := c.writeExpressionInContext(item, ctx); err != nil {
				return err
			}
		}
		c.writeByte(')')
	case schema.BetweenExpr:
		if err := c.writeExpression(value.Left); err != nil {
			return err
		}
		if value.Negated {
			c.writeString(" NOT BETWEEN ")
		} else {
			c.writeString(" BETWEEN ")
		}
		if err := c.writeExpression(value.Start); err != nil {
			return err
		}
		c.writeString(" AND ")
		if err := c.writeExpression(value.End); err != nil {
			return err
		}
	case schema.NotExpr:
		c.writeString("NOT (")
		if err := c.writePredicate(value.Expr); err != nil {
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
			if err := c.writePredicate(part); err != nil {
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
			if err := c.writeExpression(value.ValueExpression); err != nil {
				return err
			}
		}
		for _, pair := range value.WhenThenPairs {
			c.writeString(" WHEN ")
			if err := c.writeExpression(pair.When); err != nil {
				return err
			}
			c.writeString(" THEN ")
			if err := c.writeExpression(pair.Then); err != nil {
				return err
			}
		}
		if value.ElseExpression != nil {
			c.writeString(" ELSE ")
			if err := c.writeExpression(value.ElseExpression); err != nil {
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
	case excludedColumn:
		if c.dialect.Name() == "mysql" {
			c.writeString("VALUES(")
			c.writeColumnName(value.column)
			c.writeByte(')')
		} else {
			c.writeString("EXCLUDED.")
			c.writeColumnName(value.column)
		}
	default:
		return fmt.Errorf("rain: unsupported expression type %T", expr)
	}

	return nil
}

func (c *compileContext) writeColumnName(column schema.ColumnReference) {
	c.writeQuotedIdentifier(column.ColumnDef().Name)
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
		val := raw.Args[argIndex]
		if err := c.writeAny(val); err != nil {
			return err
		}
		argIndex++
	}
	if argIndex != len(raw.Args) {
		return errors.New("rain: raw SQL has unused args")
	}

	return nil
}

func (c *compileContext) writeColumn(column schema.ColumnReference) {
	def := column.ColumnDef()
	if c.columnCache == nil {
		c.writeColumnInternal(def)
		return
	}

	if cached, ok := c.columnCache.Load(def); ok {
		c.writeString(cached.(string))
		return
	}

	// Capture the current buffer length to derive the qualified column string.
	// OPTIMIZATION: Extracting the substring from the byte slice avoids
	// allocating the entire buffer string via c.buffer.String().
	start := c.buffer.Len()
	c.writeColumnInternal(def)
	qualified := string(c.buffer.Bytes()[start:])
	c.columnCache.Store(def, qualified)
}

func (c *compileContext) activateNamedPlaceholders() {
	if c.hasNames {
		return
	}
	c.hasNames = true

	// Ensure argPlan has enough capacity and set its length to match c.args.
	// We use len(c.args)+1 to account for the named placeholder that is
	// typically appended immediately after activation.
	if cap(c.argPlan) < len(c.args)+1 {
		c.argPlan = make([]compiledArg, len(c.args), max(cap(c.argPlan), len(c.args)+1))
	} else {
		c.argPlan = c.argPlan[:len(c.args)]
	}

	for i, val := range c.args {
		c.argPlan[i] = compiledArg{kind: compiledArgLiteral, value: val}
	}
}

func (c *compileContext) writeColumnInternal(def *schema.ColumnDef) {
	table := def.Table
	qualifier := table.Name
	if table.Alias != "" {
		qualifier = table.Alias
	}

	c.writeQuotedIdentifier(qualifier)
	c.writeByte('.')
	c.writeQuotedIdentifier(def.Name)
}
