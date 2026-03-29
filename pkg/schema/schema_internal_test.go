package schema

import (
	"reflect"
	"testing"
	"time"
)

type internalAuditColumns struct {
	CreatedAt *Column[time.Time]
}

type internalUsersTable struct {
	TableModel
	ID    *Column[int64]
	Email *Column[string]
	internalAuditColumns
}

type nestedTableContainer struct {
	Wrapped struct {
		TableModel
	}
}

func TestSchemaInternalHelpersAndExpressions(t *testing.T) {
	users := Define("users", func(tu *internalUsersTable) {
		tu.ID = tu.BigSerial("id").PrimaryKey()
		tu.Email = tu.VarChar("email", 255).Nullable().NotNull().Unique().Default("")
		tu.CreatedAt = tu.TimestampTZ("created_at").NotNull().DefaultNow()
		tu.Index("users_email_idx").On(tu.Email)
		tu.UniqueIndex("users_created_idx").On(tu.CreatedAt.Desc())
		tu.Unique("users_email_created_key").On(tu.Email, tu.CreatedAt)
		tu.Check("users_email_present_check", tu.Email.IsNotNull())
	})

	col, ok := users.TableDef().ColumnByName("email")
	if !ok || col.Name != "email" {
		t.Fatalf("expected ColumnByName to find email")
	}

	anyEmail := users.C("email")
	if anyEmail.ColumnDef().Name != "email" {
		t.Fatalf("expected C to return email column")
	}
	if anyEmail.Asc().Direction != SortAsc {
		t.Fatalf("expected AnyColumn Asc direction")
	}
	if anyEmail.Desc().Direction != SortDesc {
		t.Fatalf("expected AnyColumn Desc direction")
	}
	anyEmail.isExpression()

	ref := Ref(col)
	if ref.ColumnDef() != col {
		t.Fatalf("expected Ref to wrap the provided column")
	}

	if users.Email.ColumnDef() != col {
		t.Fatalf("expected typed ColumnDef to match metadata")
	}
	if users.Email.Asc().Direction != SortAsc {
		t.Fatalf("expected typed column Asc direction")
	}
	if users.Email.Desc().Direction != SortDesc {
		t.Fatalf("expected typed column Desc direction")
	}
	users.Email.isExpression()

	eq := users.Email.Eq("alice@example.com")
	ne := users.Email.Ne("bob@example.com")
	gt := users.ID.Gt(10)
	gte := users.ID.Gte(11)
	lt := users.ID.Lt(12)
	lte := users.ID.Lte(13)
	eqCol := users.ID.EqCol(users.ID)
	in := users.ID.In(1, 2, 3)
	anyIn := anyEmail.In("alice@example.com", "bob@example.com")
	isNull := users.Email.IsNull()
	isNotNull := users.Email.IsNotNull()
	raw := Raw("LOWER(?)", users.Email)
	andExpr := And(eq, ne)
	orExpr := Or(gt, lt)

	if eq.Operator != "=" || ne.Operator != "<>" || gt.Operator != ">" || gte.Operator != ">=" || lt.Operator != "<" || lte.Operator != "<=" || eqCol.Operator != "=" {
		t.Fatalf("unexpected comparison operator values")
	}
	if len(in.Values) != 3 || len(anyIn.Values) != 2 {
		t.Fatalf("unexpected IN expression values")
	}
	if isNull.Negated || !isNotNull.Negated {
		t.Fatalf("unexpected null-check negation flags")
	}
	if raw.SQL != "LOWER(?)" || len(raw.Args) != 1 {
		t.Fatalf("unexpected raw expression payload")
	}
	if andExpr.Operator != "AND" || orExpr.Operator != "OR" {
		t.Fatalf("unexpected logical expression operators")
	}

	ValueExpr{Value: 1}.isExpression()
	eq.isExpression()
	eq.isPredicate()
	in.isExpression()
	in.isPredicate()
	isNull.isExpression()
	isNull.isPredicate()
	andExpr.isExpression()
	andExpr.isPredicate()
	raw.isExpression()

	if users.TableDef().Indexes[0].Columns[0].Direction != SortAsc {
		t.Fatalf("expected plain index column direction ASC")
	}
	if users.TableDef().Indexes[1].Columns[0].Direction != SortDesc {
		t.Fatalf("expected ordered index column direction DESC")
	}
	if users.TableDef().Constraints[0].Columns[1].Name != "created_at" {
		t.Fatalf("expected table constraint column metadata to be preserved")
	}
}

