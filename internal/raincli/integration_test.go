package raincli

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

func TestSQLiteCLIIntegrationGenerateCheckMigrate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cwd := repoRoot(t)
	tempDir := t.TempDir()
	outputDir := filepath.Join(tempDir, "migrations")
	configPath := filepath.Join(tempDir, "rain.yml")
	dbPath := filepath.Join(tempDir, "app.sqlite")

	writeConfig(t, configPath, `
dialect: sqlite
schema_package: ./examples/schema/registry
schema_function: ManagedTables
out: `+outputDir+`
migration_table: rain_schema_migrations
dsn: `+dbPath+`
`)

	runHappyPathCLIIntegration(t, ctx, cwd, configPath, outputDir)

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open sqlite: %v", err)
	}
	defer func() { _ = db.Close() }()

	assertSQLiteTableExists(t, ctx, db, "users")
	assertSQLiteTableExists(t, ctx, db, "posts")
	assertSQLiteTableExists(t, ctx, db, "memberships")
	assertMigrationChecksumsRecordedSQLite(t, ctx, db, "rain_schema_migrations", 1)
}

func TestPostgresCLIIntegrationGenerateCheckMigrate(t *testing.T) {
	dsn, ok := postgresCLIIntegrationDSN()
	if !ok {
		t.Skip("set RAIN_POSTGRES_DSN or RAIN_POSTGRES_HOST/RAIN_POSTGRES_USER/RAIN_POSTGRES_DB to run postgres CLI integration tests")
	}

	ctx := context.Background()
	cwd := repoRoot(t)
	tempDir := t.TempDir()
	outputDir := filepath.Join(tempDir, "migrations")
	configPath := filepath.Join(tempDir, "rain.yml")
	migrationTable := "rain_schema_migrations_cli_it"

	writeConfig(t, configPath, `
dialect: postgres
schema_package: ./examples/schema/registry
schema_function: ManagedTables
out: `+outputDir+`
migration_table: `+migrationTable+`
dsn: `+dsn+`
`)

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	resetPostgresCLIIntegrationState(t, ctx, db, migrationTable)
	t.Cleanup(func() {
		resetPostgresCLIIntegrationState(t, context.Background(), db, migrationTable)
	})

	runHappyPathCLIIntegration(t, ctx, cwd, configPath, outputDir)

	assertPostgresTableExists(t, ctx, db, "users")
	assertPostgresTableExists(t, ctx, db, "posts")
	assertPostgresTableExists(t, ctx, db, "memberships")
	assertMigrationChecksumsRecordedPostgres(t, ctx, db, migrationTable, 1)
}

func runHappyPathCLIIntegration(t *testing.T, ctx context.Context, cwd, configPath, outputDir string) {
	t.Helper()

	var stdout strings.Builder
	if err := Run(ctx, cwd, []string{"generate", "--config", configPath, "--name", "init"}, &stdout, &stdout); err != nil {
		t.Fatalf("generate returned error: %v", err)
	}
	if !strings.Contains(stdout.String(), "migration.sql") {
		t.Fatalf("expected generate output to mention migration.sql, got %q", stdout.String())
	}

	migrations := mustListMigrationDirs(t, outputDir)
	if len(migrations) != 1 {
		t.Fatalf("expected one migration directory, got %v", migrations)
	}

	stdout.Reset()
	if err := Run(ctx, cwd, []string{"check", "--config", configPath}, &stdout, &stdout); err != nil {
		t.Fatalf("check returned error: %v", err)
	}
	if strings.TrimSpace(stdout.String()) != "OK" {
		t.Fatalf("expected OK from check, got %q", stdout.String())
	}

	stdout.Reset()
	if err := Run(ctx, cwd, []string{"migrate", "--config", configPath}, &stdout, &stdout); err != nil {
		t.Fatalf("migrate returned error: %v", err)
	}
	if !strings.Contains(stdout.String(), filepath.Base(migrations[0])) {
		t.Fatalf("expected applied migration id in output, got %q", stdout.String())
	}

	stdout.Reset()
	if err := Run(ctx, cwd, []string{"migrate", "--config", configPath}, &stdout, &stdout); err != nil {
		t.Fatalf("second migrate returned error: %v", err)
	}
	if !strings.Contains(stdout.String(), "No pending migrations.") {
		t.Fatalf("expected no pending migrations message, got %q", stdout.String())
	}
}

func assertSQLiteTableExists(t *testing.T, ctx context.Context, db *sql.DB, table string) {
	t.Helper()

	row := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table)
	var count int
	if err := row.Scan(&count); err != nil {
		t.Fatalf("scan sqlite table existence for %s: %v", table, err)
	}
	if count != 1 {
		t.Fatalf("expected sqlite table %q to exist, count=%d", table, count)
	}
}

