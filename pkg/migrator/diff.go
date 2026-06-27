package migrator

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
)

// Plan summarizes one additive migration diff.
type Plan struct {
	Statements []string
}

// Empty reports whether the plan contains no SQL statements.
func (p Plan) Empty() bool {
	return len(p.Statements) == 0
}

// DiffSnapshots creates an additive-only migration plan.
func DiffSnapshots(previous *Snapshot, current Snapshot) (Plan, error) {
	if previous == nil {
		return planCreateAll(current), nil
	}
	if previous.Dialect != current.Dialect {
		return Plan{}, fmt.Errorf("migrator: snapshot dialect changed from %q to %q", previous.Dialect, current.Dialect)
	}

	previousTables := make(map[string]TableSnapshot, len(previous.Tables))
	for _, table := range previous.Tables {
		previousTables[table.Name] = table
	}
	currentTables := make(map[string]TableSnapshot, len(current.Tables))
	for _, table := range current.Tables {
		currentTables[table.Name] = table
	}

	tableNames := make([]string, 0, len(currentTables))
	for name := range currentTables {
		tableNames = append(tableNames, name)
	}
	slices.Sort(tableNames)

	var statements []string
	for _, name := range tableNames {
		currentTable := currentTables[name]
		previousTable, exists := previousTables[name]
		if !exists {
			statements = append(statements, currentTable.CreateTableSQL)
			for _, index := range currentTable.Indexes {
				statements = append(statements, index.SQL)
			}
			continue
		}

		if previousTable.IsView != currentTable.IsView {
			return Plan{}, fmt.Errorf("migrator: changing table %q to view (or vice versa) is not supported", name)
		}

		tableStatements, err := diffTable(previousTable, currentTable, current.Dialect)
		if err != nil {
			return Plan{}, err
		}
		statements = append(statements, tableStatements...)
	}

	for name := range previousTables {
		if _, exists := currentTables[name]; !exists {
			return Plan{}, fmt.Errorf("migrator: dropping table %q is not supported", name)
		}
	}

	return Plan{Statements: statements}, nil
}

func planCreateAll(snapshot Snapshot) Plan {
	var statements []string
	for _, table := range snapshot.Tables {
		statements = append(statements, table.CreateTableSQL)
		for _, index := range table.Indexes {
			statements = append(statements, index.SQL)
		}
	}
	return Plan{Statements: statements}
}

func diffTable(previous, current TableSnapshot, dialectName string) ([]string, error) {
	var statements []string

	if current.IsView {
		if normalizeSQL(previous.CreateTableSQL) != normalizeSQL(current.CreateTableSQL) {
			return nil, fmt.Errorf("migrator: changing view %q definition is not supported", current.Name)
		}
		return nil, nil
	}

	previousColumns := make(map[string]ColumnSnapshot, len(previous.Columns))
	for _, column := range previous.Columns {
		previousColumns[column.Name] = column
	}
	for _, column := range current.Columns {
		previousColumn, exists := previousColumns[column.Name]
		if !exists {
			statements = append(statements, fmt.Sprintf(
				"ALTER TABLE %s ADD COLUMN %s",
				quoteIdentifier(dialectName, current.Name),
				column.DefinitionSQL,
			))
			continue
		}
		if !columnsEqual(previousColumn, column) {
			return nil, fmt.Errorf("migrator: changing column %q on table %q is not supported", column.Name, current.Name)
		}
		delete(previousColumns, column.Name)
	}
	if len(previousColumns) != 0 {
		names := make([]string, 0, len(previousColumns))
		for name := range previousColumns {
			names = append(names, name)
		}
		slices.Sort(names)
		return nil, fmt.Errorf("migrator: dropping columns on table %q is not supported: %s", current.Name, strings.Join(names, ", "))
	}

	constraintStatements, err := diffNamedSQL(
		previousNamedConstraints(previous.Constraints),
		currentNamedConstraints(current.Constraints),
		func(name string) error {
			return constraintSupportError(dialectName, "constraint", current.Name, name)
		},
	)
	if err != nil {
		return nil, err
	}
	statements = append(statements, constraintStatements...)

	foreignKeyStatements, err := diffNamedSQL(
		previousNamedForeignKeys(previous.ForeignKeys),
		currentNamedForeignKeys(current.ForeignKeys),
		func(name string) error {
			return constraintSupportError(dialectName, "foreign key", current.Name, name)
		},
	)
	if err != nil {
		return nil, err
	}
	statements = append(statements, foreignKeyStatements...)

	indexStatements, err := diffNamedSQL(previousNamedIndexes(previous.Indexes), currentNamedIndexes(current.Indexes), nil)
	if err != nil {
		return nil, err
	}
	statements = append(statements, indexStatements...)

	return statements, nil
}

