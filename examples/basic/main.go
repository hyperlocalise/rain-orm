package main

import (
	"fmt"
	"time"

	"github.com/hyperlocalise/rain-orm/pkg/rain"
	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

type UsersTable struct {
	schema.TableModel
	ID        *schema.Column[int64]
	Email     *schema.Column[string]
	Name      *schema.Column[string]
	Active    *schema.Column[bool]
	CreatedAt *schema.Column[time.Time]
}

type PostsTable struct {
	schema.TableModel
	ID     *schema.Column[int64]
	UserID *schema.Column[int64]
	Title  *schema.Column[string]
	Body   *schema.Column[string]
}

type User struct {
	ID     int64  `db:"id"`
	Email  string `db:"email"`
	Name   string `db:"name"`
	Active bool   `db:"active"`
}

var Users = schema.Define("users", func(t *UsersTable) {
	t.ID = t.BigSerial("id").PrimaryKey()
	t.Email = t.VarChar("email", 255).NotNull().Unique()
	t.Name = t.Text("name").NotNull()
	t.Active = t.Boolean("active").NotNull().Default(true)
	t.CreatedAt = t.TimestampTZ("created_at").NotNull().DefaultNow()
})

var Posts = schema.Define("posts", func(t *PostsTable) {
	t.ID = t.BigSerial("id").PrimaryKey()
	t.UserID = t.BigInt("user_id").NotNull().References(Users.ID)
	t.Title = t.Text("title").NotNull()
	t.Body = t.Text("body").NotNull()
})

func main() {
	db, err := rain.Open("postgres", "postgres://example")
	if err != nil {
		panic(err)
	}
	defer func() { _ = db.Close() }()

	u := schema.Alias(Users, "u")
	p := schema.Alias(Posts, "p")

	selectSQL, selectArgs, _ := db.Select().
		Table(p).
		Column(p.ID, p.Title, u.Email).
		Join(u, p.UserID.EqCol(u.ID)).
		Where(u.Active.Eq(true)).
		OrderBy(p.ID.Desc()).
		Limit(10).
		ToSQL()

	aggSQL, aggArgs, _ := db.Select().
		Table(p).
		Column(
			p.UserID,
			schema.As(schema.Count(), "post_count"),
			schema.As(schema.Max(p.ID), "last_post_id"),
		).
		GroupBy(p.UserID).
		Having(schema.ComparisonExpr{Left: schema.Count(), Operator: ">", Right: schema.ValueExpr{Value: 1}}).
		ToSQL()

	insertSQL, insertArgs, _ := db.Insert().
		Table(Users).
		Model(&User{Email: "alice@example.com", Name: "Alice", Active: true}).
		Returning(Users.ID).
		ToSQL()

	updateSQL, updateArgs, _ := db.Update().
		Table(Users).
		Set(Users.Name, "Alice Smith").
		Where(Users.ID.Eq(int64(1))).
		ToSQL()

	deleteSQL, deleteArgs, _ := db.Delete().
		Table(Users).
		Where(Users.ID.Eq(int64(99))).
		ToSQL()

	fmt.Println(selectSQL, selectArgs)
	fmt.Println(aggSQL, aggArgs)
	fmt.Println(insertSQL, insertArgs)
	fmt.Println(updateSQL, updateArgs)
	fmt.Println(deleteSQL, deleteArgs)
}
