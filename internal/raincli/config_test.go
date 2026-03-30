package raincli

import (
	"strings"
	"testing"
)

func TestValidateConfigForGenerateRejectsInvalidSchemaFunction(t *testing.T) {
	t.Parallel()

	config := Config{
		Dialect:        "sqlite",
		SchemaPackage:  "./examples/schema/registry",
		SchemaFunction: "ManagedTables(); panic(1)",
		Out:            "rain/migrations",
		MigrationTable: "rain_schema_migrations",
	}

	err := validateConfigForGenerate(config)
	if err == nil || !strings.Contains(err.Error(), "schema_function must be a valid Go identifier") {
		t.Fatalf("expected invalid schema_function error, got %v", err)
	}
}

func TestValidateConfigForCheckAcceptsValidSchemaFunction(t *testing.T) {
	t.Parallel()

	config := Config{
		Dialect:        "sqlite",
		SchemaPackage:  "./examples/schema/registry",
		SchemaFunction: "ManagedTables",
		Out:            "rain/migrations",
		MigrationTable: "rain_schema_migrations",
	}

	if err := validateConfigForCheck(config); err != nil {
		t.Fatalf("expected valid schema_function, got %v", err)
	}
}
