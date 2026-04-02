package migrator

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	exampleregistry "github.com/hyperlocalise/rain-orm/examples/schema/registry"
	"github.com/hyperlocalise/rain-orm/pkg/schema"
	_ "modernc.org/sqlite"
)

func TestBuildSnapshotDeterministic(t *testing.T) {
	t.Parallel()

	first, err := BuildSnapshot("sqlite", exampleregistry.ManagedTables())
	if err != nil {
		t.Fatalf("BuildSnapshot returned error: %v", err)
	}
	second, err := BuildSnapshot("sqlite", exampleregistry.ManagedTables())
	if err != nil {
		t.Fatalf("BuildSnapshot returned error: %v", err)
	}

	firstData, err := MarshalSnapshot(first)
	if err != nil {
		t.Fatalf("MarshalSnapshot returned error: %v", err)
	}
	secondData, err := MarshalSnapshot(second)
	if err != nil {
		t.Fatalf("MarshalSnapshot returned error: %v", err)
	}
	if string(firstData) != string(secondData) {
		t.Fatalf("expected deterministic snapshots\nfirst:\n%s\nsecond:\n%s", firstData, secondData)
	}
}

func TestDiffSnapshotsCreateAllFromEmpty(t *testing.T) {
	t.Parallel()

	current, err := BuildSnapshot("sqlite", exampleregistry.ManagedTables())
	if err != nil {
		t.Fatalf("BuildSnapshot returned error: %v", err)
	}

	plan, err := DiffSnapshots(nil, current)
	if err != nil {
		t.Fatalf("DiffSnapshots returned error: %v", err)
	}
	if plan.Empty() {
		t.Fatalf("expected create statements")
	}
}

func TestDiffSnapshotsAddColumn(t *testing.T) {
	t.Parallel()

	before := mustBuildSnapshot(t, "sqlite", []schema.TableReference{usersTableWithoutNickname()})
	after := mustBuildSnapshot(t, "sqlite", []schema.TableReference{usersTableWithNickname()})

	plan, err := DiffSnapshots(&before, after)
	if err != nil {
		t.Fatalf("DiffSnapshots returned error: %v", err)
	}
	if len(plan.Statements) != 1 {
		t.Fatalf("expected one statement, got %d", len(plan.Statements))
	}
	if !strings.Contains(plan.Statements[0], `ADD COLUMN "nickname" TEXT`) {
		t.Fatalf("expected ADD COLUMN statement, got %q", plan.Statements[0])
	}
}

func TestDiffSnapshotsRejectChangedColumn(t *testing.T) {
	t.Parallel()

	before := mustBuildSnapshot(t, "sqlite", []schema.TableReference{usersTableWithoutNickname()})
	after := mustBuildSnapshot(t, "sqlite", []schema.TableReference{usersTableChangedEmailType()})

	if _, err := DiffSnapshots(&before, after); err == nil || !strings.Contains(err.Error(), "changing column") {
		t.Fatalf("expected changing column error, got %v", err)
	}
}

func TestDiffSnapshotsAddIndex(t *testing.T) {
	t.Parallel()

	before := mustBuildSnapshot(t, "sqlite", []schema.TableReference{usersTableWithoutIndex()})
	after := mustBuildSnapshot(t, "sqlite", []schema.TableReference{usersTableWithIndex()})

	plan, err := DiffSnapshots(&before, after)
	if err != nil {
		t.Fatalf("DiffSnapshots returned error: %v", err)
	}
	if len(plan.Statements) != 1 || !strings.Contains(plan.Statements[0], `CREATE INDEX "users_name_idx"`) {
		t.Fatalf("expected new index statement, got %v", plan.Statements)
	}
}

func TestDiffSnapshotsAddConstraintSupportedDialect(t *testing.T) {
	t.Parallel()

	before := mustBuildSnapshot(t, "postgres", []schema.TableReference{usersTableWithoutConstraint()})
	after := mustBuildSnapshot(t, "postgres", []schema.TableReference{usersTableWithConstraint()})

	plan, err := DiffSnapshots(&before, after)
	if err != nil {
		t.Fatalf("DiffSnapshots returned error: %v", err)
	}
	if len(plan.Statements) != 1 || !strings.Contains(plan.Statements[0], `ADD CONSTRAINT "users_name_key" UNIQUE ("name")`) {
		t.Fatalf("expected add constraint statement, got %v", plan.Statements)
	}
}

