package migrator

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const (
	defaultLockLease = 30 * time.Second
	defaultLockTable = "rain_schema_migration_locks"
)

type migrationLock struct {
	cancel      context.CancelFunc
	done        chan struct{}
	db          *sql.DB
	dialectName string
	tableName   string
	lockName    string
	owner       string
}

var placeholderPattern = regexp.MustCompile(`\?`)

func acquireMigrationLock(ctx context.Context, db *sql.DB, dialectName, migrationTableName string) (*migrationLock, error) {
	lock := &migrationLock{
		db:          db,
		dialectName: dialectName,
		tableName:   defaultLockTable,
		lockName:    "default",
		owner:       fmt.Sprintf("%d", time.Now().UTC().UnixNano()),
	}
	if strings.TrimSpace(migrationTableName) != "" {
		lock.lockName = migrationTableName
	}
	if err := lock.ensureTable(ctx); err != nil {
		return nil, err
	}
	if err := lock.tryAcquire(ctx, time.Now().UTC()); err != nil {
		return nil, err
	}

	heartbeatCtx, cancel := context.WithCancel(context.Background())
	lock.cancel = cancel
	lock.done = make(chan struct{})
	go lock.heartbeat(heartbeatCtx)

	return lock, nil
}

func (l *migrationLock) Unlock(ctx context.Context) error {
	if l.cancel != nil {
		l.cancel()
		<-l.done
	}

	deleteQuery := fmt.Sprintf(
		`DELETE FROM %s WHERE lock_name = ? AND owner = ?`,
		quoteMigrationIdentifier(l.dialectName, l.tableName),
	)
	result, err := execWithPlaceholders(ctx, l.db, deleteQuery, l.lockName, l.owner)
	if err != nil {
		return fmt.Errorf("migrator: release migration lock: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err == nil && rowsAffected == 0 {
		return fmt.Errorf("migrator: migration lock was lost before release")
	}

	return nil
}

func (l *migrationLock) heartbeat(ctx context.Context) {
	ticker := time.NewTicker(defaultLockLease / 2)
	defer ticker.Stop()
	defer close(l.done)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			renewCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = l.renew(renewCtx, time.Now().UTC())
			cancel()
		}
	}
}

func (l *migrationLock) ensureTable(ctx context.Context) error {
	query := fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS %s (
  lock_name TEXT PRIMARY KEY,
  owner TEXT NOT NULL,
  expires_at TIMESTAMP NOT NULL
);`, quoteMigrationIdentifier(l.dialectName, l.tableName))

	if _, err := l.db.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("migrator: create migration lock table %q: %w", l.tableName, err)
	}

	return nil
}

func (l *migrationLock) tryAcquire(ctx context.Context, now time.Time) error {
	expiresAt := now.Add(defaultLockLease)
	insertQuery := fmt.Sprintf(
		`INSERT INTO %s (lock_name, owner, expires_at) VALUES (?, ?, ?)`,
		quoteMigrationIdentifier(l.dialectName, l.tableName),
	)
	if _, err := execWithPlaceholders(ctx, l.db, insertQuery, l.lockName, l.owner, expiresAt); err == nil {
		return nil
	}

	updateQuery := fmt.Sprintf(
		`UPDATE %s SET owner = ?, expires_at = ? WHERE lock_name = ? AND expires_at <= ?`,
		quoteMigrationIdentifier(l.dialectName, l.tableName),
	)
	result, err := execWithPlaceholders(ctx, l.db, updateQuery, l.owner, expiresAt, l.lockName, now)
	if err != nil {
		return fmt.Errorf("migrator: acquire migration lock %q: another migration run is active", l.lockName)
	}
	rowsAffected, rowsErr := result.RowsAffected()
	if rowsErr == nil && rowsAffected == 1 {
		return nil
	}

	return fmt.Errorf("migrator: acquire migration lock %q: another migration run is active", l.lockName)
}

func (l *migrationLock) renew(ctx context.Context, now time.Time) error {
	query := fmt.Sprintf(
		`UPDATE %s SET expires_at = ? WHERE lock_name = ? AND owner = ?`,
		quoteMigrationIdentifier(l.dialectName, l.tableName),
	)
	_, err := execWithPlaceholders(ctx, l.db, query, now.Add(defaultLockLease), l.lockName, l.owner)
	return err
}

func execWithPlaceholders(ctx context.Context, db *sql.DB, query string, args ...any) (sql.Result, error) {
	result, err := db.ExecContext(ctx, query, args...)
	if err == nil {
		return result, nil
	}

	if !strings.Contains(query, "?") {
		return nil, err
	}

	lowerErr := strings.ToLower(err.Error())
	if !strings.Contains(lowerErr, `near "?"`) &&
		!strings.Contains(lowerErr, `at or near "?"`) &&
		!strings.Contains(lowerErr, "syntax error") &&
		!strings.Contains(lowerErr, "expected") {
		return nil, err
	}

	counter := 0
	fallbackQuery := placeholderPattern.ReplaceAllStringFunc(query, func(_ string) string {
		counter++
		return fmt.Sprintf("$%d", counter)
	})
	return db.ExecContext(ctx, fallbackQuery, args...)
}
