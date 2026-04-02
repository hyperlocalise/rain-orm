package migrator

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const defaultLockTable = "rain_schema_migration_locks"

var (
	defaultLockLease      = 30 * time.Second
	migrationLockOwnerSeq uint64
)

type migrationLock struct {
	cancel      context.CancelFunc
	done        chan struct{}
	conn        *sql.Conn
	dialectName string
	tableName   string
	lockName    string
	owner       string
	sessionLock bool
	mu          sync.Mutex
	err         error
}

var placeholderPattern = regexp.MustCompile(`\?`)

func acquireMigrationLock(ctx context.Context, db *sql.DB, dialectName, migrationTableName string) (context.Context, *migrationLock, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("migrator: acquire migration lock connection: %w", err)
	}

	lock := &migrationLock{
		conn:        conn,
		dialectName: dialectName,
		tableName:   defaultLockTable,
		lockName:    "default",
		owner:       newMigrationLockOwner(),
	}
	if strings.TrimSpace(migrationTableName) != "" {
		lock.lockName = migrationTableName
	}
	if normalizeMigratorDialectName(dialectName) == "mysql" {
		if err := lock.tryAcquireMySQLSessionLock(ctx); err != nil {
			_ = conn.Close()
			return nil, nil, err
		}
		return ctx, lock, nil
	}
	if err := lock.ensureTable(ctx); err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	if err := lock.tryAcquire(ctx, time.Now().UTC()); err != nil {
		_ = conn.Close()
		return nil, nil, err
	}

	heartbeatCtx, cancel := context.WithCancel(ctx)
	lock.cancel = cancel
	lock.done = make(chan struct{})
	go lock.heartbeat(heartbeatCtx)

	return heartbeatCtx, lock, nil
}

func newMigrationLockOwner() string {
	return fmt.Sprintf("%d-%d-%d", os.Getpid(), time.Now().UTC().UnixNano(), atomic.AddUint64(&migrationLockOwnerSeq, 1))
}

func (l *migrationLock) Unlock(ctx context.Context) error {
	defer func() {
		if l.conn != nil {
			_ = l.conn.Close()
		}
	}()

	if l.sessionLock {
		return l.releaseMySQLSessionLock(ctx)
	}

	if l.cancel != nil {
		l.cancel()
		<-l.done
	}

	deleteQuery := fmt.Sprintf(
		`DELETE FROM %s WHERE lock_name = ? AND owner = ?`,
		quoteMigrationIdentifier(l.dialectName, l.tableName),
	)
	result, err := execWithPlaceholders(ctx, l.conn, l.dialectName, deleteQuery, l.lockName, l.owner)
	if err != nil {
		return fmt.Errorf("migrator: release migration lock: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err == nil && rowsAffected == 0 {
		return fmt.Errorf("migrator: migration lock was lost before release")
	}

	return nil
}

func (l *migrationLock) Err() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.err
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
			if err := l.renew(renewCtx, time.Now().UTC()); err != nil {
				cancel()
				l.fail(fmt.Errorf("migrator: renew migration lock %q: %w", l.lockName, err))
				return
			}
			cancel()
		}
	}
}

func (l *migrationLock) ensureTable(ctx context.Context) error {
	query := fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS %s (
  lock_name %s,
  owner TEXT NOT NULL,
  expires_at TIMESTAMP NOT NULL
);`, quoteMigrationIdentifier(l.dialectName, l.tableName), lockNameColumnDDL(l.dialectName))

	if _, err := l.conn.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("migrator: create migration lock table %q: %w", l.tableName, err)
	}

	return nil
}

func lockNameColumnDDL(dialectName string) string {
	if dialectName == "mysql" {
		return "VARCHAR(191) PRIMARY KEY"
	}

	return "TEXT PRIMARY KEY"
}

func (l *migrationLock) tryAcquire(ctx context.Context, now time.Time) error {
	expiresAt := now.Add(defaultLockLease)
	insertQuery := fmt.Sprintf(
		`INSERT INTO %s (lock_name, owner, expires_at) VALUES (?, ?, ?)`,
		quoteMigrationIdentifier(l.dialectName, l.tableName),
	)
	_, insertErr := execWithPlaceholders(ctx, l.conn, l.dialectName, insertQuery, l.lockName, l.owner, expiresAt)
	if insertErr == nil {
		return nil
	}
	if !isDuplicateKeyError(insertErr) {
		return fmt.Errorf("migrator: acquire migration lock %q: %w", l.lockName, insertErr)
	}

	updateQuery := fmt.Sprintf(
		`UPDATE %s SET owner = ?, expires_at = ? WHERE lock_name = ? AND expires_at <= ?`,
		quoteMigrationIdentifier(l.dialectName, l.tableName),
	)
	result, err := execWithPlaceholders(ctx, l.conn, l.dialectName, updateQuery, l.owner, expiresAt, l.lockName, now)
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
	result, err := execWithPlaceholders(ctx, l.conn, l.dialectName, query, now.Add(defaultLockLease), l.lockName, l.owner)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err == nil && rowsAffected != 1 {
		return fmt.Errorf("migration lock row is missing")
	}
	return nil
}

func (l *migrationLock) fail(err error) {
	l.mu.Lock()
	if l.err == nil {
		l.err = err
	}
	l.mu.Unlock()
	if l.cancel != nil {
		l.cancel()
	}
}

func (l *migrationLock) tryAcquireMySQLSessionLock(ctx context.Context) error {
	row := l.conn.QueryRowContext(ctx, `SELECT GET_LOCK(?, 0)`, l.lockName)
	var acquired sql.NullInt64
	if err := row.Scan(&acquired); err != nil {
		return fmt.Errorf("migrator: acquire mysql migration lock %q: %w", l.lockName, err)
	}
	if !acquired.Valid || acquired.Int64 != 1 {
		return fmt.Errorf("migrator: acquire migration lock %q: another migration run is active", l.lockName)
	}
	l.sessionLock = true
	return nil
}

func (l *migrationLock) releaseMySQLSessionLock(ctx context.Context) error {
	row := l.conn.QueryRowContext(ctx, `SELECT RELEASE_LOCK(?)`, l.lockName)
	var released sql.NullInt64
	if err := row.Scan(&released); err != nil {
		return fmt.Errorf("migrator: release migration lock: %w", err)
	}
	if !released.Valid || released.Int64 != 1 {
		return fmt.Errorf("migrator: migration lock was lost before release")
	}
	return nil
}

type execContext interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func execWithPlaceholders(ctx context.Context, exec execContext, dialectName, query string, args ...any) (sql.Result, error) {
	preparedQuery := replacePlaceholdersForDialect(normalizeMigratorDialectName(dialectName), query)
	result, err := exec.ExecContext(ctx, preparedQuery, args...)
	if err == nil {
		return result, nil
	}

	if preparedQuery != query || !strings.Contains(query, "?") {
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
	return exec.ExecContext(ctx, fallbackQuery, args...)
}

func isDuplicateKeyError(err error) bool {
	lowerErr := strings.ToLower(err.Error())
	return strings.Contains(lowerErr, "duplicate key value violates unique constraint") ||
		strings.Contains(lowerErr, "unique constraint failed") ||
		strings.Contains(lowerErr, "duplicate entry")
}
