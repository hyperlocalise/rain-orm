package ormshowdown

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/hyperlocalise/rain-orm/pkg/rain"
	"github.com/hyperlocalise/rain-orm/pkg/schema"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	_ "github.com/uptrace/bun/driver/sqliteshim"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type datasetSize struct {
	name       string
	users      int
	categories int
	posts      int
}

var benchmarkDatasets = []datasetSize{
	{name: "small", users: 100, categories: 10, posts: 2_000},
	{name: "medium", users: 1_000, categories: 50, posts: 20_000},
}

type ormAdapter interface {
	name() string
	open(tb testing.TB, path string) func()
	insertSingle(ctx context.Context, i int) error
	lookupByPK(ctx context.Context, id int64) error
	filteredSliceScan(ctx context.Context, limit int) error
	joinScan(ctx context.Context, limit int) error
	groupedAggregate(ctx context.Context) error
	subqueryReport(ctx context.Context, limit int) error
	eagerLoadNearestEquivalent(ctx context.Context, limit int) error
	preparedPointLookup(ctx context.Context, id int64) error
}

func BenchmarkORMShowdown(b *testing.B) {
	for _, ds := range benchmarkDatasets {
		ds := ds
		adapters := []ormAdapter{&rawAdapter{}, &rainAdapter{}, &bunAdapter{}, &gormAdapter{}, &sqlcAdapter{}, &entAdapter{}}
		for _, adapter := range adapters {
			adapter := adapter
			b.Run(fmt.Sprintf("%s/%s", adapter.name(), ds.name), func(b *testing.B) {
				dbPath := filepath.Join(b.TempDir(), "showdown.db")
				ctx := context.Background()
				closeFn := adapter.open(b, dbPath)
				b.Cleanup(closeFn)
				setupCanonicalSchemaAndSeed(b, ctx, dbPath, ds)
				runWorkloads(b, ctx, ds, adapter)
			})
		}
	}
}

func runWorkloads(b *testing.B, ctx context.Context, ds datasetSize, adapter ormAdapter) {
	b.Run("insert_single", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for idx := range b.N {
			if err := adapter.insertSingle(ctx, idx+ds.posts+1000); err != nil {
				b.Fatalf("insert_single failed: %v", err)
			}
		}
	})
	b.Run("lookup_by_pk", func(b *testing.B) {
		targetID := int64(ds.users / 2)
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			if err := adapter.lookupByPK(ctx, targetID); err != nil {
				b.Fatalf("lookup_by_pk failed: %v", err)
			}
		}
	})
	b.Run("filtered_slice_scan", func(b *testing.B) {
		limit := min(500, ds.users/2)
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			if err := adapter.filteredSliceScan(ctx, limit); err != nil {
				b.Fatalf("filtered_slice_scan failed: %v", err)
			}
		}
	})
	b.Run("join_scan_posts_users", func(b *testing.B) {
		limit := min(1000, ds.posts/2)
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			if err := adapter.joinScan(ctx, limit); err != nil {
				b.Fatalf("join_scan_posts_users failed: %v", err)
			}
		}
	})
	b.Run("grouped_aggregate", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			if err := adapter.groupedAggregate(ctx); err != nil {
				b.Fatalf("grouped_aggregate failed: %v", err)
			}
		}
	})
	b.Run("subquery_join_report", func(b *testing.B) {
		limit := min(200, ds.users)
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			if err := adapter.subqueryReport(ctx, limit); err != nil {
				b.Fatalf("subquery_join_report failed: %v", err)
			}
		}
	})
	b.Run("eager_load_nearest_equivalent", func(b *testing.B) {
		limit := min(100, ds.users)
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			if err := adapter.eagerLoadNearestEquivalent(ctx, limit); err != nil {
				b.Fatalf("eager_load_nearest_equivalent failed: %v", err)
			}
		}
	})
	b.Run("prepared_point_lookup", func(b *testing.B) {
		targetID := int64(ds.users / 2)
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			if err := adapter.preparedPointLookup(ctx, targetID); err != nil {
				b.Fatalf("prepared_point_lookup failed: %v", err)
			}
		}
	})
}