func diffNamedSQL(previous, current map[string]string, onAdd func(name string) error) ([]string, error) {
	var statements []string

	for name, previousSQL := range previous {
		currentSQL, exists := current[name]
		if !exists {
			return nil, fmt.Errorf("migrator: dropping %q is not supported", name)
		}
		if normalizeSQL(previousSQL) != normalizeSQL(currentSQL) {
			return nil, fmt.Errorf("migrator: changing %q is not supported", name)
		}
	}

	names := make([]string, 0, len(current))
	for name := range current {
		if _, exists := previous[name]; exists {
			continue
		}
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if onAdd != nil {
			if err := onAdd(name); err != nil {
				return nil, err
			}
		}
		statements = append(statements, current[name])
	}

	return statements, nil
}

func previousNamedConstraints(items []ConstraintSnapshot) map[string]string {
	return namedConstraints(items)
}

func currentNamedConstraints(items []ConstraintSnapshot) map[string]string {
	return namedConstraints(items)
}

func namedConstraints(items []ConstraintSnapshot) map[string]string {
	named := make(map[string]string, len(items))
	for _, item := range items {
		named[item.Name] = item.SQL
	}
	return named
}

func previousNamedForeignKeys(items []ForeignKeySnapshot) map[string]string {
	return namedForeignKeys(items)
}

func currentNamedForeignKeys(items []ForeignKeySnapshot) map[string]string {
	return namedForeignKeys(items)
}

func namedForeignKeys(items []ForeignKeySnapshot) map[string]string {
	named := make(map[string]string, len(items))
	for _, item := range items {
		named[item.Name] = item.SQL
	}
	return named
}

func previousNamedIndexes(items []IndexSnapshot) map[string]string {
	return namedIndexes(items)
}

func currentNamedIndexes(items []IndexSnapshot) map[string]string {
	return namedIndexes(items)
}

func namedIndexes(items []IndexSnapshot) map[string]string {
	named := make(map[string]string, len(items))
	for _, item := range items {
		named[item.Name] = item.SQL
	}
	return named
}

func columnsEqual(left, right ColumnSnapshot) bool {
	return left.Name == right.Name &&
		reflect.DeepEqual(left.Type, right.Type) &&
		left.Nullable == right.Nullable &&
		left.DefaultSQL == right.DefaultSQL &&
		left.HasDefault == right.HasDefault &&
		left.PrimaryKey == right.PrimaryKey &&
		left.AutoIncrement == right.AutoIncrement &&
		left.Unique == right.Unique &&
		left.Identity == right.Identity &&
		left.GeneratedStored == right.GeneratedStored &&
		normalizeSQL(left.DefinitionSQL) == normalizeSQL(right.DefinitionSQL)
}

func normalizeSQL(sql string) string {
	return strings.Join(strings.Fields(sql), " ")
}

func constraintSupportError(dialectName, kind, tableName, name string) error {
	switch dialectName {
	case "postgres", "postgresql", "mysql":
		return nil
	case "sqlite", "sqlite3":
		return fmt.Errorf("migrator: adding %s %q to existing sqlite table %q is not supported", kind, name, tableName)
	default:
		return errors.New("migrator: unsupported dialect for additive constraints")
	}
}

func quoteIdentifier(dialectName, name string) string {
	escaped := strings.ReplaceAll(name, `"`, `""`)
	switch dialectName {
	case "mysql":
		return "`" + strings.ReplaceAll(name, "`", "``") + "`"
	default:
		return `"` + escaped + `"`
	}
}
