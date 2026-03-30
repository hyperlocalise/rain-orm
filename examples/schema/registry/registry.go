package registry

import (
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

type MembershipsTable struct {
	schema.TableModel
	UserID *schema.Column[int64]
	OrgID  *schema.Column[int64]
	Role   *schema.Column[string]
	Active *schema.Column[bool]
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

var Memberships = schema.Define("memberships", func(t *MembershipsTable) {
	t.UserID = t.BigInt("user_id").NotNull()
	t.OrgID = t.BigInt("org_id").NotNull()
	t.Role = t.Text("role").NotNull()
	t.Active = t.Boolean("active").NotNull().Default(true)
	t.PrimaryKey("memberships_pkey").On(t.UserID, t.OrgID)
	t.Unique("memberships_org_role_key").On(t.OrgID, t.Role)
	t.Check("memberships_role_check", t.Role.In("owner", "member"))
	t.ForeignKey("memberships_user_fk").On(t.UserID).References(Users.ID).OnDelete(schema.ForeignKeyActionCascade)
	t.Index("memberships_role_idx").On(t.Role, t.OrgID.Desc())
})

// ManagedTables exposes the example schema registry used by the CLI.
func ManagedTables() []schema.TableReference {
	return []schema.TableReference{Users, Posts, Memberships}
}