func TestDiffSnapshotsRejectAddConstraintOnSQLite(t *testing.T) {
	t.Parallel()

	before := mustBuildSnapshot(t, "sqlite", []schema.TableReference{usersTableWithoutConstraint()})
	after := mustBuildSnapshot(t, "sqlite", []schema.TableReference{usersTableWithConstraint()})

	if _, err := DiffSnapshots(&before, after); err == nil || !strings.Contains(err.Error(), "adding constraint") {
		t.Fatalf("expected sqlite add constraint rejection, got %v", err)
	}
}

func TestDiffSnapshotsRejectAddForeignKeyOnSQLite(t *testing.T) {
	t.Parallel()

	beforeUsers, beforePosts := tablesWithoutForeignKey()
	afterUsers, afterPosts := tablesWithForeignKey()
	before := mustBuildSnapshot(t, "sqlite", []schema.TableReference{beforePosts, beforeUsers})
	after := mustBuildSnapshot(t, "sqlite", []schema.TableReference{afterPosts, afterUsers})

	if _, err := DiffSnapshots(&before, after); err == nil || !strings.Contains(err.Error(), "adding constraint") {
		t.Fatalf("expected sqlite add foreign key rejection, got %v", err)
	}
}

func TestSplitSQLStatements(t *testing.T) {
	t.Parallel()

	statements, err := SplitSQLStatements("CREATE TABLE users (name TEXT DEFAULT 'a;bc');\n-- comment;\nCREATE INDEX users_name_idx ON users (name);")
	if err != nil {
		t.Fatalf("SplitSQLStatements returned error: %v", err)
	}
	if len(statements) != 2 {
		t.Fatalf("expected 2 statements, got %d", len(statements))
	}
}

func TestSplitSQLStatementsHandlesEscapedQuotesAndBlockComments(t *testing.T) {
	t.Parallel()

	sqlText := "/* leading; comment */\nINSERT INTO users (name) VALUES ('it''s; fine');\nCREATE TABLE \"semi;colon\" (id INTEGER);"
	statements, err := SplitSQLStatements(sqlText)
	if err != nil {
		t.Fatalf("SplitSQLStatements returned error: %v", err)
	}
	if len(statements) != 2 {
		t.Fatalf("expected 2 statements, got %d", len(statements))
	}
	if !strings.Contains(statements[0], "it''s; fine") {
		t.Fatalf("expected escaped quote content to be preserved, got %q", statements[0])
	}
	if !strings.Contains(statements[1], `"semi;colon"`) {
		t.Fatalf("expected quoted identifier with semicolon to be preserved, got %q", statements[1])
	}
}

func TestSplitSQLStatementsRejectsUnterminatedBlockComment(t *testing.T) {
	t.Parallel()

	if _, err := SplitSQLStatements("CREATE TABLE users (id INTEGER); /* unterminated"); err == nil || !strings.Contains(err.Error(), "unterminated") {
		t.Fatalf("expected unterminated comment error, got %v", err)
	}
}

func TestSplitSQLStatementsHandlesPostgresDollarQuotes(t *testing.T) {
	t.Parallel()

	sqlText := "DO $$ BEGIN RAISE NOTICE 'hello'; PERFORM 1; END $$;\nCREATE TABLE users (id INTEGER);"
	statements, err := SplitSQLStatements(sqlText)
	if err != nil {
		t.Fatalf("SplitSQLStatements returned error: %v", err)
	}
	if len(statements) != 2 {
		t.Fatalf("expected 2 statements, got %d", len(statements))
	}
	if !strings.Contains(statements[0], "PERFORM 1;") {
		t.Fatalf("expected semicolon inside dollar quote to be preserved, got %q", statements[0])
	}
}

