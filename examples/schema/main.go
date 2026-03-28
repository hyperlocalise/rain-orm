// Package main demonstrates schema definition with Rain ORM.
//
// This example shows:
// - Programmatic schema definition
// - Column types and constraints
// - Index definitions
package main

import (
	"fmt"

	"github.com/quiet-circles/rain-orm/pkg/schema"
)

func main() {
	// Create a new schema
	s := schema.NewSchema()

	// Define users table
	usersTable := s.CreateTable("users", func(t *schema.TableBuilder) {
		// Primary key - auto incrementing
		t.Column("id", schema.TypeBigSerial).PrimaryKey()

		// Required fields
		t.Column("email", schema.TypeVarchar).NotNull().Unique()
		t.Column("username", schema.TypeVarchar).NotNull().Unique()
		t.Column("password_hash", schema.TypeVarchar).NotNull()

		// Optional fields
		t.Column("display_name", schema.TypeVarchar).Nullable()
		t.Column("bio", schema.TypeText).Nullable()
		t.Column("avatar_url", schema.TypeVarchar).Nullable()

		// Boolean with default
		t.Column("email_verified", schema.TypeBoolean).NotNull().Default(false)
		t.Column("is_admin", schema.TypeBoolean).NotNull().Default(false)
		t.Column("is_active", schema.TypeBoolean).NotNull().Default(true)

		// Timestamps
		t.Column("created_at", schema.TypeTimestampTZ).NotNull().Default("CURRENT_TIMESTAMP")
		t.Column("updated_at", schema.TypeTimestampTZ).NotNull().Default("CURRENT_TIMESTAMP")
		t.Column("last_login_at", schema.TypeTimestampTZ).Nullable()

		// Indexes
		t.Index("idx_users_email", "email")
		t.Index("idx_users_username", "username")
		t.Index("idx_users_created", "created_at")

		// Composite index for common queries
		t.Index("idx_users_active_admin", "is_active", "is_admin")
	})

	fmt.Println("Users table defined:")
	printTable(usersTable)

	// Define posts table with foreign keys
	postsTable := s.CreateTable("posts", func(t *schema.TableBuilder) {
		t.Column("id", schema.TypeBigSerial).PrimaryKey()
		t.Column("user_id", schema.TypeBigInt).NotNull().
			References("users", "id")

		t.Column("slug", schema.TypeVarchar).NotNull().Unique()
		t.Column("title", schema.TypeVarchar).NotNull()
		t.Column("excerpt", schema.TypeText).Nullable()
		t.Column("content", schema.TypeText).NotNull()

		// Post metadata
		t.Column("status", schema.TypeVarchar).NotNull().Default("'draft'")
		t.Column("published_at", schema.TypeTimestampTZ).Nullable()

		// Statistics
		t.Column("view_count", schema.TypeInteger).NotNull().Default(0)
		t.Column("like_count", schema.TypeInteger).NotNull().Default(0)

		// JSON for flexible metadata
		t.Column("metadata", schema.TypeJSONB).Nullable()

		// Timestamps
		t.Column("created_at", schema.TypeTimestampTZ).NotNull()
		t.Column("updated_at", schema.TypeTimestampTZ).NotNull()

		// Indexes for common queries
		t.Index("idx_posts_user", "user_id")
		t.Index("idx_posts_slug", "slug")
		t.Index("idx_posts_status", "status")
		t.Index("idx_posts_published", "published_at")
		t.UniqueIndex("idx_posts_user_slug", "user_id", "slug")
	})

	fmt.Println("\nPosts table defined:")
	printTable(postsTable)

	// Define comments table
	commentsTable := s.CreateTable("comments", func(t *schema.TableBuilder) {
		t.Column("id", schema.TypeBigSerial).PrimaryKey()
		t.Column("post_id", schema.TypeBigInt).NotNull().
			References("posts", "id")
		t.Column("user_id", schema.TypeBigInt).NotNull().
			References("users", "id")
		t.Column("parent_id", schema.TypeBigInt).Nullable().
			References("comments", "id")

		t.Column("content", schema.TypeText).NotNull()
		t.Column("is_approved", schema.TypeBoolean).NotNull().Default(false)

		t.Column("created_at", schema.TypeTimestampTZ).NotNull()
		t.Column("updated_at", schema.TypeTimestampTZ).NotNull()

		t.Index("idx_comments_post", "post_id")
		t.Index("idx_comments_user", "user_id")
		t.Index("idx_comments_parent", "parent_id")
		t.Index("idx_comments_approved", "is_approved", "created_at")
	})

	fmt.Println("\nComments table defined:")
	printTable(commentsTable)

	// Example: Getting table from schema
	if table, ok := s.GetTable("users"); ok {
		fmt.Printf("\nRetrieved '%s' table with %d columns\n", table.Name, len(table.Columns))
	}
}

func printTable(table *schema.Table) {
	fmt.Printf("  Table: %s\n", table.Name)
	fmt.Println("  Columns:")
	for _, col := range table.Columns {
		nullable := "NOT NULL"
		if col.Nullable {
			nullable = "NULL"
		}
		pk := ""
		if col.PrimaryKey {
			pk = "PRIMARY KEY"
		}
		unique := ""
		if col.Unique {
			unique = "UNIQUE"
		}
		fmt.Printf("    - %s %s %s %s %s\n", col.Name, col.Type, nullable, pk, unique)
	}
	if len(table.Indexes) > 0 {
		fmt.Println("  Indexes:")
		for _, idx := range table.Indexes {
			unique := ""
			if idx.Unique {
				unique = "UNIQUE "
			}
			fmt.Printf("    - %s%s on (%v)\n", unique, idx.Name, idx.Columns)
		}
	}
}
