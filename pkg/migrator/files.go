package migrator

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	migrationSQLFile  = "migration.sql"
	migrationSnapFile = "snapshot.json"
)

// DiskMigration represents one migration folder on disk.
type DiskMigration struct {
	ID           string
	Name         string
	Checksum     string
	DirPath      string
	SQLPath      string
	SnapshotPath string
	SQL          string
	Snapshot     Snapshot
}

// ReadLatestSnapshotFromMigrations returns the newest migration snapshot or nil when no migrations exist.
func ReadLatestSnapshotFromMigrations(dir string) (*Snapshot, error) {
	migrations, err := LoadDiskMigrations(dir)
	if err != nil {
		return nil, err
	}
	if len(migrations) == 0 {
		return nil, nil
	}

	latest := migrations[len(migrations)-1].Snapshot
	return &latest, nil
}

// WriteMigrationFiles writes one Drizzle-style migration folder containing migration.sql and snapshot.json.
func WriteMigrationFiles(dir, name string, plan Plan, snapshot Snapshot, now time.Time) (DiskMigration, error) {
	if plan.Empty() {
		return DiskMigration{}, errors.New("migrator: cannot write an empty migration")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return DiskMigration{}, err
	}

	slug := slugify(name)
	if slug == "" {
		slug = "migration"
	}

	baseID := now.UTC().Format("20060102150405") + "_" + slug
	id, dirPath, err := uniqueMigrationDir(dir, baseID)
	if err != nil {
		return DiskMigration{}, err
	}
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		return DiskMigration{}, err
	}

	sqlPath := filepath.Join(dirPath, migrationSQLFile)
	snapshotPath := filepath.Join(dirPath, migrationSnapFile)

	sqlBody := renderMigrationSQL(plan.Statements)
	if writeErr := os.WriteFile(sqlPath, []byte(sqlBody), 0o644); writeErr != nil {
		return DiskMigration{}, writeErr
	}

	snapshotData, snapshotErr := MarshalSnapshot(snapshot)
	if snapshotErr != nil {
		return DiskMigration{}, snapshotErr
	}
	if writeErr := os.WriteFile(snapshotPath, snapshotData, 0o644); writeErr != nil {
		return DiskMigration{}, writeErr
	}

	return DiskMigration{
		ID:           id,
		Name:         migrationName(id),
		Checksum:     checksumSQL(sqlBody),
		DirPath:      dirPath,
		SQLPath:      sqlPath,
		SnapshotPath: snapshotPath,
		SQL:          sqlBody,
		Snapshot:     snapshot,
	}, nil
}

// LoadDiskMigrations reads ordered migration folders and validates required files.
func LoadDiskMigrations(dir string) ([]DiskMigration, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	migrationByID := make(map[string]DiskMigration)
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		id := entry.Name()
		if _, exists := migrationByID[id]; exists {
			return nil, fmt.Errorf("migrator: duplicate migration %q", id)
		}

		dirPath := filepath.Join(dir, id)
		sqlPath := filepath.Join(dirPath, migrationSQLFile)
		sqlData, readErr := os.ReadFile(sqlPath)
		if readErr != nil {
			if errors.Is(readErr, fs.ErrNotExist) {
				return nil, fmt.Errorf("migrator: missing %s for migration %q", migrationSQLFile, id)
			}
			return nil, readErr
		}
		snapshotPath := filepath.Join(dirPath, migrationSnapFile)
		snapshotData, readSnapshotErr := os.ReadFile(snapshotPath)
		if readSnapshotErr != nil {
			if errors.Is(readSnapshotErr, fs.ErrNotExist) {
				return nil, fmt.Errorf("migrator: missing %s for migration %q", migrationSnapFile, id)
			}
			return nil, readSnapshotErr
		}
		snapshot, snapshotErr := UnmarshalSnapshot(snapshotData)
		if snapshotErr != nil {
			return nil, fmt.Errorf("migrator: read snapshot for %q: %w", id, snapshotErr)
		}

		migrationByID[id] = DiskMigration{
			ID:           id,
			Name:         migrationName(id),
			Checksum:     checksumSQL(string(sqlData)),
			DirPath:      dirPath,
			SQLPath:      sqlPath,
			SnapshotPath: snapshotPath,
			SQL:          string(sqlData),
			Snapshot:     snapshot,
		}
		ids = append(ids, id)
	}

	slices.Sort(ids)
	migrations := make([]DiskMigration, 0, len(ids))
	for idx, id := range ids {
		if idx > 0 && ids[idx-1] == id {
			return nil, fmt.Errorf("migrator: duplicate migration %q", id)
		}
		migrations = append(migrations, migrationByID[id])
	}

	return migrations, nil
}

func uniqueMigrationDir(rootDir, baseID string) (string, string, error) {
	for attempt := 0; attempt < 1000; attempt++ {
		id := baseID
		if attempt > 0 {
			id = fmt.Sprintf("%s_%02d", baseID, attempt)
		}
		dirPath := filepath.Join(rootDir, id)
		_, err := os.Stat(dirPath)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return id, dirPath, nil
		case err != nil:
			return "", "", err
		}
	}

	return "", "", fmt.Errorf("migrator: could not allocate a unique migration id for %q", baseID)
}

func migrationName(id string) string {
	parts := strings.Split(id, "_")
	if len(parts) < 2 {
		return id
	}
	return strings.Join(parts[1:], "_")
}

func renderMigrationSQL(statements []string) string {
	var builder strings.Builder
	for idx, statement := range statements {
		builder.WriteString(strings.TrimSpace(statement))
		builder.WriteString(";\n")
		if idx < len(statements)-1 {
			builder.WriteByte('\n')
		}
	}
	return builder.String()
}

func slugify(value string) string {
	var builder strings.Builder
	lastDash := false
	for _, char := range strings.ToLower(strings.TrimSpace(value)) {
		switch {
		case char >= 'a' && char <= 'z':
			builder.WriteRune(char)
			lastDash = false
		case char >= '0' && char <= '9':
			builder.WriteRune(char)
			lastDash = false
		case !lastDash:
			builder.WriteByte('_')
			lastDash = true
		}
	}

	return strings.Trim(builder.String(), "_")
}

func checksumSQL(sqlText string) string {
	sum := sha256.Sum256([]byte(sqlText))
	return hex.EncodeToString(sum[:])
}
