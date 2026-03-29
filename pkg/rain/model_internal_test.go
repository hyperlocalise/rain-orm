package rain

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

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

func (s modelScanStatus) Value() (driver.Value, error) {
	return string(s), nil
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