func TestWriteMigrationFilesUsesDrizzleStyleFoldersAndAvoidsCollision(t *testing.T) {
	t.Parallel()

	snapshot := mustBuildSnapshot(t, "sqlite", []schema.TableReference{usersTableWithoutNickname()})
	plan := Plan{Statements: []string{`CREATE TABLE "users" ("id" INTEGER PRIMARY KEY)`}}
	dir := t.TempDir()
	now := time.Date(2026, time.March, 30, 12, 0, 0, 0, time.UTC)

	first, err := WriteMigrationFiles(dir, "init", plan, snapshot, now)
	if err != nil {
		t.Fatalf("WriteMigrationFiles returned error: %v", err)
	}
	second, err := WriteMigrationFiles(filepath.Dir(first.DirPath), "init", plan, snapshot, now)
	if err != nil {
		t.Fatalf("second WriteMigrationFiles returned error: %v", err)
	}

	if filepath.Base(first.DirPath) == filepath.Base(second.DirPath) {
		t.Fatalf("expected unique directory names, got %q", first.DirPath)
	}
	if filepath.Base(first.SQLPath) != "migration.sql" || filepath.Base(first.SnapshotPath) != "snapshot.json" {
		t.Fatalf("expected drizzle-style filenames, got %q and %q", first.SQLPath, first.SnapshotPath)
	}
	if _, err := os.Stat(first.SQLPath); err != nil {
		t.Fatalf("expected migration.sql to exist: %v", err)
	}
	if _, err := os.Stat(first.SnapshotPath); err != nil {
		t.Fatalf("expected snapshot.json to exist: %v", err)
	}
}

func TestLoadDiskMigrationsAndReadLatestSnapshotFromChain(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	plan := Plan{Statements: []string{`CREATE TABLE "users" ("id" INTEGER PRIMARY KEY)`}}
	firstSnapshot := mustBuildSnapshot(t, "sqlite", []schema.TableReference{usersTableWithoutNickname()})
	secondSnapshot := mustBuildSnapshot(t, "sqlite", []schema.TableReference{usersTableWithNickname()})

	first, err := WriteMigrationFiles(dir, "init", plan, firstSnapshot, time.Date(2026, time.March, 30, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("first WriteMigrationFiles returned error: %v", err)
	}
	second, err := WriteMigrationFiles(dir, "add nickname", plan, secondSnapshot, time.Date(2026, time.March, 30, 12, 1, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("second WriteMigrationFiles returned error: %v", err)
	}

	migrations, err := LoadDiskMigrations(dir)
	if err != nil {
		t.Fatalf("LoadDiskMigrations returned error: %v", err)
	}
	if len(migrations) != 2 {
		t.Fatalf("expected 2 migrations, got %d", len(migrations))
	}
	if migrations[0].ID != first.ID || migrations[1].ID != second.ID {
		t.Fatalf("expected migrations ordered by id, got %q then %q", migrations[0].ID, migrations[1].ID)
	}

	latest, err := ReadLatestSnapshotFromMigrations(dir)
	if err != nil {
		t.Fatalf("ReadLatestSnapshotFromMigrations returned error: %v", err)
	}
	if latest == nil {
		t.Fatalf("expected latest snapshot")
	}
	latestData, err := MarshalSnapshot(*latest)
	if err != nil {
		t.Fatalf("MarshalSnapshot latest: %v", err)
	}
	secondData, err := MarshalSnapshot(secondSnapshot)
	if err != nil {
		t.Fatalf("MarshalSnapshot second snapshot: %v", err)
	}
	if string(latestData) != string(secondData) {
		t.Fatalf("expected latest snapshot to match second migration snapshot")
	}
}

func TestLoadDiskMigrationsRejectsMissingSnapshotFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	plan := Plan{Statements: []string{`CREATE TABLE "users" ("id" INTEGER PRIMARY KEY)`}}
	snapshot := mustBuildSnapshot(t, "sqlite", []schema.TableReference{usersTableWithoutNickname()})

	migration, err := WriteMigrationFiles(dir, "init", plan, snapshot, time.Date(2026, time.March, 30, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("WriteMigrationFiles returned error: %v", err)
	}
	if err := os.Remove(filepath.Join(migration.DirPath, "snapshot.json")); err != nil {
		t.Fatalf("Remove snapshot.json: %v", err)
	}

	if _, err := LoadDiskMigrations(dir); err == nil || !strings.Contains(err.Error(), "missing snapshot.json") {
		t.Fatalf("expected missing snapshot.json error, got %v", err)
	}
}

func TestLoadDiskMigrationsRejectsMissingMigrationSQLFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	plan := Plan{Statements: []string{`CREATE TABLE "users" ("id" INTEGER PRIMARY KEY)`}}
	snapshot := mustBuildSnapshot(t, "sqlite", []schema.TableReference{usersTableWithoutNickname()})

	migration, err := WriteMigrationFiles(dir, "init", plan, snapshot, time.Date(2026, time.March, 30, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("WriteMigrationFiles returned error: %v", err)
	}
	if err := os.Remove(filepath.Join(migration.DirPath, "migration.sql")); err != nil {
		t.Fatalf("Remove migration.sql: %v", err)
	}

	if _, err := LoadDiskMigrations(dir); err == nil || !strings.Contains(err.Error(), "missing migration.sql") {
		t.Fatalf("expected missing migration.sql error, got %v", err)
	}
}

func TestApplySQLMigrationsRejectsPendingOlderThanLastApplied(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "migrator.sqlite"))
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	if _, err := db.ExecContext(ctx, `CREATE TABLE "rain_schema_migrations" (id TEXT PRIMARY KEY, checksum TEXT NOT NULL DEFAULT '', applied_at TIMESTAMP NOT NULL, runtime_ms INTEGER NOT NULL, tool_version TEXT NOT NULL DEFAULT '', notes TEXT NOT NULL DEFAULT '')`); err != nil {
		t.Fatalf("create migration table: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO "rain_schema_migrations" (id, checksum, applied_at, runtime_ms, tool_version, notes) VALUES ('20260330130000_newer', '', CURRENT_TIMESTAMP, 1, '', '')`); err != nil {
		t.Fatalf("insert applied migration: %v", err)
	}

	pending := []DiskMigration{
		{
			ID:  "20260330120000_older",
			SQL: `CREATE TABLE "users" ("id" INTEGER PRIMARY KEY);`,
		},
	}
	if _, err := ApplySQLMigrations(ctx, db, "sqlite", "rain_schema_migrations", pending); err == nil || !strings.Contains(err.Error(), "older than the last applied migration") {
		t.Fatalf("expected older-than-last-applied error, got %v", err)
	}
}

func TestApplySQLMigrationsRejectsDatabaseAheadOfLocalArtifacts(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "migrator-ahead.sqlite"))
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	if _, err := db.ExecContext(ctx, `CREATE TABLE "rain_schema_migrations" (id TEXT PRIMARY KEY, checksum TEXT NOT NULL DEFAULT '', applied_at TIMESTAMP NOT NULL, runtime_ms INTEGER NOT NULL, tool_version TEXT NOT NULL DEFAULT '', notes TEXT NOT NULL DEFAULT '')`); err != nil {
		t.Fatalf("create migration table: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO "rain_schema_migrations" (id, checksum, applied_at, runtime_ms, tool_version, notes) VALUES ('20260330130000_newer', 'abc', CURRENT_TIMESTAMP, 1, '', '')`); err != nil {
		t.Fatalf("insert applied migration: %v", err)
	}

	if _, err := ApplySQLMigrations(ctx, db, "sqlite", "rain_schema_migrations", nil); err == nil || !strings.Contains(err.Error(), "database is ahead of local migration artifacts") {
		t.Fatalf("expected database-ahead error, got %v", err)
	}
}

