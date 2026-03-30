package raincli

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/hyperlocalise/rain-orm/pkg/migrator"
)

// Run executes the rain CLI.
func Run(ctx context.Context, cwd string, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		printUsage(stdout)
		return nil
	}

	switch args[0] {
	case "generate":
		return runGenerate(ctx, cwd, args[1:], stdout, stderr)
	case "migrate":
		return runMigrate(ctx, cwd, args[1:], stdout, stderr)
	case "check":
		return runCheck(ctx, cwd, args[1:], stdout, stderr)
	case "-h", "--help", "help":
		printUsage(stdout)
		return nil
	default:
		return fmt.Errorf("raincli: unknown command %q", args[0])
	}
}

func printUsage(writer io.Writer) {
	_, _ = fmt.Fprintln(writer, "Usage: rain <generate|migrate|check> [flags]")
}

func runGenerate(ctx context.Context, cwd string, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("generate", flag.ContinueOnError)
	flags.SetOutput(stderr)

	var options Options
	var name string
	flags.StringVar(&options.ConfigPath, "config", "", "path to rain.yml")
	flags.StringVar(&options.Dialect, "dialect", "", "database dialect")
	flags.StringVar(&options.SchemaPackage, "schema-package", "", "schema registry package")
	flags.StringVar(&options.SchemaFunction, "schema-function", "", "schema registry function")
	flags.StringVar(&options.Out, "out", "", "migration output directory")
	flags.StringVar(&options.MigrationTable, "migration-table", "", "migration tracking table")
	flags.StringVar(&name, "name", "migration", "migration name")
	if err := flags.Parse(args); err != nil {
		return err
	}

	config, err := LoadConfig(cwd, options)
	if err != nil {
		return err
	}
	if err := validateConfigForGenerate(config); err != nil {
		return err
	}
	outputDir := resolveOutputDir(cwd, config.Out)

	current, err := LoadSchemaSnapshot(ctx, cwd, config)
	if err != nil {
		return err
	}
	previous, err := migrator.ReadLatestSnapshotFromMigrations(outputDir)
	if err != nil {
		return err
	}

	plan, err := migrator.DiffSnapshots(previous, current)
	if err != nil {
		return err
	}
	if plan.Empty() {
		_, _ = fmt.Fprintln(stdout, "No schema changes detected.")
		return nil
	}

	migrationOnDisk, err := migrator.WriteMigrationFiles(outputDir, name, plan, current, time.Now())
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "Generated %s\n", migrationOnDisk.DirPath)
	_, _ = fmt.Fprintf(stdout, "SQL      %s\n", migrationOnDisk.SQLPath)
	_, _ = fmt.Fprintf(stdout, "Snapshot %s\n", migrationOnDisk.SnapshotPath)

	return nil
}

func runMigrate(ctx context.Context, cwd string, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("migrate", flag.ContinueOnError)
	flags.SetOutput(stderr)

	var options Options
	flags.StringVar(&options.ConfigPath, "config", "", "path to rain.yml")
	flags.StringVar(&options.Dialect, "dialect", "", "database dialect")
	flags.StringVar(&options.Out, "out", "", "migration output directory")
	flags.StringVar(&options.MigrationTable, "migration-table", "", "migration tracking table")
	flags.StringVar(&options.DSN, "dsn", "", "database DSN")
	flags.StringVar(&options.SchemaPackage, "schema-package", "", "schema registry package")
	flags.StringVar(&options.SchemaFunction, "schema-function", "", "schema registry function")
	if err := flags.Parse(args); err != nil {
		return err
	}

	config, err := LoadConfig(cwd, options)
	if err != nil {
		return err
	}
	if err := validateConfigForMigrate(config); err != nil {
		return err
	}

	outputDir := resolveOutputDir(cwd, config.Out)
	migrationsOnDisk, err := migrator.LoadDiskMigrations(outputDir)
	if err != nil {
		return err
	}

	db, err := sql.Open(sqlDriverName(config.Dialect), config.DSN)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	result, err := migrator.ApplySQLMigrations(ctx, db, config.MigrationTable, migrationsOnDisk)
	if err != nil {
		return err
	}

	if len(result.AppliedIDs) == 0 {
		_, _ = fmt.Fprintln(stdout, "No pending migrations.")
		return nil
	}
	for _, id := range result.AppliedIDs {
		_, _ = fmt.Fprintln(stdout, id)
	}

	return nil
}

func runCheck(ctx context.Context, cwd string, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("check", flag.ContinueOnError)
	flags.SetOutput(stderr)

	var options Options
	flags.StringVar(&options.ConfigPath, "config", "", "path to rain.yml")
	flags.StringVar(&options.Dialect, "dialect", "", "database dialect")
	flags.StringVar(&options.SchemaPackage, "schema-package", "", "schema registry package")
	flags.StringVar(&options.SchemaFunction, "schema-function", "", "schema registry function")
	flags.StringVar(&options.Out, "out", "", "migration output directory")
	flags.StringVar(&options.MigrationTable, "migration-table", "", "migration tracking table")
	if err := flags.Parse(args); err != nil {
		return err
	}

	config, err := LoadConfig(cwd, options)
	if err != nil {
		return err
	}
	if err := validateConfigForCheck(config); err != nil {
		return err
	}
	outputDir := resolveOutputDir(cwd, config.Out)

	current, err := LoadSchemaSnapshot(ctx, cwd, config)
	if err != nil {
		return err
	}
	migrationsOnDisk, err := migrator.LoadDiskMigrations(outputDir)
	if err != nil {
		return err
	}

	if len(migrationsOnDisk) == 0 {
		if len(current.Tables) == 0 {
			_, _ = fmt.Fprintln(stdout, "No migrations and no managed tables.")
			return nil
		}
		return errors.New("raincli: schema has managed tables but no generated migrations")
	}
	lastMigration := migrationsOnDisk[len(migrationsOnDisk)-1]

	plan, err := migrator.DiffSnapshots(&lastMigration.Snapshot, current)
	if err != nil {
		return err
	}
	if !plan.Empty() {
		return errors.New("raincli: schema changes detected without a generated migration")
	}

	_, _ = fmt.Fprintln(stdout, "OK")
	return nil
}

func resolveOutputDir(cwd, output string) string {
	if filepath.IsAbs(output) {
		return output
	}
	return filepath.Join(cwd, output)
}

func sqlDriverName(dialectName string) string {
	switch dialectName {
	case "postgres", "postgresql":
		return "pgx"
	default:
		return dialectName
	}
}

// Main is a thin wrapper around Run for cmd/rain.
func Main() {
	if err := Run(context.Background(), mustGetwd(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func mustGetwd() string {
	cwd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	return cwd
}
