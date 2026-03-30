package raincli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"

	"github.com/hyperlocalise/rain-orm/pkg/migrator"
)

type schemaLoaderInput struct {
	Dialect        string
	SchemaImport   string
	SchemaFunction string
}

const schemaLoaderSource = `package main

import (
	"fmt"
	"os"

	registrypkg "{{.SchemaImport}}"

	"github.com/hyperlocalise/rain-orm/pkg/migrator"
)

func main() {
	snapshot, err := migrator.BuildSnapshot({{.Dialect}}, registrypkg.{{.SchemaFunction}}())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	data, err := migrator.MarshalSnapshot(snapshot)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if _, err := os.Stdout.Write(data); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}`

// LoadSchemaSnapshot shells out to a generated helper so user schema packages can stay regular Go code.
func LoadSchemaSnapshot(ctx context.Context, cwd string, config Config) (migrator.Snapshot, error) {
	schemaImport, err := resolveImportPath(ctx, cwd, config.SchemaPackage)
	if err != nil {
		return migrator.Snapshot{}, err
	}

	tempDir, err := os.MkdirTemp("", "rain-schema-loader-*")
	if err != nil {
		return migrator.Snapshot{}, err
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	loaderPath := filepath.Join(tempDir, "main.go")
	source, renderErr := renderSchemaLoaderSource(schemaLoaderInput{
		Dialect:        strconv.Quote(config.Dialect),
		SchemaImport:   schemaImport,
		SchemaFunction: config.SchemaFunction,
	})
	if renderErr != nil {
		return migrator.Snapshot{}, renderErr
	}
	if writeErr := os.WriteFile(loaderPath, source, 0o644); writeErr != nil {
		return migrator.Snapshot{}, writeErr
	}

	command := exec.CommandContext(ctx, "go", "run", loaderPath)
	command.Dir = cwd
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if runErr := command.Run(); runErr != nil {
		return migrator.Snapshot{}, fmt.Errorf("raincli: load schema registry: %w: %s", runErr, strings.TrimSpace(stderr.String()))
	}

	snapshot, snapshotErr := migrator.UnmarshalSnapshot(stdout.Bytes())
	if snapshotErr != nil {
		return migrator.Snapshot{}, snapshotErr
	}

	return snapshot, nil
}

func renderSchemaLoaderSource(input schemaLoaderInput) ([]byte, error) {
	var source bytes.Buffer
	templateValue, parseErr := template.New("schema-loader").Parse(schemaLoaderSource)
	if parseErr != nil {
		return nil, parseErr
	}
	if executeErr := templateValue.Execute(&source, input); executeErr != nil {
		return nil, executeErr
	}
	return source.Bytes(), nil
}

func resolveImportPath(ctx context.Context, cwd, packageRef string) (string, error) {
	command := exec.CommandContext(ctx, "go", "list", "-f", "{{.ImportPath}}", packageRef)
	command.Dir = cwd
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("raincli: resolve schema package %q: %w: %s", packageRef, err, strings.TrimSpace(string(output)))
	}

	return strings.TrimSpace(string(output)), nil
}
