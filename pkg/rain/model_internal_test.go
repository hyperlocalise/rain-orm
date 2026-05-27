package rain

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/hyperlocalise/rain-orm/pkg/schema"
	_ "modernc.org/sqlite"
)

type modelScanStatus string

func (s *modelScanStatus) Scan(src any) error {
	switch value := src.(type) {
	case string:
		*s = modelScanStatus(strings.ToUpper(value))
		return nil
	case []byte:
		*s = modelScanStatus(strings.ToUpper(string(value)))
		return nil
	default:
		return fmt.Errorf("unsupported status source %T", src)
	}
}

type modelScanCurrency int64

func (c *modelScanCurrency) Scan(src any) error {
	value, ok := src.(int64)
	if !ok {
		return fmt.Errorf("unsupported currency source %T", src)
	}
	*c = modelScanCurrency(value * 100)
	return nil
}

type ModelScanEmbedded struct {
	ID int64 `db:"id"`
}

type ModelScanProfile struct {
	Name *string `db:"name"`
}

type modelScanRow struct {
	ModelScanEmbedded
	*ModelScanProfile
	Age      *int32           `db:"age"`
	Score    *float32         `db:"score"`
	Visits   *uint16          `db:"visits"`
	Status   *modelScanStatus `db:"status"`
	Disabled *bool            `db:"disabled"`
}

type modelUnsupportedRow struct {
	Payload *struct{} `db:"payload"`
}

func openModelInternalDB(t *testing.T) *sql.DB {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "model-internal.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	return db
}

func TestRowScanPlanCacheUsesStableTableIdentityForAliases(t *testing.T) {
	rowScanPlanCache.Clear()

	users, _ := defineInternalQueryTables()
	cols := []string{"id", "email", "name", "nickname"}
	columnKey := strings.Join(cols, "\x00")
	modelType := reflect.TypeFor[internalUserRow]()

	firstAlias := schema.Alias(users, "u")
	firstPlan, err := newRowScanPlanForColumns(cols, modelType, firstAlias.TableDef())
	if err != nil {
		t.Fatalf("first row scan plan: %v", err)
	}

	for range 5 {
		aliased := schema.Alias(users, "u")
		nextPlan, err := newRowScanPlanForColumns(cols, modelType, aliased.TableDef())
		if err != nil {
			t.Fatalf("aliased row scan plan: %v", err)
		}
		if nextPlan != firstPlan {
			t.Fatalf("expected dynamic aliases with the same table name and alias to reuse the cached plan")
		}
	}

	var matchingEntries int
	rowScanPlanCache.Range(func(key, _ any) bool {
		planKey, ok := key.(rowScanPlanKey)
		if ok && planKey.hasTable && planKey.tableName == "users" && planKey.tableAlias == "u" && planKey.columns == columnKey {
			matchingEntries++
		}
		return true
	})
	if matchingEntries != 1 {
		t.Fatalf("expected one row scan plan cache entry for dynamic aliases, got %d", matchingEntries)
	}
}

