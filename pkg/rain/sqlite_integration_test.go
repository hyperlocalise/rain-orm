package rain_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hyperlocalise/rain-orm/pkg/dialect"
	"github.com/hyperlocalise/rain-orm/pkg/rain"
	"github.com/hyperlocalise/rain-orm/pkg/schema"
	_ "modernc.org/sqlite"
)

type sqliteUsersTable struct {
	schema.TableModel
	ID        *schema.Column[int64]
	Email     *schema.Column[string]
	Name      *schema.Column[string]
	Active    *schema.Column[bool]
	Nickname  *schema.Column[string]
	CreatedAt *schema.Column[time.Time]
}

type sqlitePostsTable struct {
	schema.TableModel
	ID     *schema.Column[int64]
	UserID *schema.Column[int64]
	Title  *schema.Column[string]
}

type sqliteInsertModel struct {
	Email    string
	Name     rain.Set[string]
	Active   rain.Set[bool]
	Nickname *string
}

type sqliteUserRow struct {
	ID        int64
	Email     string
	Name      string
	Active    bool
	Nickname  *string
	CreatedAt string
}

type joinedPostRow struct {
	Title string
	Email string
}

type aliasedJoinRow struct {
	PostTitle string `db:"post_title"`
	UserEmail string `db:"user_email"`
}

type userPostCountRow struct {
	UserEmail string `db:"user_email"`
	PostCount int64  `db:"post_count"`
}

type sqliteRichUsersTable struct {
	schema.TableModel
	ID         *schema.Column[int64]
	Email      *schema.Column[string]
	Name       *schema.Column[string]
	Active     *schema.Column[bool]
	Nickname   *schema.Column[string]
	ExternalID *schema.Column[string]
	Status     *schema.Column[string]
	CreatedAt  *schema.Column[time.Time]
	UpdatedAt  *schema.Column[time.Time]
}

type sqliteRichCategoriesTable struct {
	schema.TableModel
	ID   *schema.Column[int64]
	Slug *schema.Column[string]
	Name *schema.Column[string]
}

type sqliteRichPostsTable struct {
	schema.TableModel
	ID         *schema.Column[int64]
	UserID     *schema.Column[int64]
	CategoryID *schema.Column[int64]
	Title      *schema.Column[string]
	Body       *schema.Column[string]
	Published  *schema.Column[bool]
	CreatedAt  *schema.Column[time.Time]
}

type sqliteRichUserMutationRow struct {
	ID         int64
	Email      string
	Name       string
	ExternalID string
	Status     string
}

type sqliteRichStatusRow struct {
	Status string
}

type sqliteRichUserAggregateRow struct {
	Status    string `db:"status"`
	UserCount int64  `db:"user_count"`
}

type sqliteRichUserSummaryRow struct {
	Email     string `db:"email"`
	Status    string `db:"status"`
	PostCount int64  `db:"post_count"`
}

type sqliteRichPreparedUserRow struct {
	ID     int64
	Email  string
	Status string
	Active bool
}

type sqliteRichLeftJoinSummaryRow struct {
	Email     string `db:"email"`
	PostCount *int64 `db:"post_count"`
}

type sqliteRichAuthorRow struct {
	ID    int64
	Email string
	Name  string
}

type sqliteRichCategoryRow struct {
	ID   int64
	Slug string
	Name string
}

type sqliteRichPostWithRelationsRow struct {
	ID         int64
	UserID     int64
	CategoryID *int64
	Title      string
	Published  bool
	Author     sqliteRichAuthorRow    `rain:"relation:author"`
	Category   *sqliteRichCategoryRow `rain:"relation:category"`
}

type sqliteRichPostWithRelationPointersRow struct {
	ID         int64
	UserID     int64
	CategoryID *int64
	Title      string
	Published  bool
	Author     *sqliteRichAuthorRow   `rain:"relation:author"`
	Category   *sqliteRichCategoryRow `rain:"relation:category"`
}

type sqliteRichUserWithPostsRow struct {
	ID    int64
	Email string
	Posts []sqliteRichPostWithRelationsRow `rain:"relation:posts"`
}

type sqliteRichUserWithPostPointersRow struct {
	ID    int64
	Email string
	Posts []*sqliteRichPostWithRelationPointersRow `rain:"relation:posts"`
}

type sqliteRichFixture struct {
	users      *sqliteRichUsersTable
	categories *sqliteRichCategoriesTable
	posts      *sqliteRichPostsTable
}

type sqliteRichSeedData struct {
	AliceID       int64
	BobID         int64
	CarolID       int64
	ProductID     int64
	EngineeringID int64
}

func defineSQLiteTables() (*sqliteUsersTable, *sqlitePostsTable) {
	users := schema.Define("users", func(t *sqliteUsersTable) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.Email = t.VarChar("email", 255).NotNull()
		t.Name = t.Text("name").NotNull().Default("guest")
		t.Active = t.Boolean("active").NotNull().Default(true)
		t.Nickname = t.Text("nickname").Nullable().Default("buddy")
		t.CreatedAt = t.TimestampTZ("created_at").NotNull().DefaultNow()
	})

	posts := schema.Define("posts", func(t *sqlitePostsTable) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.UserID = t.BigInt("user_id").NotNull().References(users.ID)
		t.Title = t.Text("title").NotNull()
		t.BelongsTo("author", t.UserID, users.ID)
	})
	users.HasMany("posts", users.ID, posts.UserID)

	return users, posts
}

