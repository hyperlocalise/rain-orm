package raincli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

const defaultConfigPath = "rain.yml"

// Config defines the CLI settings loaded from disk and flags.
type Config struct {
	Dialect        string `yaml:"dialect"`
	SchemaPackage  string `yaml:"schema_package"`
	SchemaFunction string `yaml:"schema_function"`
	Out            string `yaml:"out"`
	MigrationTable string `yaml:"migration_table"`
	DSN            string `yaml:"dsn"`
}

// Options are one-command overrides layered on top of config file settings.
type Options struct {
	ConfigPath     string
	Dialect        string
	SchemaPackage  string
	SchemaFunction string
	Out            string
	MigrationTable string
	DSN            string
}

// LoadConfig reads and merges one command configuration.
func LoadConfig(cwd string, options Options) (Config, error) {
	configPath := options.ConfigPath
	if strings.TrimSpace(configPath) == "" {
		configPath = filepath.Join(cwd, defaultConfigPath)
	} else if !filepath.IsAbs(configPath) {
		configPath = filepath.Join(cwd, configPath)
	}

	config := Config{
		Out:            "rain/migrations",
		MigrationTable: "rain_schema_migrations",
	}
	if data, err := os.ReadFile(configPath); err == nil {
		if decodeErr := yaml.Unmarshal(data, &config); decodeErr != nil {
			return Config{}, fmt.Errorf("raincli: parse %s: %w", configPath, decodeErr)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Config{}, err
	}

	overrideString(&config.Dialect, options.Dialect)
	overrideString(&config.SchemaPackage, options.SchemaPackage)
	overrideString(&config.SchemaFunction, options.SchemaFunction)
	overrideString(&config.Out, options.Out)
	overrideString(&config.MigrationTable, options.MigrationTable)
	overrideString(&config.DSN, options.DSN)

	return config, nil
}

func validateConfigForGenerate(config Config) error {
	return errors.Join(
		validateSharedConfig(config),
		requireField("schema_package", config.SchemaPackage),
		requireField("schema_function", config.SchemaFunction),
	)
}

func validateConfigForMigrate(config Config) error {
	return errors.Join(
		validateSharedConfig(config),
		requireField("dsn", config.DSN),
	)
}

func validateConfigForCheck(config Config) error {
	return errors.Join(
		validateSharedConfig(config),
		requireField("schema_package", config.SchemaPackage),
		requireField("schema_function", config.SchemaFunction),
	)
}

func validateSharedConfig(config Config) error {
	return errors.Join(
		requireField("dialect", config.Dialect),
		requireField("out", config.Out),
		requireField("migration_table", config.MigrationTable),
	)
}

func overrideString(target *string, value string) {
	if strings.TrimSpace(value) != "" {
		*target = value
	}
}

func requireField(name, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("raincli: %s is required", name)
	}
	return nil
}
