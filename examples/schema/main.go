package main

import (
	"fmt"
	"time"

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

var Users = schema.Define("users", func(t *UsersTable) {
	t.ID = t.BigSerial("id").PrimaryKey()
	t.Email = t.VarChar("email", 255).NotNull().Unique()
	t.Name = t.Text("name").NotNull()
	t.Active = t.Boolean("active").NotNull().Default(true)
	t.CreatedAt = t.TimestampTZ("created_at").NotNull().DefaultNow()
	t.Index("users_email_key").On(t.Email)
	t.UniqueIndex("users_active_email_idx").On(t.Active, t.Email.Desc())
})

var Posts = schema.Define("posts", func(t *PostsTable) {
	t.ID = t.BigSerial("id").PrimaryKey()
	t.UserID = t.BigInt("user_id").NotNull().References(Users.ID)
	t.Title = t.Text("title").NotNull()
	t.Body = t.Text("body").NotNull()
	t.Index("posts_user_id_idx").On(t.UserID)
})

func main() {
	fmt.Printf("table=%s columns=%d indexes=%d fks=%d\n",
		Users.TableDef().Name,
		len(Users.TableDef().Columns),
		len(Users.TableDef().Indexes),
		len(Users.TableDef().ForeignKeys),
	)
	fmt.Printf("posts fk: %s -> %s.%s\n",
		Posts.TableDef().ForeignKeys[0].Column.Name,
		Posts.TableDef().ForeignKeys[0].ReferencedTable.Name,
		Posts.TableDef().ForeignKeys[0].ReferencedColumn.Name,
	)
}