func defineSQLiteRichTables() sqliteRichFixture {
	users := schema.Define("rich_users", func(t *sqliteRichUsersTable) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.Email = t.VarChar("email", 255).NotNull()
		t.Name = t.Text("name").NotNull()
		t.Active = t.Boolean("active").NotNull().Default(true)
		t.Nickname = t.Text("nickname").Nullable()
		t.ExternalID = t.VarChar("external_id", 64).NotNull()
		t.Status = t.Enum("status", "trial", "active", "disabled").NotNull().Default("trial")
		t.CreatedAt = t.TimestampTZ("created_at").NotNull().DefaultNow()
		t.UpdatedAt = t.TimestampTZ("updated_at").NotNull().DefaultNow()
		t.UniqueIndex("rich_users_email_idx").On(t.Email)
		t.UniqueIndex("rich_users_external_id_idx").On(t.ExternalID)
		t.Index("rich_users_status_created_at_idx").On(t.Status, t.CreatedAt.Desc())
	})

	categories := schema.Define("rich_categories", func(t *sqliteRichCategoriesTable) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.Slug = t.VarChar("slug", 128).NotNull()
		t.Name = t.Text("name").NotNull()
		t.UniqueIndex("rich_categories_slug_idx").On(t.Slug)
	})

	posts := schema.Define("rich_posts", func(t *sqliteRichPostsTable) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.UserID = t.BigInt("user_id").NotNull().References(users.ID)
		t.CategoryID = t.BigInt("category_id").Nullable().References(categories.ID)
		t.Title = t.Text("title").NotNull()
		t.Body = t.Text("body").NotNull()
		t.Published = t.Boolean("published").NotNull().Default(false)
		t.CreatedAt = t.TimestampTZ("created_at").NotNull().DefaultNow()
		t.Index("rich_posts_user_id_idx").On(t.UserID)
		t.Index("rich_posts_category_id_idx").On(t.CategoryID)
		t.Index("rich_posts_published_created_at_idx").On(t.Published, t.CreatedAt.Desc())
		t.BelongsTo("author", t.UserID, users.ID)
		t.BelongsTo("category", t.CategoryID, categories.ID)
	})

	users.HasMany("posts", users.ID, posts.UserID)
	categories.HasMany("posts", categories.ID, posts.CategoryID)

	return sqliteRichFixture{users: users, categories: categories, posts: posts}
}

func TestOpenUnknownDriverReturnsError(t *testing.T) {
	t.Parallel()

	db, err := rain.Open("definitely-missing-driver", "dsn")
	if err == nil {
		t.Fatalf("expected unknown driver error, got nil")
	}
	if db != nil {
		t.Fatalf("expected nil db when open fails")
	}
}

func TestOpenPostgresAliasReturnsHelpfulDriverError(t *testing.T) {
	t.Parallel()

	db, err := rain.Open("postgresql", "dsn")
	if err == nil {
		t.Fatalf("expected alias driver error, got nil")
	}
	if db != nil {
		t.Fatalf("expected nil db when open fails")
	}
	if !strings.Contains(err.Error(), `dialect "postgresql" maps to "postgres"`) {
		t.Fatalf("expected helpful alias message, got %v", err)
	}
}