func assertPostgresTableExists(t *testing.T, ctx context.Context, db *sql.DB, table string) {
	t.Helper()

	row := db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM information_schema.tables
WHERE table_schema = 'public' AND table_name = $1
`, table)
	var count int
	if err := row.Scan(&count); err != nil {
		t.Fatalf("scan postgres table existence for %s: %v", table, err)
	}
	if count != 1 {
		t.Fatalf("expected postgres table %q to exist, count=%d", table, count)
	}
}

func assertMigrationChecksumsRecordedSQLite(t *testing.T, ctx context.Context, db *sql.DB, table string, expectedCount int) {
	t.Helper()

	query := fmt.Sprintf(`SELECT COUNT(*), SUM(CASE WHEN checksum <> '' THEN 1 ELSE 0 END) FROM %s`, quoteSQLIdentifier(table))
	row := db.QueryRowContext(ctx, query)
	var count int
	var withChecksum int
	if err := row.Scan(&count, &withChecksum); err != nil {
		t.Fatalf("scan migration checksums for %s: %v", table, err)
	}
	if count != expectedCount {
		t.Fatalf("expected %d migration rows in %s, got %d", expectedCount, table, count)
	}
	if withChecksum != expectedCount {
		t.Fatalf("expected %d migration checksums in %s, got %d", expectedCount, table, withChecksum)
	}
}

func assertMigrationChecksumsRecordedPostgres(t *testing.T, ctx context.Context, db *sql.DB, table string, expectedCount int) {
	t.Helper()

	query := fmt.Sprintf(`SELECT COUNT(*), COUNT(*) FILTER (WHERE checksum <> '') FROM %s`, quoteSQLIdentifier(table))
	row := db.QueryRowContext(ctx, query)
	var count int
	var withChecksum int
	if err := row.Scan(&count, &withChecksum); err != nil {
		t.Fatalf("scan migration checksums for %s: %v", table, err)
	}
	if count != expectedCount {
		t.Fatalf("expected %d migration rows in %s, got %d", expectedCount, table, count)
	}
	if withChecksum != expectedCount {
		t.Fatalf("expected %d migration checksums in %s, got %d", expectedCount, table, withChecksum)
	}
}

func resetPostgresCLIIntegrationState(t *testing.T, ctx context.Context, db *sql.DB, migrationTable string) {
	t.Helper()

	for _, statement := range []string{
		fmt.Sprintf(`DROP TABLE IF EXISTS %s CASCADE`, quoteSQLIdentifier("memberships")),
		fmt.Sprintf(`DROP TABLE IF EXISTS %s CASCADE`, quoteSQLIdentifier("posts")),
		fmt.Sprintf(`DROP TABLE IF EXISTS %s CASCADE`, quoteSQLIdentifier("users")),
		fmt.Sprintf(`DROP TABLE IF EXISTS %s CASCADE`, quoteSQLIdentifier(migrationTable)),
		fmt.Sprintf(`DROP TABLE IF EXISTS %s CASCADE`, quoteSQLIdentifier("rain_schema_migration_locks")),
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("reset postgres state with %q: %v", statement, err)
		}
	}
}

func postgresCLIIntegrationDSN() (string, bool) {
	if dsn := strings.TrimSpace(os.Getenv("RAIN_POSTGRES_DSN")); dsn != "" {
		return dsn, true
	}

	host := strings.TrimSpace(os.Getenv("RAIN_POSTGRES_HOST"))
	user := strings.TrimSpace(os.Getenv("RAIN_POSTGRES_USER"))
	dbName := strings.TrimSpace(os.Getenv("RAIN_POSTGRES_DB"))
	if host == "" || user == "" || dbName == "" {
		return "", false
	}

	port := strings.TrimSpace(os.Getenv("RAIN_POSTGRES_PORT"))
	if port == "" {
		port = "5432"
	}

	sslmode := strings.TrimSpace(os.Getenv("RAIN_POSTGRES_SSLMODE"))
	if sslmode == "" {
		sslmode = "disable"
	}

	password := strings.TrimSpace(os.Getenv("RAIN_POSTGRES_PASSWORD"))
	query := url.Values{"sslmode": []string{sslmode}}
	dsn := &url.URL{
		Scheme:   "postgres",
		Host:     net.JoinHostPort(host, port),
		Path:     "/" + dbName,
		RawQuery: query.Encode(),
	}
	if password == "" {
		dsn.User = url.User(user)
		return dsn.String(), true
	}

	dsn.User = url.UserPassword(user, password)
	return dsn.String(), true
}

func quoteSQLIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