func TestScanRowsSupportsExpandedNullableTypesAndEmbeddedStructs(t *testing.T) {
	t.Parallel()

	db := openModelInternalDB(t)
	if _, err := db.Exec(`
		CREATE TABLE scan_rows (
			id INTEGER NOT NULL,
			name TEXT,
			age INTEGER,
			score REAL,
			visits INTEGER,
			status TEXT,
			disabled INTEGER
		)
	`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	if _, err := db.Exec(`
		INSERT INTO scan_rows(id, name, age, score, visits, status, disabled)
		VALUES (1, 'alice', 33, 9.5, 12, 'active', NULL)
	`); err != nil {
		t.Fatalf("insert table row: %v", err)
	}

	rows, err := db.Query(`
		SELECT id, name, age, score, visits, status, disabled
		FROM scan_rows
	`)
	if err != nil {
		t.Fatalf("query rows: %v", err)
	}
	t.Cleanup(func() {
		_ = rows.Close()
	})

	var scanned modelScanRow
	if err := scanRows(rows, &scanned); err != nil {
		t.Fatalf("scan rows: %v", err)
	}

	if scanned.ID != 1 {
		t.Fatalf("expected embedded id 1, got %d", scanned.ID)
	}
	if scanned.ModelScanProfile == nil || scanned.Name == nil || *scanned.Name != "alice" {
		t.Fatalf("expected embedded profile name alice, got %#v", scanned.ModelScanProfile)
	}
	if scanned.Age == nil || *scanned.Age != 33 {
		t.Fatalf("expected age 33, got %#v", scanned.Age)
	}
	if scanned.Score == nil || *scanned.Score != float32(9.5) {
		t.Fatalf("expected score 9.5, got %#v", scanned.Score)
	}
	if scanned.Visits == nil || *scanned.Visits != uint16(12) {
		t.Fatalf("expected visits 12, got %#v", scanned.Visits)
	}
	if scanned.Status == nil || *scanned.Status != modelScanStatus("ACTIVE") {
		t.Fatalf("expected custom scanner status ACTIVE, got %#v", scanned.Status)
	}
	if scanned.Disabled != nil {
		t.Fatalf("expected null bool pointer, got %#v", scanned.Disabled)
	}
}

func TestScanRowsUsesScannerForNamedPrimitiveType(t *testing.T) {
	t.Parallel()

	db := openModelInternalDB(t)
	if _, err := db.Exec(`CREATE TABLE primitive_scanner (amount INTEGER NOT NULL)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO primitive_scanner(amount) VALUES (7)`); err != nil {
		t.Fatalf("insert row: %v", err)
	}

	rows, err := db.Query(`SELECT amount FROM primitive_scanner`)
	if err != nil {
		t.Fatalf("query rows: %v", err)
	}
	t.Cleanup(func() {
		_ = rows.Close()
	})

	type row struct {
		Amount modelScanCurrency `db:"amount"`
	}

	var scanned row
	if err := scanRows(rows, &scanned); err != nil {
		t.Fatalf("scan rows: %v", err)
	}
	if scanned.Amount != 700 {
		t.Fatalf("expected custom scanner amount 700, got %d", scanned.Amount)
	}
}

func TestBoundDirectFallbackReadsCurrentScannedValue(t *testing.T) {
	t.Parallel()

	type row struct {
		Name string `db:"name"`
	}

	colPlan := scanColumnPlan{
		scanIndex:    0,
		scratchIndex: 0,
		fieldIndex:   []int{0},
		index0:       0,
		isDirect:     true,
		fieldType:    reflect.TypeFor[string](),
	}
	plan := &rowScanPlan{
		columns:         []scanColumnPlan{colPlan},
		stringValueCols: []scanColumnPlan{colPlan},
	}

	scratch := &rowScanScratch{
		scanTargets: []any{nil},
		scanned:     []any{nil},
		strings:     []sql.NullString{{String: "stale", Valid: true}},
	}
	scratch.scanned[0] = &scratch.strings[0]
	scratch.scanTargets[0] = &scratch.strings[0]
	scratch.strings[0].String = "fresh"

	var got row
	v := reflect.ValueOf(&got).Elem()
	if err := scanDirectRowAddr(v.Addr().UnsafePointer(), v, plan, scratch); err != nil {
		t.Fatalf("scan direct fallback: %v", err)
	}
	if got.Name != "fresh" {
		t.Fatalf("expected fallback to read current scanned value, got %q", got.Name)
	}
}

func TestScanRowsDiscardsUnknownColumnsAndLeavesMissingFields(t *testing.T) {
	t.Parallel()

	db := openModelInternalDB(t)
	if _, err := db.Exec(`
		CREATE TABLE unknown_columns (
			id INTEGER NOT NULL,
			ghost TEXT
		)
	`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO unknown_columns(id, ghost) VALUES (7, 'ignored')`); err != nil {
		t.Fatalf("insert row: %v", err)
	}

	type row struct {
		ID   int64   `db:"id"`
		Name *string `db:"name"`
	}

	rows, err := db.Query(`SELECT id, ghost FROM unknown_columns`)
	if err != nil {
		t.Fatalf("query rows: %v", err)
	}
	t.Cleanup(func() {
		_ = rows.Close()
	})

	var scanned row
	if err := scanRows(rows, &scanned); err != nil {
		t.Fatalf("scan rows: %v", err)
	}

	if scanned.ID != 7 {
		t.Fatalf("expected id 7, got %d", scanned.ID)
	}
	if scanned.Name != nil {
		t.Fatalf("expected missing field to remain nil, got %#v", scanned.Name)
	}
}

func TestScanRowsSupportsPointerSliceDestinations(t *testing.T) {
	t.Parallel()

	db := openModelInternalDB(t)
	if _, err := db.Exec(`
		CREATE TABLE pointer_rows (
			id INTEGER NOT NULL,
			name TEXT
		)
	`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO pointer_rows(id, name) VALUES (1, 'alice'), (2, 'bob')`); err != nil {
		t.Fatalf("insert rows: %v", err)
	}

	type row struct {
		ID   int64  `db:"id"`
		Name string `db:"name"`
	}

	rows, err := db.Query(`SELECT id, name FROM pointer_rows ORDER BY id`)
	if err != nil {
		t.Fatalf("query rows: %v", err)
	}
	t.Cleanup(func() {
		_ = rows.Close()
	})

	var scanned []*row
	if err := scanRows(rows, &scanned); err != nil {
		t.Fatalf("scan rows: %v", err)
	}
	if len(scanned) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(scanned))
	}
	if scanned[0] == nil || scanned[0].ID != 1 || scanned[0].Name != "alice" {
		t.Fatalf("unexpected first row: %#v", scanned[0])
	}
	if scanned[1] == nil || scanned[1].ID != 2 || scanned[1].Name != "bob" {
		t.Fatalf("unexpected second row: %#v", scanned[1])
	}
}

func TestScanRowsUnsupportedNullableTypeReturnsClearError(t *testing.T) {
	t.Parallel()

	db := openModelInternalDB(t)
	if _, err := db.Exec(`CREATE TABLE unsupported_type (payload TEXT)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO unsupported_type(payload) VALUES ('bad')`); err != nil {
		t.Fatalf("insert row: %v", err)
	}

	rows, err := db.Query(`SELECT payload FROM unsupported_type`)
	if err != nil {
		t.Fatalf("query rows: %v", err)
	}
	t.Cleanup(func() {
		_ = rows.Close()
	})

	var scanned modelUnsupportedRow
	err = scanRows(rows, &scanned)
	if err == nil {
		t.Fatalf("expected unsupported nullable type error")
	}
	if !strings.Contains(err.Error(), "unsupported nullable field type") {
		t.Fatalf("expected clear unsupported type message, got %v", err)
	}
}
