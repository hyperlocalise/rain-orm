package migrator

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
)

type appliedMigrationState struct {
	ID       string
	Checksum string
}

func validateMigrationState(ctx context.Context, db *sql.DB, dialectName, tableName string, migrationsOnDisk []DiskMigration) error {
	applied, lastAppliedID, err := loadAppliedMigrationState(ctx, db, dialectName, tableName)
	if err != nil {
		return err
	}
	if err := validatePendingMigrationOrder(lastAppliedID, applied, migrationsOnDisk); err != nil {
		return err
	}

	localByID := make(map[string]DiskMigration, len(migrationsOnDisk))
	for _, migrationOnDisk := range migrationsOnDisk {
		localByID[migrationOnDisk.ID] = migrationOnDisk
	}

	dbAhead := make([]string, 0, len(applied))
	for id, appliedMigration := range applied {
		local, exists := localByID[id]
		if !exists {
			dbAhead = append(dbAhead, id)
			continue
		}
		if appliedMigration.Checksum != "" && local.Checksum != "" && appliedMigration.Checksum != local.Checksum {
			return fmt.Errorf(
				"migrator: applied migration %q checksum mismatch: db=%s local=%s",
				id,
				appliedMigration.Checksum,
				local.Checksum,
			)
		}
	}
	if len(dbAhead) != 0 {
		slices.Sort(dbAhead)
		return fmt.Errorf("migrator: database is ahead of local migration artifacts: %v", dbAhead)
	}

	return nil
}

func validatePendingMigrationOrder(lastAppliedID string, appliedIDs map[string]appliedMigrationState, migrationsOnDisk []DiskMigration) error {
	if lastAppliedID == "" {
		return nil
	}

	for _, migrationOnDisk := range migrationsOnDisk {
		if migrationOnDisk.ID >= lastAppliedID {
			continue
		}
		if _, applied := appliedIDs[migrationOnDisk.ID]; applied {
			continue
		}
		return fmt.Errorf("migrator: pending migration %q is older than the last applied migration %q", migrationOnDisk.ID, lastAppliedID)
	}

	return nil
}

func loadAppliedMigrationState(ctx context.Context, db *sql.DB, dialectName, tableName string) (map[string]appliedMigrationState, string, error) {
	query := fmt.Sprintf(`SELECT id, checksum FROM %s ORDER BY id DESC`, quoteMigrationIdentifier(dialectName, tableName))
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		if isMissingTableError(err) {
			return nil, "", nil
		}
		return nil, "", fmt.Errorf("migrator: load applied migrations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	applied := make(map[string]appliedMigrationState)
	lastAppliedID := ""
	for rows.Next() {
		var migration appliedMigrationState
		if scanErr := rows.Scan(&migration.ID, &migration.Checksum); scanErr != nil {
			return nil, "", fmt.Errorf("migrator: scan applied migration id: %w", scanErr)
		}
		if lastAppliedID == "" {
			lastAppliedID = migration.ID
		}
		applied[migration.ID] = migration
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, "", fmt.Errorf("migrator: read applied migrations: %w", rowsErr)
	}

	return applied, lastAppliedID, nil
}
