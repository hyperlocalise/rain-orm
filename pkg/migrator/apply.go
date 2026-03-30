package migrator

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/hyperlocalise/rain-orm/pkg/migrate"
)

// ApplySQLMigrations applies ordered SQL migrations using the existing migration table tracking.
func ApplySQLMigrations(ctx context.Context, db *sql.DB, tableName string, migrationsOnDisk []DiskMigration) (migrate.ApplyResult, error) {
	if err := validatePendingMigrationOrder(ctx, db, tableName, migrationsOnDisk); err != nil {
		return migrate.ApplyResult{}, err
	}

	migrations := make([]migrate.Migration, 0, len(migrationsOnDisk))
	for _, diskMigration := range migrationsOnDisk {
		statements, err := SplitSQLStatements(diskMigration.SQL)
		if err != nil {
			return migrate.ApplyResult{}, fmt.Errorf("migrator: split %q: %w", diskMigration.ID, err)
		}

		currentStatements := append([]string(nil), statements...)
		migrations = append(migrations, migrate.Migration{
			ID: diskMigration.ID,
			Up: func(ctx context.Context, exec migrate.Executor) error {
				for _, statement := range currentStatements {
					if strings.TrimSpace(statement) == "" {
						continue
					}
					if _, execErr := exec.ExecContext(ctx, statement); execErr != nil {
						return execErr
					}
				}
				return nil
			},
		})
	}

	return migrate.NewRunner(tableName).ApplyPending(ctx, db, migrations)
}

func validatePendingMigrationOrder(ctx context.Context, db *sql.DB, tableName string, migrationsOnDisk []DiskMigration) error {
	appliedIDs, lastAppliedID, err := loadAppliedMigrationIDs(ctx, db, tableName)
	if err != nil {
		return err
	}
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

func loadAppliedMigrationIDs(ctx context.Context, db *sql.DB, tableName string) (map[string]struct{}, string, error) {
	query := fmt.Sprintf(`SELECT id FROM %s ORDER BY id DESC`, quoteMigrationIdentifier(tableName))
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		if isMissingTableError(err) {
			return nil, "", nil
		}
		return nil, "", fmt.Errorf("migrator: load applied migrations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	applied := make(map[string]struct{})
	lastAppliedID := ""
	for rows.Next() {
		var id string
		if scanErr := rows.Scan(&id); scanErr != nil {
			return nil, "", fmt.Errorf("migrator: scan applied migration id: %w", scanErr)
		}
		if lastAppliedID == "" {
			lastAppliedID = id
		}
		applied[id] = struct{}{}
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, "", fmt.Errorf("migrator: read applied migrations: %w", rowsErr)
	}

	return applied, lastAppliedID, nil
}

func isMissingTableError(err error) bool {
	message := err.Error()
	return strings.Contains(message, "no such table") ||
		strings.Contains(message, "does not exist") ||
		strings.Contains(message, "doesn't exist")
}

func quoteMigrationIdentifier(name string) string {
	escaped := strings.ReplaceAll(name, `"`, `""`)
	return `"` + escaped + `"`
}

// SplitSQLStatements splits one migration file into executable statements.
func SplitSQLStatements(sqlText string) ([]string, error) {
	var (
		statements        []string
		builder           strings.Builder
		inSingleQuote     bool
		inDoubleQuote     bool
		inLineComment     bool
		inBlockComment    bool
		dollarQuoteMarker string
	)

	for idx := 0; idx < len(sqlText); idx++ {
		current := sqlText[idx]
		next := byte(0)
		if idx+1 < len(sqlText) {
			next = sqlText[idx+1]
		}

		switch {
		case dollarQuoteMarker != "":
			builder.WriteByte(current)
			if strings.HasPrefix(sqlText[idx:], dollarQuoteMarker) {
				builder.WriteString(dollarQuoteMarker[1:])
				idx += len(dollarQuoteMarker) - 1
				dollarQuoteMarker = ""
			}
			continue
		case inLineComment:
			builder.WriteByte(current)
			if current == '\n' {
				inLineComment = false
			}
			continue
		case inBlockComment:
			builder.WriteByte(current)
			if current == '*' && next == '/' {
				builder.WriteByte(next)
				idx++
				inBlockComment = false
			}
			continue
		case !inSingleQuote && !inDoubleQuote && current == '-' && next == '-':
			builder.WriteByte(current)
			builder.WriteByte(next)
			idx++
			inLineComment = true
			continue
		case !inSingleQuote && !inDoubleQuote && current == '/' && next == '*':
			builder.WriteByte(current)
			builder.WriteByte(next)
			idx++
			inBlockComment = true
			continue
		case current == '\'' && !inDoubleQuote:
			builder.WriteByte(current)
			if inSingleQuote && next == '\'' {
				builder.WriteByte(next)
				idx++
				continue
			}
			inSingleQuote = !inSingleQuote
			continue
		case current == '"' && !inSingleQuote:
			builder.WriteByte(current)
			inDoubleQuote = !inDoubleQuote
			continue
		case current == '$' && !inSingleQuote && !inDoubleQuote:
			marker, ok := parseDollarQuoteMarker(sqlText[idx:])
			if ok {
				builder.WriteString(marker)
				idx += len(marker) - 1
				dollarQuoteMarker = marker
				continue
			}
			builder.WriteByte(current)
			continue
		case current == ';' && !inSingleQuote && !inDoubleQuote:
			statement := strings.TrimSpace(builder.String())
			if statement != "" {
				statements = append(statements, statement)
			}
			builder.Reset()
			continue
		default:
			builder.WriteByte(current)
		}
	}

	if inSingleQuote || inDoubleQuote || inBlockComment || dollarQuoteMarker != "" {
		return nil, fmt.Errorf("unterminated SQL literal or comment")
	}

	last := strings.TrimSpace(builder.String())
	if last != "" {
		statements = append(statements, last)
	}

	return statements, nil
}

func parseDollarQuoteMarker(input string) (string, bool) {
	if len(input) < 2 || input[0] != '$' {
		return "", false
	}
	for idx := 1; idx < len(input); idx++ {
		switch char := input[idx]; {
		case char == '$':
			return input[:idx+1], true
		case char == '_' || char >= '0' && char <= '9' || char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z':
			continue
		default:
			return "", false
		}
	}
	return "", false
}