func TestSQLiteIntegrationInsertDefaultsOverridesAndScan(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openSQLiteTestDB(t)
	users, posts := defineSQLiteTables()

	createSQLiteSchema(t, ctx, db)

	if _, err := db.Insert().
		Table(users).
		Model(&sqliteInsertModel{Email: "defaults@example.com"}).
		Exec(ctx); err != nil {
		t.Fatalf("default-backed insert failed: %v", err)
	}

	if _, err := db.Insert().
		Table(users).
		Model(&sqliteInsertModel{
			Email:  "override@example.com",
			Name:   rain.Set[string]{Value: "Alice", Valid: true},
			Active: rain.Set[bool]{Value: false, Valid: true},
		}).
		Set(users.Name, "Alice").
		Set(users.Active, false).
		Set(users.Nickname, "ali").
		Exec(ctx); err != nil {
		t.Fatalf("override insert failed: %v", err)
	}

	var first sqliteUserRow
	if err := db.Select().
		Table(users).
		Where(users.Email.Eq("defaults@example.com")).
		Scan(ctx, &first); err != nil {
		t.Fatalf("select first row failed: %v", err)
	}
	if first.Name != "guest" {
		t.Fatalf("expected default name guest, got %q", first.Name)
	}
	if !first.Active {
		t.Fatalf("expected default active=true")
	}
	if first.Nickname == nil || *first.Nickname != "buddy" {
		t.Fatalf("expected default nickname buddy, got %#v", first.Nickname)
	}
	if first.CreatedAt == "" {
		t.Fatalf("expected created_at to be populated")
	}

	var second sqliteUserRow
	if err := db.Select().
		Table(users).
		Where(users.Email.Eq("override@example.com")).
		Scan(ctx, &second); err != nil {
		t.Fatalf("select override row failed: %v", err)
	}
	if second.Name != "Alice" {
		t.Fatalf("expected override name Alice, got %q", second.Name)
	}
	if second.Active {
		t.Fatalf("expected explicit active=false override")
	}
	if second.Nickname == nil || *second.Nickname != "ali" {
		t.Fatalf("expected explicit nickname ali, got %#v", second.Nickname)
	}

	var allUsers []sqliteUserRow
	if err := db.Select().
		Table(users).
		OrderBy(users.ID.Asc()).
		Scan(ctx, &allUsers); err != nil {
		t.Fatalf("scan users slice failed: %v", err)
	}
	if len(allUsers) != 2 {
		t.Fatalf("expected 2 users, got %d", len(allUsers))
	}

	if _, err := db.Insert().
		Table(posts).
		Set(posts.UserID, first.ID).
		Set(posts.Title, "Hello").
		Exec(ctx); err != nil {
		t.Fatalf("insert post failed: %v", err)
	}

	u := schema.Alias(users, "u")
	p := schema.Alias(posts, "p")
	var joined []joinedPostRow
	if err := db.Select().
		Table(p).
		Column(p.Title, u.Email).
		Join(u, p.UserID.EqCol(u.ID)).
		Where(u.ID.Eq(first.ID)).
		Scan(ctx, &joined); err != nil {
		t.Fatalf("aliased join scan failed: %v", err)
	}
	if len(joined) != 1 || joined[0].Title != "Hello" || joined[0].Email != "defaults@example.com" {
		t.Fatalf("unexpected joined rows: %#v", joined)
	}

	var aliasedJoined []aliasedJoinRow
	if err := db.Select().
		Table(p).
		Column(p.Title.As("post_title"), u.Email.As("user_email")).
		Join(u, p.UserID.EqCol(u.ID)).
		Where(u.ID.Eq(first.ID)).
		Scan(ctx, &aliasedJoined); err != nil {
		t.Fatalf("aliased projection join scan failed: %v", err)
	}
	if len(aliasedJoined) != 1 || aliasedJoined[0].PostTitle != "Hello" || aliasedJoined[0].UserEmail != "defaults@example.com" {
		t.Fatalf("unexpected aliased joined rows: %#v", aliasedJoined)
	}

	postCounts := db.Select().
		Table(posts).
		Column(posts.UserID.As("user_id"), schema.Count().As("post_count")).
		GroupBy(posts.UserID)

	var counts []userPostCountRow
	if err := db.Select().
		Table(users).
		Column(users.Email.As("user_email"), schema.Raw("pc.post_count").As("post_count")).
		JoinSubquery(postCounts, "pc", schema.ComparisonExpr{
			Left:     users.ID,
			Operator: "=",
			Right:    schema.Raw("pc.user_id"),
		}).
		Scan(ctx, &counts); err != nil {
		t.Fatalf("aliased subquery projection scan failed: %v", err)
	}
	if len(counts) != 1 || counts[0].UserEmail != "defaults@example.com" || counts[0].PostCount != 1 {
		t.Fatalf("unexpected post count rows: %#v", counts)
	}
}

func TestSQLiteIntegrationRichWriteQueries(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openSQLiteTestDB(t)
	fixture := defineSQLiteRichTables()
	createSQLiteRichSchema(t, ctx, db, fixture)

	var inserted sqliteRichUserMutationRow
	if err := db.Insert().
		Table(fixture.users).
		Set(fixture.users.Email, "dora@example.com").
		Set(fixture.users.Name, "Dora").
		Set(fixture.users.Active, true).
		Set(fixture.users.ExternalID, "ext-dora").
		Set(fixture.users.Status, "trial").
		Returning(
			fixture.users.ID,
			fixture.users.Email,
			fixture.users.Name,
			fixture.users.ExternalID,
			fixture.users.Status,
		).
		Scan(ctx, &inserted); err != nil {
		t.Fatalf("insert returning scan failed: %v", err)
	}
	if inserted.ID == 0 || inserted.Email != "dora@example.com" || inserted.Status != "trial" {
		t.Fatalf("unexpected inserted row: %#v", inserted)
	}

	var upserted sqliteRichUserMutationRow
	if err := db.Insert().
		Table(fixture.users).
		Set(fixture.users.Email, "dora@example.com").
		Set(fixture.users.Name, "Dora Updated").
		Set(fixture.users.Active, false).
		Set(fixture.users.ExternalID, "ext-dora-updated").
		Set(fixture.users.Status, "active").
		OnConflict(fixture.users.Email).
		DoUpdateSet(
			fixture.users.Name,
			fixture.users.Active,
			fixture.users.ExternalID,
			fixture.users.Status,
		).
		Returning(
			fixture.users.ID,
			fixture.users.Email,
			fixture.users.Name,
			fixture.users.ExternalID,
			fixture.users.Status,
		).
		Scan(ctx, &upserted); err != nil {
		t.Fatalf("upsert returning scan failed: %v", err)
	}
	if upserted.ID != inserted.ID || upserted.Name != "Dora Updated" || upserted.Status != "active" {
		t.Fatalf("unexpected upserted row: %#v", upserted)
	}

	var updated sqliteRichUserMutationRow
	if err := db.Update().
		Table(fixture.users).
		Set(fixture.users.Name, "Dora Disabled").
		Set(fixture.users.Status, "disabled").
		Where(fixture.users.ID.Eq(inserted.ID)).
		Returning(
			fixture.users.ID,
			fixture.users.Email,
			fixture.users.Name,
			fixture.users.ExternalID,
			fixture.users.Status,
		).
		Scan(ctx, &updated); err != nil {
		t.Fatalf("update returning scan failed: %v", err)
	}
	if updated.Name != "Dora Disabled" || updated.Status != "disabled" {
		t.Fatalf("unexpected updated row: %#v", updated)
	}

	var deleted sqliteRichUserMutationRow
	if err := db.Delete().
		Table(fixture.users).
		Where(fixture.users.ID.Eq(inserted.ID)).
		Returning(
			fixture.users.ID,
			fixture.users.Email,
			fixture.users.Name,
			fixture.users.ExternalID,
			fixture.users.Status,
		).
		Scan(ctx, &deleted); err != nil {
		t.Fatalf("delete returning scan failed: %v", err)
	}
	if deleted.ID != inserted.ID || deleted.Email != "dora@example.com" {
		t.Fatalf("unexpected deleted row: %#v", deleted)
	}

	exists, err := db.Select().Table(fixture.users).Where(fixture.users.ID.Eq(inserted.ID)).Exists(ctx)
	if err != nil {
		t.Fatalf("check deleted row exists: %v", err)
	}
	if exists {
		t.Fatalf("expected deleted row to be gone")
	}
}

