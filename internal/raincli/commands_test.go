package raincli

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestRunGenerateCheckAndMigrate(t *testing.T) {
	t.Parallel()

	cwd := repoRoot(t)
	tempDir := t.TempDir()
	outputDir := filepath.Join(tempDir, "migrations")
	configPath := filepath.Join(tempDir, "rain.yml")
	deployConfigPath := filepath.Join(tempDir, "rain.deploy.yml")
	dbPath := filepath.Join(tempDir, "app.sqlite")

	writeConfig(t, configPath, `
dialect: sqlite
schema_package: ./examples/schema/registry
schema_function: ManagedTables
out: `+outputDir+`
migration_table: rain_schema_migrations
dsn: `+dbPath+`
`)
	writeConfig(t, deployConfigPath, `
dialect: sqlite
out: `+outputDir+`
migration_table: rain_schema_migrations
dsn: `+dbPath+`
`)

	var stdout strings.Builder
	if err := Run(context.Background(), cwd, []string{"generate", "--config", configPath, "--name", "init"}, &stdout, &stdout); err != nil {
		t.Fatalf("generate returned error: %v", err)
	}
	if !strings.Contains(stdout.String(), "migration.sql") {
		t.Fatalf("expected generate output to mention migration.sql, got %q", stdout.String())
	}

	migrations := mustListMigrationDirs(t, outputDir)
	if len(migrations) != 1 {
		t.Fatalf("expected one migration directory, got %v", migrations)
	}
	firstDir := migrations[0]

	stdout.Reset()
	if err := Run(context.Background(), cwd, []string{"check", "--config", configPath}, &stdout, &stdout); err != nil {
		t.Fatalf("check returned error: %v", err)
	}
	if strings.TrimSpace(stdout.String()) != "OK" {
		t.Fatalf("expected OK, got %q", stdout.String())
	}

	stdout.Reset()
	if err := Run(context.Background(), cwd, []string{"migrate", "--config", deployConfigPath}, &stdout, &stdout); err != nil {
		t.Fatalf("migrate returned error: %v", err)
	}
	if !strings.Contains(stdout.String(), filepath.Base(firstDir)) {
		t.Fatalf("expected applied migration id, got %q", stdout.String())
	}

	stdout.Reset()
	if err := Run(context.Background(), cwd, []string{"migrate", "--config", deployConfigPath}, &stdout, &stdout); err != nil {
		t.Fatalf("second migrate returned error: %v", err)
	}
	if !strings.Contains(stdout.String(), "No pending migrations.") {
		t.Fatalf("expected no pending migrations message, got %q", stdout.String())
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	row := db.QueryRow(`SELECT COUNT(*) FROM rain_schema_migrations`)
	var count int
	if err := row.Scan(&count); err != nil {
		t.Fatalf("row.Scan: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 applied migration, got %d", count)
	}

	stdout.Reset()
	if err := Run(context.Background(), cwd, []string{"generate", "--config", configPath, "--name", "noop"}, &stdout, &stdout); err != nil {
		t.Fatalf("generate noop returned error: %v", err)
	}
	if !strings.Contains(stdout.String(), "No schema changes detected.") {
		t.Fatalf("expected noop message, got %q", stdout.String())
	}

	if err := os.Remove(filepath.Join(firstDir, "snapshot.json")); err != nil {
		t.Fatalf("Remove snapshot.json: %v", err)
	}
	stdout.Reset()
	if err := Run(context.Background(), cwd, []string{"check", "--config", configPath}, &stdout, &stdout); err == nil || !strings.Contains(err.Error(), "missing snapshot.json") {
		t.Fatalf("expected missing snapshot.json error, got %v", err)
	}
	stdout.Reset()
	if err := Run(context.Background(), cwd, []string{"generate", "--config", configPath, "--name", "broken"}, &stdout, &stdout); err == nil || !strings.Contains(err.Error(), "missing snapshot.json") {
		t.Fatalf("expected generate to fail on broken migration chain, got %v", err)
	}

	if err := os.RemoveAll(outputDir); err != nil {
		t.Fatalf("RemoveAll outputDir: %v", err)
	}
	stdout.Reset()
	if err := Run(context.Background(), cwd, []string{"generate", "--config", configPath, "--name", "regen"}, &stdout, &stdout); err != nil {
		t.Fatalf("regenerate returned error: %v", err)
	}

	migrations = mustListMigrationDirs(t, outputDir)
	latestDir := migrations[len(migrations)-1]
	driftedSnapshot := `{"version":1,"dialect":"sqlite","tables":[]}` + "\n"
	writeFile(t, filepath.Join(latestDir, "snapshot.json"), driftedSnapshot)
	stdout.Reset()
	if err := Run(context.Background(), cwd, []string{"check", "--config", configPath}, &stdout, &stdout); err == nil || !strings.Contains(err.Error(), "schema changes detected without a generated migration") {
		t.Fatalf("expected schema drift error, got %v", err)
	}
}

func TestRunMigrateFailsWhenMigrationArtifactsAreMissing(t *testing.T) {
	t.Parallel()

	cwd := repoRoot(t)
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "rain.yml")
	dbPath := filepath.Join(tempDir, "app.sqlite")
	outputDir := filepath.Join(tempDir, "missing-migrations")

	writeConfig(t, configPath, `
dialect: sqlite
out: `+outputDir+`
migration_table: rain_schema_migrations
dsn: `+dbPath+`
`)

	var stdout strings.Builder
	if err := Run(context.Background(), cwd, []string{"migrate", "--config", configPath}, &stdout, &stdout); err == nil || !strings.Contains(err.Error(), "migration output directory") {
		t.Fatalf("expected missing migration directory error, got %v", err)
	}
}

func writeConfig(t *testing.T, path, body string) {
	t.Helper()
	writeFile(t, path, strings.TrimSpace(body)+"\n")
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
}

func mustListMigrationDirs(t *testing.T, outputDir string) []string {
	t.Helper()

	entries, err := os.ReadDir(outputDir)
	if err != nil {
		t.Fatalf("ReadDir %s: %v", outputDir, err)
	}
	dirs := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, filepath.Join(outputDir, entry.Name()))
		}
	}
	slices.Sort(dirs)
	return dirs
}

func repoRoot(t *testing.T) string {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("filepath.Abs: %v", err)
	}
	return root
}
