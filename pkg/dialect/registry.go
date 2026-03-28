package dialect

import "fmt"

// GetDialect returns a dialect by name.
func GetDialect(name string) (Dialect, error) {
	switch name {
	case "postgres", "postgresql":
		return &PostgresDialect{}, nil
	case "mysql":
		return &MySQLDialect{}, nil
	case "sqlite", "sqlite3":
		return &SQLiteDialect{}, nil
	default:
		return nil, fmt.Errorf("rain: unsupported dialect %q", name)
	}
}