func TestSchemaInternalPanicsAndCloners(t *testing.T) {
	users := Define("users", func(tu *internalUsersTable) {
		tu.ID = tu.BigSerial("id").PrimaryKey()
		tu.Email = tu.VarChar("email", 255).NotNull()
		tu.CreatedAt = tu.TimestampTZ("created_at").NotNull()
	})

	aliasedDef := cloneTableDef(users.TableDef(), "u")

	anyClone := Ref(users.ID.ColumnDef()).cloneForTable(aliasedDef).(*AnyColumn)
	if anyClone.ColumnDef().Table.Alias != "u" {
		t.Fatalf("expected AnyColumn clone to target aliased table")
	}

	typedClone := users.ID.cloneForTable(aliasedDef).(*Column[int64])
	if typedClone.ColumnDef().Table.Alias != "u" {
		t.Fatalf("expected typed column clone to target aliased table")
	}

	if tableDefOf(users) != users.TableDef() {
		t.Fatalf("expected tableDefOf to unwrap TableReference")
	}

	var target internalUsersTable
	def := &TableDef{Name: "bound", columnsByName: map[string]*ColumnDef{}}
	bindTableModel(&target, def)
	if target.TableDef() != def {
		t.Fatalf("expected bindTableModel to assign def")
	}

	if !locateTableModel(reflect.ValueOf(target)).IsValid() {
		t.Fatalf("expected locateTableModel to find embedded TableModel")
	}
	var nested nestedTableContainer
	if !locateTableModel(reflect.ValueOf(nested)).IsValid() {
		t.Fatalf("expected locateTableModel to recurse into nested structs")
	}
	if locateTableModel(reflect.ValueOf(struct{ Name string }{})).IsValid() {
		t.Fatalf("expected locateTableModel to return invalid for non-table structs")
	}

	assertPanics(t, func() { (*TableModel)(nil).C("id") })
	assertPanics(t, func() {
		var zero TableModel
		zero.C("id")
	})
	assertPanics(t, func() { users.C("missing") })
	assertPanics(t, func() { users.PrimaryKey("") })
	assertPanics(t, func() { users.Unique("") })
	assertPanics(t, func() { users.ForeignKey("") })
	assertPanics(t, func() { users.Check("", users.Email.IsNotNull()) })
	assertPanics(t, func() { users.Check("users_bad_check", nil) })
	assertPanics(t, func() { users.Check("users_bad_or_check", Or()) })
	assertPanics(t, func() { users.Check("users_bad_and_check", And()) })
	assertPanics(t, func() {
		idx := users.Index("also-broken")
		idx.On(OrderExpr{Expr: Raw("x"), Direction: SortAsc})
	})
	assertPanics(t, func() {
		other := Define("other_users", func(tu *internalUsersTable) {
			tu.ID = tu.BigSerial("id").PrimaryKey()
			tu.Email = tu.VarChar("email", 255).NotNull()
			tu.CreatedAt = tu.TimestampTZ("created_at").NotNull()
		})
		users.PrimaryKey("users_broken_pkey").On(other.ID)
	})
	assertPanics(t, func() {
		missing := &Column[int64]{def: &ColumnDef{Name: "missing"}}
		_ = missing.cloneForTable(aliasedDef)
	})
	assertPanics(t, func() {
		missing := Ref(&ColumnDef{Name: "missing"})
		_ = missing.cloneForTable(aliasedDef)
	})
	assertPanics(t, func() { _ = addColumn[int64](nil, "id", ColumnType{DataType: TypeBigInt}, true, false) })
	assertPanics(t, func() {
		_ = addColumn[int64](users.TableDef(), "id", ColumnType{DataType: TypeBigInt}, true, false)
	})
	assertPanics(t, func() { bindTableModel(nil, def) })
	assertPanics(t, func() { bindTableModel(&struct{ Name string }{}, def) })
	assertPanics(t, func() { _ = tableDefOf(struct{}{}) })

	var nilPtr *Column[int64]
	ptrToStruct := users
	var standaloneTableModel TableModel
	rebindAliasedColumns(reflect.Value{}, aliasedDef)
	rebindAliasedColumns(reflect.ValueOf(users.ID), aliasedDef)
	rebindAliasedColumns(reflect.ValueOf(&nilPtr).Elem(), aliasedDef)
	rebindAliasedColumns(reflect.ValueOf(&ptrToStruct).Elem(), aliasedDef)
	rebindAliasedColumns(reflect.ValueOf(&standaloneTableModel).Elem(), aliasedDef)
	rebindAliasedColumns(reflect.ValueOf(users).Elem(), aliasedDef)
	if users.ID.ColumnDef().Table.Name != "users" {
		t.Fatalf("expected rebinding direct value not to corrupt original handle")
	}

	posts := Define("posts", func(tp *struct {
		TableModel
		ID     *Column[int64]
		UserID *Column[int64]
		Status *Column[string]
	},
	) {
		tp.ID = tp.BigSerial("id").PrimaryKey()
		tp.UserID = tp.BigInt("user_id").NotNull().References(users.ID)
		tp.Status = tp.Enum("status", "draft", "published")
		tp.Index("posts_user_idx").On(tp.UserID)
		tp.Check("posts_status_check", tp.Status.In("draft", "published"))
		tp.ForeignKey("posts_status_fk").On(tp.UserID).References(users.ID).OnDelete(ForeignKeyActionCascade)
	})
	clonedWithFK := cloneTableDef(posts.TableDef(), "p")
	if clonedWithFK.Alias != "p" || len(clonedWithFK.ForeignKeys) != 1 || len(clonedWithFK.Indexes) != 1 || len(clonedWithFK.Constraints) != 2 {
		t.Fatalf("expected cloneTableDef to preserve alias, indexes, foreign keys, and constraints")
	}
	posts.Status.ColumnDef().Type.EnumValues[0] = "mutated"
	if clonedWithFK.columnsByName["status"].Type.EnumValues[0] != "draft" {
		t.Fatalf("expected enum metadata to be deep-cloned")
	}
	if clonedWithFK.Constraints[0].Check.(InExpr).Left.(ColumnReference).ColumnDef().Table.Alias != "p" {
		t.Fatalf("expected cloned check constraint to bind to aliased table")
	}
}

func assertPanics(t *testing.T, fn func()) {
	t.Helper()

	defer func() {
		if recover() == nil {
			t.Fatalf("expected panic")
		}
	}()

	fn()
}
