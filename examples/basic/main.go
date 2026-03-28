// Package main demonstrates basic Rain ORM usage.
//
// This example shows:
// - Database connection
// - Basic CRUD operations
// - Query building
// - Transactions
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/quiet-circles/rain-orm/pkg/rain"
	"github.com/quiet-circles/rain-orm/pkg/schema"
)

// User represents a user in the system.
// Embed schema.Timestamps to automatically manage created_at/updated_at.
type User struct {
	ID     int64  `db:"id"`
	Email  string `db:"email"`
	Name   string `db:"name"`
	Age    int    `db:"age"`
	Active bool   `db:"active"`
	schema.Timestamps
}

// Post represents a blog post.
type Post struct {
	ID        int64  `db:"id"`
	UserID    int64  `db:"user_id"`
	Title     string `db:"title"`
	Content   string `db:"content"`
	Published bool   `db:"published"`
	schema.Timestamps
}

func main() {
	// Open database connection
	db := rain.Open("postgres", "postgres://user:pass@localhost/mydb")
	defer db.Close()

	ctx := context.Background()

	// ========== CREATE ==========
	// Insert a new user
	newUser := User{
		Email:  "alice@example.com",
		Name:   "Alice",
		Age:    30,
		Active: true,
	}

	err := db.Model(&newUser).Create()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Created user with ID: %d\n", newUser.ID)

	// ========== READ ==========
	// Find a single user by ID
	var user User
	err = db.Model(&User{}).Where("id", "=", 1).First(&user)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Found user: %+v\n", user)

	// Find all active users
	var users []User
	err = db.Model(&User{}).
		Where("active", "=", true).
		OrderBy("created_at DESC").
		Find(&users)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Found %d active users\n", len(users))

	// Query with multiple conditions
	var adults []User
	err = db.Select("*").From("users").
		Where("age", ">=", 18).
		Where("active", "=", true).
		Limit(10).
		Find(&adults)
	if err != nil {
		log.Fatal(err)
	}

	// ========== UPDATE ==========
	// Update a user's email
	affected, err := db.Update("users").
		Set("email", "alice.new@example.com").
		Set("updated_at", time.Now()).
		Where("id", "=", 1).
		Update()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Updated %d rows\n", affected)

	// Update using model
	user.Name = "Alice Smith"
	err = db.Model(&user).Save()
	if err != nil {
		log.Fatal(err)
	}

	// ========== DELETE ==========
	// Soft delete (recommended) - update deleted_at
	affected, err = db.Update("users").
		Set("deleted_at", time.Now()).
		Where("id", "=", 1).
		Update()
	if err != nil {
		log.Fatal(err)
	}

	// Hard delete
	affected, err = db.Delete("users").Where("id", "=", 99).Delete()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Deleted %d rows\n", affected)

	// ========== TRANSACTIONS ==========
	tx, err := db.Begin(ctx)
	if err != nil {
		log.Fatal(err)
	}

	// Perform operations within transaction
	post := Post{
		UserID:    user.ID,
		Title:     "My First Post",
		Content:   "Hello, World!",
		Published: true,
	}
	err = tx.Model(&post).Create()
	if err != nil {
		tx.Rollback()
		log.Fatal(err)
	}

	err = tx.Commit()
	if err != nil {
		log.Fatal(err)
	}

	// ========== ADVANCED QUERIES ==========
	// Join example
	var results []struct {
		UserName  string `db:"user_name"`
		PostTitle string `db:"post_title"`
	}

	err = db.Select("u.name as user_name, p.title as post_title").
		From("users u").
		InnerJoin("posts p", "p.user_id = u.id").
		Where("p.published", "=", true).
		Find(&results)
	if err != nil {
		log.Fatal(err)
	}

	// Count
	count, err := db.Model(&User{}).Where("active", "=", true).Count()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Active users count: %d\n", count)

	// Check existence
	exists, err := db.Model(&User{}).Where("email", "=", "alice@example.com").Exists()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("User exists: %v\n", exists)

	// Raw SQL for complex queries
	_, err = db.Exec(ctx, "ANALYZE users")
	if err != nil {
		log.Fatal(err)
	}
}