func TestApplySQLMigrationsRejectsChecksumMismatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "migrator-checksum.sqlite"))
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	if _, err := db.ExecContext(ctx, `CREATE TABLE "rain_schema_migrations" (id TEXT PRIMARY KEY, checksum TEXT NOT NULL DEFAULT '', applied_at TIMESTAMP NOT NULL, runtime_ms INTEGER NOT NULL, tool_version TEXT NOT NULL DEFAULT '', notes TEXT NOT NULL DEFAULT '')`); err != nil {
		t.Fatalf("create migration table: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO "rain_schema_migrations" (id, checksum, applied_at, runtime_ms, tool_version, notes) VALUES ('20260330130000_init', 'db-checksum', CURRENT_TIMESTAMP, 1, '', '')`); err != nil {
		t.Fatalf("insert applied migration: %v", err)
	}

	migrations := []DiskMigration{{
		ID:       "20260330130000_init",
		Checksum: "local-checksum",
		SQL:      `CREATE TABLE "users" ("id" INTEGER PRIMARY KEY);`,
	}}
	if _, err := ApplySQLMigrations(ctx, db, "sqlite", "rain_schema_migrations", migrations); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected checksum mismatch error, got %v", err)
	}
}

func TestAcquireMigrationLockRejectsConcurrentOwner(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "migrator-lock.sqlite"))
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	first, err := acquireMigrationLock(ctx, db, "sqlite", "rain_schema_migrations")
	if err != nil {
		t.Fatalf("acquire first lock: %v", err)
	}
	defer func() { _ = first.Unlock(context.Background()) }()

	if _, err := acquireMigrationLock(ctx, db, "sqlite", "rain_schema_migrations"); err == nil || !strings.Contains(err.Error(), "another migration run is active") {
		t.Fatalf("expected concurrent lock error, got %v", err)
	}
}