func TestSQLiteIntegrationRichAdvancedSelectsAndPreparedQueries(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openSQLiteTestDB(t)
	fixture := defineSQLiteRichTables()
	createSQLiteRichSchema(t, ctx, db, fixture)
	seeded := seedSQLiteRichFixture(t, ctx, db, fixture)
	if seeded.AliceID == 0 {
		t.Fatalf("expected seeded alice id to be populated")
	}

	var distinctStatuses []sqliteRichStatusRow
	if err := db.Select().
		Table(fixture.users).
		Distinct().
		Column(fixture.users.Status).
		OrderBy(fixture.users.Status.Asc()).
		Scan(ctx, &distinctStatuses); err != nil {
		t.Fatalf("distinct status scan failed: %v", err)
	}
	if len(distinctStatuses) != 3 {
		t.Fatalf("expected 3 distinct statuses, got %#v", distinctStatuses)
	}

	var grouped []sqliteRichUserAggregateRow
	if err := db.Select().
		Table(fixture.users).
		Column(fixture.users.Status, schema.Count().As("user_count")).
		GroupBy(fixture.users.Status).
		Having(schema.ComparisonExpr{
			Left:     schema.Count(),
			Operator: ">",
			Right:    schema.ValueExpr{Value: 0},
		}).
		OrderBy(fixture.users.Status.Asc()).
		Scan(ctx, &grouped); err != nil {
		t.Fatalf("group by having scan failed: %v", err)
	}
	if len(grouped) != 3 {
		t.Fatalf("expected 3 grouped rows, got %#v", grouped)
	}

	postCounts := db.Select().
		Table(fixture.posts).
		Column(fixture.posts.UserID.As("user_id"), schema.Count().As("post_count")).
		GroupBy(fixture.posts.UserID)

	var summaries []sqliteRichUserSummaryRow
	if err := db.Select().
		TableSubquery(
			db.Select().
				Table(fixture.users).
				Column(fixture.users.ID, fixture.users.Email, fixture.users.Status).
				Where(fixture.users.Active.Eq(true)),
			"active_users",
		).
		Column(
			schema.Raw("active_users.email").As("email"),
			schema.Raw("active_users.status").As("status"),
			schema.Raw("pc.post_count").As("post_count"),
		).
		JoinSubquery(postCounts, "pc", schema.ComparisonExpr{
			Left:     schema.Raw("active_users.id"),
			Operator: "=",
			Right:    schema.Raw("pc.user_id"),
		}).
		OrderBy(schema.OrderExpr{Expr: schema.Raw("active_users.email"), Direction: schema.SortAsc}).
		Scan(ctx, &summaries); err != nil {
		t.Fatalf("table subquery join scan failed: %v", err)
	}
	if len(summaries) != 2 || summaries[0].Email != "alice@example.com" || summaries[0].PostCount != 2 {
		t.Fatalf("unexpected summaries: %#v", summaries)
	}

	var leftJoined []sqliteRichLeftJoinSummaryRow
	if err := db.Select().
		Table(fixture.users).
		Column(fixture.users.Email, schema.Raw("pc.post_count").As("post_count")).
		LeftJoinSubquery(postCounts, "pc", schema.ComparisonExpr{
			Left:     fixture.users.ID,
			Operator: "=",
			Right:    schema.Raw("pc.user_id"),
		}).
		OrderBy(fixture.users.Email.Asc()).
		Scan(ctx, &leftJoined); err != nil {
		t.Fatalf("left join subquery scan failed: %v", err)
	}
	if len(leftJoined) != 3 {
		t.Fatalf("expected 3 left joined rows, got %#v", leftJoined)
	}
	if leftJoined[2].Email != "carol@example.com" || leftJoined[2].PostCount != nil {
		t.Fatalf("expected carol to have nil post count, got %#v", leftJoined[2])
	}

	query := db.Select().
		Table(fixture.users).
		Column(fixture.users.ID, fixture.users.Email, fixture.users.Status, fixture.users.Active).
		Where(fixture.users.Status.EqExpr(schema.Placeholder("status"))).
		Where(fixture.users.Active.EqExpr(schema.Placeholder("active"))).
		OrderBy(fixture.users.ID.Asc())

	prepared, err := query.Prepare(ctx)
	if err != nil {
		t.Fatalf("prepare select failed: %v", err)
	}

	var preparedRows []sqliteRichPreparedUserRow
	if err := prepared.Scan(ctx, rain.PreparedArgs{
		"status": "active",
		"active": true,
	}, &preparedRows); err != nil {
		t.Fatalf("prepared scan failed: %v", err)
	}
	if len(preparedRows) != 1 || preparedRows[0].Email != "alice@example.com" {
		t.Fatalf("unexpected prepared rows: %#v", preparedRows)
	}

	count, err := prepared.Count(ctx, rain.PreparedArgs{
		"status": "active",
		"active": true,
	})
	if err != nil {
		t.Fatalf("prepared count failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected prepared count 1, got %d", count)
	}

	exists, err := prepared.Exists(ctx, rain.PreparedArgs{
		"status": "disabled",
		"active": false,
	})
	if err != nil {
		t.Fatalf("prepared exists failed: %v", err)
	}
	if !exists {
		t.Fatalf("expected prepared exists to be true")
	}

	if err := prepared.Scan(ctx, rain.PreparedArgs{"status": "active"}, &preparedRows); err == nil || !strings.Contains(err.Error(), "missing prepared arg") {
		t.Fatalf("expected missing prepared arg error, got %v", err)
	}
	if err := prepared.Scan(ctx, rain.PreparedArgs{"status": "active", "active": true, "extra": 1}, &preparedRows); err == nil || !strings.Contains(err.Error(), "unexpected prepared arg") {
		t.Fatalf("expected unexpected prepared arg error, got %v", err)
	}
	if err := prepared.Close(); err != nil {
		t.Fatalf("close prepared query failed: %v", err)
	}
	if err := prepared.Close(); err != nil {
		t.Fatalf("second close prepared query failed: %v", err)
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx for prepared lifecycle: %v", err)
	}
	preparedTx, err := tx.Select().
		Table(fixture.users).
		Where(fixture.users.Email.EqExpr(schema.Placeholder("email"))).
		Prepare(ctx)
	if err != nil {
		t.Fatalf("prepare tx select failed: %v", err)
	}
	var txRow sqliteRichPreparedUserRow
	if err := preparedTx.Scan(ctx, rain.PreparedArgs{"email": "alice@example.com"}, &txRow); err != nil {
		t.Fatalf("prepared tx scan failed: %v", err)
	}
	if txRow.Email != "alice@example.com" {
		t.Fatalf("unexpected tx prepared row: %#v", txRow)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit tx failed: %v", err)
	}
	if err := preparedTx.Scan(ctx, rain.PreparedArgs{"email": "alice@example.com"}, &txRow); err == nil {
		t.Fatalf("expected prepared tx scan after commit to fail")
	}
	if err := preparedTx.Close(); err != nil {
		t.Fatalf("close tx prepared query failed: %v", err)
	}
}

