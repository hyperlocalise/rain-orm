package raincli

import (
	"strconv"
	"strings"
	"testing"
)

func TestRenderSchemaLoaderSourceQuotesDialectLiteral(t *testing.T) {
	t.Parallel()

	source, err := renderSchemaLoaderSource(schemaLoaderInput{
		Dialect:        strconv.Quote(`sqlite" + func()string{return "pwn"}() + "`),
		SchemaImport:   "example.com/schema",
		SchemaFunction: "ManagedTables",
	})
	if err != nil {
		t.Fatalf("renderSchemaLoaderSource returned error: %v", err)
	}

	rendered := string(source)
	if !strings.Contains(rendered, `migrator.BuildSnapshot("sqlite\" + func()string{return \"pwn\"}() + \""`) {
		t.Fatalf("expected dialect to be embedded as a quoted Go string literal, got %q", rendered)
	}
	if strings.Contains(rendered, `migrator.BuildSnapshot("sqlite" +`) {
		t.Fatalf("expected injected Go expression to remain escaped, got %q", rendered)
	}
}