func TestLoadAppliedMigrationIDsTreatsMySQLMissingTableAsEmpty(t *testing.T) {
	t.Parallel()

	if !isMissingTableError(errors.New(`Error 1146 (42S02): Table 'app.rain_schema_migrations' doesn't exist`)) {
		t.Fatalf("expected MySQL missing-table error to be treated as empty state")
	}
}

func TestQuoteMigrationIdentifierUsesDialectQuoting(t *testing.T) {
	t.Parallel()

	if got := quoteMigrationIdentifier("mysql", "rain_schema_migrations"); got != "`rain_schema_migrations`" {
		t.Fatalf("expected MySQL identifier quoting, got %q", got)
	}
	if got := quoteMigrationIdentifier("postgres", `rain"schema`); got != `"rain""schema"` {
		t.Fatalf("expected ANSI identifier quoting, got %q", got)
	}
}

func mustBuildSnapshot(t *testing.T, dialectName string, tables []schema.TableReference) Snapshot {
	t.Helper()

	snapshot, err := BuildSnapshot(dialectName, tables)
	if err != nil {
		t.Fatalf("BuildSnapshot returned error: %v", err)
	}

	return snapshot
}

type usersTable struct {
	schema.TableModel
	ID       *schema.Column[int64]
	Email    *schema.Column[string]
	Name     *schema.Column[string]
	Nickname *schema.Column[string]
}

type postsTable struct {
	schema.TableModel
	ID     *schema.Column[int64]
	UserID *schema.Column[int64]
}

func usersTableWithoutNickname() schema.TableReference {
	return schema.Define("users", func(t *usersTable) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.Email = t.Text("email").NotNull()
		t.Name = t.Text("name").NotNull()
	})
}

func usersTableWithNickname() schema.TableReference {
	return schema.Define("users", func(t *usersTable) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.Email = t.Text("email").NotNull()
		t.Name = t.Text("name").NotNull()
		t.Nickname = t.Text("nickname")
	})
}

func usersTableChangedEmailType() schema.TableReference {
	return schema.Define("users", func(t *usersTable) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.Email = t.VarChar("email", 255).NotNull()
		t.Name = t.Text("name").NotNull()
	})
}

func usersTableWithoutIndex() schema.TableReference {
	return schema.Define("users", func(t *usersTable) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.Name = t.Text("name").NotNull()
	})
}

func usersTableWithIndex() schema.TableReference {
	return schema.Define("users", func(t *usersTable) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.Name = t.Text("name").NotNull()
		t.Index("users_name_idx").On(t.Name)
	})
}

func usersTableWithoutConstraint() schema.TableReference {
	return schema.Define("users", func(t *usersTable) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.Name = t.Text("name").NotNull()
	})
}

func usersTableWithConstraint() schema.TableReference {
	return schema.Define("users", func(t *usersTable) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.Name = t.Text("name").NotNull()
		t.Unique("users_name_key").On(t.Name)
	})
}

func tablesWithoutForeignKey() (schema.TableReference, schema.TableReference) {
	users := schema.Define("users", func(t *usersTable) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.Email = t.Text("email").NotNull()
		t.Name = t.Text("name").NotNull()
	})
	posts := schema.Define("posts", func(t *postsTable) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.UserID = t.BigInt("user_id").NotNull()
	})
	return users, posts
}

func tablesWithForeignKey() (schema.TableReference, schema.TableReference) {
	type fkUsersTable struct {
		schema.TableModel
		ID    *schema.Column[int64]
		Email *schema.Column[string]
		Name  *schema.Column[string]
	}
	type fkPostsTable struct {
		schema.TableModel
		ID     *schema.Column[int64]
		UserID *schema.Column[int64]
	}

	var users *fkUsersTable
	usersTable := schema.Define("users", func(t *fkUsersTable) {
		users = t
		t.ID = t.BigSerial("id").PrimaryKey()
		t.Email = t.Text("email").NotNull()
		t.Name = t.Text("name").NotNull()
	})
	postsTable := schema.Define("posts", func(t *fkPostsTable) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.UserID = t.BigInt("user_id").NotNull()
		t.ForeignKey("posts_user_fk").On(t.UserID).References(users.ID)
	})
	return usersTable, postsTable
}