func TestSQLiteIntegrationOperators(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openSQLiteTestDB(t)
	users, _ := defineSQLiteTables()
	createSQLiteSchema(t, ctx, db)

	// Seed data
	if _, err := db.Insert().Table(users).Values(
		map[schema.ColumnReference]any{users.ID: 1, users.Email: "alice@example.com", users.Name: "Alice", users.Active: true},
		map[schema.ColumnReference]any{users.ID: 2, users.Email: "bob@example.com", users.Name: "Bob", users.Active: true},
		map[schema.ColumnReference]any{users.ID: 3, users.Email: "carol@example.com", users.Name: "Carol", users.Active: false},
	).Exec(ctx); err != nil {
		t.Fatalf("seed data failed: %v", err)
	}

	t.Run("NotIn", func(t *testing.T) {
		var rows []sqliteUserRow
		if err := db.Select().Table(users).Where(users.ID.NotIn(1, 2)).Scan(ctx, &rows); err != nil {
			t.Fatalf("NotIn failed: %v", err)
		}
		if len(rows) != 1 || rows[0].Email != "carol@example.com" {
			t.Fatalf("unexpected rows for NotIn: %#v", rows)
		}
	})

	t.Run("Like", func(t *testing.T) {
		var rows []sqliteUserRow
		if err := db.Select().Table(users).Where(users.Email.Like("bob%")).Scan(ctx, &rows); err != nil {
			t.Fatalf("Like failed: %v", err)
		}
		if len(rows) != 1 || rows[0].Email != "bob@example.com" {
			t.Fatalf("unexpected rows for Like: %#v", rows)
		}
	})

	t.Run("Between", func(t *testing.T) {
		var rows []sqliteUserRow
		if err := db.Select().Table(users).Where(users.ID.Between(1, 2)).Scan(ctx, &rows); err != nil {
			t.Fatalf("Between failed: %v", err)
		}
		if len(rows) != 2 {
			t.Fatalf("expected 2 rows for Between, got %d", len(rows))
		}
	})

	t.Run("LogicalNot", func(t *testing.T) {
		var rows []sqliteUserRow
		if err := db.Select().Table(users).Where(schema.Not(users.Active.Eq(true))).Scan(ctx, &rows); err != nil {
			t.Fatalf("LogicalNot failed: %v", err)
		}
		if len(rows) != 1 || rows[0].Email != "carol@example.com" {
			t.Fatalf("unexpected rows for LogicalNot: %#v", rows)
		}
	})

	t.Run("Exists", func(t *testing.T) {
		var rows []sqliteUserRow
		subquery := db.Select().Table(users).Where(users.ID.Eq(1))
		if err := db.Select().Table(users).Where(schema.Exists(subquery)).Scan(ctx, &rows); err != nil {
			t.Fatalf("Exists failed: %v", err)
		}
		// Exists is true for all rows if subquery returns anything
		if len(rows) != 3 {
			t.Fatalf("expected 3 rows for Exists(true), got %d", len(rows))
		}

		rows = nil
		subqueryEmpty := db.Select().Table(users).Where(users.ID.Eq(999))
		if err := db.Select().Table(users).Where(schema.Exists(subqueryEmpty)).Scan(ctx, &rows); err != nil && !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("Exists empty failed: %v", err)
		}
		if len(rows) != 0 {
			t.Fatalf("expected 0 rows for Exists(false), got %d", len(rows))
		}
	})
}

