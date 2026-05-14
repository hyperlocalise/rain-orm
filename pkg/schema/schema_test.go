package schema_test

import (
	"reflect"
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

type membershipsTable struct {
	schema.TableModel
	UserID  *schema.Column[int64]
	OrgID   *schema.Column[int64]
	Role    *schema.Column[string]
	Active  *schema.Column[bool]
	Manager *schema.Column[int64]
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

type expandedTypesTable struct {
	schema.TableModel
	TinyScore       *schema.Column[int16]
	Quantity        *schema.Column[int32]
	Ratio           *schema.Column[float32]
	Weight          *schema.Column[float64]
	Amount          *schema.Column[string]
	Metadata        *schema.Column[any]
	MetadataB       *schema.Column[any]
	ExternalID      *schema.Column[string]
	Payload         *schema.Column[[]byte]
	PublishedOn     *schema.Column[time.Time]
	ProcessedAt     *schema.Column[time.Time]
	ProcessedAtPrec *schema.Column[time.Time]
	ReviewedAtPrec  *schema.Column[time.Time]
	Visibility      *schema.Column[string]
	Description     *schema.Column[string]
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

	memberships := schema.Define("memberships", func(tm *membershipsTable) {
		tm.UserID = tm.BigInt("user_id").NotNull()
		tm.OrgID = tm.BigInt("org_id").NotNull()
		tm.Role = tm.Text("role").NotNull()
		tm.Active = tm.Boolean("active").NotNull().Default(true)
		tm.Manager = tm.BigInt("manager").Nullable()
		tm.PrimaryKey("memberships_pkey").On(tm.UserID, tm.OrgID)
		tm.Unique("memberships_role_org_key").On(tm.OrgID, tm.Role)
		tm.Check("memberships_active_manager_check", schema.Or(tm.Active.Eq(true), tm.Manager.IsNotNull()))
		tm.ForeignKey("memberships_user_fk").On(tm.UserID).References(users.ID).OnDelete(schema.ForeignKeyActionCascade).OnUpdate(schema.ForeignKeyActionRestrict)
	})
	if got := len(memberships.TableDef().Constraints); got != 4 {
		t.Fatalf("expected 4 constraints, got %d", got)
	}
	if memberships.TableDef().Constraints[0].Type != schema.ConstraintPrimaryKey {
		t.Fatalf("expected first constraint to be primary key")
	}
	if memberships.TableDef().Constraints[1].Type != schema.ConstraintUnique {
		t.Fatalf("expected second constraint to be unique")
	}
	if memberships.TableDef().Constraints[2].Type != schema.ConstraintCheck {
		t.Fatalf("expected third constraint to be check")
	}
	if memberships.TableDef().Constraints[3].OnDelete != schema.ForeignKeyActionCascade || memberships.TableDef().Constraints[3].OnUpdate != schema.ForeignKeyActionRestrict {
		t.Fatalf("expected foreign key actions to be preserved")
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

	aliasedMemberships := schema.Alias(memberships, "m")
	if aliasedMemberships.TableDef().Constraints[2].Check.(schema.LogicalExpr).Exprs[1].(schema.NullCheckExpr).Expr.(schema.ColumnReference).ColumnDef().Table.Alias != "m" {
		t.Fatalf("expected check constraint columns to point at aliased table")
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

func TestExpandedColumnTypesMetadata(t *testing.T) {
	table := schema.Define("expanded", func(tt *expandedTypesTable) {
		tt.TinyScore = tt.SmallInt("tiny_score")
		tt.Quantity = tt.Integer("quantity")
		tt.Ratio = tt.Real("ratio")
		tt.Weight = tt.Double("weight")
		tt.Amount = tt.Decimal("amount", 12, 2)
		tt.Metadata = tt.JSON("metadata")
		tt.MetadataB = tt.JSONB("metadata_b")
		tt.ExternalID = tt.UUID("external_id")
		tt.Payload = tt.Bytes("payload")
		tt.PublishedOn = tt.Date("published_on")
		tt.ProcessedAt = tt.Timestamp("processed_at")
		tt.ProcessedAtPrec = tt.TimestampPrecision("processed_at_prec", 6)
		tt.ReviewedAtPrec = tt.TimestampTZPrecision("reviewed_at_prec", 3)
		tt.Visibility = tt.Enum("visibility", "public", "private")
		tt.Description = tt.Text("description")
	})

	cases := []struct {
		name      string
		dataType  schema.DataType
		size      int
		precision int
		scale     int
		timePrec  int
		tsKind    schema.TimestampKind
		enum      []string
	}{
		{name: "tiny_score", dataType: schema.TypeSmallInt},
		{name: "quantity", dataType: schema.TypeInteger},
		{name: "ratio", dataType: schema.TypeReal},
		{name: "weight", dataType: schema.TypeDouble},
		{name: "amount", dataType: schema.TypeDecimal, precision: 12, scale: 2},
		{name: "metadata", dataType: schema.TypeJSON},
		{name: "metadata_b", dataType: schema.TypeJSONB},
		{name: "external_id", dataType: schema.TypeUUID},
		{name: "payload", dataType: schema.TypeBytes},
		{name: "published_on", dataType: schema.TypeDate},
		{name: "processed_at", dataType: schema.TypeTimestamp, tsKind: schema.TimestampKindWithoutTZ},
		{name: "processed_at_prec", dataType: schema.TypeTimestamp, timePrec: 6, tsKind: schema.TimestampKindWithoutTZ},
		{name: "reviewed_at_prec", dataType: schema.TypeTimestampTZ, timePrec: 3, tsKind: schema.TimestampKindWithTZ},
		{name: "visibility", dataType: schema.TypeEnum, enum: []string{"public", "private"}},
		{name: "description", dataType: schema.TypeText},
	}

	for _, tc := range cases {
		column, ok := table.TableDef().ColumnByName(tc.name)
		if !ok {
			t.Fatalf("expected to find column %q", tc.name)
		}
		if column.Type.DataType != tc.dataType {
			t.Fatalf("column %q expected type %q got %q", tc.name, tc.dataType, column.Type.DataType)
		}
		if column.Type.Size != tc.size {
			t.Fatalf("column %q expected size %d got %d", tc.name, tc.size, column.Type.Size)
		}
		if column.Type.Precision != tc.precision || column.Type.Scale != tc.scale {
			t.Fatalf("column %q expected precision/scale %d/%d got %d/%d", tc.name, tc.precision, tc.scale, column.Type.Precision, column.Type.Scale)
		}
		if column.Type.TimePrecision != tc.timePrec {
			t.Fatalf("column %q expected time precision %d got %d", tc.name, tc.timePrec, column.Type.TimePrecision)
		}
		if column.Type.TimestampKind != tc.tsKind {
			t.Fatalf("column %q expected timestamp kind %q got %q", tc.name, tc.tsKind, column.Type.TimestampKind)
		}
		if len(column.Type.EnumValues) != len(tc.enum) {
			t.Fatalf("column %q expected %d enum values got %d", tc.name, len(tc.enum), len(column.Type.EnumValues))
		}
		for idx := range tc.enum {
			if column.Type.EnumValues[idx] != tc.enum[idx] {
				t.Fatalf("column %q enum[%d] expected %q got %q", tc.name, idx, tc.enum[idx], column.Type.EnumValues[idx])
			}
		}
	}
}

func TestRelationMetadataRegistration(t *testing.T) {
	users := schema.Define("users", func(tu *usersTable) {
		tu.ID = tu.BigSerial("id").PrimaryKey()
		tu.Email = tu.VarChar("email", 255).NotNull()
		tu.Active = tu.Boolean("active").NotNull().Default(true)
		tu.CreatedAt = tu.TimestampTZ("created_at").NotNull().DefaultNow()
	})
	posts := schema.Define("posts", func(tp *postsTable) {
		tp.ID = tp.BigSerial("id").PrimaryKey()
		tp.UserID = tp.BigInt("user_id").NotNull().References(users.ID)
		tp.Title = tp.Text("title").NotNull()
		tp.BelongsTo("author", tp.UserID, users.ID)
	})

	if got := len(posts.TableDef().Relations); got != 1 {
		t.Fatalf("expected 1 relation, got %d", got)
	}
	relation, ok := posts.TableDef().RelationByName("author")
	if !ok {
		t.Fatalf("expected relation author")
	}
	if relation.Type != schema.RelationTypeBelongsTo {
		t.Fatalf("expected belongs_to relation, got %q", relation.Type)
	}
	if relation.SourceColumn.Name != "user_id" || relation.TargetColumn.Name != "id" || relation.TargetTable.Name != "users" {
		t.Fatalf("unexpected relation metadata: %#v", relation)
	}
}

func TestComputedExpressionHelpers(t *testing.T) {
	users := schema.Define("users", func(tu *usersTable) {
		tu.ID = tu.BigSerial("id").PrimaryKey()
		tu.Email = tu.VarChar("email", 255).NotNull()
		tu.Active = tu.Boolean("active").NotNull().Default(true)
		tu.CreatedAt = tu.TimestampTZ("created_at").NotNull().DefaultNow()
	})

	if got := schema.Count(); !got.Star || got.Function != "COUNT" {
		t.Fatalf("expected COUNT(*) aggregate helper, got %#v", got)
	}
	if got := schema.Sum(users.ID); got.Function != "SUM" || got.Expr == nil {
		t.Fatalf("expected SUM aggregate helper, got %#v", got)
	}
	if got := schema.Coalesce(users.Email, schema.ValueExpr{Value: ""}); len(got.Exprs) != 2 {
		t.Fatalf("expected COALESCE helper with 2 expressions, got %#v", got)
	}
	aliased := schema.Max(users.ID).As("max_id")
	if aliased.Alias != "max_id" {
		t.Fatalf("expected alias max_id, got %q", aliased.Alias)
	}
	coalesceAlias := schema.Coalesce(users.Email, schema.ValueExpr{Value: ""}).As("safe_email")
	if coalesceAlias.Alias != "safe_email" {
		t.Fatalf("expected alias safe_email, got %q", coalesceAlias.Alias)
	}
	columnAlias := users.Email.As("user_email")
	if columnAlias.Alias != "user_email" {
		t.Fatalf("expected alias user_email, got %q", columnAlias.Alias)
	}
	rawAlias := schema.Raw("COUNT(*)").As("post_count")
	if rawAlias.Alias != "post_count" {
		t.Fatalf("expected alias post_count, got %q", rawAlias.Alias)
	}
}

func TestCaseExpressionMetadata(t *testing.T) {
	users := schema.Define("users", func(tu *usersTable) {
		tu.ID = tu.BigSerial("id").PrimaryKey()
		tu.Active = tu.Boolean("active").NotNull().Default(true)
	})

	// Searched CASE
	searched := schema.Case().
		When(users.Active.Eq(true), schema.ValueExpr{Value: "active"}).
		When(users.Active.Eq(false), schema.ValueExpr{Value: "inactive"}).
		Else(schema.ValueExpr{Value: "unknown"}).
		End()

	if searched.ValueExpression != nil {
		t.Fatalf("expected nil ValueExpression for searched CASE")
	}
	if len(searched.WhenThenPairs) != 2 {
		t.Fatalf("expected 2 WHEN clauses, got %d", len(searched.WhenThenPairs))
	}
	if searched.ElseExpression == nil {
		t.Fatalf("expected non-nil ElseExpression")
	}

	// Simple CASE
	simple := schema.Case(users.ID).
		When(schema.ValueExpr{Value: int64(1)}, schema.ValueExpr{Value: "one"}).
		End()

	if simple.ValueExpression == nil {
		t.Fatalf("expected non-nil ValueExpression for simple CASE")
	}
	if len(simple.WhenThenPairs) != 1 {
		t.Fatalf("expected 1 WHEN clause, got %d", len(simple.WhenThenPairs))
	}
}

func TestInSubqueryPredicate(t *testing.T) {
	users := schema.Define("users", func(tu *usersTable) {
		tu.ID = tu.BigSerial("id").PrimaryKey()
	})

	subquery := schema.Raw("SELECT id FROM other")
	in := users.ID.InSubquery(subquery)

	if in.Left != users.ID {
		t.Fatalf("unexpected Left column in IN predicate")
	}
	if len(in.Values) != 1 || !reflect.DeepEqual(in.Values[0], subquery) {
		t.Fatalf("unexpected subquery in IN predicate")
	}
	if in.Negated {
		t.Fatalf("expected non-negated IN predicate")
	}

	notIn := users.ID.NotInSubquery(subquery)
	if !notIn.Negated {
		t.Fatalf("expected negated NOT IN predicate")
	}
}

func TestOrderExprNulls(t *testing.T) {
	users := schema.Define("users", func(tu *usersTable) {
		tu.ID = tu.BigSerial("id").PrimaryKey()
	})

	asc := users.ID.Asc().NullsFirst()
	if asc.Direction != schema.SortAsc {
		t.Fatalf("expected ASC direction")
	}
	if asc.NullsOrder != schema.NullsFirst {
		t.Fatalf("expected NULLS FIRST")
	}

	desc := users.ID.Desc().NullsLast()
	if desc.Direction != schema.SortDesc {
		t.Fatalf("expected DESC direction")
	}
	if desc.NullsOrder != schema.NullsLast {
		t.Fatalf("expected NULLS LAST")
	}
}
