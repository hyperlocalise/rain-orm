// Package main demonstrates dialect-specific SQL generation.
//
// This example shows:
// - Using different database dialects
// - Dialect-specific syntax differences
package main

import (
	"fmt"

	"github.com/quiet-circles/rain-orm/pkg/dialect"
)

func main() {
	// Test with PostgreSQL
	fmt.Println("=== PostgreSQL ===")
	demoDialect(dialect.GetDialect("postgres"))

	// Test with MySQL
	fmt.Println("\n=== MySQL ===")
	demoDialect(dialect.GetDialect("mysql"))

	// Test with SQLite
	fmt.Println("\n=== SQLite ===")
	demoDialect(dialect.GetDialect("sqlite"))
}

func demoDialect(d dialect.Dialect) {
	fmt.Printf("Dialect: %s\n", d.Name())

	// Identifier quoting
	fmt.Printf("Quote 'users': %s\n", d.QuoteIdentifier("users"))
	fmt.Printf("Quote 'email': %s\n", d.QuoteIdentifier("email"))

	// Placeholders
	fmt.Printf("Placeholder 1: %s\n", d.Placeholder(1))
	fmt.Printf("Placeholder 2: %s\n", d.Placeholder(2))

	// Data types
	fmt.Printf("Type 'string': %s\n", d.DataType("string", 255))
	fmt.Printf("Type 'int64': %s\n", d.DataType("int64", 0))
	fmt.Printf("Type 'bool': %s\n", d.DataType("bool", 0))

	// Auto increment
	fmt.Printf("Auto increment keyword: %s\n", d.AutoIncrementKeyword())

	// Limit/Offset
	fmt.Printf("LIMIT 10: %s\n", d.LimitOffset(10, 0))
	fmt.Printf("LIMIT 10 OFFSET 20: %s\n", d.LimitOffset(10, 20))

	// RETURNING support
	fmt.Printf("Supports RETURNING: %v\n", d.ReturningClause())

	// Boolean literals
	fmt.Printf("Boolean true: %s\n", d.BooleanLiteral(true))
	fmt.Printf("Boolean false: %s\n", d.BooleanLiteral(false))

	// Current timestamp
	fmt.Printf("Current timestamp: %s\n", d.CurrentTimestamp())

	// Upsert
	fmt.Printf("Upsert clause: %s\n", d.UpsertClause("users", []string{"email"}, []string{"name"}))
}
