package schema_test

import (
	"testing"
	"time"

	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

type usersTable struct {
	schema.TableModel
	ID        *schema.Column[int64]
	Email     *schema.Column[string]
	Active    *schema.Column[bool]
	CreatedAt *schema.Column[time.Time]
}

type postsTable struct {
	schema.TableModel
	ID     *schema.Column[int64]
	UserID *schema.Column[int64]
	Title  *schema.Column[string]
}

type auditColumns struct {
	CreatedAt *schema.Column[time.Time]
}

type embeddedUsersTable struct {
	schema.TableModel
	ID    *schema.Column[int64]
	Email *schema.Column[string]
	auditColumns
}

func TestSchemaMetadataAndAlias(t *testing.T) {
	users := schema.Define("users", func(tu *usersTable) {
		tu.ID = tu.BigSerial("id").PrimaryKey()
		tu.Email = tu.VarChar("email", 255).NotNull().Unique()
		tu.Active = tu.Boolean("active").NotNull().Default(true)
		tu.CreatedAt = tu.TimestampTZ("created_at").NotNull().DefaultNow()
		tu.UniqueIndex("users_email_key").On(tu.Email)
		tu.Index("users_active_created_idx").On(tu.Active, tu.CreatedAt.Desc())
	})

	posts := schema.Define("posts", func(tp *postsTable) {
		tp.ID = tp.BigSerial("id").PrimaryKey()
		tp.UserID = tp.BigInt("user_id").NotNull().References(users.ID)
		tp.Title = tp.Text("title").NotNull()
	})

	if got := len(users.TableDef().Indexes); got != 2 {
		t.Fatalf("expected 2 indexes, got %d", got)
	}
	if users.TableDef().Indexes[1].Columns[1].Direction != schema.SortDesc {
		t.Fatalf("expected descending index column")
	}
	if got := len(posts.TableDef().ForeignKeys); got != 1 {
		t.Fatalf("expected 1 foreign key, got %d", got)
	}
	if posts.TableDef().ForeignKeys[0].ReferencedColumn.Name != "id" {
		t.Fatalf("expected foreign key to users.id")
	}

	aliased := schema.Alias(users, "u")
	if users.TableDef().Alias != "" {
		t.Fatalf("base table alias mutated")
	}
	if aliased.TableDef().Alias != "u" {
		t.Fatalf("expected alias u, got %q", aliased.TableDef().Alias)
	}
	if aliased.ID.ColumnDef().Table.Alias != "u" {
		t.Fatalf("expected aliased column metadata to point at aliased table")
	}
}

func TestAliasRebindsEmbeddedColumns(t *testing.T) {
	users := schema.Define("users", func(tu *embeddedUsersTable) {
		tu.ID = tu.BigSerial("id").PrimaryKey()
		tu.Email = tu.VarChar("email", 255).NotNull()
		tu.CreatedAt = tu.TimestampTZ("created_at").NotNull().DefaultNow()
	})

	aliased := schema.Alias(users, "u")
	if aliased.CreatedAt == nil {
		t.Fatalf("expected embedded column to be initialized")
	}
	if aliased.CreatedAt.ColumnDef().Table.Alias != "u" {
		t.Fatalf("expected embedded column to point at aliased table")
	}
	if users.CreatedAt.ColumnDef().Table.Alias != "" {
		t.Fatalf("base embedded column metadata mutated")
	}
}