func TestSQLiteIntegrationRichRelationsAndTransactions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openSQLiteTestDB(t)
	fixture := defineSQLiteRichTables()
	createSQLiteRichSchema(t, ctx, db, fixture)
	seeded := seedSQLiteRichFixture(t, ctx, db, fixture)

	var usersWithPosts []sqliteRichUserWithPostsRow
	if err := db.Select().
		Table(fixture.users).
		Where(fixture.users.ID.Eq(seeded.AliceID)).
		WithRelations("posts.author", "posts.category").
		Scan(ctx, &usersWithPosts); err != nil {
		t.Fatalf("scan nested relations failed: %v", err)
	}
	if len(usersWithPosts) != 1 || len(usersWithPosts[0].Posts) != 2 {
		t.Fatalf("unexpected users with posts: %#v", usersWithPosts)
	}
	if usersWithPosts[0].Posts[0].Author.Email != "alice@example.com" {
		t.Fatalf("expected nested author email, got %#v", usersWithPosts[0].Posts[0].Author)
	}
	if usersWithPosts[0].Posts[0].Category == nil || usersWithPosts[0].Posts[0].Category.Slug == "" {
		t.Fatalf("expected nested category relation, got %#v", usersWithPosts[0].Posts[0].Category)
	}

	var usersWithPostPointers []sqliteRichUserWithPostPointersRow
	if err := db.Select().
		Table(fixture.users).
		Where(fixture.users.ID.Eq(seeded.AliceID)).
		WithRelations("posts.author", "posts.category").
		Scan(ctx, &usersWithPostPointers); err != nil {
		t.Fatalf("scan nested pointer relations failed: %v", err)
	}
	if len(usersWithPostPointers) != 1 || len(usersWithPostPointers[0].Posts) != 2 {
		t.Fatalf("unexpected users with post pointers: %#v", usersWithPostPointers)
	}
	if usersWithPostPointers[0].Posts[0] == nil || usersWithPostPointers[0].Posts[0].Author == nil {
		t.Fatalf("expected pointer relations to populate, got %#v", usersWithPostPointers)
	}

	var bad []sqliteRichPreparedUserRow
	err := db.Select().Table(fixture.users).WithRelations("unknown").Scan(ctx, &bad)
	if err == nil || !strings.Contains(err.Error(), "unknown relation") {
		t.Fatalf("expected unknown relation error, got %v", err)
	}
	err = db.Select().Table(fixture.users).WithRelations("posts.unknown").Scan(ctx, &bad)
	if err == nil || !strings.Contains(err.Error(), "unknown relation") {
		t.Fatalf("expected unknown nested relation error, got %v", err)
	}

	if err := db.RunInTx(ctx, func(tx *rain.Tx) error {
		_, execErr := tx.Insert().
			Table(fixture.users).
			Set(fixture.users.Email, "tx-commit@example.com").
			Set(fixture.users.Name, "Tx Commit").
			Set(fixture.users.Active, true).
			Set(fixture.users.ExternalID, "ext-tx-commit").
			Set(fixture.users.Status, "active").
			Exec(ctx)
		return execErr
	}); err != nil {
		t.Fatalf("RunInTx commit path failed: %v", err)
	}
	committedExists, err := db.Select().Table(fixture.users).Where(fixture.users.Email.Eq("tx-commit@example.com")).Exists(ctx)
	if err != nil {
		t.Fatalf("check committed tx row: %v", err)
	}
	if !committedExists {
		t.Fatalf("expected committed tx row to exist")
	}

	rollbackErr := errors.New("rollback me")
	if err := db.RunInTx(ctx, func(tx *rain.Tx) error {
		if _, execErr := tx.Insert().
			Table(fixture.users).
			Set(fixture.users.Email, "tx-rollback@example.com").
			Set(fixture.users.Name, "Tx Rollback").
			Set(fixture.users.Active, true).
			Set(fixture.users.ExternalID, "ext-tx-rollback").
			Set(fixture.users.Status, "trial").
			Exec(ctx); execErr != nil {
			return execErr
		}
		return rollbackErr
	}); !errors.Is(err, rollbackErr) {
		t.Fatalf("expected rollback error %v, got %v", rollbackErr, err)
	}
	rolledBackExists, err := db.Select().Table(fixture.users).Where(fixture.users.Email.Eq("tx-rollback@example.com")).Exists(ctx)
	if err != nil {
		t.Fatalf("check rolled back row: %v", err)
	}
	if rolledBackExists {
		t.Fatalf("expected rolled back row to be absent")
	}

	if err := db.RunInTx(ctx, func(tx *rain.Tx) error {
		if _, execErr := tx.Insert().
			Table(fixture.users).
			Set(fixture.users.Email, "outer@example.com").
			Set(fixture.users.Name, "Outer").
			Set(fixture.users.Active, true).
			Set(fixture.users.ExternalID, "ext-outer").
			Set(fixture.users.Status, "active").
			Exec(ctx); execErr != nil {
			return execErr
		}

		nestedErr := errors.New("nested rollback")
		if runErr := tx.RunInTx(ctx, func(nested *rain.Tx) error {
			if _, execErr := nested.Insert().
				Table(fixture.users).
				Set(fixture.users.Email, "nested@example.com").
				Set(fixture.users.Name, "Nested").
				Set(fixture.users.Active, true).
				Set(fixture.users.ExternalID, "ext-nested").
				Set(fixture.users.Status, "trial").
				Exec(ctx); execErr != nil {
				return execErr
			}
			return nestedErr
		}); !errors.Is(runErr, nestedErr) {
			return fmt.Errorf("nested tx returned %v", runErr)
		}
		return nil
	}); err != nil {
		t.Fatalf("nested RunInTx failed: %v", err)
	}

	outerExists, err := db.Select().Table(fixture.users).Where(fixture.users.Email.Eq("outer@example.com")).Exists(ctx)
	if err != nil {
		t.Fatalf("check outer row exists: %v", err)
	}
	if !outerExists {
		t.Fatalf("expected outer row to persist after nested rollback")
	}
	nestedExists, err := db.Select().Table(fixture.users).Where(fixture.users.Email.Eq("nested@example.com")).Exists(ctx)
	if err != nil {
		t.Fatalf("check nested row exists: %v", err)
	}
	if nestedExists {
		t.Fatalf("expected nested row to roll back to savepoint")
	}
}