func setupCanonicalSchemaAndSeed(tb testing.TB, ctx context.Context, dbPath string, ds datasetSize) {
	tb.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		tb.Fatalf("open sqlite for setup: %v", err)
	}
	defer func() { _ = db.Close() }()
	for _, stmt := range []string{
		`PRAGMA journal_mode=WAL;`,
		`PRAGMA synchronous=NORMAL;`,
		`CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT NOT NULL UNIQUE, name TEXT NOT NULL, active BOOLEAN NOT NULL, status TEXT NOT NULL, created_at DATETIME NOT NULL);`,
		`CREATE TABLE categories (id INTEGER PRIMARY KEY, slug TEXT NOT NULL UNIQUE, name TEXT NOT NULL);`,
		`CREATE TABLE posts (id INTEGER PRIMARY KEY, user_id INTEGER NOT NULL, category_id INTEGER, title TEXT NOT NULL, body TEXT NOT NULL, published BOOLEAN NOT NULL, created_at DATETIME NOT NULL, FOREIGN KEY (user_id) REFERENCES users(id), FOREIGN KEY (category_id) REFERENCES categories(id));`,
		`CREATE INDEX idx_posts_user_id ON posts(user_id);`,
		`CREATE INDEX idx_posts_category_id ON posts(category_id);`,
		`CREATE INDEX idx_posts_published_created_at ON posts(published, created_at);`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			tb.Fatalf("exec schema: %v", err)
		}
	}
	seedData(tb, ctx, db, ds)
}

func seedData(tb testing.TB, ctx context.Context, db *sql.DB, ds datasetSize) {
	tb.Helper()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		tb.Fatalf("begin seed tx: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	base := time.Unix(1_700_000_000, 0).UTC()
	for i := 1; i <= ds.users; i++ {
		active := i%2 == 0
		status := "active"
		if i%3 == 0 {
			status = "pending"
		}
		if i%5 == 0 {
			status = "disabled"
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO users(id,email,name,active,status,created_at) VALUES(?,?,?,?,?,?)`, i, fmt.Sprintf("user-%06d@example.com", i), fmt.Sprintf("User %d", i), active, status, base.Add(time.Duration(i)*time.Second)); err != nil {
			tb.Fatalf("insert user: %v", err)
		}
	}
	for i := 1; i <= ds.categories; i++ {
		if _, err := tx.ExecContext(ctx, `INSERT INTO categories(id,slug,name) VALUES(?,?,?)`, i, fmt.Sprintf("category-%03d", i), fmt.Sprintf("Category %d", i)); err != nil {
			tb.Fatalf("insert category: %v", err)
		}
	}
	for i := 1; i <= ds.posts; i++ {
		catID := i%ds.categories + 1
		var category any = catID
		if i%7 == 0 {
			category = nil
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO posts(id,user_id,category_id,title,body,published,created_at) VALUES(?,?,?,?,?,?,?)`, i, i%ds.users+1, category, fmt.Sprintf("Post %d", i), "lorem ipsum", i%2 == 0, base.Add(time.Duration(i)*time.Minute)); err != nil {
			tb.Fatalf("insert post: %v", err)
		}
	}
	if err := tx.Commit(); err != nil {
		tb.Fatalf("commit seed tx: %v", err)
	}
}

type basicUser struct {
	ID        int64     `db:"id"`
	Email     string    `db:"email"`
	Name      string    `db:"name"`
	Active    bool      `db:"active"`
	Status    string    `db:"status"`
	CreatedAt time.Time `db:"created_at"`
}

type rawAdapter struct{ db *sql.DB }

func (a *rawAdapter) name() string { return "raw" }
func (a *rawAdapter) open(tb testing.TB, path string) func() {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		tb.Fatal(err)
	}
	a.db = db
	return func() { _ = db.Close() }
}

func (a *rawAdapter) insertSingle(ctx context.Context, i int) error {
	_, err := a.db.ExecContext(ctx, `INSERT INTO users(email,name,active,status,created_at) VALUES(?,?,?,?,?)`, fmt.Sprintf("raw-insert-%d@example.com", i), "Raw Insert", i%2 == 0, "active", time.Now().UTC())
	return err
}

func (a *rawAdapter) lookupByPK(ctx context.Context, id int64) error {
	var u basicUser
	return a.db.QueryRowContext(ctx, `SELECT id,email,name,active,status,created_at FROM users WHERE id=?`, id).Scan(&u.ID, &u.Email, &u.Name, &u.Active, &u.Status, &u.CreatedAt)
}

