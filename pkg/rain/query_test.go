package rain_test

import (
	"time"

	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

type usersTable struct {
	schema.TableModel
	ID        *schema.Column[int64]
	Email     *schema.Column[string]
	Name      *schema.Column[string]
	Active    *schema.Column[bool]
	CreatedAt *schema.Column[time.Time]
}

type postsTable struct {
	schema.TableModel
	ID     *schema.Column[int64]
	UserID *schema.Column[int64]
	Title  *schema.Column[string]
}

type expandedTypesTable struct {
	schema.TableModel
	ID          *schema.Column[int64]
	SmallCount  *schema.Column[int16]
	Count       *schema.Column[int32]
	Score       *schema.Column[float32]
	Precise     *schema.Column[float64]
	Amount      *schema.Column[string]
	Meta        *schema.Column[any]
	MetaBin     *schema.Column[any]
	ExternalID  *schema.Column[string]
	Payload     *schema.Column[[]byte]
	PublishedOn *schema.Column[time.Time]
	ProcessedAt *schema.Column[time.Time]
	Category    *schema.Column[string]
}

type userModel struct {
	ID     int64
	Email  string
	Name   string
	Active bool
}

func defineTables() (*usersTable, *postsTable) {
	users := schema.Define("users", func(t *usersTable) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.Email = t.VarChar("email", 255).NotNull().Unique()
		t.Name = t.Text("name").NotNull()
		t.Active = t.Boolean("active").NotNull().Default(true)
		t.CreatedAt = t.TimestampTZ("created_at").NotNull().DefaultNow()
	})

	posts := schema.Define("posts", func(t *postsTable) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.UserID = t.BigInt("user_id").NotNull().References(users.ID)
		t.Title = t.Text("title").NotNull()
	})

	return users, posts
}

func defineExpandedTypesTable() *expandedTypesTable {
	return schema.Define("expanded_types", func(t *expandedTypesTable) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.SmallCount = t.SmallInt("small_count").NotNull()
		t.Count = t.Integer("count").NotNull()
		t.Score = t.Real("score").NotNull()
		t.Precise = t.Double("precise").NotNull()
		t.Amount = t.Decimal("amount", 12, 2).NotNull()
		t.Meta = t.JSON("meta").NotNull()
		t.MetaBin = t.JSONB("meta_bin").NotNull()
		t.ExternalID = t.UUID("external_id").NotNull()
		t.Payload = t.Bytes("payload").NotNull()
		t.PublishedOn = t.Date("published_on").NotNull()
		t.ProcessedAt = t.Timestamp("processed_at").NotNull()
		t.Category = t.Enum("category", "alpha", "beta").NotNull()
	})
}