func TestSQLiteIntegrationRichCreateIndexesSQL(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openSQLiteTestDB(t)
	fixture := defineSQLiteRichTables()
	createSQLiteTablesOnly(t, ctx, db, fixture.users, fixture.categories, fixture.posts)

	for _, table := range []schema.TableReference{fixture.users, fixture.categories, fixture.posts} {
		statements, err := db.CreateIndexesSQL(table)
		if err != nil {
			t.Fatalf("CreateIndexesSQL(%s): %v", table.TableDef().Name, err)
		}
		if len(statements) == 0 {
			t.Fatalf("expected indexes for %s", table.TableDef().Name)
		}
		for _, statement := range statements {
			if _, err := db.Exec(ctx, statement); err != nil {
				t.Fatalf("exec index statement %q: %v", statement, err)
			}
		}
	}

	rows, err := db.Query(ctx, `SELECT name FROM sqlite_master WHERE type = 'index' AND tbl_name IN ('rich_users', 'rich_categories', 'rich_posts') ORDER BY name`)
	if err != nil {
		t.Fatalf("query sqlite_master indexes: %v", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			t.Errorf("close sqlite_master index rows: %v", closeErr)
		}
	}()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan sqlite_master index row: %v", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate sqlite_master index rows: %v", err)
	}

	wantIndexes := []string{
		"rich_categories_slug_idx",
		"rich_posts_category_id_idx",
		"rich_posts_published_created_at_idx",
		"rich_posts_user_id_idx",
		"rich_users_email_idx",
		"rich_users_external_id_idx",
		"rich_users_status_created_at_idx",
	}
	for _, want := range wantIndexes {
		if !slices.Contains(names, want) {
			t.Fatalf("expected index %q in sqlite_master, got %#v", want, names)
		}
	}
}

func TestSQLiteIntegrationDialectTypeRendering(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openSQLiteTestDB(t)

	sqliteDialect, err := dialect.GetDialect("sqlite")
	if err != nil {
		t.Fatalf("get sqlite dialect: %v", err)
	}

	statement := `CREATE TABLE dialect_types (
		ratio ` + sqliteDialect.DataType(schema.ColumnType{DataType: schema.TypeReal}) + ` NOT NULL,
		precise ` + sqliteDialect.DataType(schema.ColumnType{DataType: schema.TypeDouble}) + ` NOT NULL,
		amount ` + sqliteDialect.DataType(schema.ColumnType{DataType: schema.TypeDecimal, Precision: 12, Scale: 2}) + ` NOT NULL,
		created_at ` + sqliteDialect.DataType(schema.ColumnType{DataType: schema.TypeTimestampTZ}) + ` NOT NULL,
		metadata ` + sqliteDialect.DataType(schema.ColumnType{DataType: schema.TypeJSONB}) + `,
		external_id ` + sqliteDialect.DataType(schema.ColumnType{DataType: schema.TypeUUID}) + `,
		status ` + sqliteDialect.DataType(schema.ColumnType{DataType: schema.TypeEnum, EnumValues: []string{"draft", "published"}}) + `,
		payload ` + sqliteDialect.DataType(schema.ColumnType{DataType: schema.TypeBytes}) + `
	)`

	if _, err := db.Exec(ctx, statement); err != nil {
		t.Fatalf("create dialect_types table failed: %v", err)
	}

	rows, err := db.Query(ctx, `PRAGMA table_info(dialect_types)`)
	if err != nil {
		t.Fatalf("query pragma table_info: %v", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			t.Errorf("close pragma table_info rows: %v", err)
		}
	}()

	got := map[string]string{}
	for rows.Next() {
		var (
			cid        int
			name       string
			declared   string
			notNull    int
			defaultSQL any
			primaryKey int
		)
		if err := rows.Scan(&cid, &name, &declared, &notNull, &defaultSQL, &primaryKey); err != nil {
			t.Fatalf("scan pragma table_info row: %v", err)
		}
		got[name] = declared
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate pragma table_info rows: %v", err)
	}

	want := map[string]string{
		"ratio":       "REAL",
		"precise":     "REAL",
		"amount":      "REAL",
		"created_at":  "TEXT",
		"metadata":    "TEXT",
		"external_id": "TEXT",
		"status":      "TEXT",
		"payload":     "BLOB",
	}

	if len(got) != len(want) {
		t.Fatalf("expected %d columns, got %d: %#v", len(want), len(got), got)
	}
	for name, expectedType := range want {
		if got[name] != expectedType {
			t.Fatalf("column %q: want declared type %q got %q", name, expectedType, got[name])
		}
	}
}