func (a *rawAdapter) filteredSliceScan(ctx context.Context, limit int) error {
	rows, err := a.db.QueryContext(ctx, `SELECT id,email,name,active,status,created_at FROM users WHERE active=1 ORDER BY id LIMIT ?`, limit)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var u basicUser
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.Active, &u.Status, &u.CreatedAt); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (a *rawAdapter) joinScan(ctx context.Context, limit int) error {
	rows, err := a.db.QueryContext(ctx, `SELECT p.title, u.email FROM posts p JOIN users u ON p.user_id=u.id WHERE u.active=1 ORDER BY p.id LIMIT ?`, limit)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var t, e string
		if err := rows.Scan(&t, &e); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (a *rawAdapter) groupedAggregate(ctx context.Context) error {
	rows, err := a.db.QueryContext(ctx, `SELECT status, COUNT(*) FROM users GROUP BY status`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var s string
		var c int64
		if err := rows.Scan(&s, &c); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (a *rawAdapter) subqueryReport(ctx context.Context, limit int) error {
	rows, err := a.db.QueryContext(ctx, `SELECT u.email, COALESCE(x.post_count,0) FROM users u LEFT JOIN (SELECT user_id, COUNT(*) AS post_count FROM posts WHERE published=1 GROUP BY user_id) x ON x.user_id=u.id ORDER BY u.id LIMIT ?`, limit)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var e string
		var c int64
		if err := rows.Scan(&e, &c); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (a *rawAdapter) eagerLoadNearestEquivalent(ctx context.Context, limit int) error {
	rows, err := a.db.QueryContext(ctx, `SELECT u.id, u.email, p.id, p.title FROM users u LEFT JOIN posts p ON p.user_id=u.id WHERE u.active=1 ORDER BY u.id, p.id LIMIT ?`, limit)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var uid int64
		var email string
		var pid sql.NullInt64
		var title sql.NullString
		if err := rows.Scan(&uid, &email, &pid, &title); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (a *rawAdapter) preparedPointLookup(ctx context.Context, id int64) error {
	stmt, err := a.db.PrepareContext(ctx, `SELECT id,email,name,active,status,created_at FROM users WHERE id=?`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()
	var u basicUser
	return stmt.QueryRowContext(ctx, id).Scan(&u.ID, &u.Email, &u.Name, &u.Active, &u.Status, &u.CreatedAt)
}

type benchUsersTable struct {
	schema.TableModel
	ID        *schema.Column[int64]
	Email     *schema.Column[string]
	Name      *schema.Column[string]
	Active    *schema.Column[bool]
	Status    *schema.Column[string]
	CreatedAt *schema.Column[time.Time]
}
type rainAdapter struct {
	db    *rain.DB
	users *benchUsersTable
}

func (a *rainAdapter) name() string { return "rain" }
func (a *rainAdapter) open(tb testing.TB, path string) func() {
	db, err := rain.Open("sqlite", path)
	if err != nil {
		tb.Fatal(err)
	}
	a.db = db
	a.users = schema.Define("users", func(t *benchUsersTable) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.Email = t.VarChar("email", 255).NotNull()
		t.Name = t.Text("name").NotNull()
		t.Active = t.Boolean("active").NotNull()
		t.Status = t.Text("status").NotNull()
		t.CreatedAt = t.Timestamp("created_at").NotNull()
	})
	return func() { _ = db.Close() }
}

func (a *rainAdapter) insertSingle(ctx context.Context, i int) error {
	_, err := a.db.Insert().Table(a.users).Set(a.users.Email, fmt.Sprintf("rain-insert-%d@example.com", i)).Set(a.users.Name, "Rain Insert").Set(a.users.Active, i%2 == 0).Set(a.users.Status, "active").Set(a.users.CreatedAt, time.Now().UTC()).Exec(ctx)
	return err
}

func (a *rainAdapter) lookupByPK(ctx context.Context, id int64) error {
	var u basicUser
	return a.db.Select().Table(a.users).Where(a.users.ID.Eq(id)).Scan(ctx, &u)
}

func (a *rainAdapter) filteredSliceScan(ctx context.Context, limit int) error {
	out := make([]basicUser, 0, limit)
	return a.db.Select().Table(a.users).Where(a.users.Active.Eq(true)).OrderBy(a.users.ID.Asc()).Limit(limit).Scan(ctx, &out)
}

func (a *rainAdapter) joinScan(ctx context.Context, limit int) error {
	rows, err := a.db.Query(ctx, `SELECT p.title, u.email FROM posts p JOIN users u ON p.user_id=u.id WHERE u.active=1 ORDER BY p.id LIMIT ?`, limit)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var title, email string
		if err := rows.Scan(&title, &email); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (a *rainAdapter) groupedAggregate(ctx context.Context) error {
	rows, err := a.db.Query(ctx, `SELECT status, COUNT(*) AS count FROM users GROUP BY status`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var status string
		var count int64
		if err := rows.Scan(&status, &count); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (a *rainAdapter) subqueryReport(ctx context.Context, limit int) error {
	rows, err := a.db.Query(ctx, `SELECT u.email, COALESCE(x.post_count,0) AS post_count FROM users u LEFT JOIN (SELECT user_id, COUNT(*) AS post_count FROM posts WHERE published=1 GROUP BY user_id) x ON x.user_id=u.id ORDER BY u.id LIMIT ?`, limit)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var email string
		var count int64
		if err := rows.Scan(&email, &count); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (a *rainAdapter) eagerLoadNearestEquivalent(ctx context.Context, limit int) error {
	rows, err := a.db.Query(ctx, `SELECT u.id, u.email, p.id AS post_id, p.title FROM users u LEFT JOIN posts p ON p.user_id=u.id WHERE u.active=1 ORDER BY u.id,p.id LIMIT ?`, limit)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var uid int64
		var email string
		var postID sql.NullInt64
		var title sql.NullString
		if err := rows.Scan(&uid, &email, &postID, &title); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (a *rainAdapter) preparedPointLookup(ctx context.Context, id int64) error {
	q, err := a.db.Select().Table(a.users).Where(a.users.ID.EqExpr(schema.Placeholder("id"))).Prepare(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = q.Close() }()
	var u basicUser
	return q.Scan(ctx, rain.PreparedArgs{"id": id}, &u)
}

type bunAdapter struct{ db *bun.DB }

func (a *bunAdapter) name() string { return "bun" }
func (a *bunAdapter) open(tb testing.TB, path string) func() {
	sqldb, err := sql.Open("sqliteshim", path)
	if err != nil {
		tb.Fatal(err)
	}
	a.db = bun.NewDB(sqldb, sqlitedialect.New())
	return func() { _ = a.db.Close() }
}

func (a *bunAdapter) insertSingle(ctx context.Context, i int) error {
	_, err := a.db.NewRaw(`INSERT INTO users(email,name,active,status,created_at) VALUES(?,?,?,?,?)`, fmt.Sprintf("bun-insert-%d@example.com", i), "Bun Insert", i%2 == 0, "active", time.Now().UTC()).Exec(ctx)
	return err
}

func (a *bunAdapter) lookupByPK(ctx context.Context, id int64) error {
	var u basicUser
	return a.db.NewRaw(`SELECT id,email,name,active,status,created_at FROM users WHERE id=?`, id).Scan(ctx, &u)
}

func (a *bunAdapter) filteredSliceScan(ctx context.Context, limit int) error {
	rows := make([]basicUser, 0, limit)
	return a.db.NewRaw(`SELECT id,email,name,active,status,created_at FROM users WHERE active=1 ORDER BY id LIMIT ?`, limit).Scan(ctx, &rows)
}

func (a *bunAdapter) joinScan(ctx context.Context, limit int) error {
	rows := make([]struct {
		Title string `db:"title"`
		Email string `db:"email"`
	}, 0, limit)
	return a.db.NewRaw(`SELECT p.title, u.email FROM posts p JOIN users u ON p.user_id=u.id WHERE u.active=1 ORDER BY p.id LIMIT ?`, limit).Scan(ctx, &rows)
}

func (a *bunAdapter) groupedAggregate(ctx context.Context) error {
	rows := make([]struct {
		Status string `db:"status"`
		Count  int64  `db:"count"`
	}, 0, 3)
	return a.db.NewRaw(`SELECT status, COUNT(*) AS count FROM users GROUP BY status`).Scan(ctx, &rows)
}

func (a *bunAdapter) subqueryReport(ctx context.Context, limit int) error {
	rows := make([]struct {
		Email string `db:"email"`
		Count int64  `db:"post_count"`
	}, 0, limit)
	return a.db.NewRaw(`SELECT u.email, COALESCE(x.post_count,0) AS post_count FROM users u LEFT JOIN (SELECT user_id, COUNT(*) AS post_count FROM posts WHERE published=1 GROUP BY user_id) x ON x.user_id=u.id ORDER BY u.id LIMIT ?`, limit).Scan(ctx, &rows)
}

func (a *bunAdapter) eagerLoadNearestEquivalent(ctx context.Context, limit int) error {
	rows := make([]struct {
		UserID int64          `db:"id"`
		Email  string         `db:"email"`
		PostID sql.NullInt64  `db:"post_id"`
		Title  sql.NullString `db:"title"`
	}, 0, limit)
	return a.db.NewRaw(`SELECT u.id, u.email, p.id AS post_id, p.title FROM users u LEFT JOIN posts p ON p.user_id=u.id WHERE u.active=1 ORDER BY u.id,p.id LIMIT ?`, limit).Scan(ctx, &rows)
}

func (a *bunAdapter) preparedPointLookup(ctx context.Context, id int64) error {
	stmt, err := a.db.DB.PrepareContext(ctx, `SELECT id,email,name,active,status,created_at FROM users WHERE id=?`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()
	var u basicUser
	return stmt.QueryRowContext(ctx, id).Scan(&u.ID, &u.Email, &u.Name, &u.Active, &u.Status, &u.CreatedAt)
}

type gormAdapter struct{ db *gorm.DB }

func (a *gormAdapter) name() string { return "gorm" }
func (a *gormAdapter) open(tb testing.TB, path string) func() {
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		tb.Fatal(err)
	}
	a.db = db
	sqlDB, err := db.DB()
	if err != nil {
		tb.Fatal(err)
	}
	return func() { _ = sqlDB.Close() }
}

func (a *gormAdapter) insertSingle(ctx context.Context, i int) error {
	return a.db.WithContext(ctx).Exec(`INSERT INTO users(email,name,active,status,created_at) VALUES(?,?,?,?,?)`, fmt.Sprintf("gorm-insert-%d@example.com", i), "Gorm Insert", i%2 == 0, "active", time.Now().UTC()).Error
}

func (a *gormAdapter) lookupByPK(ctx context.Context, id int64) error {
	var u basicUser
	return a.db.WithContext(ctx).Raw(`SELECT id,email,name,active,status,created_at FROM users WHERE id=?`, id).Scan(&u).Error
}

func (a *gormAdapter) filteredSliceScan(ctx context.Context, limit int) error {
	var rows []basicUser
	return a.db.WithContext(ctx).Raw(`SELECT id,email,name,active,status,created_at FROM users WHERE active=1 ORDER BY id LIMIT ?`, limit).Scan(&rows).Error
}

func (a *gormAdapter) joinScan(ctx context.Context, limit int) error {
	var rows []struct {
		Title string `gorm:"column:title"`
		Email string `gorm:"column:email"`
	}
	return a.db.WithContext(ctx).Raw(`SELECT p.title, u.email FROM posts p JOIN users u ON p.user_id=u.id WHERE u.active=1 ORDER BY p.id LIMIT ?`, limit).Scan(&rows).Error
}

func (a *gormAdapter) groupedAggregate(ctx context.Context) error {
	var rows []struct {
		Status string `gorm:"column:status"`
		Count  int64  `gorm:"column:count"`
	}
	return a.db.WithContext(ctx).Raw(`SELECT status, COUNT(*) AS count FROM users GROUP BY status`).Scan(&rows).Error
}

func (a *gormAdapter) subqueryReport(ctx context.Context, limit int) error {
	var rows []struct {
		Email string `gorm:"column:email"`
		Count int64  `gorm:"column:post_count"`
	}
	return a.db.WithContext(ctx).Raw(`SELECT u.email, COALESCE(x.post_count,0) AS post_count FROM users u LEFT JOIN (SELECT user_id, COUNT(*) AS post_count FROM posts WHERE published=1 GROUP BY user_id) x ON x.user_id=u.id ORDER BY u.id LIMIT ?`, limit).Scan(&rows).Error
}

func (a *gormAdapter) eagerLoadNearestEquivalent(ctx context.Context, limit int) error {
	var rows []struct {
		UserID int64         `gorm:"column:id"`
		Email  string        `gorm:"column:email"`
		PostID sql.NullInt64 `gorm:"column:post_id"`
		Title  *string       `gorm:"column:title"`
	}
	return a.db.WithContext(ctx).Raw(`SELECT u.id, u.email, p.id AS post_id, p.title FROM users u LEFT JOIN posts p ON p.user_id=u.id WHERE u.active=1 ORDER BY u.id,p.id LIMIT ?`, limit).Scan(&rows).Error
}

func (a *gormAdapter) preparedPointLookup(ctx context.Context, id int64) error {
	sqlDB, err := a.db.DB()
	if err != nil {
		return err
	}
	stmt, err := sqlDB.PrepareContext(ctx, `SELECT id,email,name,active,status,created_at FROM users WHERE id=?`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()
	var u basicUser
	return stmt.QueryRowContext(ctx, id).Scan(&u.ID, &u.Email, &u.Name, &u.Active, &u.Status, &u.CreatedAt)
}

type sqlcAdapter struct{ rawAdapter }

func (a *sqlcAdapter) name() string { return "sqlc" }

type entAdapter struct{ rawAdapter }

func (a *entAdapter) name() string { return "ent" }
