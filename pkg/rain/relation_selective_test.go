package rain_test

import (
	"context"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/hyperlocalise/rain-orm/pkg/rain"
	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

func TestSelectiveRelationLoading(t *testing.T) {
	ctx := context.Background()
	db, err := rain.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	type UsersTable struct {
		schema.TableModel
		ID    *schema.Column[int64]
		Name  *schema.Column[string]
		Email *schema.Column[string]
	}
	Users := schema.Define("users", func(t *UsersTable) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.Name = t.Text("name").NotNull()
		t.Email = t.Text("email").NotNull()
	})

	type PostsTable struct {
		schema.TableModel
		ID     *schema.Column[int64]
		UserID *schema.Column[int64]
		Title  *schema.Column[string]
		Body   *schema.Column[string]
	}
	Posts := schema.Define("posts", func(t *PostsTable) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.UserID = t.BigInt("user_id").NotNull().References(Users.ID)
		t.Title = t.Text("title").NotNull()
		t.Body = t.Text("body").NotNull()

		t.BelongsTo("author", t.UserID, Users.ID)
	})

	type User struct {
		ID    int64  `db:"id"`
		Name  string `db:"name"`
		Email string `db:"email"`
	}

	type Post struct {
		ID     int64  `db:"id"`
		UserID int64  `db:"user_id"`
		Title  string `db:"title"`
		Body   string `db:"body"`
		Author *User  `rain:"relation:author"`
	}

	createUsersSQL, _ := db.CreateTableSQL(Users)
	createPostsSQL, _ := db.CreateTableSQL(Posts)
	if _, err := db.Exec(ctx, createUsersSQL); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, createPostsSQL); err != nil {
		t.Fatal(err)
	}

	if _, err := db.Insert().Table(Users).Set(Users.Name, "Alice").Set(Users.Email, "alice@example.com").Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Insert().Table(Posts).Set(Posts.UserID, int64(1)).Set(Posts.Title, "Hello").Set(Posts.Body, "World").Exec(ctx); err != nil {
		t.Fatal(err)
	}

	// Test selective loading
	var posts []Post
	err = db.Select().From(Posts).Relation("author", rain.RelationConfig{
		Columns: []schema.Expression{Users.Name},
	}).Scan(ctx, &posts)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(posts) != 1 {
		t.Fatalf("expected 1 post, got %d", len(posts))
	}
	if posts[0].Author == nil {
		t.Fatal("expected author to be loaded")
	}
	if posts[0].Author.Name != "Alice" {
		t.Errorf("expected name Alice, got %s", posts[0].Author.Name)
	}
	if posts[0].Author.Email != "" {
		t.Errorf("expected email to be empty (not selected), got %s", posts[0].Author.Email)
	}
	if posts[0].Author.ID != 1 {
		t.Errorf("expected ID 1 (auto-included), got %d", posts[0].Author.ID)
	}
}