func openSQLiteTestDB(tb testing.TB) *rain.DB {
	tb.Helper()

	dbPath := filepath.Join(tb.TempDir(), "rain.sqlite")
	db, err := rain.Open("sqlite", dbPath)
	if err != nil {
		tb.Fatalf("open sqlite db: %v", err)
	}
	tb.Cleanup(func() {
		_ = db.Close()
	})

	return db
}

func createSQLiteSchema(tb testing.TB, ctx context.Context, db *rain.DB) {
	tb.Helper()

	users, posts := defineSQLiteTables()

	for _, table := range []schema.TableReference{users, posts} {
		statement, err := db.CreateTableSQL(table)
		if err != nil {
			tb.Fatalf("compile schema for %q: %v", table.TableDef().Name, err)
		}
		if _, err := db.Exec(ctx, statement); err != nil {
			tb.Fatalf("exec schema statement %q: %v", statement, err)
		}
	}
}

func createSQLiteRichSchema(tb testing.TB, ctx context.Context, db *rain.DB, fixture sqliteRichFixture) {
	tb.Helper()

	createSQLiteTablesOnly(tb, ctx, db, fixture.users, fixture.categories, fixture.posts)
	for _, table := range []schema.TableReference{fixture.users, fixture.categories, fixture.posts} {
		statements, err := db.CreateIndexesSQL(table)
		if err != nil {
			tb.Fatalf("CreateIndexesSQL(%q): %v", table.TableDef().Name, err)
		}
		for _, statement := range statements {
			if _, err := db.Exec(ctx, statement); err != nil {
				tb.Fatalf("exec index statement %q: %v", statement, err)
			}
		}
	}
}

func createSQLiteTablesOnly(tb testing.TB, ctx context.Context, db *rain.DB, tables ...schema.TableReference) {
	tb.Helper()

	for _, table := range tables {
		statement, err := db.CreateTableSQL(table)
		if err != nil {
			tb.Fatalf("compile schema for %q: %v", table.TableDef().Name, err)
		}
		if _, err := db.Exec(ctx, statement); err != nil {
			tb.Fatalf("exec schema statement %q: %v", statement, err)
		}
	}
}

func seedSQLiteRichFixture(tb testing.TB, ctx context.Context, db *rain.DB, fixture sqliteRichFixture) sqliteRichSeedData {
	tb.Helper()

	insertUser := func(email, name string, active bool, nickname *string, externalID, status string, createdAt, updatedAt time.Time) int64 {
		tb.Helper()

		result, err := db.Insert().
			Table(fixture.users).
			Set(fixture.users.Email, email).
			Set(fixture.users.Name, name).
			Set(fixture.users.Active, active).
			Set(fixture.users.Nickname, nickname).
			Set(fixture.users.ExternalID, externalID).
			Set(fixture.users.Status, status).
			Set(fixture.users.CreatedAt, createdAt).
			Set(fixture.users.UpdatedAt, updatedAt).
			Exec(ctx)
		if err != nil {
			tb.Fatalf("insert user %s: %v", email, err)
		}
		id, err := result.LastInsertId()
		if err != nil {
			tb.Fatalf("last insert id for %s: %v", email, err)
		}
		return id
	}

	insertCategory := func(slug, name string) int64 {
		tb.Helper()

		result, err := db.Insert().
			Table(fixture.categories).
			Set(fixture.categories.Slug, slug).
			Set(fixture.categories.Name, name).
			Exec(ctx)
		if err != nil {
			tb.Fatalf("insert category %s: %v", slug, err)
		}
		id, err := result.LastInsertId()
		if err != nil {
			tb.Fatalf("last insert id for category %s: %v", slug, err)
		}
		return id
	}

	insertPost := func(userID int64, categoryID any, title, body string, published bool, createdAt time.Time) {
		tb.Helper()

		query := db.Insert().
			Table(fixture.posts).
			Set(fixture.posts.UserID, userID).
			Set(fixture.posts.CategoryID, categoryID).
			Set(fixture.posts.Title, title).
			Set(fixture.posts.Body, body).
			Set(fixture.posts.Published, published).
			Set(fixture.posts.CreatedAt, createdAt)
		if _, err := query.Exec(ctx); err != nil {
			tb.Fatalf("insert post %s: %v", title, err)
		}
	}

	baseTime := time.Date(2026, time.March, 29, 12, 0, 0, 0, time.UTC)
	aliceNick := "ali"
	bobNick := "builder"
	aliceID := insertUser("alice@example.com", "Alice", true, &aliceNick, "ext-alice", "active", baseTime, baseTime.Add(2*time.Hour))
	bobID := insertUser("bob@example.com", "Bob", true, &bobNick, "ext-bob", "trial", baseTime.Add(time.Hour), baseTime.Add(3*time.Hour))
	carolID := insertUser("carol@example.com", "Carol", false, nil, "ext-carol", "disabled", baseTime.Add(2*time.Hour), baseTime.Add(4*time.Hour))
	productID := insertCategory("product", "Product")
	engineeringID := insertCategory("engineering", "Engineering")

	insertPost(aliceID, productID, "Launch Checklist", "Prepare launch materials", true, baseTime.Add(24*time.Hour))
	insertPost(aliceID, engineeringID, "Index Tuning", "Tune sqlite indexes", false, baseTime.Add(25*time.Hour))
	insertPost(bobID, productID, "Onboarding Flow", "Polish onboarding", true, baseTime.Add(26*time.Hour))

	return sqliteRichSeedData{
		AliceID:       aliceID,
		BobID:         bobID,
		CarolID:       carolID,
		ProductID:     productID,
		EngineeringID: engineeringID,
	}
}
