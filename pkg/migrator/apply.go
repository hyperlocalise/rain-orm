package migrator

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/hyperlocalise/rain-orm/pkg/migrate"
)

// ApplySQLMigrations applies ordered SQL migrations using the existing migration table tracking.
func ApplySQLMigrations(ctx context.Context, db *sql.DB, dialectName, tableName string, migrationsOnDisk []DiskMigration) (migrate.ApplyResult, error) {
	lockCtx, lock, err := acquireMigrationLock(ctx, db, dialectName, tableName)
	if err != nil {
		return migrate.ApplyResult{}, err
	}
	defer func() { _ = lock.Unlock(context.Background()) }()

	if _, err := migrate.NewRunner(tableName).ApplyPending(lockCtx, db, nil); err != nil {
		if lockErr := lock.Err(); lockErr != nil {
			return migrate.ApplyResult{}, errors.Join(err, lockErr)
		}
		return migrate.ApplyResult{}, err
	}
	if err := validateMigrationState(lockCtx, db, dialectName, tableName, migrationsOnDisk); err != nil {
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
			ID:       diskMigration.ID,
			Checksum: diskMigration.Checksum,
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

	result, err := migrate.NewRunner(tableName).ApplyPending(lockCtx, db, migrations)
	if lockErr := lock.Err(); lockErr != nil {
		if err != nil {
			return result, errors.Join(err, lockErr)
		}
		return result, lockErr
	}
	return result, err
}

func isMissingTableError(err error) bool {
	message := err.Error()
	return strings.Contains(message, "no such table") ||
		strings.Contains(message, "does not exist") ||
		strings.Contains(message, "doesn't exist")
}

func quoteMigrationIdentifier(dialectName, name string) string {
	if dialectName == "mysql" {
		escaped := strings.ReplaceAll(name, "`", "``")
		return "`" + escaped + "`"
	}
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
